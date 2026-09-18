package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"roundtable/internal/db"
)

type memoryResponse struct {
	ID                string         `json:"id"`
	WorkspaceID       string         `json:"workspace_id,omitempty"`
	Scope             string         `json:"scope"`
	Kind              string         `json:"kind"`
	Title             string         `json:"title"`
	BodyMD            string         `json:"body_md"`
	Tags              []string       `json:"tags"`
	Provenance        map[string]any `json:"provenance"`
	SourceEventID     int64          `json:"source_event_id,omitempty"`
	SourceSessionID   string         `json:"source_session_id,omitempty"`
	SourceProposalID  string         `json:"source_proposal_id,omitempty"`
	Confidence        float64        `json:"confidence"`
	Reliability       float64        `json:"reliability"`
	Importance        int            `json:"importance"`
	Pinned            bool           `json:"pinned"`
	Revision          int            `json:"revision"`
	Status            string         `json:"status"`
	ResynthesisStatus string         `json:"resynthesis_status"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type memoryInput struct {
	Scope            string         `json:"scope"`
	Kind             string         `json:"kind"`
	Title            string         `json:"title"`
	BodyMD           string         `json:"body_md"`
	Tags             []string       `json:"tags"`
	Provenance       map[string]any `json:"provenance"`
	SourceEventID    int64          `json:"source_event_id"`
	SourceSessionID  string         `json:"source_session_id"`
	SourceProposalID string         `json:"source_proposal_id"`
	Confidence       *float64       `json:"confidence"`
	Reliability      *float64       `json:"reliability"`
	Importance       int            `json:"importance"`
}

func (s *Server) listMemoriesAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
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
	where := []string{"(workspace_id=? OR workspace_id IS NULL)", "status != 'archived'"}
	args := []any{workspace.ID}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		where = append(where, "(title LIKE ? OR body_md LIKE ?)")
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	for _, key := range []string{"scope", "kind", "status"} {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			where = append(where, key+"=?")
			args = append(args, value)
		}
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		created, id, ok := decodeMemoryCursor(cursor)
		if !ok {
			WriteProblem(w, r, 400, "invalid_cursor", "Invalid memory cursor", "cursor is malformed")
			return
		}
		where = append(where, "(created_at > ? OR (created_at = ? AND id > ?))")
		args = append(args, created, created, id)
	}
	query := `SELECT id,COALESCE(workspace_id,''),scope,kind,title,body_md,COALESCE(tags_json,'[]'),COALESCE(provenance_json,'{}'),COALESCE(source_event_id,0),COALESCE(source_session_id,''),COALESCE(source_proposal_id,''),confidence,reliability,importance,pinned,revision,status,resynthesis_status,created_at,updated_at FROM memory_entries WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at,id LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.config.Store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		WriteProblem(w, r, 500, "memory_list_failed", "Unable to list memories", err.Error())
		return
	}
	defer rows.Close()
	items := make([]memoryResponse, 0, limit)
	for rows.Next() {
		item, scanErr := scanMemory(rows)
		if scanErr != nil {
			WriteProblem(w, r, 500, "memory_list_failed", "Unable to list memories", scanErr.Error())
			return
		}
		if len(items) < limit {
			items = append(items, item)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "next_cursor": encodeMemoryCursor(items[len(items)-1].CreatedAt, items[len(items)-1].ID)})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

func (s *Server) getMemoryAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	item, err := s.readMemory(r, workspace.ID, r.PathValue("memory_id"))
	if err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) createMemoryAPI(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input memoryInput
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.BodyMD) == "" {
		WriteProblem(w, r, 400, "invalid_memory", "Invalid memory", "title and body_md are required")
		return
	}
	id := "M-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	tags, _ := json.Marshal(input.Tags)
	provenance, _ := json.Marshal(input.Provenance)
	confidence, reliability := 0.5, 0.5
	if input.Confidence != nil {
		confidence = *input.Confidence
	}
	if input.Reliability != nil {
		reliability = *input.Reliability
	}
	_, err = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO memory_entries(id,workspace_id,scope,kind,title,body_md,tags_json,provenance_json,source_event_id,source_session_id,source_proposal_id,confidence,reliability,importance,status,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, id, workspace.ID, input.Scope, input.Kind, input.Title, redactText(input.BodyMD), string(tags), string(provenance), zeroInt64ToNull(input.SourceEventID), nullIfEmpty(input.SourceSessionID), nullIfEmpty(input.SourceProposalID), confidence, reliability, input.Importance, "active")
	if err != nil {
		WriteProblem(w, r, 409, "memory_create_conflict", "Unable to create memory", err.Error())
		return
	}
	s.memoryAudit(r, workspace.ID, r.Header.Get("X-Actor-ID"), "memory.created", id)
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "memory.created", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"memory_id":%q}`, id)})
	item, _ := s.readMemory(r, workspace.ID, id)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(item)
}

func (s *Server) memoryActionAPI(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	id := r.PathValue("memory_id")
	action := r.PathValue("action")
	if action == "resynthesize" {
		if _, err := s.config.Store.DB().ExecContext(r.Context(), `UPDATE memory_entries SET resynthesis_status='requested', updated_at=CURRENT_TIMESTAMP WHERE id=? AND (workspace_id=? OR workspace_id IS NULL)`, id, workspace.ID); err != nil {
			WriteProblem(w, r, 409, "memory_resynthesis_conflict", "Unable to request resynthesis", err.Error())
			return
		}
		s.memoryAudit(r, workspace.ID, r.Header.Get("X-Actor-ID"), "memory.resynthesis_requested", id)
		_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "memory.resynthesis_requested", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"memory_id":%q}`, id)})
		item, err := s.readMemory(r, workspace.ID, id)
		if err != nil {
			WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
			return
		}
		writeJSON(w, 200, item)
		return
	}
	if action == "merge" {
		target := strings.TrimSpace(r.URL.Query().Get("target_id"))
		if target == "" || target == id {
			WriteProblem(w, r, 400, "invalid_memory_merge", "Invalid memory merge", "target_id must identify a different memory")
			return
		}
		tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
		if err != nil {
			WriteProblem(w, r, 500, "memory_merge_failed", "Unable to merge memory", err.Error())
			return
		}
		defer tx.Rollback()
		var exists int
		if err := tx.QueryRowContext(r.Context(), `SELECT 1 FROM memory_entries WHERE id=? AND (workspace_id=? OR workspace_id IS NULL)`, target, workspace.ID).Scan(&exists); err != nil {
			WriteProblem(w, r, 404, "memory_target_not_found", "Merge target not found", err.Error())
			return
		}
		if _, err := tx.ExecContext(r.Context(), `UPDATE memory_entries SET status='merged', provenance_json=json_set(COALESCE(provenance_json,'{}'),'$.merged_into',?), updated_at=CURRENT_TIMESTAMP WHERE id=? AND (workspace_id=? OR workspace_id IS NULL)`, target, id, workspace.ID); err != nil {
			WriteProblem(w, r, 409, "memory_merge_conflict", "Unable to merge memory", err.Error())
			return
		}
		if err := tx.Commit(); err != nil {
			WriteProblem(w, r, 409, "memory_merge_conflict", "Unable to commit memory merge", err.Error())
			return
		}
		s.memoryAudit(r, workspace.ID, r.Header.Get("X-Actor-ID"), "memory.merged", id)
		_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "memory.merged", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"memory_id":%q,"target_id":%q}`, id, target)})
		item, err := s.readMemory(r, workspace.ID, id)
		if err != nil {
			WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
			return
		}
		writeJSON(w, 200, item)
		return
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "memory_action_failed", "Unable to update memory", err.Error())
		return
	}
	defer tx.Rollback()
	var status string
	var pinned int
	if err := tx.QueryRowContext(r.Context(), `SELECT status,pinned FROM memory_entries WHERE id=? AND (workspace_id=? OR workspace_id IS NULL)`, id, workspace.ID).Scan(&status, &pinned); err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	switch action {
	case "pin":
		pinned = 1
	case "unpin":
		pinned = 0
	case "archive":
		status = "archived"
	case "restore":
		status = "active"
	default:
		WriteProblem(w, r, 400, "invalid_memory_action", "Invalid memory action", action)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE memory_entries SET status=?,pinned=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, pinned, id); err != nil {
		WriteProblem(w, r, 409, "memory_update_conflict", "Unable to update memory", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "memory_update_conflict", "Unable to update memory", err.Error())
		return
	}
	s.memoryAudit(r, workspace.ID, r.Header.Get("X-Actor-ID"), "memory."+action, id)
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "memory." + action, ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"memory_id":%q}`, id)})
	item, err := s.readMemory(r, workspace.ID, id)
	if err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) createMemoryRevisionAPI(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input struct {
		BodyMD string `json:"body_md"`
		Title  string `json:"title"`
		Reason string `json:"reason"`
	}
	if err = decodeJSON(r, &input); err != nil || strings.TrimSpace(input.BodyMD) == "" {
		WriteProblem(w, r, 400, "invalid_revision", "Invalid memory revision", "body_md is required")
		return
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "memory_revision_failed", "Unable to create revision", err.Error())
		return
	}
	defer tx.Rollback()
	var revision int
	var title string
	if err = tx.QueryRowContext(r.Context(), `SELECT revision,title FROM memory_entries WHERE id=? AND (workspace_id=? OR workspace_id IS NULL)`, r.PathValue("memory_id"), workspace.ID).Scan(&revision, &title); err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	if input.Title != "" {
		title = input.Title
	}
	revision++
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO memory_revisions(id,memory_id,revision,body_md,provenance_json,created_by) VALUES(?,?,?,?,?,?)`, fmt.Sprintf("%s-r%d", r.PathValue("memory_id"), revision), r.PathValue("memory_id"), revision, redactText(input.BodyMD), `{"reason":"revision"}`, r.Header.Get("X-Actor-ID")); err != nil {
		WriteProblem(w, r, 409, "memory_revision_conflict", "Unable to create revision", err.Error())
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE memory_entries SET body_md=?,title=?,revision=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, redactText(input.BodyMD), title, revision, r.PathValue("memory_id")); err != nil {
		WriteProblem(w, r, 409, "memory_revision_conflict", "Unable to update memory", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "memory_revision_conflict", "Unable to commit revision", err.Error())
		return
	}
	s.memoryAudit(r, workspace.ID, r.Header.Get("X-Actor-ID"), "memory.revised", r.PathValue("memory_id"))
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "memory.revised", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: fmt.Sprintf(`{"memory_id":%q,"revision":%d}`, r.PathValue("memory_id"), revision)})
	item, _ := s.readMemory(r, workspace.ID, r.PathValue("memory_id"))
	writeJSON(w, 200, item)
}

func (s *Server) memoryAudit(r *http.Request, workspaceID, actor, action, id string) {
	payload, _ := json.Marshal(map[string]string{"memory_id": id})
	_, _ = s.config.Store.DB().ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?,?,'memory',?,?,?)`, workspaceID, actor, action, id, requestID(r.Context()), string(payload))
}

func (s *Server) readMemory(r *http.Request, workspaceID, id string) (memoryResponse, error) {
	row := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT id,COALESCE(workspace_id,''),scope,kind,title,body_md,COALESCE(tags_json,'[]'),COALESCE(provenance_json,'{}'),COALESCE(source_event_id,0),COALESCE(source_session_id,''),COALESCE(source_proposal_id,''),confidence,reliability,importance,pinned,revision,status,resynthesis_status,created_at,updated_at FROM memory_entries WHERE id=? AND (workspace_id=? OR workspace_id IS NULL)`, id, workspaceID)
	return scanMemory(row)
}

type memoryScanner interface{ Scan(...any) error }

func scanMemory(row memoryScanner) (memoryResponse, error) {
	var v memoryResponse
	var tags, provenance string
	var pinned int
	if err := row.Scan(&v.ID, &v.WorkspaceID, &v.Scope, &v.Kind, &v.Title, &v.BodyMD, &tags, &provenance, &v.SourceEventID, &v.SourceSessionID, &v.SourceProposalID, &v.Confidence, &v.Reliability, &v.Importance, &pinned, &v.Revision, &v.Status, &v.ResynthesisStatus, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return v, err
	}
	_ = json.Unmarshal([]byte(tags), &v.Tags)
	_ = json.Unmarshal([]byte(provenance), &v.Provenance)
	if v.Tags == nil {
		v.Tags = []string{}
	}
	if v.Provenance == nil {
		v.Provenance = map[string]any{}
	}
	v.BodyMD = redactText(v.BodyMD)
	v.Title = redactText(v.Title)
	v.Pinned = pinned != 0
	return v, nil
}
func encodeMemoryCursor(created, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(created + "\n" + id))
}
func decodeMemoryCursor(value string) (string, string, bool) {
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
