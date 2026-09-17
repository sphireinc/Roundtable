package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestEventSnapshotIsBoundedAndReportsWorkspaceMarkers(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-snapshot", DisplayName: "Snapshot", RootPath: t.TempDir(), Status: "active", Revision: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(t.Context(), db.Event{RunID: "ws-snapshot", Type: "workspace.changed", PayloadJSON: `{"note":"safe"}`}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-snapshot/events/snapshot", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Header().Get("X-Request-ID") == "" {
		t.Fatalf("response = %d headers=%v body=%s", res.Code, res.Header(), res.Body.String())
	}
	var got eventSnapshotResponse
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WorkspaceID != "ws-snapshot" || got.Sequence < 1 || got.Retention.MaxReplayEvents != 1000 || got.VersionMarkers["events"].Count != 1 {
		t.Fatalf("snapshot = %#v", got)
	}
	if got.Snapshot.Workspace.Revision != 3 || got.Snapshot.Counts == nil {
		t.Fatalf("state snapshot = %#v", got.Snapshot)
	}
}
