package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestDashboardSummaryIsBoundedAndDoesNotEmbedLists(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-dashboard", DisplayName: "Dashboard", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-dashboard/dashboard/summary", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"measurement_window"`) || !contains(res.Body.String(), `"mcp_enforcement":"orchestrator-only"`) {
		t.Fatalf("summary=%d %s", res.Code, res.Body.String())
	}
	if contains(res.Body.String(), `"items"`) {
		t.Fatalf("summary unexpectedly embeds list: %s", res.Body.String())
	}
}
