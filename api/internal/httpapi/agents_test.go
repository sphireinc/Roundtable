package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestAgentRegistryRedactsCommandAndSupportsDrainDisable(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-agent", DisplayName: "Agent", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "agent-1", Name: "Worker", Role: "implementer", Adapter: "codex", Command: "codex --secret token", Status: "idle", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-agent/agents", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || bytes.Contains(res.Body.Bytes(), []byte("secret")) || !bytes.Contains(res.Body.Bytes(), []byte(`"supports_resume":true`)) {
		t.Fatalf("list = %d %s", res.Code, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-agent/agents/agent-1/disable", nil)
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "disable-1")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"enabled":false`)) {
		t.Fatalf("disable = %d %s", res.Code, res.Body.String())
	}
	var audits int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action = 'agent.state_changed'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit count = %d", audits)
	}
}

func TestAgentDiagnosticsAreBoundedRedactedAndAudited(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-diag", DisplayName: "Diag", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "agent-diag", Name: "Diag", Role: "tester", Adapter: "generic", Command: "/bin/echo token=secret", Status: "idle", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-diag/agents/agent-diag/diagnostics", nil)
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "diag-1")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted || bytes.Contains(res.Body.Bytes(), []byte("secret")) || !bytes.Contains(res.Body.Bytes(), []byte(`"mcp_surface"`)) {
		t.Fatalf("diagnostic = %d %s", res.Code, res.Body.String())
	}
}
