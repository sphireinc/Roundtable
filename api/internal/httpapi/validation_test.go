package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestProposalValidationRerunIsAuthorizedAndPersisted(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable", "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n"
	if err := os.WriteFile(filepath.Join(root, ".roundtable", "patches", "P.diff"), []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-validate", DisplayName: "Validate", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProposal(t.Context(), db.Proposal{ID: "P-validate", Title: "Validate", SummaryMD: "safe", PatchPath: ".roundtable/patches/P.diff", Status: "pending", Risk: "normal"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE proposals SET workspace_id = 'ws-validate' WHERE id = 'P-validate'`); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-validate/proposals/P-validate/validation", bytes.NewBufferString(`{"stages":["patch_parse","stale_base"]}`))
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "validate-1")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted || !contains(res.Body.String(), "run_id") || !contains(res.Body.String(), "patch_parse") {
		t.Fatalf("validation = %d %s", res.Code, res.Body.String())
	}
	var runs int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM test_runs WHERE proposal_id = 'P-validate'").Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("test runs = %d", runs)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-validate/proposals/P-validate/validation", nil)
	req.Header.Set("Idempotency-Key", "validate-2")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized validation = %d", res.Code)
	}
}
