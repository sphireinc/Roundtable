package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"roundtable/internal/db"
)

type notificationResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	RecipientID string `json:"recipient_id"`
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	EntityType  string `json:"entity_type,omitempty"`
	EntityID    string `json:"entity_id,omitempty"`
	ReadAt      string `json:"read_at,omitempty"`
	Actionable  bool   `json:"actionable"`
	ResolvedAt  string `json:"resolved_at,omitempty"`
	CreatedAt   string `json:"created_at"`
}

func (s *Server) listNotificationsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	recipient := strings.TrimSpace(r.URL.Query().Get("recipient_id"))
	if recipient == "" {
		recipient = strings.TrimSpace(r.Header.Get("X-Actor-ID"))
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			WriteProblem(w, r, 400, "invalid_limit", "Invalid limit", "limit must be between 1 and 200")
			return
		}
	}
	where := []string{"workspace_id=?"}
	args := []any{workspace.ID}
	if recipient != "" {
		where = append(where, "recipient_id=?")
		args = append(args, recipient)
	}
	for _, key := range []string{"category", "severity"} {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			where = append(where, key+"=?")
			args = append(args, value)
		}
	}
	if r.URL.Query().Get("unread") == "true" {
		where = append(where, "read_at IS NULL")
	}
	if r.URL.Query().Get("actionable") == "true" {
		where = append(where, "actionable=1 AND resolved_at IS NULL")
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		created, id, ok := decodeNotificationCursor(cursor)
		if !ok {
			WriteProblem(w, r, 400, "invalid_cursor", "Invalid notification cursor", "cursor is malformed")
			return
		}
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, created, created, id)
	}
	query := `SELECT id,workspace_id,recipient_id,severity,category,title,body,COALESCE(entity_type,''),COALESCE(entity_id,''),COALESCE(read_at,''),actionable,COALESCE(resolved_at,''),created_at FROM notifications WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.config.Store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		WriteProblem(w, r, 500, "notification_list_failed", "Unable to list notifications", err.Error())
		return
	}
	defer rows.Close()
	items := []notificationResponse{}
	for rows.Next() {
		item, scanErr := scanNotification(rows)
		if scanErr != nil {
			WriteProblem(w, r, 500, "notification_list_failed", "Unable to list notifications", scanErr.Error())
			return
		}
		if len(items) < limit {
			items = append(items, item)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": encodeNotificationCursor(items[len(items)-1].CreatedAt, items[len(items)-1].ID)})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

func (s *Server) notificationCountsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	recipient := strings.TrimSpace(r.URL.Query().Get("recipient_id"))
	if recipient == "" {
		recipient = strings.TrimSpace(r.Header.Get("X-Actor-ID"))
	}
	where := "workspace_id=?"
	args := []any{workspace.ID}
	if recipient != "" {
		where += " AND recipient_id=?"
		args = append(args, recipient)
	}
	var unread, actionable int
	if err := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*),COALESCE(SUM(CASE WHEN actionable=1 AND resolved_at IS NULL THEN 1 ELSE 0 END),0) FROM notifications WHERE `+where+` AND read_at IS NULL`, args...).Scan(&unread, &actionable); err != nil {
		WriteProblem(w, r, 500, "notification_counts_failed", "Unable to count notifications", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"workspace_id": workspace.ID, "unread": unread, "actionable": actionable})
}

func (s *Server) acknowledgeNotificationAPI(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	actor := r.Header.Get("X-Actor-ID")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "notification_ack_failed", "Unable to acknowledge notification", err.Error())
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE notifications SET read_at=COALESCE(read_at,?) WHERE id=? AND workspace_id=? AND (recipient_id=? OR recipient_id='*')`, now, r.PathValue("notification_id"), workspace.ID, actor)
	if err != nil {
		WriteProblem(w, r, 409, "notification_ack_failed", "Unable to acknowledge notification", err.Error())
		return
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		WriteProblem(w, r, 404, "notification_not_found", "Notification not found", "notification does not belong to this workspace or actor")
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "notification_ack_failed", "Unable to commit acknowledgement", err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]string{"notification_id": r.PathValue("notification_id")})
	_, _ = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?,?,'notification',?,?,?)`, workspace.ID, actor, "notification.acknowledged", r.PathValue("notification_id"), requestID(r.Context()), string(payload))
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "notification.acknowledged", ActorID: actor, PayloadJSON: string(payload)})
	itemErr := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id,workspace_id,recipient_id,severity,category,title,body,COALESCE(entity_type,''),COALESCE(entity_id,''),COALESCE(read_at,''),actionable,COALESCE(resolved_at,''),created_at FROM notifications WHERE id=?`, r.PathValue("notification_id"))
	item, scanErr := scanNotification(itemErr)
	if scanErr != nil {
		WriteProblem(w, r, 404, "notification_not_found", "Notification not found", scanErr.Error())
		return
	}
	writeJSON(w, 200, item)
}

type notificationScanner interface{ Scan(...any) error }

func scanNotification(row notificationScanner) (notificationResponse, error) {
	var item notificationResponse
	var actionable int
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.RecipientID, &item.Severity, &item.Category, &item.Title, &item.Body, &item.EntityType, &item.EntityID, &item.ReadAt, &actionable, &item.ResolvedAt, &item.CreatedAt); err != nil {
		return item, err
	}
	item.Title = redactText(item.Title)
	item.Body = redactText(item.Body)
	item.RecipientID = redactText(item.RecipientID)
	item.Actionable = actionable != 0
	return item, nil
}
func encodeNotificationCursor(created, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(created + "\n" + id))
}
func decodeNotificationCursor(value string) (string, string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(raw), "\n", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
