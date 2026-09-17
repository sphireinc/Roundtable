package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestRepositoryStatusAndBranchSwitch(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Roundtable Test")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-qm", "initial")
	runGit(t, root, "branch", "feature")

	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	workspace := db.Workspace{ID: "ws-repo", DisplayName: "Repo", RootPath: root, Status: "active", Revision: 1}
	if err := store.UpsertWorkspace(t.Context(), workspace); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store, AllowedWorkspaceRoots: []string{root}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-repo/repository", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"branch":"master"`)) && !bytes.Contains(res.Body.Bytes(), []byte(`"branch":"main"`)) {
		t.Fatalf("repository status = %d %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-repo/repository/branch-switch/preflight", bytes.NewBufferString(`{"branch":"feature"}`))
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"allowed":true`)) {
		t.Fatalf("preflight = %d %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-repo/repository/branch-switch", bytes.NewBufferString(`{"branch":"feature"}`))
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "switch-1")
	req.Header.Set("If-Match", "1")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("switch = %d %s", res.Code, res.Body.String())
	}
	var switched branchSwitchResponse
	if err := json.Unmarshal(res.Body.Bytes(), &switched); err != nil {
		t.Fatal(err)
	}
	if switched.After.Branch != "feature" || switched.Workspace.Revision != 2 {
		t.Fatalf("unexpected switch response: %+v", switched)
	}
	var audits, outbox int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action = 'repository.branch_switched'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM event_outbox WHERE event_type = 'repository.branch_switched'").Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || outbox != 1 {
		t.Fatalf("branch audit/outbox = %d/%d", audits, outbox)
	}
}

func TestBranchSwitchRequiresHumanRoleAndIdempotencyKey(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{root}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/missing/repository/branch-switch", bytes.NewBufferString(`{"branch":"main"}`))
	req.Header.Set("X-Actor-ID", "agent-1")
	req.Header.Set("X-Actor-Role", "agent")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden || !bytes.Contains(res.Body.Bytes(), []byte(`"code":"forbidden"`)) {
		t.Fatalf("role guard = %d %s", res.Code, res.Body.String())
	}
}

func TestRepositoryEntitiesRejectTraversalAndExposeSymbols(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package sample\n\nfunc Build() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.NewStore(sqlDB, nil).UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-entities", DisplayName: "Entities", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{root}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-entities/repository/entities?type=symbol&q=build", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"name":"Build"`)) {
		t.Fatalf("entities = %d %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-entities/repository/entities?path=../secret", nil)
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !bytes.Contains(res.Body.Bytes(), []byte(`invalid_repository_path`)) {
		t.Fatalf("traversal = %d %s", res.Code, res.Body.String())
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
