package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"roundtable/internal/db"
)

type approvalResponse struct {
	ID               string         `json:"id"`
	WorkspaceID      string         `json:"workspace_id"`
	ProposalID       string         `json:"proposal_id,omitempty"`
	TaskID           string         `json:"task_id,omitempty"`
	Subject          string         `json:"subject"`
	Reason           string         `json:"reason"`
	Risk             string         `json:"risk"`
	Status           string         `json:"status"`
	RequiredRole     string         `json:"required_role"`
	Decision         string         `json:"decision,omitempty"`
	DecisionMetadata map[string]any `json:"decision_metadata"`
	RequestedBy      string         `json:"requested_by"`
	DecidedBy        string         `json:"decided_by,omitempty"`
	OverridePolicy   bool           `json:"override_policy"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

func (s *Server) listApprovalsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT h.id,COALESCE(h.proposal_id,''),COALESCE(h.task_id,''),h.subject,h.reason_md,h.status,COALESCE(h.requested_by,''),COALESCE(h.decided_by,''),h.override_policy,h.created_at,h.updated_at,COALESCE(p.risk,'normal') FROM human_approvals h LEFT JOIN proposals p ON p.id=h.proposal_id WHERE p.workspace_id=? ORDER BY h.created_at,h.id`, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "approval_list_failed", "Unable to list approvals", err.Error())
		return
	}
	defer rows.Close()
	items := make([]approvalResponse, 0)
	for rows.Next() {
		v, err := scanApproval(rows, workspace.ID)
		if err != nil {
			WriteProblem(w, r, 500, "approval_list_failed", "Unable to list approvals", err.Error())
			return
		}
		items = append(items, v)
	}
	writePage(w, r, items)
}

func (s *Server) getApprovalAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	v, err := s.readApproval(r, workspace.ID, r.PathValue("approval_id"))
	if err != nil {
		WriteProblem(w, r, 404, "approval_not_found", "Approval not found", err.Error())
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) requestApprovalAPI(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Roundtable-Orchestrator") != "true" && !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Authorization required", "Approval requests require the orchestrator or a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Approval requests require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var in struct {
		ID               string         `json:"id"`
		ProposalID       string         `json:"proposal_id"`
		TaskID           string         `json:"task_id"`
		Subject          string         `json:"subject"`
		Reason           string         `json:"reason"`
		Risk             string         `json:"risk"`
		RequiredRole     string         `json:"required_role"`
		DecisionMetadata map[string]any `json:"decision_metadata"`
		OverridePolicy   bool           `json:"override_policy"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Subject) == "" || strings.TrimSpace(in.Reason) == "" {
		WriteProblem(w, r, 400, "invalid_approval", "Invalid approval", "subject and reason are required")
		return
	}
	if in.ProposalID != "" {
		var one int
		if err := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT 1 FROM proposals WHERE id=? AND workspace_id=?`, in.ProposalID, workspace.ID).Scan(&one); err != nil {
			WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
			return
		}
	}
	if in.ID == "" {
		in.ID = fmt.Sprintf("approval-%d", time.Now().UnixNano())
	}
	metadata, _ := json.Marshal(in.DecisionMetadata)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "approval_request_failed", "Unable to request approval", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO human_approvals(id,proposal_id,task_id,subject,reason_md,status,requested_by,override_policy,decision_md,decided_by) VALUES(?,?,?,?,?,'requested',?,?,NULL,NULL)`, in.ID, nullIfApproval(in.ProposalID), nullIfApproval(in.TaskID), in.Subject, redactText(in.Reason), r.Header.Get("X-Actor-ID"), boolInt(in.OverridePolicy))
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,reason,payload_json) VALUES(?,?, 'approval.requested','approval',?,?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), in.ID, requestID(r.Context()), redactText(in.Reason), string(metadata))
	}
	if err != nil || tx.Commit() != nil {
		WriteProblem(w, r, 409, "approval_request_failed", "Unable to request approval", fmt.Sprint(err))
		return
	}
	v, _ := s.readApproval(r, workspace.ID, in.ID)
	writeJSON(w, 201, v)
}

func (s *Server) decideApprovalAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Approval decisions require a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Approval decisions require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	v, err := s.readApproval(r, workspace.ID, r.PathValue("approval_id"))
	if err != nil {
		WriteProblem(w, r, 404, "approval_not_found", "Approval not found", err.Error())
		return
	}
	if v.Status != "requested" {
		WriteProblem(w, r, 409, "approval_already_decided", "Approval already decided", v.Status)
		return
	}
	var in struct {
		Reason           string         `json:"reason"`
		DecisionMetadata map[string]any `json:"decision_metadata"`
		Override         bool           `json:"override"`
	}
	if err := decodeJSON(r, &in); err != nil {
		WriteProblem(w, r, 400, "invalid_decision", "Invalid decision", err.Error())
		return
	}
	action := r.PathValue("action")
	if !map[string]bool{"approve": true, "reject": true, "request-changes": true, "defer": true}[action] {
		WriteProblem(w, r, 400, "invalid_decision", "Invalid decision", action)
		return
	}
	if (action == "reject" || action == "request-changes") && strings.TrimSpace(in.Reason) == "" {
		WriteProblem(w, r, 400, "reason_required", "Reason required", "This decision requires a reason")
		return
	}
	if in.Override && !v.OverridePolicy {
		WriteProblem(w, r, 409, "override_not_requested", "Override not requested", "Approval does not permit a policy override")
		return
	}
	if in.Override && r.Header.Get("X-Override-Permission") != "true" {
		WriteProblem(w, r, 403, "override_permission_required", "Override permission required", "Explicit override permission is required")
		return
	}
	status := map[string]string{"approve": "approved", "reject": "rejected", "request-changes": "changes_requested", "defer": "deferred"}[action]
	metadata, _ := json.Marshal(in.DecisionMetadata)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "approval_decision_failed", "Unable to decide approval", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE human_approvals SET status=?,decision_md=?,decided_by=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='requested'`, status, redactText(in.Reason), r.Header.Get("X-Actor-ID"), v.ID)
	if err == nil && v.ProposalID != "" {
		proposalState := map[string]string{"approve": "approved", "reject": "rejected", "request-changes": "in_review", "defer": "pending"}[action]
		_, err = tx.ExecContext(r.Context(), `UPDATE proposals SET approval_state=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND workspace_id=? AND approval_state IN ('pending','requested')`, status, v.ProposalID, workspace.ID)
		if proposalState != "" {
			_, _ = tx.ExecContext(r.Context(), `UPDATE proposals SET status=CASE WHEN ?='in_review' THEN 'in_review' ELSE status END WHERE id=? AND workspace_id=?`, proposalState, v.ProposalID, workspace.ID)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,reason,payload_json) VALUES(?,?,?,'approval',?,?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), "approval."+action, v.ID, requestID(r.Context()), redactText(in.Reason), string(metadata))
	}
	if err != nil || tx.Commit() != nil {
		WriteProblem(w, r, 409, "approval_decision_failed", "Unable to decide approval", fmt.Sprint(err))
		return
	}
	v, _ = s.readApproval(r, workspace.ID, v.ID)
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "approval." + action, ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"approval_id":%q,"status":%q}`, v.ID, v.Status)})
	writeJSON(w, 200, v)
}

func (s *Server) readApproval(r *http.Request, workspaceID, id string) (approvalResponse, error) {
	row := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT h.id,COALESCE(h.proposal_id,''),COALESCE(h.task_id,''),h.subject,h.reason_md,h.status,COALESCE(h.requested_by,''),COALESCE(h.decided_by,''),h.override_policy,h.created_at,h.updated_at,COALESCE(p.risk,'normal') FROM human_approvals h LEFT JOIN proposals p ON p.id=h.proposal_id WHERE h.id=? AND p.workspace_id=?`, id, workspaceID)
	return scanApproval(row, workspaceID)
}

type approvalScanner interface{ Scan(...any) error }

func scanApproval(row approvalScanner, workspace string) (approvalResponse, error) {
	var v approvalResponse
	var override int
	if err := row.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Subject, &v.Reason, &v.Status, &v.RequestedBy, &v.DecidedBy, &override, &v.CreatedAt, &v.UpdatedAt, &v.Risk); err != nil {
		return v, err
	}
	v.WorkspaceID = workspace
	v.OverridePolicy = override != 0
	v.RequiredRole = "human"
	v.DecisionMetadata = map[string]any{}
	return v, nil
}
func nullIfApproval(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
