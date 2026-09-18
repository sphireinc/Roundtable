package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type memoryRevisionResponse struct {
	ID         string         `json:"id"`
	MemoryID   string         `json:"memory_id"`
	Revision   int            `json:"revision"`
	BodyMD     string         `json:"body_md"`
	Provenance map[string]any `json:"provenance"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  string         `json:"created_at"`
}

func (s *Server) listMemoryRevisionsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	id := r.PathValue("memory_id")
	if _, err := s.readMemory(r, workspace.ID, id); err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT mr.id,mr.memory_id,mr.revision,mr.body_md,mr.provenance_json,mr.created_by,mr.created_at FROM memory_revisions mr WHERE mr.memory_id=? ORDER BY mr.revision`, id)
	if err != nil {
		WriteProblem(w, r, 500, "memory_revision_list_failed", "Unable to list revisions", err.Error())
		return
	}
	defer rows.Close()
	items := []memoryRevisionResponse{}
	for rows.Next() {
		var item memoryRevisionResponse
		var provenance string
		if err := rows.Scan(&item.ID, &item.MemoryID, &item.Revision, &item.BodyMD, &provenance, &item.CreatedBy, &item.CreatedAt); err != nil {
			WriteProblem(w, r, 500, "memory_revision_list_failed", "Unable to list revisions", err.Error())
			return
		}
		item.BodyMD = redactText(item.BodyMD)
		item.Provenance = map[string]any{}
		_ = jsonUnmarshalRedacted(provenance, &item.Provenance)
		items = append(items, item)
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) memoryRevisionDiffAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	id := r.PathValue("memory_id")
	if _, err = s.readMemory(r, workspace.ID, id); err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	if from == "" {
		from = "1"
	}
	if to == "" {
		to = "latest"
	}
	var current string
	err = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT body_md FROM memory_entries WHERE id=?`, id).Scan(&current)
	if err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"memory_id": id, "from_revision": from, "to_revision": to, "changed": true, "current_body_md": redactText(current)})
}

func (s *Server) memoryGraphAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	id := r.PathValue("memory_id")
	if _, err = s.readMemory(r, workspace.ID, id); err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT alias_id,canonical_id,reason,created_by,created_at FROM memory_aliases WHERE alias_id=? OR canonical_id=? ORDER BY created_at`, id, id)
	if err != nil {
		WriteProblem(w, r, 500, "memory_graph_failed", "Unable to read memory graph", err.Error())
		return
	}
	defer rows.Close()
	aliases := []map[string]any{}
	for rows.Next() {
		var alias, canonical, reason, createdBy, createdAt string
		if err := rows.Scan(&alias, &canonical, &reason, &createdBy, &createdAt); err != nil {
			WriteProblem(w, r, 500, "memory_graph_failed", "Unable to read memory graph", err.Error())
			return
		}
		aliases = append(aliases, map[string]any{"alias_id": alias, "canonical_id": canonical, "reason": redactText(reason), "created_by": redactText(createdBy), "created_at": createdAt})
	}
	writeJSON(w, 200, map[string]any{"memory_id": id, "aliases": aliases})
}

func (s *Server) memorySourceChainAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	id := r.PathValue("memory_id")
	if _, err = s.readMemory(r, workspace.ID, id); err != nil {
		WriteProblem(w, r, 404, "memory_not_found", "Memory not found", err.Error())
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT source_type,source_id,relation,created_at FROM memory_provenance_edges WHERE memory_id=? ORDER BY created_at`, id)
	if err != nil {
		WriteProblem(w, r, 500, "memory_source_chain_failed", "Unable to read source chain", err.Error())
		return
	}
	defer rows.Close()
	edges := []map[string]any{}
	for rows.Next() {
		var sourceType, sourceID, relation, createdAt string
		if err := rows.Scan(&sourceType, &sourceID, &relation, &createdAt); err != nil {
			WriteProblem(w, r, 500, "memory_source_chain_failed", "Unable to read source chain", err.Error())
			return
		}
		edges = append(edges, map[string]any{"source_type": sourceType, "source_id": redactText(sourceID), "relation": relation, "created_at": createdAt})
	}
	writeJSON(w, 200, map[string]any{"memory_id": id, "edges": edges})
}

func jsonUnmarshalRedacted(value string, target *map[string]any) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var raw any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return err
	}
	if redacted, ok := redactJSON(raw).(map[string]any); ok {
		*target = redacted
	}
	return nil
}
