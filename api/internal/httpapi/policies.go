package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type policyResponse struct {
	ID                    string         `json:"id"`
	WorkspaceID           string         `json:"workspace_id"`
	Name                  string         `json:"name"`
	Status                string         `json:"status"`
	CurrentRevisionID     string         `json:"current_revision_id,omitempty"`
	Scope                 string         `json:"scope"`
	Selector              map[string]any `json:"selector"`
	Severity              string         `json:"severity"`
	EnforcementMode       string         `json:"enforcement_mode"`
	HumanApprovalRequired bool           `json:"human_approval_required"`
	Metadata              map[string]any `json:"metadata"`
	CreatedAt             string         `json:"created_at"`
	UpdatedAt             string         `json:"updated_at"`
}
type policyRevisionResponse struct {
	ID         string         `json:"id"`
	PolicyID   string         `json:"policy_id"`
	Version    int            `json:"version"`
	Status     string         `json:"status"`
	Definition map[string]any `json:"definition"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  string         `json:"created_at"`
}
type policyImpact struct {
	ActiveSessions     int `json:"active_sessions"`
	ActiveClaims       int `json:"active_claims"`
	OpenProposals      int `json:"open_proposals"`
	ActiveTransactions int `json:"active_transactions"`
}

func (s *Server) listPoliciesAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id, name, status, COALESCE(current_revision_id,''), scope, selector_json, severity, enforcement_mode, human_approval_required, metadata_json, created_at, updated_at FROM policies WHERE workspace_id = ? ORDER BY created_at, id`, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "policy_list_failed", "Unable to list policies", err.Error())
		return
	}
	defer rows.Close()
	items := make([]policyResponse, 0)
	for rows.Next() {
		var p policyResponse
		var selector, metadata string
		var approval int
		if err := rows.Scan(&p.ID, &p.Name, &p.Status, &p.CurrentRevisionID, &p.Scope, &selector, &p.Severity, &p.EnforcementMode, &approval, &metadata, &p.CreatedAt, &p.UpdatedAt); err != nil {
			WriteProblem(w, r, 500, "policy_list_failed", "Unable to list policies", err.Error())
			return
		}
		p.WorkspaceID = workspace.ID
		p.HumanApprovalRequired = approval != 0
		p.Selector = decodeMap(selector)
		p.Metadata = decodeMap(metadata)
		items = append(items, p)
	}
	writePage(w, r, items)
}

func (s *Server) getPolicyAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	p, err := s.readPolicy(r, workspace.ID, r.PathValue("policy_id"))
	if err != nil {
		WriteProblem(w, r, 404, "policy_not_found", "Policy not found", err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) listPolicyRevisionsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	if _, err = s.readPolicy(r, workspace.ID, r.PathValue("policy_id")); err != nil {
		WriteProblem(w, r, 404, "policy_not_found", "Policy not found", err.Error())
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id,policy_id,version,status,definition_json,created_by,created_at FROM policy_revisions WHERE policy_id=? ORDER BY version DESC`, r.PathValue("policy_id"))
	if err != nil {
		WriteProblem(w, r, 500, "policy_revision_list_failed", "Unable to list policy revisions", err.Error())
		return
	}
	defer rows.Close()
	items := make([]policyRevisionResponse, 0)
	for rows.Next() {
		var v policyRevisionResponse
		var definition string
		if err := rows.Scan(&v.ID, &v.PolicyID, &v.Version, &v.Status, &definition, &v.CreatedBy, &v.CreatedAt); err != nil {
			WriteProblem(w, r, 500, "policy_revision_list_failed", "Unable to list policy revisions", err.Error())
			return
		}
		v.Definition = decodeMap(definition)
		items = append(items, v)
	}
	writePage(w, r, items)
}

func (s *Server) createPolicyAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Policy mutations require a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Policy creation requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var in struct {
		ID                    string         `json:"id"`
		Name                  string         `json:"name"`
		Scope                 string         `json:"scope"`
		Severity              string         `json:"severity"`
		EnforcementMode       string         `json:"enforcement_mode"`
		Selector              map[string]any `json:"selector"`
		Metadata              map[string]any `json:"metadata"`
		HumanApprovalRequired bool           `json:"human_approval_required"`
		Definition            map[string]any `json:"definition"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		WriteProblem(w, r, 400, "invalid_policy", "Invalid policy", "name is required")
		return
	}
	if in.ID == "" {
		in.ID = fmt.Sprintf("policy-%d", time.Now().UnixNano())
	}
	if in.Scope == "" {
		in.Scope = "workspace"
	}
	if in.Severity == "" {
		in.Severity = "normal"
	}
	if in.EnforcementMode == "" {
		in.EnforcementMode = "advisory"
	}
	selector, _ := json.Marshal(in.Selector)
	metadata, _ := json.Marshal(in.Metadata)
	definition, _ := json.Marshal(in.Definition)
	if in.Definition == nil {
		definition = []byte(`{}`)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rev := fmt.Sprintf("%s-r1", in.ID)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "policy_create_failed", "Unable to create policy", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO policies(id,workspace_id,name,status,current_revision_id,scope,selector_json,severity,enforcement_mode,human_approval_required,metadata_json,created_at,updated_at) VALUES(?,?,?,'disabled',?,?,?,?,?,?,?,?,?)`, in.ID, workspace.ID, in.Name, rev, in.Scope, string(selector), in.Severity, in.EnforcementMode, boolInt(in.HumanApprovalRequired), string(metadata), now, now)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO policy_revisions(id,policy_id,version,status,definition_json,created_by) VALUES(?,?,1,'draft',?,?)`, rev, in.ID, string(definition), r.Header.Get("X-Actor-ID"))
	}
	if err == nil {
		err = insertPolicyAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), in.ID, requestID(r.Context()), "policy.created")
	}
	if err != nil {
		WriteProblem(w, r, 409, "policy_create_failed", "Unable to create policy", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "policy_create_failed", "Unable to create policy", err.Error())
		return
	}
	p, _ := s.readPolicy(r, workspace.ID, in.ID)
	writeJSON(w, 201, p)
}

func (s *Server) transitionPolicyAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Policy mutations require a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Policy mutations require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	p, err := s.readPolicy(r, workspace.ID, r.PathValue("policy_id"))
	if err != nil {
		WriteProblem(w, r, 404, "policy_not_found", "Policy not found", err.Error())
		return
	}
	action := r.PathValue("action")
	if !map[string]bool{"publish": true, "enable": true, "disable": true, "update-draft": true, "clone": true}[action] {
		WriteProblem(w, r, 400, "invalid_policy_action", "Invalid policy action", action)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "policy_transition_failed", "Unable to change policy", err.Error())
		return
	}
	defer tx.Rollback()
	target := p.ID
	if action == "clone" {
		target = fmt.Sprintf("policy-%d", time.Now().UnixNano())
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO policies(id,workspace_id,name,status,current_revision_id,scope,selector_json,severity,enforcement_mode,human_approval_required,metadata_json) SELECT ?,workspace_id,name||' (clone)','disabled',?,scope,selector_json,severity,enforcement_mode,human_approval_required,metadata_json FROM policies WHERE id=?`, target, target+"-r1", p.ID); err == nil {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO policy_revisions(id,policy_id,version,status,definition_json,created_by) SELECT ?,?,1,'draft',definition_json,? FROM policy_revisions WHERE id=?`, target+"-r1", target, r.Header.Get("X-Actor-ID"), p.CurrentRevisionID)
		}
	} else if action == "update-draft" {
		var next int
		err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(version),0)+1 FROM policy_revisions WHERE policy_id=?`, p.ID).Scan(&next)
		if err == nil {
			revisionID := fmt.Sprintf("%s-r%d", p.ID, next)
			_, err = tx.ExecContext(r.Context(), `INSERT INTO policy_revisions(id,policy_id,version,status,definition_json,created_by) SELECT ?,?,?,'draft',definition_json,? FROM policy_revisions WHERE policy_id=? ORDER BY version DESC LIMIT 1`, revisionID, p.ID, next, r.Header.Get("X-Actor-ID"), p.ID)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `UPDATE policies SET current_revision_id=?, updated_at=? WHERE id=? AND workspace_id=?`, revisionID, now, p.ID, workspace.ID)
			}
		}
	} else {
		status := p.Status
		if action == "publish" {
			status = "active"
			_, err = tx.ExecContext(r.Context(), `UPDATE policy_revisions SET status='published' WHERE id=?`, p.CurrentRevisionID)
		} else if action == "enable" {
			status = "active"
		} else {
			status = "disabled"
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE policies SET status=?,updated_at=? WHERE id=? AND workspace_id=?`, status, now, p.ID, workspace.ID)
		}
	}
	if err == nil {
		err = insertPolicyAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), p.ID, requestID(r.Context()), "policy."+action)
	}
	if err != nil {
		WriteProblem(w, r, 409, "policy_transition_failed", "Unable to change policy", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "policy_transition_failed", "Unable to change policy", err.Error())
		return
	}
	if action == "clone" {
		p, _ = s.readPolicy(r, workspace.ID, target)
	} else {
		p, _ = s.readPolicy(r, workspace.ID, p.ID)
	}
	writeJSON(w, 200, map[string]any{"policy": p, "impact": s.policyImpact(r)})
}

func (s *Server) readPolicy(r *http.Request, workspaceID, id string) (policyResponse, error) {
	var p policyResponse
	var selector, metadata string
	var approval int
	err := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id,name,status,COALESCE(current_revision_id,''),scope,selector_json,severity,enforcement_mode,human_approval_required,metadata_json,created_at,updated_at FROM policies WHERE id=? AND workspace_id=?`, id, workspaceID).Scan(&p.ID, &p.Name, &p.Status, &p.CurrentRevisionID, &p.Scope, &selector, &p.Severity, &p.EnforcementMode, &approval, &metadata, &p.CreatedAt, &p.UpdatedAt)
	p.WorkspaceID = workspaceID
	p.HumanApprovalRequired = approval != 0
	p.Selector = decodeMap(selector)
	p.Metadata = decodeMap(metadata)
	return p, err
}
func (s *Server) policyImpact(r *http.Request) policyImpact {
	var i policyImpact
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM agent_sessions WHERE status IN ('active','running')`).Scan(&i.ActiveSessions)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM claims WHERE status='active'`).Scan(&i.ActiveClaims)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE status IN ('pending','in_review')`).Scan(&i.OpenProposals)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM transactions WHERE status IN ('pending','running')`).Scan(&i.ActiveTransactions)
	return i
}
func decodeMap(raw string) map[string]any {
	var out map[string]any
	if json.Unmarshal([]byte(raw), &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func insertPolicyAudit(ctx context.Context, tx *sql.Tx, workspace, actor, entity, request, action string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?,?,'policy',?,?,?)`, workspace, actor, action, entity, request, `{}`)
	return err
}
