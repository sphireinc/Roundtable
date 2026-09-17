package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"roundtable/internal/db"
)

type contentionResponse struct {
	ID                  string   `json:"id"`
	WorkspaceID         string   `json:"workspace_id"`
	ResourceID          string   `json:"resource_id"`
	RequestedResourceID string   `json:"requested_resource_id"`
	RequestedAgentID    string   `json:"requested_agent_id"`
	RequestedTaskID     string   `json:"requested_task_id"`
	RequestedMode       string   `json:"requested_mode"`
	RequestedPath       string   `json:"requested_path,omitempty"`
	CurrentOwnerClaimID string   `json:"current_owner_claim_id"`
	CurrentOwnerAgentID string   `json:"current_owner_agent_id"`
	State               string   `json:"state"`
	Reason              string   `json:"reason"`
	AcquiredAt          string   `json:"acquired_at"`
	LeaseExpiresAt      string   `json:"lease_expires_at"`
	AllowedResolutions  []string `json:"allowed_resolutions"`
}

func (s *Server) listContentionsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	resourceID := r.URL.Query().Get("resource_id")
	query := `SELECT cc.id, cc.resource_id, COALESCE(cc.requested_resource_id,''), COALESCE(cc.requested_agent_id,''), COALESCE(cc.requested_task_id,''), COALESCE(cc.requested_mode,''), COALESCE(cc.requested_path,''), cc.challenged_claim_id, c.agent_id, cc.status, cc.reason, cc.created_at, c.expires_at FROM claim_contentions cc LEFT JOIN claims c ON c.id = cc.challenged_claim_id WHERE (? = '' OR cc.resource_id = ?) ORDER BY cc.created_at, cc.id`
	rows, err := s.config.Store.DB().QueryContext(r.Context(), query, resourceID, resourceID)
	if err != nil {
		WriteProblem(w, r, 500, "contention_list_failed", "Unable to list contentions", err.Error())
		return
	}
	defer rows.Close()
	items := make([]contentionResponse, 0)
	for rows.Next() {
		var value contentionResponse
		if err := rows.Scan(&value.ID, &value.ResourceID, &value.RequestedResourceID, &value.RequestedAgentID, &value.RequestedTaskID, &value.RequestedMode, &value.RequestedPath, &value.CurrentOwnerClaimID, &value.CurrentOwnerAgentID, &value.State, &value.Reason, &value.AcquiredAt, &value.LeaseExpiresAt); err != nil {
			WriteProblem(w, r, 500, "contention_list_failed", "Unable to list contentions", err.Error())
			return
		}
		if !claimWorkspace(s.config.Store.DB(), r.Context(), value.CurrentOwnerClaimID, workspace.ID) {
			continue
		}
		value.WorkspaceID = workspace.ID
		value.AllowedResolutions = []string{"keep_owner", "transfer", "extend_lease", "reject_contender"}
		items = append(items, value)
	}
	writePage(w, r, items)
}

func (s *Server) resolveContentionAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Contention resolution requires a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Contention resolution requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input struct {
		Resolution string `json:"resolution"`
		Reason     string `json:"reason"`
	}
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Resolution) == "" {
		WriteProblem(w, r, 400, "invalid_resolution", "Invalid contention resolution", "resolution is required")
		return
	}
	allowed := map[string]bool{"keep_owner": true, "transfer": true, "split": true, "narrow": true, "extend_lease": true, "reject_contender": true}
	if !allowed[input.Resolution] {
		WriteProblem(w, r, 400, "invalid_resolution", "Invalid contention resolution", input.Resolution)
		return
	}
	var ownerClaimID, requestedAgent, requestedTask, requestedMode string
	var status string
	err = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT challenged_claim_id, requested_agent_id, requested_task_id, requested_mode, status FROM claim_contentions WHERE id = ? AND resource_id IN (SELECT resource_id FROM claims WHERE workspace_id = ?)`, r.PathValue("contention_id"), workspace.ID).Scan(&ownerClaimID, &requestedAgent, &requestedTask, &requestedMode, &status)
	if err != nil {
		WriteProblem(w, r, 404, "contention_not_found", "Contention not found", err.Error())
		return
	}
	if status != "open" {
		WriteProblem(w, r, 409, "contention_already_resolved", "Contention already resolved", status)
		return
	}
	if input.Resolution == "split" || input.Resolution == "narrow" {
		WriteProblem(w, r, 409, "resolution_not_supported", "Resolution is not supported", "split and narrow require resource-specific semantics")
		return
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "contention_resolution_failed", "Unable to resolve contention", err.Error())
		return
	}
	defer tx.Rollback()
	if input.Resolution == "transfer" {
		if _, err = tx.ExecContext(r.Context(), `UPDATE claims SET agent_id = ?, task_id = ?, claim_type = ? WHERE id = ? AND status = 'active'`, requestedAgent, requestedTask, requestedMode, ownerClaimID); err != nil {
			WriteProblem(w, r, 409, "contention_resolution_failed", "Unable to transfer claim", err.Error())
			return
		}
	}
	if input.Resolution == "extend_lease" {
		if _, err = tx.ExecContext(r.Context(), `UPDATE claims SET expires_at = ? WHERE id = ? AND status = 'active'`, time.Now().UTC().Add(15*time.Minute).Format(time.RFC3339), ownerClaimID); err != nil {
			WriteProblem(w, r, 409, "contention_resolution_failed", "Unable to extend lease", err.Error())
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE claim_contentions SET status = 'resolved', resolution = ? WHERE id = ? AND status = 'open'`, input.Resolution, r.PathValue("contention_id"))
	if err == nil {
		payload, _ := json.Marshal(map[string]string{"resolution": input.Resolution, "reason": redactText(input.Reason)})
		_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events (workspace_id, actor_id, action, entity_type, entity_id, request_id, reason, payload_json) VALUES (?, ?, 'claim.contention_resolved', 'contention', ?, ?, ?, ?)`, workspace.ID, r.Header.Get("X-Actor-ID"), r.PathValue("contention_id"), requestID(r.Context()), redactText(input.Reason), string(payload))
	}
	if err != nil {
		WriteProblem(w, r, 409, "contention_resolution_failed", "Unable to resolve contention", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "contention_resolution_failed", "Unable to resolve contention", err.Error())
		return
	}
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "claim.contention_resolved", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"contention_id":%q,"resolution":%q}`, r.PathValue("contention_id"), input.Resolution)})
	writeJSON(w, 200, map[string]any{"contention_id": r.PathValue("contention_id"), "state": "resolved", "resolution": input.Resolution})
}
