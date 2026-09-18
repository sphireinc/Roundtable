package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type operationalLogResponse struct {
	ID            int64          `json:"id"`
	Type          string         `json:"type"`
	Component     string         `json:"component"`
	Severity      string         `json:"severity"`
	ActorID       string         `json:"actor_id,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	ProposalID    string         `json:"proposal_id,omitempty"`
	TransactionID string         `json:"transaction_id,omitempty"`
	Payload       map[string]any `json:"payload"`
	CreatedAt     string         `json:"created_at"`
}
type auditResponse struct {
	ID         int64          `json:"id"`
	ActorID    string         `json:"actor_id"`
	Action     string         `json:"action"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	RequestID  string         `json:"request_id,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Payload    map[string]any `json:"payload"`
	CreatedAt  string         `json:"created_at"`
}

func (s *Server) listOperationalLogsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	limit := parseLogLimit(w, r)
	if limit == 0 {
		return
	}
	where, args := eventFilter(r, workspace.ID)
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		value, e := strconv.ParseInt(cursor, 10, 64)
		if e != nil || value < 1 {
			WriteProblem(w, r, 400, "invalid_cursor", "Invalid cursor", "cursor must be a positive event id")
			return
		}
		where = append(where, "e.id < ?")
		args = append(args, value)
	}
	query := `SELECT e.id,e.type,COALESCE(e.actor_id,''),e.payload_json,e.created_at FROM events e WHERE ` + strings.Join(where, " AND ") + ` ORDER BY e.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.config.Store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		WriteProblem(w, r, 500, "operational_log_list_failed", "Unable to list operational logs", err.Error())
		return
	}
	defer rows.Close()
	items := []operationalLogResponse{}
	for rows.Next() {
		var id int64
		var typ, actor, payload, created string
		if err := rows.Scan(&id, &typ, &actor, &payload, &created); err != nil {
			WriteProblem(w, r, 500, "operational_log_list_failed", "Unable to list operational logs", err.Error())
			return
		}
		if len(items) >= limit {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": strconv.FormatInt(items[len(items)-1].ID, 10)})
			return
		}
		items = append(items, normalizeOperationalLog(id, typ, actor, payload, created))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

func (s *Server) listAuditAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	limit := parseLogLimit(w, r)
	if limit == 0 {
		return
	}
	where, args := auditFilter(r, workspace.ID)
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		value, e := strconv.ParseInt(cursor, 10, 64)
		if e != nil || value < 1 {
			WriteProblem(w, r, 400, "invalid_cursor", "Invalid cursor", "cursor must be a positive audit id")
			return
		}
		where = append(where, "a.id < ?")
		args = append(args, value)
	}
	query := `SELECT a.id,a.actor_id,a.action,a.entity_type,a.entity_id,COALESCE(a.request_id,''),COALESCE(a.reason,''),a.payload_json,a.created_at FROM audit_events a WHERE ` + strings.Join(where, " AND ") + ` ORDER BY a.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.config.Store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		WriteProblem(w, r, 500, "audit_list_failed", "Unable to list audit events", err.Error())
		return
	}
	defer rows.Close()
	items := []auditResponse{}
	for rows.Next() {
		var item auditResponse
		var payload string
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.EntityType, &item.EntityID, &item.RequestID, &item.Reason, &payload, &item.CreatedAt); err != nil {
			WriteProblem(w, r, 500, "audit_list_failed", "Unable to list audit events", err.Error())
			return
		}
		item.Payload = redactedMap(payload)
		if len(items) >= limit {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": strconv.FormatInt(items[len(items)-1].ID, 10)})
			return
		}
		item.ActorID = redactText(item.ActorID)
		item.EntityID = redactText(item.EntityID)
		item.Reason = redactText(item.Reason)
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

func (s *Server) exportAuditAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	where, args := auditFilter(r, workspace.ID)
	query := `SELECT a.id,a.actor_id,a.action,a.entity_type,a.entity_id,COALESCE(a.request_id,''),COALESCE(a.reason,''),a.payload_json,a.created_at FROM audit_events a WHERE ` + strings.Join(where, " AND ") + ` ORDER BY a.id DESC LIMIT 1000`
	rows, err := s.config.Store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		WriteProblem(w, r, 500, "audit_export_failed", "Unable to export audit events", err.Error())
		return
	}
	defer rows.Close()
	items := []auditResponse{}
	for rows.Next() {
		var item auditResponse
		var payload string
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.EntityType, &item.EntityID, &item.RequestID, &item.Reason, &payload, &item.CreatedAt); err != nil {
			WriteProblem(w, r, 500, "audit_export_failed", "Unable to export audit events", err.Error())
			return
		}
		item.Payload = redactedMap(payload)
		item.ActorID = redactText(item.ActorID)
		item.EntityID = redactText(item.EntityID)
		item.Reason = redactText(item.Reason)
		items = append(items, item)
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"id", "actor_id", "action", "entity_type", "entity_id", "request_id", "reason", "created_at"})
		for _, item := range items {
			_ = writer.Write([]string{strconv.FormatInt(item.ID, 10), item.ActorID, item.Action, item.EntityType, item.EntityID, item.RequestID, item.Reason, item.CreatedAt})
		}
		writer.Flush()
	} else if format == "ndjson" {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for _, item := range items {
			b, _ := json.Marshal(item)
			_, _ = fmt.Fprintln(w, string(b))
		}
	} else {
		writeJSON(w, 200, map[string]any{"items": items, "bounded": true})
	}
	payload, _ := json.Marshal(map[string]any{"format": format, "count": len(items)})
	_, _ = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?,?,'workspace',?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), "audit.exported", workspace.ID, requestID(r.Context()), string(payload))
}

func parseLogLimit(w http.ResponseWriter, r *http.Request) int {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			WriteProblem(w, r, 400, "invalid_limit", "Invalid limit", "limit must be between 1 and 200")
			return 0
		}
	}
	return limit
}
func eventFilter(r *http.Request, workspaceID string) ([]string, []any) {
	where := []string{"(e.run_id=? OR EXISTS(SELECT 1 FROM runs rr WHERE rr.id=e.run_id AND rr.workspace_id=?))"}
	args := []any{workspaceID, workspaceID}
	return appendLogFilters(where, args, r, "e.payload_json", "e.type")
}
func auditFilter(r *http.Request, workspaceID string) ([]string, []any) {
	where := []string{"a.workspace_id=?"}
	args := []any{workspaceID}
	if value := r.URL.Query().Get("actor"); value != "" {
		where = append(where, "a.actor_id=?")
		args = append(args, value)
	}
	if value := r.URL.Query().Get("action"); value != "" {
		where = append(where, "a.action=?")
		args = append(args, value)
	}
	if value := r.URL.Query().Get("request_id"); value != "" {
		where = append(where, "a.request_id=?")
		args = append(args, value)
	}
	if value := r.URL.Query().Get("entity_id"); value != "" {
		where = append(where, "a.entity_id=?")
		args = append(args, value)
	}
	return appendTimeFilters(where, args, r, "a.created_at")
}
func appendLogFilters(where []string, args []any, r *http.Request, payload, typeColumn string) ([]string, []any) {
	if value := r.URL.Query().Get("component"); value != "" {
		where = append(where, typeColumn+" LIKE ?")
		args = append(args, value+".%")
	}
	if value := r.URL.Query().Get("agent_id"); value != "" {
		where = append(where, payload+" LIKE ?")
		args = append(args, "%\"agent_id\":\""+value+"%")
	}
	for _, key := range []string{"session_id", "proposal_id", "transaction_id", "request_id", "severity"} {
		if value := r.URL.Query().Get(key); value != "" {
			where = append(where, payload+" LIKE ?")
			args = append(args, "%\""+key+"\":\""+value+"%")
		}
	}
	return appendTimeFilters(where, args, r, "e.created_at")
}
func appendTimeFilters(where []string, args []any, r *http.Request, column string) ([]string, []any) {
	for _, pair := range []struct{ key, op string }{{"from", ">="}, {"to", "<="}} {
		if value := r.URL.Query().Get(pair.key); value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				continue
			}
			where = append(where, column+" "+pair.op+" ?")
			args = append(args, value)
		}
	}
	return where, args
}
func normalizeOperationalLog(id int64, typ, actor, payload, created string) operationalLogResponse {
	data := redactedMap(payload)
	component := typ
	if i := strings.IndexByte(component, '.'); i >= 0 {
		component = component[:i]
	}
	get := func(key string) string {
		if value, ok := data[key].(string); ok {
			return redactText(value)
		}
		return ""
	}
	severity := get("severity")
	if severity == "" {
		severity = "info"
	}
	return operationalLogResponse{ID: id, Type: typ, Component: component, Severity: severity, ActorID: redactText(actor), RequestID: get("request_id"), AgentID: get("agent_id"), SessionID: get("session_id"), ProposalID: get("proposal_id"), TransactionID: get("transaction_id"), Payload: data, CreatedAt: created}
}
func redactedMap(payload string) map[string]any {
	var raw any
	if json.Unmarshal([]byte(payload), &raw) == nil {
		if value, ok := redactJSON(raw).(map[string]any); ok {
			return value
		}
	}
	return map[string]any{"text": redactText(payload)}
}
