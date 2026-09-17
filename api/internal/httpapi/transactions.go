package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"roundtable/internal/proposals"
)

type transactionResponse struct {
	ID                string         `json:"id"`
	WorkspaceID       string         `json:"workspace_id"`
	ProposalID        string         `json:"proposal_id"`
	RunID             string         `json:"run_id"`
	BeforeGitHash     string         `json:"before_git_hash"`
	AfterGitHash      string         `json:"after_git_hash,omitempty"`
	Status            string         `json:"status"`
	AppliedBy         string         `json:"applied_by"`
	AppliedAt         string         `json:"applied_at,omitempty"`
	RollbackPatchPath string         `json:"rollback_patch_path,omitempty"`
	Metadata          map[string]any `json:"metadata"`
}
type transactionPhaseResponse struct {
	Phase     string `json:"phase"`
	Status    string `json:"status"`
	Actor     string `json:"actor"`
	Reason    string `json:"reason,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

func (s *Server) listTransactionsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT t.id,t.proposal_id,t.run_id,t.before_git_hash,COALESCE(t.after_git_hash,''),t.status,t.applied_by,COALESCE(t.applied_at,''),COALESCE(t.rollback_patch_path,''),COALESCE(t.metadata_json,'{}') FROM transactions t JOIN proposals p ON p.id=t.proposal_id WHERE p.workspace_id=? ORDER BY t.id`, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "transaction_list_failed", "Unable to list transactions", err.Error())
		return
	}
	defer rows.Close()
	items := make([]transactionResponse, 0)
	for rows.Next() {
		v, err := scanTransaction(rows, workspace.ID)
		if err != nil {
			WriteProblem(w, r, 500, "transaction_list_failed", "Unable to list transactions", err.Error())
			return
		}
		items = append(items, v)
	}
	writePage(w, r, items)
}
func (s *Server) getTransactionAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	v, err := s.readTransaction(r, workspace.ID, r.PathValue("transaction_id"))
	if err != nil {
		WriteProblem(w, r, 404, "transaction_not_found", "Transaction not found", err.Error())
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) transitionTransactionAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Transaction control requires a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Transaction control requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	action := r.PathValue("action")
	if action == "apply" && r.Header.Get("X-Roundtable-Orchestrator") != "true" {
		WriteProblem(w, r, 403, "orchestrator_only", "Orchestrator authorization required", "Repository mutation is restricted to the transaction manager")
		return
	}
	proposalID := r.PathValue("proposal_id")
	if proposalID == "" {
		proposalID = r.URL.Query().Get("proposal_id")
	}
	if action == "apply" {
		if proposalID == "" {
			WriteProblem(w, r, 400, "proposal_id_required", "Proposal ID required", "Apply requires proposal_id")
			return
		}
		service := proposals.NewService(workspace.RootPath, s.config.Store, nil)
		result, err := service.PatchApply(r.Context(), map[string]any{"proposal_id": proposalID, "transaction_id": r.PathValue("transaction_id"), "run_id": workspace.ID, "applied_by": r.Header.Get("X-Actor-ID")})
		if err != nil {
			WriteProblem(w, r, 409, "transaction_apply_blocked", "Transaction apply blocked", err.Error())
			return
		}
		writeJSON(w, 200, result)
		return
	}
	txID := r.PathValue("transaction_id")
	tx, err := s.readTransaction(r, workspace.ID, txID)
	if err != nil {
		WriteProblem(w, r, 404, "transaction_not_found", "Transaction not found", err.Error())
		return
	}
	if action == "compensate" {
		if tx.RollbackPatchPath == "" {
			WriteProblem(w, r, 409, "compensation_unavailable", "Compensation unavailable", "transaction has no rollback artifact")
			return
		}
		proposalID := fmt.Sprintf("compensating-%d", time.Now().UnixNano())
		_, err = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO proposals(id,task_id,agent_id,title,summary_md,patch_path,status,risk,workspace_id,approval_state,transaction_state,updated_at) VALUES(?,?,?,?,? ,?,'pending','high',?,'pending','not_started',CURRENT_TIMESTAMP)`, proposalID, "", r.Header.Get("X-Actor-ID"), "Compensating proposal for "+tx.ID, "Revert transaction "+tx.ID, tx.RollbackPatchPath, workspace.ID)
		if err != nil {
			WriteProblem(w, r, 409, "compensation_failed", "Unable to create compensating proposal", err.Error())
			return
		}
		_, _ = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?, 'transaction.compensation_created','proposal',?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), proposalID, requestID(r.Context()), `{}`)
		writeJSON(w, http.StatusCreated, map[string]any{"proposal_id": proposalID, "transaction_id": tx.ID, "patch_path": tx.RollbackPatchPath, "status": "pending"})
		return
	}
	if action == "validate" {
		if tx.ProposalID == "" {
			WriteProblem(w, r, 409, "transaction_invalid", "Transaction has no proposal", "validation requires a proposal")
			return
		}
		service := proposals.NewService(workspace.RootPath, s.config.Store, nil)
		result, err := service.PatchValidate(r.Context(), map[string]any{"proposal_id": tx.ProposalID})
		if err != nil {
			WriteProblem(w, r, 409, "transaction_validation_failed", "Transaction validation failed", err.Error())
			return
		}
		writeJSON(w, 200, result)
		return
	}
	target := map[string]string{"cancel": "cancelled", "retry": "pending", "stage": "staged"}[action]
	if target == "" {
		WriteProblem(w, r, 400, "invalid_transaction_action", "Invalid transaction action", action)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sqlTx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "transaction_transition_failed", "Unable to transition transaction", err.Error())
		return
	}
	defer sqlTx.Rollback()
	if _, err = sqlTx.ExecContext(r.Context(), `UPDATE transactions SET status=? WHERE id=? AND status NOT IN ('applied','cancelled')`, target, tx.ID); err == nil {
		_, err = sqlTx.ExecContext(r.Context(), `INSERT INTO transaction_phases(id,transaction_id,phase,status,actor,reason,request_id,started_at,ended_at) VALUES(?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("phase-%d", time.Now().UnixNano()), tx.ID, action, "completed", r.Header.Get("X-Actor-ID"), redactText(r.URL.Query().Get("reason")), requestID(r.Context()), now, now)
	}
	if err == nil {
		_, err = sqlTx.ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,reason,payload_json) VALUES(?,?,?,'transaction',?,?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), "transaction."+action, tx.ID, requestID(r.Context()), redactText(r.URL.Query().Get("reason")), `{}`)
	}
	if err != nil || sqlTx.Commit() != nil {
		WriteProblem(w, r, 409, "transaction_transition_failed", "Unable to transition transaction", fmt.Sprint(err))
		return
	}
	v, _ := s.readTransaction(r, workspace.ID, tx.ID)
	writeJSON(w, 200, v)
}
func (s *Server) readTransaction(r *http.Request, workspaceID, id string) (transactionResponse, error) {
	row := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT t.id,t.proposal_id,t.run_id,t.before_git_hash,COALESCE(t.after_git_hash,''),t.status,t.applied_by,COALESCE(t.applied_at,''),COALESCE(t.rollback_patch_path,''),COALESCE(t.metadata_json,'{}') FROM transactions t JOIN proposals p ON p.id=t.proposal_id WHERE t.id=? AND p.workspace_id=?`, id, workspaceID)
	return scanTransaction(row, workspaceID)
}

type txScanner interface{ Scan(...any) error }

func scanTransaction(row txScanner, workspace string) (transactionResponse, error) {
	var v transactionResponse
	var metadata string
	if err := row.Scan(&v.ID, &v.ProposalID, &v.RunID, &v.BeforeGitHash, &v.AfterGitHash, &v.Status, &v.AppliedBy, &v.AppliedAt, &v.RollbackPatchPath, &metadata); err != nil {
		return v, err
	}
	v.WorkspaceID = workspace
	v.Metadata = decodeMap(metadata)
	return v, nil
}
