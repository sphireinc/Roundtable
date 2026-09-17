package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestActivityFeedNormalizesFiltersAndRedactsMetadata(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-activity", DisplayName: "Activity", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(t.Context(), db.Event{RunID: "ws-activity", Type: "proposal.created", ActorID: "agent-1", PayloadJSON: `{"proposal_id":"P-1","severity":"warning","token":"secret"}`}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-activity/activity?category=proposal&actor=agent-1&severity=warning", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"title":"Proposal created"`) || !contains(res.Body.String(), `"entity_id":"P-1"`) || contains(res.Body.String(), "secret") {
		t.Fatalf("activity=%d %s", res.Code, res.Body.String())
	}
}
