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

type deliberationInput struct {
	Goal              string   `json:"goal"`
	AgentPool         []string `json:"agent_pool"`
	GovernanceProfile string   `json:"governance_profile"`
	ResourceScope     []string `json:"initial_resource_scope"`
	BudgetLimit       int      `json:"budget_limit"`
	TimeLimitSeconds  int      `json:"time_limit_seconds"`
	HumanConstraints  []string `json:"human_constraints"`
	ModeratorID       string   `json:"moderator_id"`
}

type deliberationResponse struct {
	ID                  string   `json:"id"`
	WorkspaceID         string   `json:"workspace_id"`
	Goal                string   `json:"goal"`
	Status              string   `json:"status"`
	CreatedBy           string   `json:"created_by"`
	ModeratorID         string   `json:"moderator_id,omitempty"`
	AgentPool           []string `json:"agent_pool"`
	GovernanceProfile   string   `json:"governance_profile,omitempty"`
	ResourceScope       []string `json:"initial_resource_scope,omitempty"`
	BudgetLimit         int      `json:"budget_limit,omitempty"`
	TimeLimitSeconds    int      `json:"time_limit_seconds,omitempty"`
	HumanConstraints    []string `json:"human_constraints,omitempty"`
	Round               int      `json:"round"`
	Participants        []string `json:"participants"`
	UnresolvedConflicts []string `json:"unresolved_conflicts"`
	LinkedProposals     []string `json:"linked_proposals"`
	StartedAt           string   `json:"started_at,omitempty"`
	EndedAt             string   `json:"ended_at,omitempty"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
}

type deliberationMessageInput struct {
	MessageType string `json:"message_type"`
	Body        string `json:"body"`
}

func (s *Server) listDeliberations(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id FROM deliberations WHERE workspace_id = ? ORDER BY created_at, id`, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "deliberation_list_failed", "Unable to list deliberations", err.Error())
		return
	}
	defer rows.Close()
	items := make([]deliberationResponse, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			WriteProblem(w, r, 500, "deliberation_list_failed", "Unable to list deliberations", err.Error())
			return
		}
		value, err := s.readDeliberation(r, workspace.ID, id)
		if err == nil {
			items = append(items, value)
		}
	}
	if err := rows.Err(); err != nil {
		WriteProblem(w, r, 500, "deliberation_list_failed", "Unable to list deliberations", err.Error())
		return
	}
	writePage(w, r, items)
}

func (s *Server) createDeliberation(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Deliberation creation requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input deliberationInput
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_deliberation", "Invalid deliberation request", err.Error())
		return
	}
	input.Goal = strings.TrimSpace(input.Goal)
	input.ModeratorID = strings.TrimSpace(input.ModeratorID)
	if input.Goal == "" || len(input.AgentPool) == 0 {
		WriteProblem(w, r, 400, "invalid_deliberation", "Invalid deliberation request", "goal and agent_pool are required")
		return
	}
	for i := range input.AgentPool {
		input.AgentPool[i] = strings.TrimSpace(input.AgentPool[i])
		if input.AgentPool[i] == "" {
			WriteProblem(w, r, 400, "invalid_deliberation", "Invalid deliberation request", "agent_pool cannot contain empty IDs")
			return
		}
		if _, err := s.config.Store.GetAgent(r.Context(), input.AgentPool[i]); err != nil {
			WriteProblem(w, r, 404, "agent_not_found", "Agent not found", err.Error())
			return
		}
	}
	metadata, _ := json.Marshal(map[string]any{"goal": input.Goal, "governance_profile": input.GovernanceProfile, "resource_scope": input.ResourceScope, "budget_limit": input.BudgetLimit, "time_limit_seconds": input.TimeLimitSeconds, "human_constraints": input.HumanConstraints})
	id := fmt.Sprintf("deliberation-%d", time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	actor := r.Header.Get("X-Actor-ID")
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "deliberation_create_failed", "Unable to create deliberation", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO deliberations (id, workspace_id, title, status, created_by, moderator_id, metadata_json, created_at, updated_at) VALUES (?, ?, ?, 'draft', ?, ?, ?, ?, ?)`, id, workspace.ID, input.Goal, actor, input.ModeratorID, string(metadata), now, now)
	if err == nil {
		for _, agentID := range input.AgentPool {
			_, err = tx.ExecContext(r.Context(), `INSERT INTO deliberation_participants (deliberation_id, agent_id) VALUES (?, ?)`, id, agentID)
			if err != nil {
				break
			}
		}
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO deliberation_rounds (id, deliberation_id, number, status, started_at) VALUES (?, ?, 0, 'pending', ?)`, id+"-round-0", id, now)
	}
	if err == nil {
		err = insertDeliberationAudit(r.Context(), tx, workspace.ID, actor, id, r.Header.Get("X-Request-ID"), "deliberation.created", metadata)
	}
	if err != nil {
		WriteProblem(w, r, 409, "deliberation_create_conflict", "Deliberation could not be created", err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "deliberation_create_conflict", "Deliberation could not be created", err.Error())
		return
	}
	value, _ := s.readDeliberation(r, workspace.ID, id)
	_, _ = s.config.Store.AppendEvent(r.Context(), eventForDeliberation(value, "deliberation.created", actor, requestID(r.Context())))
	writeJSON(w, 201, value)
}

func (s *Server) getDeliberation(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	value, err := s.readDeliberation(r, workspace.ID, r.PathValue("deliberation_id"))
	if err != nil {
		WriteProblem(w, r, 404, "deliberation_not_found", "Deliberation not found", err.Error())
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) transitionDeliberation(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Lifecycle changes require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	target := strings.TrimPrefix(r.URL.Path, "/api/v1/workspaces/"+workspace.ID+"/deliberations/"+r.PathValue("deliberation_id")+"/")
	if target != "start" && target != "pause" && target != "resume" && target != "terminate" {
		WriteProblem(w, r, 400, "invalid_transition", "Invalid deliberation transition", target)
		return
	}
	value, err := s.readDeliberation(r, workspace.ID, r.PathValue("deliberation_id"))
	if err != nil {
		WriteProblem(w, r, 404, "deliberation_not_found", "Deliberation not found", err.Error())
		return
	}
	allowed := map[string]map[string]bool{"start": {"draft": true, "paused": true}, "pause": {"running": true}, "resume": {"paused": true}, "terminate": {"draft": true, "running": true, "paused": true}}
	if !allowed[target][value.Status] {
		WriteProblem(w, r, 409, "illegal_deliberation_transition", "Illegal deliberation transition", value.Status+" cannot transition to "+target)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	status := map[string]string{"start": "running", "pause": "paused", "resume": "running", "terminate": "terminated"}[target]
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "deliberation_transition_failed", "Unable to transition deliberation", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE deliberations SET status = ?, started_at = CASE WHEN ? = 'running' AND started_at IS NULL THEN ? ELSE started_at END, ended_at = CASE WHEN ? = 'terminated' THEN ? ELSE ended_at END, updated_at = ? WHERE id = ? AND workspace_id = ? AND status = ?`, status, status, now, status, now, now, value.ID, workspace.ID, value.Status)
	if err == nil {
		payload, _ := json.Marshal(map[string]string{"before": value.Status, "after": status})
		err = insertDeliberationAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), value.ID, requestID(r.Context()), "deliberation."+target, payload)
	}
	if err != nil {
		WriteProblem(w, r, 409, "deliberation_transition_conflict", "Deliberation transition failed", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "deliberation_transition_conflict", "Deliberation transition failed", err.Error())
		return
	}
	value, _ = s.readDeliberation(r, workspace.ID, value.ID)
	_, _ = s.config.Store.AppendEvent(r.Context(), eventForDeliberation(value, "deliberation."+target, r.Header.Get("X-Actor-ID"), requestID(r.Context())))
	writeJSON(w, 200, value)
}

func (s *Server) addDeliberationMessage(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	value, err := s.readDeliberation(r, workspace.ID, r.PathValue("deliberation_id"))
	if err != nil {
		WriteProblem(w, r, 404, "deliberation_not_found", "Deliberation not found", err.Error())
		return
	}
	if value.Status != "running" && value.Status != "paused" {
		WriteProblem(w, r, 409, "deliberation_not_active", "Deliberation does not accept input", value.Status)
		return
	}
	var input deliberationMessageInput
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Body) == "" {
		WriteProblem(w, r, 400, "invalid_message", "Invalid deliberation message", "body is required")
		return
	}
	if input.MessageType == "" {
		input.MessageType = "human_instruction"
	}
	input.Body = redactText(input.Body)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "message_failed", "Unable to append message", err.Error())
		return
	}
	defer tx.Rollback()
	var sequence int
	_ = tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(sequence), -1) + 1 FROM deliberation_messages WHERE deliberation_id = ?`, value.ID).Scan(&sequence)
	id := fmt.Sprintf("message-%d", time.Now().UnixNano())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO deliberation_messages (id, deliberation_id, agent_id, actor, message_type, body, sequence, created_at) VALUES (?, ?, NULL, ?, ?, ?, ?, ?)`, id, value.ID, r.Header.Get("X-Actor-ID"), input.MessageType, input.Body, sequence, now)
	if err == nil {
		err = insertDeliberationAudit(r.Context(), tx, workspace.ID, r.Header.Get("X-Actor-ID"), value.ID, requestID(r.Context()), "deliberation.message_added", []byte(`{"message_type":"human_instruction"}`))
	}
	if err != nil {
		WriteProblem(w, r, 409, "message_conflict", "Unable to append message", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "message_conflict", "Unable to append message", err.Error())
		return
	}
	_, _ = s.config.Store.AppendEvent(r.Context(), dbEvent(value.ID, "deliberation.message_added", r.Header.Get("X-Actor-ID"), requestID(r.Context())))
	writeJSON(w, 201, map[string]any{"id": id, "deliberation_id": value.ID, "message_type": input.MessageType, "body": input.Body, "sequence": sequence, "created_at": now})
}

func (s *Server) readDeliberation(r *http.Request, workspaceID, id string) (deliberationResponse, error) {
	row := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id, workspace_id, title, status, created_by, COALESCE(moderator_id,''), COALESCE(metadata_json,'') , COALESCE(started_at,''), COALESCE(ended_at,''), created_at, updated_at FROM deliberations WHERE id = ? AND workspace_id = ?`, id, workspaceID)
	var value deliberationResponse
	var metadata, started, ended string
	if err := row.Scan(&value.ID, &value.WorkspaceID, &value.Goal, &value.Status, &value.CreatedBy, &value.ModeratorID, &metadata, &started, &ended, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return value, err
	}
	value.StartedAt, value.EndedAt = started, ended
	var m map[string]any
	_ = json.Unmarshal([]byte(metadata), &m)
	value.GovernanceProfile, _ = m["governance_profile"].(string)
	value.ResourceScope = stringSlice(m["resource_scope"])
	value.HumanConstraints = stringSlice(m["human_constraints"])
	value.BudgetLimit = intValue(m["budget_limit"])
	value.TimeLimitSeconds = intValue(m["time_limit_seconds"])
	rows, _ := s.config.Store.DB().QueryContext(r.Context(), `SELECT agent_id FROM deliberation_participants WHERE deliberation_id = ? ORDER BY agent_id`, id)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var agent string
			_ = rows.Scan(&agent)
			value.Participants = append(value.Participants, agent)
		}
	}
	value.AgentPool = append([]string(nil), value.Participants...)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COALESCE(MAX(number), 0) FROM deliberation_rounds WHERE deliberation_id = ?`, id).Scan(&value.Round)
	rows, _ = s.config.Store.DB().QueryContext(r.Context(), `SELECT summary FROM deliberation_conflicts WHERE deliberation_id = ? AND status = 'open' ORDER BY created_at`, id)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var conflict string
			_ = rows.Scan(&conflict)
			value.UnresolvedConflicts = append(value.UnresolvedConflicts, redactText(conflict))
		}
	}
	return value, nil
}

func insertDeliberationAudit(ctx context.Context, tx *sql.Tx, workspaceID, actor, entity, request, action string, payload []byte) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events (workspace_id, actor_id, action, entity_type, entity_id, request_id, payload_json) VALUES (?, ?, ?, 'deliberation', ?, ?, ?)`, workspaceID, actor, action, entity, nullString(request), string(payload))
	return err
}
func eventForDeliberation(value deliberationResponse, typ, actor, request string) db.Event {
	return db.Event{RunID: value.ID, Type: typ, ActorID: actor, PayloadJSON: fmt.Sprintf(`{"deliberation_id":%q,"status":%q,"request_id":%q}`, value.ID, value.Status, request)}
}
func dbEvent(id, typ, actor, request string) db.Event {
	return db.Event{RunID: id, Type: typ, ActorID: actor, PayloadJSON: fmt.Sprintf(`{"deliberation_id":%q,"request_id":%q}`, id, request)}
}
func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if v, ok := item.(string); ok {
			out = append(out, redactText(v))
		}
	}
	return out
}
func intValue(value any) int {
	if v, ok := value.(float64); ok {
		return int(v)
	}
	return 0
}
