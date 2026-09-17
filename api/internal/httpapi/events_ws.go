package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"roundtable/internal/db"
)

var eventUpgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: func(*http.Request) bool { return true }}

type liveEventEnvelope struct {
	EventID     int64          `json:"event_id"`
	WorkspaceID string         `json:"workspace_id"`
	Sequence    int64          `json:"sequence"`
	Type        string         `json:"type"`
	OccurredAt  string         `json:"occurred_at"`
	Actor       string         `json:"actor,omitempty"`
	EntityType  string         `json:"entity_type,omitempty"`
	EntityID    string         `json:"entity_id,omitempty"`
	Payload     map[string]any `json:"payload"`
}

func (s *Server) eventsWebSocketAPI(w http.ResponseWriter, r *http.Request) {
	actor := strings.TrimSpace(r.Header.Get("X-Actor-ID"))
	if actor == "" {
		actor = strings.TrimSpace(r.URL.Query().Get("actor_id"))
	}
	if actor == "" {
		WriteProblem(w, r, 401, "authentication_required", "Authentication required", "WebSocket connections require an actor identity")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	last := int64(0)
	if raw := r.URL.Query().Get("last_event_id"); raw != "" {
		last, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || last < 0 {
			WriteProblem(w, r, 400, "invalid_event_cursor", "Invalid event cursor", "last_event_id must be a non-negative integer")
			return
		}
	}
	conn, err := eventUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	current := s.currentWorkspaceSequence(r, workspace.ID)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(map[string]any{"kind": "hello", "schema_version": "1", "workspace_id": workspace.ID, "current_sequence": current, "resumable": true}); err != nil {
		return
	}
	sub := s.config.Store.Events().Subscribe(64)
	defer s.config.Store.Events().Unsubscribe(sub)
	historical, truncated, err := s.workspaceEvents(r, workspace.ID, last)
	if err != nil {
		return
	}
	if truncated {
		_ = conn.WriteJSON(map[string]any{"kind": "resync_required", "workspace_id": workspace.ID, "reason": "cursor_not_retained", "current_sequence": current})
		return
	}
	for _, event := range historical {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(eventEnvelope(workspace.ID, event)); err != nil {
			return
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case event, ok := <-sub:
			if !ok {
				return
			}
			if !s.eventBelongsToWorkspace(r, workspace.ID, event) {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(eventEnvelope(workspace.ID, event)); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := conn.WriteJSON(map[string]any{"kind": "ping", "workspace_id": workspace.ID, "sequence": s.currentWorkspaceSequence(r, workspace.ID)}); err != nil {
				return
			}
		}
	}
}

func (s *Server) currentWorkspaceSequence(r *http.Request, workspaceID string) int64 {
	var value int64
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COALESCE(MAX(e.id),0) FROM events e WHERE e.run_id=? OR EXISTS(SELECT 1 FROM runs rr WHERE rr.id=e.run_id AND rr.workspace_id=?)`, workspaceID, workspaceID).Scan(&value)
	return value
}

func (s *Server) workspaceEvents(r *http.Request, workspaceID string, last int64) ([]db.Event, bool, error) {
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT e.id,e.run_id,e.type,COALESCE(e.actor_id,''),COALESCE(e.task_id,''),e.payload_json,e.created_at FROM events e WHERE (e.run_id=? OR EXISTS(SELECT 1 FROM runs rr WHERE rr.id=e.run_id AND rr.workspace_id=?)) AND e.id>? ORDER BY e.id LIMIT 1001`, workspaceID, workspaceID, last)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]db.Event, 0)
	for rows.Next() {
		var event db.Event
		if err := rows.Scan(&event.ID, &event.RunID, &event.Type, &event.ActorID, &event.TaskID, &event.PayloadJSON, &event.CreatedAt); err != nil {
			return nil, false, err
		}
		items = append(items, event)
	}
	return items, len(items) > 1000, rows.Err()
}

func (s *Server) eventBelongsToWorkspace(r *http.Request, workspaceID string, event db.Event) bool {
	if event.RunID == workspaceID {
		return true
	}
	var one int
	return s.config.Store.DB().QueryRowContext(r.Context(), `SELECT 1 FROM runs WHERE id=? AND workspace_id=?`, event.RunID, workspaceID).Scan(&one) == nil
}

func eventEnvelope(workspaceID string, event db.Event) liveEventEnvelope {
	metadata := map[string]any{}
	if json.Unmarshal([]byte(event.PayloadJSON), &metadata) != nil {
		metadata = map[string]any{"text": redactText(event.PayloadJSON)}
	}
	if redacted, ok := redactJSON(metadata).(map[string]any); ok {
		metadata = redacted
	}
	entityType := strings.SplitN(event.Type, ".", 2)[0]
	entityID := ""
	for _, key := range []string{"proposal_id", "claim_id", "vote_id", "approval_id", "transaction_id", "policy_id", "session_id"} {
		if value, ok := metadata[key].(string); ok {
			entityID = value
			break
		}
	}
	return liveEventEnvelope{EventID: event.ID, WorkspaceID: workspaceID, Sequence: event.ID, Type: event.Type, OccurredAt: event.CreatedAt, Actor: redactText(event.ActorID), EntityType: entityType, EntityID: redactText(entityID), Payload: metadata}
}
