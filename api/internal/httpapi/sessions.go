package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"roundtable/internal/db"
)

type sessionInput struct {
	AgentID           string `json:"agent_id"`
	RunID             string `json:"run_id"`
	Adapter           string `json:"adapter"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	ExternalSessionID string `json:"external_session_id"`
}

type sessionResponse struct {
	ID                    string `json:"id"`
	AgentID               string `json:"agent_id"`
	RunID                 string `json:"run_id"`
	Adapter               string `json:"adapter"`
	Provider              string `json:"provider,omitempty"`
	Model                 string `json:"model,omitempty"`
	ExternalSessionID     string `json:"external_session_id,omitempty"`
	ResumeCommandTemplate string `json:"resume_command_template,omitempty"`
	Status                string `json:"status"`
	StartedAt             string `json:"started_at"`
	LastSeenAt            string `json:"last_seen_at,omitempty"`
	EndedAt               string `json:"ended_at,omitempty"`
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	sessions, err := s.config.Store.ListAgentSessions(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "session_list_failed", "Unable to list sessions", err.Error())
		return
	}
	items := make([]sessionResponse, 0, len(sessions))
	for _, session := range sessions {
		if !sessionBelongsToWorkspace(session, workspace.ID) {
			continue
		}
		items = append(items, toSessionResponse(session))
	}
	returnPaginatedSessions(w, r, items)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	session, err := s.config.Store.GetAgentSession(r.Context(), r.PathValue("session_id"))
	if err != nil || !sessionBelongsToWorkspace(session, workspace.ID) {
		WriteProblem(w, r, 404, "session_not_found", "Session not found", "session does not belong to this workspace")
		return
	}
	writeJSON(w, 200, toSessionResponse(session))
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Session creation requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input sessionInput
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_session", "Invalid session request", err.Error())
		return
	}
	input.AgentID, input.RunID, input.Adapter = strings.TrimSpace(input.AgentID), strings.TrimSpace(input.RunID), strings.TrimSpace(input.Adapter)
	if input.AgentID == "" || input.RunID == "" || input.Adapter == "" {
		WriteProblem(w, r, 400, "invalid_session", "Invalid session request", "agent_id, run_id, and adapter are required")
		return
	}
	if _, err := s.config.Store.GetAgent(r.Context(), input.AgentID); err != nil {
		WriteProblem(w, r, 404, "agent_not_found", "Agent not found", err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := fmt.Sprintf("session-%d", time.Now().UnixNano())
	metadata, _ := json.Marshal(map[string]string{"workspace_id": workspace.ID, "branch": workspace.DefaultBranch})
	session := db.AgentSession{ID: id, AgentID: input.AgentID, RunID: input.RunID, Adapter: input.Adapter, Provider: input.Provider, Model: input.Model, ExternalSessionID: input.ExternalSessionID, ExternalResumeCommand: input.Adapter + " resume {{external_session_id}}", WorkingDirectory: workspace.RootPath, Status: "starting", StartedAt: now, LastSeenAt: now, MetadataJSON: string(metadata)}
	if err := s.config.Store.UpsertAgentSession(r.Context(), session); err != nil {
		WriteProblem(w, r, 409, "session_create_conflict", "Session could not be created", err.Error())
		return
	}
	saved, _ := s.config.Store.GetAgentSession(r.Context(), id)
	s.appendSessionEvent(r.Context(), saved, "started", r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	writeJSON(w, 201, toSessionResponse(saved))
}

func (s *Server) heartbeatSession(w http.ResponseWriter, r *http.Request) {
	s.transitionSession(w, r, "heartbeat", "")
}
func (s *Server) pauseSession(w http.ResponseWriter, r *http.Request) {
	s.transitionSession(w, r, "paused", "paused")
}
func (s *Server) stopSession(w http.ResponseWriter, r *http.Request) {
	s.transitionSession(w, r, "stopped", "stopped")
}

func (s *Server) resumeSession(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Session resume requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	session, err := s.sessionForWorkspace(r, workspace)
	if err != nil {
		WriteProblem(w, r, 404, "session_not_found", "Session not found", err.Error())
		return
	}
	if session.Status != "paused" && session.Status != "stopped" {
		WriteProblem(w, r, 409, "illegal_session_transition", "Session cannot be resumed", "only paused or stopped sessions can be resumed")
		return
	}
	if branch := sessionBranch(session); branch != "" {
		status, statusErr := inspectRepository(r.Context(), workspace)
		if statusErr == nil && status.Branch != branch {
			WriteProblem(w, r, 409, "session_branch_conflict", "Session branch is incompatible", "workspace branch changed since session start")
			return
		}
	}
	agent, err := s.config.Store.GetAgent(r.Context(), session.AgentID)
	if err != nil || !agent.IsEnabled {
		WriteProblem(w, r, 409, "agent_unavailable", "Agent is unavailable", "the session agent is disabled or missing")
		return
	}
	session.Status = "resuming"
	session.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.config.Store.UpsertAgentSession(r.Context(), session); err != nil {
		WriteProblem(w, r, 409, "session_resume_conflict", "Session could not be resumed", err.Error())
		return
	}
	s.appendSessionEvent(r.Context(), session, "resuming", r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	writeJSON(w, 200, toSessionResponse(session))
}

func (s *Server) terminateSession(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Session termination requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	session, err := s.sessionForWorkspace(r, workspace)
	if err != nil {
		WriteProblem(w, r, 404, "session_not_found", "Session not found", err.Error())
		return
	}
	var body struct {
		ImpactDetails string `json:"impact_details"`
	}
	if r.Body != nil {
		_ = decodeJSON(r, &body)
	}
	var claims int
	if err := s.config.Store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM claims WHERE agent_id = ? AND status = 'active'", session.AgentID).Scan(&claims); err != nil {
		WriteProblem(w, r, 500, "session_impact_failed", "Unable to determine session impact", err.Error())
		return
	}
	if claims > 0 && strings.TrimSpace(body.ImpactDetails) == "" {
		WriteProblem(w, r, 409, "termination_impact_required", "Termination impact is required", "provide impact_details while active claims exist")
		return
	}
	if !isActiveSessionState(session.Status) {
		WriteProblem(w, r, 409, "illegal_session_transition", "Session cannot be terminated", "session is already terminal")
		return
	}
	session.Status = "terminated"
	session.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
	session.LastSeenAt = session.EndedAt
	if err := s.config.Store.UpsertAgentSession(r.Context(), session); err != nil {
		WriteProblem(w, r, 409, "session_terminate_conflict", "Session could not be terminated", err.Error())
		return
	}
	s.appendSessionEvent(r.Context(), session, "terminated", r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	writeJSON(w, 200, toSessionResponse(session))
}

func (s *Server) transitionSession(w http.ResponseWriter, r *http.Request, event, target string) {
	if event != "heartbeat" && !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	session, err := s.sessionForWorkspace(r, workspace)
	if err != nil {
		WriteProblem(w, r, 404, "session_not_found", "Session not found", err.Error())
		return
	}
	if event == "heartbeat" {
		if !isActiveSessionState(session.Status) {
			WriteProblem(w, r, 409, "illegal_session_transition", "Session is not active", "terminal sessions cannot heartbeat")
			return
		}
		if value := r.URL.Query().Get("status"); value != "" {
			target = value
		}
		if target != "" && !isHeartbeatSessionState(target) {
			WriteProblem(w, r, 400, "invalid_session_status", "Invalid session status", "heartbeat status must be starting, active, running, paused, or resuming")
			return
		}
		session.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
		if target != "" {
			session.Status = target
		}
	} else {
		if !isActiveSessionState(session.Status) || (target == "paused" && session.Status == "paused") {
			WriteProblem(w, r, 409, "illegal_session_transition", "Illegal session transition", "session state does not permit this transition")
			return
		}
		session.Status = target
		if target == "stopped" {
			session.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		session.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := s.config.Store.UpsertAgentSession(r.Context(), session); err != nil {
		WriteProblem(w, r, 409, "session_transition_conflict", "Session transition failed", err.Error())
		return
	}
	s.appendSessionEvent(r.Context(), session, event, r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	writeJSON(w, 200, toSessionResponse(session))
}

func (s *Server) sessionForWorkspace(r *http.Request, workspace db.Workspace) (db.AgentSession, error) {
	session, err := s.config.Store.GetAgentSession(r.Context(), r.PathValue("session_id"))
	if err != nil {
		return db.AgentSession{}, err
	}
	if !sessionBelongsToWorkspace(session, workspace.ID) {
		return db.AgentSession{}, errors.New("session does not belong to workspace")
	}
	return session, nil
}
func sessionBelongsToWorkspace(session db.AgentSession, workspaceID string) bool {
	var metadata map[string]any
	return json.Unmarshal([]byte(session.MetadataJSON), &metadata) == nil && metadata["workspace_id"] == workspaceID
}
func sessionBranch(session db.AgentSession) string {
	var metadata map[string]any
	if json.Unmarshal([]byte(session.MetadataJSON), &metadata) != nil {
		return ""
	}
	branch, _ := metadata["branch"].(string)
	return strings.TrimSpace(branch)
}
func isActiveSessionState(status string) bool {
	return status == "starting" || status == "active" || status == "running" || status == "paused" || status == "resuming"
}
func isHeartbeatSessionState(status string) bool {
	return isActiveSessionState(status)
}
func toSessionResponse(session db.AgentSession) sessionResponse {
	return sessionResponse{ID: session.ID, AgentID: session.AgentID, RunID: session.RunID, Adapter: session.Adapter, Provider: session.Provider, Model: session.Model, ExternalSessionID: session.ExternalSessionID, ResumeCommandTemplate: session.Adapter + " resume <external_session_id>", Status: session.Status, StartedAt: session.StartedAt, LastSeenAt: session.LastSeenAt, EndedAt: session.EndedAt}
}
func (s *Server) appendSessionEvent(ctx context.Context, session db.AgentSession, event, actor, requestID string) {
	payload, _ := json.Marshal(map[string]any{"session_id": session.ID, "status": session.Status, "request_id": requestID})
	_, _ = s.config.Store.AppendAgentSessionEvent(ctx, db.AgentSessionEvent{SessionID: session.ID, EventType: event, PayloadJSON: string(payload)})
	_, _ = s.config.Store.AppendEvent(ctx, db.Event{RunID: session.RunID, Type: "session." + event, ActorID: actor, PayloadJSON: string(payload)})
}
func returnPaginatedSessions(w http.ResponseWriter, r *http.Request, items []sessionResponse) {
	start, err := cursorOffset(r.URL.Query().Get("cursor"))
	if err != nil {
		WriteProblem(w, r, 400, "invalid_cursor", "Invalid cursor", err.Error())
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			WriteProblem(w, r, 400, "invalid_limit", "Invalid limit", "limit must be between 1 and 200")
			return
		}
	}
	if start > len(items) {
		start = len(items)
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	var next *string
	if end < len(items) {
		value := encodeCursor(end)
		next = &value
	}
	writeJSON(w, 200, map[string]any{"items": items[start:end], "next_cursor": next})
}
