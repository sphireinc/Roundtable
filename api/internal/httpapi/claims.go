package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	claimsvc "roundtable/internal/claims"
	"roundtable/internal/db"
)

type claimInput struct {
	ClaimID      string `json:"claim_id"`
	AgentID      string `json:"agent_id"`
	SessionID    string `json:"session_id"`
	TaskID       string `json:"task_id"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	Path         string `json:"path"`
	Symbol       string `json:"symbol"`
	Mode         string `json:"mode"`
	RunID        string `json:"run_id"`
	BaseHash     string `json:"base_hash"`
	TTLSeconds   int    `json:"ttl_seconds"`
	Rationale    string `json:"rationale"`
}
type claimResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	AgentID        string `json:"agent_id"`
	SessionID      string `json:"session_id,omitempty"`
	TaskID         string `json:"task_id"`
	ResourceID     string `json:"resource_id"`
	ResourceType   string `json:"resource_type"`
	Path           string `json:"path,omitempty"`
	Symbol         string `json:"symbol,omitempty"`
	Mode           string `json:"mode"`
	BaseHash       string `json:"base_hash,omitempty"`
	State          string `json:"state"`
	AcquiredAt     string `json:"acquired_at"`
	LeaseExpiresAt string `json:"lease_expires_at"`
	Rationale      string `json:"rationale,omitempty"`
}

func (s *Server) listClaimsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	claims, err := s.config.Store.ListClaims(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "claim_list_failed", "Unable to list claims", err.Error())
		return
	}
	items := make([]claimResponse, 0)
	for _, claim := range claims {
		if !claimWorkspace(s.config.Store.DB(), r.Context(), claim.ID, workspace.ID) {
			continue
		}
		items = append(items, s.toClaimResponse(r.Context(), workspace.ID, claim))
	}
	writePage(w, r, items)
}
func (s *Server) getClaimAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	claim, err := s.config.Store.GetClaim(r.Context(), r.PathValue("claim_id"))
	if err != nil || !claimWorkspace(s.config.Store.DB(), r.Context(), claim.ID, workspace.ID) {
		WriteProblem(w, r, 404, "claim_not_found", "Claim not found", "claim does not belong to this workspace")
		return
	}
	writeJSON(w, 200, s.toClaimResponse(r.Context(), workspace.ID, claim))
}
func (s *Server) createClaimAPI(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Roundtable-Orchestrator") != "true" {
		WriteProblem(w, r, 403, "orchestrator_only", "Orchestrator authorization required", "Claims are created by the orchestrator or MCP path")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Claim creation requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input claimInput
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_claim", "Invalid claim request", err.Error())
		return
	}
	input.AgentID, input.TaskID, input.ResourceType, input.Path, input.Mode = strings.TrimSpace(input.AgentID), strings.TrimSpace(input.TaskID), strings.TrimSpace(input.ResourceType), strings.TrimSpace(input.Path), strings.TrimSpace(input.Mode)
	if input.AgentID == "" || input.TaskID == "" || input.ResourceType == "" || input.Mode == "" {
		WriteProblem(w, r, 400, "invalid_claim", "Invalid claim request", "agent_id, task_id, resource_type, and mode are required")
		return
	}
	if input.Path != "" {
		if _, err := safeWorkspacePath(workspace.RootPath, input.Path); err != nil {
			WriteProblem(w, r, 400, "invalid_claim_path", "Invalid claim path", err.Error())
			return
		}
	}
	service := claimsvc.NewService(s.config.Store)
	claim, err := service.Create(r.Context(), claimsvc.CreateRequest{ID: input.ClaimID, RunID: input.RunID, AgentID: input.AgentID, TaskID: input.TaskID, ResourceID: input.ResourceID, ResourceType: input.ResourceType, ResourcePath: input.Path, SymbolName: input.Symbol, ClaimType: input.Mode, BaseHash: input.BaseHash, TTL: time.Duration(input.TTLSeconds) * time.Second, RationaleMD: redactText(input.Rationale)})
	if err != nil {
		status := 409
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") {
			status = 400
		}
		WriteProblem(w, r, status, "claim_conflict", "Claim could not be acquired", err.Error())
		return
	}
	if _, err := s.config.Store.DB().ExecContext(r.Context(), `UPDATE claims SET workspace_id = ? WHERE id = ?`, workspace.ID, claim.ID); err != nil {
		WriteProblem(w, r, 500, "claim_persist_failed", "Claim workspace binding failed", err.Error())
		return
	}
	writeJSON(w, 201, s.toClaimResponse(r.Context(), workspace.ID, claim))
}
func (s *Server) transitionClaimAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	claim, err := s.config.Store.GetClaim(r.Context(), r.PathValue("claim_id"))
	if err != nil || !claimWorkspace(s.config.Store.DB(), r.Context(), claim.ID, workspace.ID) {
		WriteProblem(w, r, 404, "claim_not_found", "Claim not found", "claim does not belong to this workspace")
		return
	}
	actor := r.Header.Get("X-Actor-ID")
	owner := actor != "" && actor == claim.AgentID
	action := r.PathValue("action")
	if !owner && !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Claim owner or human authorization required", "Only the owner may release or extend a claim")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Claim transitions require Idempotency-Key")
		return
	}
	service := claimsvc.NewService(s.config.Store)
	var updated db.Claim
	if action == "extend" {
		var body struct {
			TTLSeconds int `json:"ttl_seconds"`
		}
		_ = decodeJSON(r, &body)
		updated, err = service.Extend(r.Context(), claim.TaskID, claim.ID, actor, time.Duration(body.TTLSeconds)*time.Second)
	} else if action == "release" || action == "force-release" {
		reason := r.URL.Query().Get("reason")
		if action == "force-release" && strings.TrimSpace(reason) == "" {
			WriteProblem(w, r, 400, "reason_required", "Reason required", "force release requires a reason")
			return
		}
		updated, err = service.Release(r.Context(), claim.TaskID, claim.ID, actor, redactText(reason))
	} else {
		WriteProblem(w, r, 400, "invalid_claim_transition", "Invalid claim transition", action)
		return
	}
	if err != nil {
		WriteProblem(w, r, 409, "claim_transition_conflict", "Claim transition failed", err.Error())
		return
	}
	if !owner {
		_, _ = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO audit_events (workspace_id, actor_id, action, entity_type, entity_id, request_id, reason, payload_json) VALUES (?, ?, ?, 'claim', ?, ?, ?, ?)`, workspace.ID, actor, "claim."+action, updated.ID, requestID(r.Context()), r.URL.Query().Get("reason"), `{"forced":true}`)
	}
	writeJSON(w, 200, s.toClaimResponse(r.Context(), workspace.ID, updated))
}
func (s *Server) listClaimContentions(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	resourceID := r.URL.Query().Get("resource_id")
	claims, err := s.config.Store.ListClaims(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "contention_list_failed", "Unable to list contentions", err.Error())
		return
	}
	items := make([]map[string]any, 0)
	for _, claim := range claims {
		if claim.Status == "active" && claimWorkspace(s.config.Store.DB(), r.Context(), claim.ID, workspace.ID) && (resourceID == "" || claim.ResourceID == resourceID) {
			items = append(items, map[string]any{"claim_id": claim.ID, "resource_id": claim.ResourceID, "agent_id": claim.AgentID, "state": claim.Status, "reason": "active claim may contend with a requested resource"})
		}
	}
	writePage(w, r, items)
}
func claimWorkspace(sqlDB *sql.DB, ctx context.Context, claimID, workspaceID string) bool {
	var value string
	return sqlDB.QueryRowContext(ctx, `SELECT COALESCE(workspace_id,'') FROM claims WHERE id = ?`, claimID).Scan(&value) == nil && value == workspaceID
}
func (s *Server) toClaimResponse(ctx context.Context, workspaceID string, claim db.Claim) claimResponse {
	resource, _ := s.config.Store.GetResource(ctx, claim.ResourceID)
	return claimResponse{ID: claim.ID, WorkspaceID: workspaceID, AgentID: claim.AgentID, TaskID: claim.TaskID, ResourceID: claim.ResourceID, ResourceType: resource.Type, Path: resource.Path, Symbol: resource.Symbol, Mode: claim.ClaimType, BaseHash: claim.BaseHash, State: claim.Status, AcquiredAt: claim.CreatedAt, LeaseExpiresAt: claim.ExpiresAt, Rationale: redactText(claim.RationaleMD)}
}
