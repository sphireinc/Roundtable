package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type policyEvaluationResponse struct {
	ID                    string   `json:"id"`
	PolicyID              string   `json:"policy_id"`
	PolicyRevisionID      string   `json:"policy_revision_id"`
	SubjectType           string   `json:"subject_type"`
	SubjectID             string   `json:"subject_id"`
	Result                string   `json:"result"`
	MatchedRules          []string `json:"matched_rules"`
	Evidence              []string `json:"evidence"`
	Remediation           []string `json:"remediation"`
	HumanApprovalRequired bool     `json:"human_approval_required"`
	DeterministicKey      string   `json:"deterministic_key"`
	Simulation            bool     `json:"simulation"`
	EvaluatedAt           string   `json:"evaluated_at"`
}

func (s *Server) policySchemaAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"schema_version": "1", "schema": map[string]any{"type": "object", "required": []string{"name", "definition"}, "properties": map[string]any{"name": map[string]string{"type": "string"}, "scope": map[string]string{"type": "string"}, "selector": map[string]string{"type": "object"}, "severity": map[string]string{"type": "string"}, "enforcement_mode": map[string]string{"type": "string"}, "human_approval_required": map[string]string{"type": "boolean"}, "definition": map[string]string{"type": "object"}, "metadata": map[string]string{"type": "object"}}}})
}

func (s *Server) validatePolicyAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	p, rev, definition, err := s.policyDefinition(r, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 404, "policy_not_found", "Policy not found", err.Error())
		return
	}
	issues := make([]string, 0)
	if strings.TrimSpace(p.Name) == "" {
		issues = append(issues, "name is required")
	}
	if len(definition) == 0 {
		issues = append(issues, "definition must contain at least one rule")
	}
	if p.EnforcementMode != "advisory" && p.EnforcementMode != "blocking" {
		issues = append(issues, "enforcement_mode must be advisory or blocking")
	}
	result := "pass"
	if len(issues) > 0 {
		result = "fail"
	}
	writeJSON(w, 200, map[string]any{"valid": len(issues) == 0, "result": result, "policy_id": p.ID, "policy_revision_id": rev, "issues": issues, "simulation": true, "deterministic_key": evaluationKey(p.ID, rev, "validation", p.ID)})
}

func (s *Server) simulatePolicyAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	p, rev, definition, err := s.policyDefinition(r, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 404, "policy_not_found", "Policy not found", err.Error())
		return
	}
	var in struct {
		SubjectType string   `json:"subject_type"`
		SubjectID   string   `json:"subject_id"`
		Evidence    []string `json:"evidence"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.SubjectType) == "" || strings.TrimSpace(in.SubjectID) == "" {
		WriteProblem(w, r, 400, "invalid_simulation", "Invalid simulation", "subject_type and subject_id are required")
		return
	}
	value := evaluateDefinition(p, rev, definition, in.SubjectType, in.SubjectID, in.Evidence, true)
	writeJSON(w, 200, value)
}

func (s *Server) evaluatePolicyAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Policy evaluation requires a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Evaluation requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	p, rev, definition, err := s.policyDefinition(r, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 404, "policy_not_found", "Policy not found", err.Error())
		return
	}
	var in struct {
		SubjectType string   `json:"subject_type"`
		SubjectID   string   `json:"subject_id"`
		Evidence    []string `json:"evidence"`
	}
	if err := decodeJSON(r, &in); err != nil || in.SubjectType == "" || in.SubjectID == "" {
		WriteProblem(w, r, 400, "invalid_evaluation", "Invalid evaluation", "subject_type and subject_id are required")
		return
	}
	value := evaluateDefinition(p, rev, definition, in.SubjectType, in.SubjectID, in.Evidence, false)
	reasons, _ := json.Marshal(map[string]any{"matched_rules": value.MatchedRules, "evidence": value.Evidence, "remediation": value.Remediation, "human_approval_required": value.HumanApprovalRequired, "deterministic_key": value.DeterministicKey})
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "evaluation_failed", "Unable to evaluate policy", err.Error())
		return
	}
	defer tx.Rollback()
	id := fmt.Sprintf("evaluation-%d", time.Now().UnixNano())
	_, err = tx.ExecContext(r.Context(), `INSERT INTO policy_evaluations(id,policy_id,policy_revision_id,subject_type,subject_id,result,reasons_json,evaluated_by) VALUES(?,?,?,?,?,?,?,?)`, id, p.ID, rev, in.SubjectType, in.SubjectID, value.Result, string(reasons), r.Header.Get("X-Actor-ID"))
	if err == nil {
		err = insertPolicyAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), p.ID, requestID(r.Context()), "policy.evaluated")
	}
	if err != nil || tx.Commit() != nil {
		WriteProblem(w, r, 409, "evaluation_failed", "Unable to store evaluation", fmt.Sprint(err))
		return
	}
	value.ID = id
	writeJSON(w, 201, value)
}

func (s *Server) policyDefinition(r *http.Request, workspaceID string) (policyResponse, string, map[string]any, error) {
	p, err := s.readPolicy(r, workspaceID, r.PathValue("policy_id"))
	if err != nil {
		return p, "", nil, err
	}
	var definition string
	var rev string
	if p.CurrentRevisionID != "" {
		err = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id,definition_json FROM policy_revisions WHERE id=? AND policy_id=?`, p.CurrentRevisionID, p.ID).Scan(&rev, &definition)
	}
	if err != nil {
		return p, "", nil, err
	}
	return p, rev, decodeMap(definition), nil
}

func evaluateDefinition(p policyResponse, rev string, definition map[string]any, subjectType, subjectID string, evidence []string, simulation bool) policyEvaluationResponse {
	matched := make([]string, 0, len(definition))
	for key := range definition {
		matched = append(matched, key)
	}
	remediation := make([]string, 0)
	result := "pass"
	if p.EnforcementMode == "blocking" && len(evidence) == 0 {
		result = "warn"
		remediation = append(remediation, "provide evidence for blocking policy evaluation")
	}
	if p.HumanApprovalRequired {
		remediation = append(remediation, "obtain human approval before applying")
	}
	return policyEvaluationResponse{PolicyID: p.ID, PolicyRevisionID: rev, SubjectType: subjectType, SubjectID: subjectID, Result: result, MatchedRules: matched, Evidence: evidence, Remediation: remediation, HumanApprovalRequired: p.HumanApprovalRequired, DeterministicKey: evaluationKey(p.ID, rev, subjectType, subjectID), Simulation: simulation, EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}
func evaluationKey(parts ...string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "|"))))
}
