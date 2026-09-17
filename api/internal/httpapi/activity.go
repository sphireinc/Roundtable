package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
)

type activityItem struct {
	ID         int64          `json:"id"`
	Type       string         `json:"type"`
	Category   string         `json:"category"`
	ActorID    string         `json:"actor_id,omitempty"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id,omitempty"`
	Severity   string         `json:"severity"`
	Title      string         `json:"title"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  string         `json:"created_at"`
}

func (s *Server) activityFeedAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	entity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entity")))
	category := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT e.id,e.type,COALESCE(e.actor_id,''),e.payload_json,e.created_at FROM events e WHERE (e.run_id=? OR EXISTS(SELECT 1 FROM runs rr WHERE rr.id=e.run_id AND rr.workspace_id=?)) ORDER BY e.id DESC`, workspace.ID, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "activity_list_failed", "Unable to list activity", err.Error())
		return
	}
	defer rows.Close()
	items := make([]activityItem, 0)
	for rows.Next() {
		var id int64
		var typ, actorID, payload, created string
		if err := rows.Scan(&id, &typ, &actorID, &payload, &created); err != nil {
			WriteProblem(w, r, 500, "activity_list_failed", "Unable to list activity", err.Error())
			return
		}
		item := normalizeActivity(id, typ, actorID, payload, created)
		if entity != "" && item.EntityType != entity {
			continue
		}
		if category != "" && item.Category != category {
			continue
		}
		if actor != "" && item.ActorID != actor {
			continue
		}
		if severity != "" && item.Severity != severity {
			continue
		}
		items = append(items, item)
	}
	writePage(w, r, items)
}

func normalizeActivity(id int64, typ, actor, payload, created string) activityItem {
	category := typ
	if dot := strings.IndexByte(category, '.'); dot >= 0 {
		category = category[:dot]
	}
	entity := category
	metadata := map[string]any{}
	var raw any
	if json.Unmarshal([]byte(payload), &raw) == nil {
		if value, ok := redactJSON(raw).(map[string]any); ok {
			metadata = value
		}
	}
	entityID := ""
	for _, key := range []string{"proposal_id", "claim_id", "approval_id", "transaction_id", "policy_id", "session_id", "contention_id", "vote_id"} {
		if value, ok := metadata[key].(string); ok {
			entityID = value
			break
		}
	}
	severity := "info"
	if value, ok := metadata["severity"].(string); ok {
		severity = strings.ToLower(value)
	}
	return activityItem{ID: id, Type: typ, Category: category, ActorID: redactText(actor), EntityType: entity, EntityID: redactText(entityID), Severity: severity, Title: activityTitle(typ), Metadata: metadata, CreatedAt: created}
}
func activityTitle(typ string) string {
	titles := map[string]string{"proposal.created": "Proposal created", "claim.acquired": "Resource claim acquired", "vote.cast": "Vote recorded", "policy.passed": "Policy passed", "approval.required": "Human approval required", "transaction.committed": "Transaction committed", "session.resumed": "Session resumed", "memory.updated": "Memory updated"}
	if title := titles[typ]; title != "" {
		return title
	}
	return strings.ReplaceAll(strings.Title(strings.ReplaceAll(typ, ".", " ")), "_", " ")
}
