package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"roundtable/internal/db"
)

func TestMaintenanceEndpointsUseStructuredSafeOperations(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), MaintenanceDirectory: root})
	get := httptest.NewRecorder()
	server.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/maintenance", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "protected_immutable_tables") {
		t.Fatalf("status = %d %s", get.Code, get.Body.String())
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("X-Actor-ID", "admin")
		req.Header.Set("X-Actor-Role", "administer")
		req.Header.Set("Idempotency-Key", strings.ReplaceAll(path, "/", "-"))
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		return res
	}
	if res := post("/api/v1/maintenance/integrity-check", "{}"); res.Code != http.StatusOK {
		t.Fatalf("integrity = %d %s", res.Code, res.Body.String())
	}
	if res := post("/api/v1/maintenance/checkpoint", "{}"); res.Code != http.StatusOK {
		t.Fatalf("checkpoint = %d %s", res.Code, res.Body.String())
	}
	backup := post("/api/v1/maintenance/backup", "{}")
	if backup.Code != http.StatusCreated || !strings.Contains(backup.Body.String(), "backups/") {
		t.Fatalf("backup = %d %s", backup.Code, backup.Body.String())
	}
	retention := post("/api/v1/maintenance/retention", `{"retention_days":30}`)
	if retention.Code != http.StatusOK || !strings.Contains(retention.Body.String(), "audit_events") {
		t.Fatalf("retention = %d %s", retention.Code, retention.Body.String())
	}
}
