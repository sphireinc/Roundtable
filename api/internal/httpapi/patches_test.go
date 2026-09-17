package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestPatchInspectionEndpointsAndPathBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Changed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,2 @@\n package main\n+func Changed() {}\n"
	if err := os.MkdirAll(filepath.Join(root, ".roundtable", "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".roundtable", "patches", "P-1.diff"), []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-patch", DisplayName: "Patch", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProposal(t.Context(), db.Proposal{ID: "P-1", Title: "Patch", SummaryMD: "summary", PatchPath: ".roundtable/patches/P-1.diff", Status: "pending", Risk: "normal"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE proposals SET workspace_id = 'ws-patch', base_revision = 'not-the-head' WHERE id = 'P-1'`); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		return res
	}
	if res := get("/api/v1/workspaces/ws-patch/proposals/P-1/patch"); res.Code != http.StatusOK || !contains(res.Body.String(), `"file_count":1`) || !contains(res.Body.String(), `"stale_base":true`) {
		t.Fatalf("metadata = %d %s", res.Code, res.Body.String())
	}
	if res := get("/api/v1/workspaces/ws-patch/proposals/P-1/patch/files?limit=1"); res.Code != http.StatusOK || !contains(res.Body.String(), "main.go") {
		t.Fatalf("files = %d %s", res.Code, res.Body.String())
	}
	if res := get("/api/v1/workspaces/ws-patch/proposals/P-1/patch/diff"); res.Code != http.StatusOK || !contains(res.Body.String(), "func Changed") {
		t.Fatalf("diff = %d %s", res.Code, res.Body.String())
	}
	if res := get("/api/v1/workspaces/ws-patch/proposals/P-1/patch/symbol-impact"); res.Code != http.StatusOK || !contains(res.Body.String(), "Changed") {
		t.Fatalf("symbols = %d %s", res.Code, res.Body.String())
	}
	if _, err := store.DB().Exec(`UPDATE proposals SET patch_path = '../outside.diff' WHERE id = 'P-1'`); err != nil {
		t.Fatal(err)
	}
	if res := get("/api/v1/workspaces/ws-patch/proposals/P-1/patch"); res.Code != http.StatusBadRequest || !contains(res.Body.String(), "path escapes workspace") {
		t.Fatalf("traversal = %d %s", res.Code, res.Body.String())
	}
}
