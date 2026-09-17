package httpapi

import (
	"net/http"
	"time"
)

type eventSnapshotResponse struct {
	WorkspaceID    string                        `json:"workspace_id"`
	Sequence       int64                         `json:"sequence"`
	GeneratedAt    string                        `json:"generated_at"`
	Retention      eventRetention                `json:"retention"`
	VersionMarkers map[string]eventVersionMarker `json:"version_markers"`
	Snapshot       eventStateSnapshot            `json:"snapshot"`
}

type eventRetention struct {
	Mode            string `json:"mode"`
	ResumeBy        string `json:"resume_by"`
	MaxReplayEvents int    `json:"max_replay_events"`
	ResyncEndpoint  string `json:"resync_endpoint"`
}

type eventVersionMarker struct {
	Count    int    `json:"count"`
	LatestAt string `json:"latest_at,omitempty"`
}

type eventStateSnapshot struct {
	Workspace workspaceResponse `json:"workspace"`
	Counts    map[string]int    `json:"counts"`
	RunState  string            `json:"run_state"`
}

func (s *Server) eventSnapshotAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	now := time.Now().UTC()
	response := eventSnapshotResponse{
		WorkspaceID:    workspace.ID,
		Sequence:       s.currentWorkspaceSequence(r, workspace.ID),
		GeneratedAt:    now.Format(time.RFC3339Nano),
		Retention:      eventRetention{Mode: "append_only_bounded_replay", ResumeBy: "last_event_id", MaxReplayEvents: 1000, ResyncEndpoint: "/api/v1/workspaces/" + workspace.ID + "/events/snapshot"},
		VersionMarkers: map[string]eventVersionMarker{},
		Snapshot:       eventStateSnapshot{Workspace: toWorkspaceResponse(workspace), Counts: map[string]int{}, RunState: "stopped"},
	}
	for name, query := range map[string]string{
		"events":        `SELECT COUNT(*), COALESCE(MAX(created_at), '') FROM events WHERE run_id=? OR EXISTS (SELECT 1 FROM runs WHERE runs.id=events.run_id AND runs.workspace_id=?)`,
		"sessions":      `SELECT COUNT(*), COALESCE(MAX(COALESCE(last_seen_at, started_at)), '') FROM agent_sessions WHERE run_id IN (SELECT id FROM runs WHERE workspace_id=?)`,
		"claims":        `SELECT COUNT(*), COALESCE(MAX(created_at), '') FROM claims WHERE workspace_id=?`,
		"proposals":     `SELECT COUNT(*), COALESCE(MAX(created_at), '') FROM proposals WHERE workspace_id=?`,
		"transactions":  `SELECT COUNT(*), COALESCE(MAX(COALESCE(applied_at, '')), '') FROM transactions WHERE workspace_id=?`,
		"deliberations": `SELECT COUNT(*), COALESCE(MAX(updated_at), '') FROM deliberations WHERE workspace_id=?`,
		"policies":      `SELECT COUNT(*), COALESCE(MAX(updated_at), '') FROM policies WHERE workspace_id=?`,
		"notifications": `SELECT COUNT(*), COALESCE(MAX(created_at), '') FROM notifications WHERE workspace_id=?`,
	} {
		var marker eventVersionMarker
		args := []any{workspace.ID}
		if name == "events" {
			args = append(args, workspace.ID)
		}
		if err := s.config.Store.DB().QueryRowContext(r.Context(), query, args...).Scan(&marker.Count, &marker.LatestAt); err == nil {
			response.VersionMarkers[name] = marker
			response.Snapshot.Counts[name] = marker.Count
		}
	}
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT status FROM runs WHERE workspace_id=? ORDER BY started_at DESC LIMIT 1`, workspace.ID).Scan(&response.Snapshot.RunState)
	writeJSON(w, http.StatusOK, response)
}
