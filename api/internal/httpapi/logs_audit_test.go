package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestOperationalLogsAuditFiltersRedactionAndExport(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-logs", DisplayName: "Logs", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(t.Context(), db.Event{RunID: "ws-logs", Type: "transaction.failed", ActorID: "agent-1", PayloadJSON: `{"severity":"error","request_id":"req-1","transaction_id":"TX-1","secret":"do-not-return"}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,reason,payload_json) VALUES('ws-logs','human','transaction.cancel','transaction','TX-1','req-1','because','{"secret":"hide"}')`); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	logs := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-logs/logs/operational?severity=error&transaction_id=TX-1", nil)
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, logs)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte("transaction.failed")) || bytes.Contains(out.Body.Bytes(), []byte("do-not-return")) {
		t.Fatalf("logs=%d %s", out.Code, out.Body.String())
	}
	audit := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-logs/audit?action=transaction.cancel", nil)
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, audit)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte("transaction.cancel")) || bytes.Contains(out.Body.Bytes(), []byte("hide")) {
		t.Fatalf("audit=%d %s", out.Code, out.Body.String())
	}
	export := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-logs/audit/export?format=ndjson", nil)
	export.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, export)
	if out.Code != 200 || out.Header().Get("Content-Type") != "application/x-ndjson" || !bytes.Contains(out.Body.Bytes(), []byte("transaction.cancel")) {
		t.Fatalf("export=%d headers=%v body=%s", out.Code, out.Header(), out.Body.String())
	}
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE action='audit.exported'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("export audit count=%d", count)
	}
}
