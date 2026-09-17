package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"roundtable/internal/db"
)

type proposalResponse struct {
	ProposalID        string `json:"proposal_id"`
	WorkspaceID       string `json:"workspace_id"`
	DeliberationID    string `json:"deliberation_id,omitempty"`
	ProposerSessionID string `json:"proposer_session_id,omitempty"`
	Title             string `json:"title"`
	Summary           string `json:"summary"`
	PatchPath         string `json:"patch_path"`
	BaseRevision      string `json:"base_revision,omitempty"`
	State             string `json:"state"`
	VoteState         string `json:"vote_state"`
	PolicyState       string `json:"policy_state"`
	ApprovalState     string `json:"approval_state"`
	TransactionState  string `json:"transaction_state"`
	Risk              string `json:"risk"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}
type proposalInput struct {
	ProposalID        string   `json:"proposal_id"`
	DeliberationID    string   `json:"deliberation_id"`
	ProposerSessionID string   `json:"proposer_session_id"`
	Title             string   `json:"title"`
	Summary           string   `json:"summary"`
	PatchPath         string   `json:"patch_path"`
	BaseRevision      string   `json:"base_revision"`
	Risk              string   `json:"risk"`
	ResourceIDs       []string `json:"resource_ids"`
}

func (s *Server) listProposalsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id FROM proposals WHERE workspace_id = ? ORDER BY created_at, id`, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "proposal_list_failed", "Unable to list proposals", err.Error())
		return
	}
	defer rows.Close()
	items := make([]proposalResponse, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			WriteProblem(w, r, 500, "proposal_list_failed", "Unable to list proposals", err.Error())
			return
		}
		value, err := s.readProposal(r.Context(), workspace.ID, id)
		if err == nil {
			items = append(items, value)
		}
	}
	if err := rows.Err(); err != nil {
		WriteProblem(w, r, 500, "proposal_list_failed", "Unable to list proposals", err.Error())
		return
	}
	writePage(w, r, items)
}

func (s *Server) getProposalAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	value, err := s.readProposal(r.Context(), workspace.ID, r.PathValue("proposal_id"))
	if err != nil {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) createInternalProposal(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Roundtable-Orchestrator") != "true" {
		WriteProblem(w, r, 403, "orchestrator_only", "Orchestrator authorization required", "Browser clients cannot create proposals directly")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Proposal creation requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input proposalInput
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_proposal", "Invalid proposal request", err.Error())
		return
	}
	input.Title, input.Summary, input.PatchPath = strings.TrimSpace(input.Title), redactText(input.Summary), strings.TrimSpace(input.PatchPath)
	if input.Title == "" || input.Summary == "" || input.PatchPath == "" {
		WriteProblem(w, r, 400, "invalid_proposal", "Invalid proposal request", "title, summary, and patch_path are required")
		return
	}
	if strings.Contains(input.PatchPath, "..") || strings.HasPrefix(input.PatchPath, "/") {
		WriteProblem(w, r, 400, "invalid_patch_path", "Invalid patch path", "patch_path must be repository-relative")
		return
	}
	id := input.ProposalID
	if id == "" {
		id = fmt.Sprintf("proposal-%d", time.Now().UnixNano())
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if input.Risk == "" {
		input.Risk = "normal"
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "proposal_create_failed", "Unable to create proposal", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO proposals (id, task_id, agent_id, title, summary_md, patch_path, status, risk, workspace_id, deliberation_id, proposer_session_id, base_revision, vote_state, policy_state, approval_state, transaction_state, updated_at) VALUES (?, '', '', ?, ?, ?, 'pending', ?, ?, ?, ?, ?, 'pending', 'pending', 'pending', 'not_started', ?)`, id, input.Title, input.Summary, input.PatchPath, input.Risk, workspace.ID, input.DeliberationID, input.ProposerSessionID, input.BaseRevision, now)
	if err == nil {
		for _, resourceID := range input.ResourceIDs {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO proposal_resources (proposal_id, resource_id) VALUES (?, ?)`, id, resourceID)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		payload, _ := json.Marshal(map[string]string{"state": "pending", "patch_path": input.PatchPath})
		err = insertProposalAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), id, requestID(r.Context()), "proposal.created", payload)
	}
	if err != nil {
		WriteProblem(w, r, 409, "proposal_create_conflict", "Proposal could not be created", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "proposal_create_conflict", "Proposal could not be created", err.Error())
		return
	}
	value, _ := s.readProposal(r.Context(), workspace.ID, id)
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: id, Type: "proposal.created", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"proposal_id":%q,"request_id":%q}`, id, requestID(r.Context()))})
	writeJSON(w, 201, value)
}

func (s *Server) transitionProposal(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Proposal transitions require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	value, err := s.readProposal(r.Context(), workspace.ID, r.PathValue("proposal_id"))
	if err != nil {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
		return
	}
	action := r.PathValue("action")
	target, ok := map[string]string{"request-review": "in_review", "reject": "rejected", "withdraw": "withdrawn"}[action]
	if !ok {
		WriteProblem(w, r, 400, "invalid_proposal_transition", "Invalid proposal transition", action)
		return
	}
	allowed := map[string]map[string]bool{"request-review": {"pending": true}, "reject": {"pending": true, "in_review": true}, "withdraw": {"pending": true, "in_review": true}}
	if !allowed[action][value.State] {
		WriteProblem(w, r, 409, "illegal_proposal_transition", "Illegal proposal transition", value.State+" cannot transition to "+target)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "proposal_transition_failed", "Unable to transition proposal", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE proposals SET status = ?, updated_at = ? WHERE id = ? AND workspace_id = ? AND status = ?`, target, now, value.ProposalID, workspace.ID, value.State)
	if err == nil {
		payload, _ := json.Marshal(map[string]string{"before": value.State, "after": target})
		err = insertProposalAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), value.ProposalID, requestID(r.Context()), "proposal."+strings.ReplaceAll(action, "-", "_"), payload)
	}
	if err != nil {
		WriteProblem(w, r, 409, "proposal_transition_conflict", "Proposal transition failed", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "proposal_transition_conflict", "Proposal transition failed", err.Error())
		return
	}
	value, _ = s.readProposal(r.Context(), workspace.ID, value.ProposalID)
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: value.ProposalID, Type: "proposal." + action, ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"proposal_id":%q,"state":%q}`, value.ProposalID, value.State)})
	writeJSON(w, 200, value)
}

func (s *Server) readProposal(ctx context.Context, workspaceID, id string) (proposalResponse, error) {
	row := s.config.Store.DB().QueryRowContext(ctx, `SELECT id, workspace_id, COALESCE(deliberation_id,''), COALESCE(proposer_session_id,''), title, summary_md, patch_path, COALESCE(base_revision,''), status, COALESCE(vote_state,'pending'), COALESCE(policy_state,'pending'), COALESCE(approval_state,'pending'), COALESCE(transaction_state,'not_started'), risk, created_at, COALESCE(updated_at, created_at) FROM proposals WHERE id = ? AND workspace_id = ?`, id, workspaceID)
	var value proposalResponse
	if err := row.Scan(&value.ProposalID, &value.WorkspaceID, &value.DeliberationID, &value.ProposerSessionID, &value.Title, &value.Summary, &value.PatchPath, &value.BaseRevision, &value.State, &value.VoteState, &value.PolicyState, &value.ApprovalState, &value.TransactionState, &value.Risk, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return value, err
	}
	value.Title, value.Summary, value.PatchPath = redactText(value.Title), redactText(value.Summary), redactText(value.PatchPath)
	return value, nil
}
func insertProposalAudit(ctx context.Context, tx *sql.Tx, workspaceID, actor, entity, request, action string, payload []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events (workspace_id, actor_id, action, entity_type, entity_id, request_id, payload_json) VALUES (?, ?, ?, 'proposal', ?, ?, ?)`, workspaceID, actor, action, entity, nullString(request), string(payload))
	return err
}
