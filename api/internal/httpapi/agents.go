package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"roundtable/internal/db"
)

type agentResponse struct {
	AgentID            string          `json:"agent_id"`
	DisplayName        string          `json:"display_name"`
	AdapterType        string          `json:"adapter_type"`
	Enabled            bool            `json:"enabled"`
	Health             string          `json:"health"`
	ExecutableVersion  string          `json:"executable_version,omitempty"`
	Capabilities       map[string]bool `json:"capabilities"`
	SupportsResume     bool            `json:"supports_resume"`
	SupportsMCP        bool            `json:"supports_mcp"`
	ConcurrencyLimit   int             `json:"concurrency_limit"`
	PolicyWeight       int             `json:"policy_weight"`
	ActiveSessionCount int             `json:"active_session_count"`
	LastHeartbeat      string          `json:"last_heartbeat,omitempty"`
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	if _, err := s.workspace(r); err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	agents, err := s.config.Store.ListAgents(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "agent_list_failed", "Unable to list agents", err.Error())
		return
	}
	items := make([]agentResponse, 0, len(agents))
	for _, agent := range agents {
		item, itemErr := s.agentResponse(r, agent)
		if itemErr != nil {
			WriteProblem(w, r, 500, "agent_context_failed", "Unable to load agent context", itemErr.Error())
			return
		}
		items = append(items, item)
	}
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
		value := strconv.Itoa(end)
		next = &value
	}
	writeJSON(w, 200, map[string]any{"items": items[start:end], "next_cursor": next})
}

func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	if _, err := s.workspace(r); err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	agent, err := s.config.Store.GetAgent(r.Context(), r.PathValue("agent_id"))
	if err != nil {
		WriteProblem(w, r, 404, "agent_not_found", "Agent not found", err.Error())
		return
	}
	item, err := s.agentResponse(r, agent)
	if err != nil {
		WriteProblem(w, r, 500, "agent_context_failed", "Unable to load agent context", err.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) setAgentEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Agent state changes require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	agent, err := s.config.Store.GetAgent(r.Context(), r.PathValue("agent_id"))
	if err != nil {
		WriteProblem(w, r, 404, "agent_not_found", "Agent not found", err.Error())
		return
	}
	if !enabled && agent.IsEnabled {
		var active int
		if err := s.config.Store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM agent_sessions WHERE agent_id = ? AND status IN ('active','running')", agent.ID).Scan(&active); err != nil {
			WriteProblem(w, r, 500, "agent_impact_failed", "Unable to determine agent impact", err.Error())
			return
		}
		if active > 0 {
			WriteProblem(w, r, 409, "agent_has_active_sessions", "Agent has active sessions", "Disable uses drain semantics until active sessions finish")
			return
		}
	}
	updated, err := s.config.Store.SetAgentEnabled(r.Context(), workspace.ID, agent.ID, enabled, r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	if err != nil {
		WriteProblem(w, r, 409, "agent_state_conflict", "Agent state could not be changed", err.Error())
		return
	}
	item, err := s.agentResponse(r, updated)
	if err != nil {
		WriteProblem(w, r, 500, "agent_context_failed", "Unable to load agent context", err.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) agentResponse(r *http.Request, agent db.Agent) (agentResponse, error) {
	var active int
	if err := s.config.Store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM agent_sessions WHERE agent_id = ? AND status IN ('active','running')", agent.ID).Scan(&active); err != nil {
		return agentResponse{}, err
	}
	resume, mcp := false, false
	switch agent.Adapter {
	case "codex", "claude", "gemini":
		resume, mcp = true, true
	case "opencode":
		mcp = true
	}
	health := agent.Status
	if health == "" {
		health = "unknown"
	}
	return agentResponse{AgentID: agent.ID, DisplayName: agent.Name, AdapterType: agent.Adapter, Enabled: agent.IsEnabled, Health: health, Capabilities: map[string]bool{"resume": resume, "mcp": mcp, "read_only_workspace": true}, SupportsResume: resume, SupportsMCP: mcp, ConcurrencyLimit: 1, PolicyWeight: 1, ActiveSessionCount: active}, nil
}
