package httpapi

import (
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"roundtable/internal/db"
)

type sessionLog struct {
	ID        int64  `json:"id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload,omitempty"`
	CreatedAt string `json:"created_at"`
}

type sessionToolCall struct {
	ID        int64  `json:"id"`
	Tool      string `json:"tool"`
	Action    string `json:"action,omitempty"`
	Status    string `json:"status,omitempty"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) sessionLogs(w http.ResponseWriter, r *http.Request) {
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
	events, err := s.config.Store.ListAgentSessionEvents(r.Context(), session.ID)
	if err != nil {
		WriteProblem(w, r, 500, "session_logs_failed", "Unable to list session logs", err.Error())
		return
	}
	items := make([]sessionLog, 0, len(events))
	for _, event := range events {
		items = append(items, sessionLog{ID: event.ID, EventType: event.EventType, Payload: redactLogPayload(event.PayloadJSON), CreatedAt: event.CreatedAt})
	}
	writePage(w, r, items)
}

func (s *Server) sessionToolCalls(w http.ResponseWriter, r *http.Request) {
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
	events, err := s.config.Store.ListEvents(r.Context(), session.RunID)
	if err != nil {
		WriteProblem(w, r, 500, "tool_calls_failed", "Unable to list tool calls", err.Error())
		return
	}
	items := make([]sessionToolCall, 0)
	for _, event := range events {
		if !strings.HasPrefix(event.Type, "tool.") && !strings.Contains(strings.ToLower(event.PayloadJSON), "\"tool\"") {
			continue
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(event.PayloadJSON), &payload)
		tool, _ := payload["tool"].(string)
		if tool == "" {
			tool = strings.TrimPrefix(event.Type, "tool.")
		}
		action, _ := payload["action"].(string)
		status, _ := payload["status"].(string)
		items = append(items, sessionToolCall{ID: event.ID, Tool: redactText(tool), Action: redactText(action), Status: redactText(status), CreatedAt: event.CreatedAt})
	}
	writePage(w, r, items)
}

func (s *Server) sessionClaims(w http.ResponseWriter, r *http.Request) {
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
	claims, err := s.config.Store.ListClaims(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "claims_failed", "Unable to list claims", err.Error())
		return
	}
	items := make([]db.Claim, 0)
	for _, claim := range claims {
		if claim.AgentID == session.AgentID {
			claim.RationaleMD = redactText(claim.RationaleMD)
			items = append(items, claim)
		}
	}
	writePage(w, r, items)
}

func (s *Server) sessionProposals(w http.ResponseWriter, r *http.Request) {
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
	proposals, err := s.config.Store.ListProposals(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "proposals_failed", "Unable to list proposals", err.Error())
		return
	}
	items := make([]map[string]any, 0)
	for _, proposal := range proposals {
		if proposal.AgentID != session.AgentID {
			continue
		}
		items = append(items, map[string]any{"id": proposal.ID, "task_id": proposal.TaskID, "title": redactText(proposal.Title), "summary_md": redactText(proposal.SummaryMD), "status": proposal.Status, "risk": proposal.Risk, "created_at": proposal.CreatedAt})
	}
	writePage(w, r, items)
}

func (s *Server) sessionMetrics(w http.ResponseWriter, r *http.Request) {
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
	var metadata map[string]any
	_ = json.Unmarshal([]byte(session.MetadataJSON), &metadata)
	usage := map[string]any{}
	if candidate, ok := metadata["usage"].(map[string]any); ok {
		for key, value := range candidate {
			if isSafeMetric(key, value) {
				usage[key] = value
			}
		}
	}
	writeJSON(w, 200, map[string]any{"session_id": session.ID, "usage": usage, "source": "adapter_metadata"})
}

func (s *Server) sessionEnvironment(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, 200, map[string]any{"session_id": session.ID, "fingerprint": map[string]string{"go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "adapter": session.Adapter}})
}

func writePage[T any](w http.ResponseWriter, r *http.Request, items []T) {
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

func redactLogPayload(raw string) string {
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		value = redactJSON(value)
		encoded, err := json.Marshal(value)
		if err == nil {
			return string(encoded)
		}
	}
	return redactText(raw)
}
func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "api_key") || strings.Contains(lower, "authorization") || strings.Contains(lower, "chain_of_thought") {
				typed[key] = "[REDACTED]"
			} else {
				typed[key] = redactJSON(child)
			}
		}
	case []any:
		for i := range typed {
			typed[i] = redactJSON(typed[i])
		}
	case string:
		return redactText(typed)
	}
	return value
}
func redactText(value string) string {
	for _, key := range []string{"token", "password", "secret", "api_key", "authorization"} {
		value = redactAfterKey(value, key)
	}
	return value
}
func redactAfterKey(value, key string) string {
	lower := strings.ToLower(value)
	marker := key + "="
	index := strings.Index(lower, marker)
	if index < 0 {
		return value
	}
	end := strings.IndexAny(value[index+len(marker):], " &,\n")
	if end < 0 {
		end = len(value) - index - len(marker)
	}
	return value[:index+len(marker)] + "[REDACTED]" + value[index+len(marker)+end:]
}
func isSafeMetric(key string, value any) bool {
	lowerKey := strings.ToLower(key)
	for _, sensitive := range []string{"secret", "password", "api_key", "authorization"} {
		if strings.Contains(lowerKey, sensitive) {
			return false
		}
	}
	switch value.(type) {
	case float64, int, int64, json.Number:
		return true
	default:
		return false
	}
}
