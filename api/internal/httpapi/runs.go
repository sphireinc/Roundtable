package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type runControlResponse struct {
	RunID               string `json:"run_id"`
	WorkspaceID         string `json:"workspace_id"`
	Goal                string `json:"goal"`
	State               string `json:"state"`
	ActiveAgentCount    int    `json:"active_agent_count"`
	ActiveDeliberations int    `json:"active_deliberations"`
	OpenProposals       int    `json:"open_proposals"`
	PendingApprovals    int    `json:"pending_approvals"`
	Blockers            int    `json:"blockers"`
	UpdatedAt           string `json:"updated_at"`
}

func (s *Server) runStatusAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var id, goal, state, updated string
	if err := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id,COALESCE(goal,''),status,started_at FROM runs WHERE workspace_id=? ORDER BY started_at DESC LIMIT 1`, workspace.ID).Scan(&id, &goal, &state, &updated); err != nil {
		writeJSON(w, 200, runControlResponse{WorkspaceID: workspace.ID, State: "stopped", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return
	}
	value := runControlResponse{RunID: id, WorkspaceID: workspace.ID, Goal: redactText(goal), State: state, UpdatedAt: updated}
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM agent_sessions WHERE run_id=? AND status IN ('active','running')`, id).Scan(&value.ActiveAgentCount)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM deliberations WHERE status IN ('active','running')`).Scan(&value.ActiveDeliberations)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id=? AND status IN ('pending','in_review')`, workspace.ID).Scan(&value.OpenProposals)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM human_approvals h JOIN proposals p ON p.id=h.proposal_id WHERE p.workspace_id=? AND h.status='requested'`, workspace.ID).Scan(&value.PendingApprovals)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id=? AND status='rejected'`, workspace.ID).Scan(&value.Blockers)
	writeJSON(w, 200, value)
}

func (s *Server) transitionRunAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Run control requires a human role")
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Run control requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	action := r.PathValue("action")
	target := map[string]string{"start": "active", "pause": "paused-intake", "resume": "active", "stop": "stopped", "force-stop": "stopped"}[action]
	if target == "" {
		WriteProblem(w, r, 400, "invalid_run_action", "Invalid run action", action)
		return
	}
	var in struct {
		RunID string `json:"run_id"`
		Goal  string `json:"goal"`
	}
	_ = decodeJSON(r, &in)
	var runID, state string
	err = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id,status FROM runs WHERE workspace_id=? ORDER BY started_at DESC LIMIT 1`, workspace.ID).Scan(&runID, &state)
	if action == "start" && (err != nil || state == "stopped") {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
		_, err = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO runs(id,workspace_id,goal,status,started_at) VALUES(?,?,?, 'starting',CURRENT_TIMESTAMP)`, runID, workspace.ID, redactText(in.Goal))
		state = "starting"
	}
	if err != nil {
		WriteProblem(w, r, 404, "run_not_found", "Run not found", err.Error())
		return
	}
	allowed := map[string]map[string]bool{"start": {"stopped": true, "starting": true}, "pause": {"active": true}, "resume": {"paused-intake": true, "degraded": true}, "stop": {"active": true, "paused-intake": true, "draining": true}, "force-stop": {"active": true, "paused-intake": true, "draining": true, "degraded": true}}
	if !allowed[action][state] {
		WriteProblem(w, r, 409, "illegal_run_transition", "Illegal run transition", state+" cannot "+action)
		return
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "run_transition_failed", "Unable to transition run", err.Error())
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE runs SET status=? WHERE id=? AND status=?`, target, runID, state); err == nil {
		payload, _ := json.Marshal(map[string]string{"action": action, "state": target})
		_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?,?,'run',?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), "run."+action, runID, requestID(r.Context()), string(payload))
	}
	if err != nil || tx.Commit() != nil {
		WriteProblem(w, r, 409, "run_transition_failed", "Unable to transition run", fmt.Sprint(err))
		return
	}
	writeJSON(w, 200, map[string]any{"run_id": runID, "state": target})
}
