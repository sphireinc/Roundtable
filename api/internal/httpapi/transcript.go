package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type transcriptEntry struct {
	ID                 string         `json:"id"`
	Round              int            `json:"round"`
	Actor              string         `json:"actor"`
	Kind               string         `json:"kind"`
	MessageType        string         `json:"message_type,omitempty"`
	Summary            string         `json:"summary,omitempty"`
	Content            string         `json:"content,omitempty"`
	ToolReferences     []string       `json:"tool_references,omitempty"`
	ClaimReferences    []string       `json:"claim_references,omitempty"`
	ProposalReferences []string       `json:"proposal_references,omitempty"`
	VoteReferences     []string       `json:"vote_references,omitempty"`
	VisibilityClass    string         `json:"visibility_class"`
	Rendering          map[string]any `json:"rendering"`
	CreatedAt          string         `json:"created_at"`
}

func (s *Server) listTranscript(w http.ResponseWriter, r *http.Request) {
	entries, err := s.transcriptForRequest(r)
	if err != nil {
		writeTranscriptError(w, r, err)
		return
	}
	writePage(w, r, entries)
}

func (s *Server) getTranscriptEntry(w http.ResponseWriter, r *http.Request) {
	entries, err := s.transcriptForRequest(r)
	if err != nil {
		writeTranscriptError(w, r, err)
		return
	}
	wanted := r.PathValue("entry_id")
	for _, entry := range entries {
		if entry.ID == wanted || strings.TrimPrefix(entry.ID, "message:") == wanted || strings.TrimPrefix(entry.ID, "event:") == wanted {
			writeJSON(w, 200, entry)
			return
		}
	}
	WriteProblem(w, r, 404, "transcript_entry_not_found", "Transcript entry not found", "entry does not belong to this deliberation")
}

func (s *Server) transcriptForRequest(r *http.Request) ([]transcriptEntry, error) {
	workspace, err := s.workspace(r)
	if err != nil {
		return nil, err
	}
	value, err := s.readDeliberation(r, workspace.ID, r.PathValue("deliberation_id"))
	if err != nil {
		return nil, fmt.Errorf("deliberation not found: %w", err)
	}
	var entries []transcriptEntry
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id, actor, message_type, body, created_at FROM deliberation_messages WHERE deliberation_id = ? ORDER BY sequence, id`, value.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, actor, typ, body, created string
		if err := rows.Scan(&id, &actor, &typ, &body, &created); err != nil {
			rows.Close()
			return nil, err
		}
		lower := strings.ToLower(typ)
		if strings.Contains(lower, "chain_of_thought") || strings.Contains(lower, "hidden_reasoning") {
			continue
		}
		entries = append(entries, transcriptEntry{ID: "message:" + id, Round: value.Round, Actor: redactText(actor), Kind: "message", MessageType: redactText(typ), Content: redactText(body), VisibilityClass: "user_visible", Rendering: safeRendering(), CreatedAt: created})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	events, err := s.config.Store.ListEvents(r.Context(), value.ID)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		entry := transcriptEntry{ID: fmt.Sprintf("event:%d", event.ID), Round: value.Round, Actor: redactText(event.ActorID), Kind: "event", Summary: redactText(event.Type), VisibilityClass: "operational", Rendering: safeRendering(), CreatedAt: event.CreatedAt}
		refs := referencesFromPayload(event.PayloadJSON)
		entry.ToolReferences, entry.ClaimReferences, entry.ProposalReferences, entry.VoteReferences = refs["tool"], refs["claim"], refs["proposal"], refs["vote"]
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].CreatedAt == entries[j].CreatedAt {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].CreatedAt < entries[j].CreatedAt
	})
	return entries, nil
}

func safeRendering() map[string]any {
	return map[string]any{"format": "markdown", "html_allowed": false, "links": "untrusted"}
}
func referencesFromPayload(raw string) map[string][]string {
	out := map[string][]string{}
	var payload map[string]any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return out
	}
	for _, key := range []string{"tool", "claim", "proposal", "vote"} {
		if value, ok := payload[key].(string); ok && value != "" {
			out[key] = []string{redactText(value)}
		}
		if value, ok := payload[key+"_id"].(string); ok && value != "" {
			out[key] = []string{redactText(value)}
		}
	}
	return out
}
func writeTranscriptError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		WriteProblem(w, r, 404, "deliberation_not_found", "Deliberation not found", err.Error())
		return
	}
	WriteProblem(w, r, 500, "transcript_failed", "Unable to load transcript", err.Error())
}
