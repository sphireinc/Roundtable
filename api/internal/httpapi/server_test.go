package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestHealthRequestID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	r.Header.Set("X-Request-ID", "test-1")
	w := httptest.NewRecorder()
	NewServer(Config{Version: "test"}).Handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Request-ID") != "test-1" {
		t.Fatalf("unexpected response: %d %q", w.Code, w.Header().Get("X-Request-ID"))
	}
}

func TestHealthGeneratesRequestID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()
	NewServer(Config{}).Handler().ServeHTTP(w, r)
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("request id missing")
	}
}

func TestWorkspaceRegistryValidatesRootsAndSupportsDetach(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqlDB.Close()
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{root}})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(`{"display_name":"Demo","root_alias":"demo","root_path":"`+root+`","default_branch":"main"}`))
	request.Header.Set("X-Actor-ID", "human-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", response.Code, response.Body.String())
	}
	var created workspaceResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Status != "active" || created.CanonicalRepositoryIdentity == "" || created.Revision != 1 {
		t.Fatalf("unexpected workspace: %+v", created)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+created.ID, nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get status = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/"+created.ID, bytes.NewBufferString(`{"display_name":"Renamed"}`))
	request.Header.Set("X-Actor-ID", "human-1")
	request.Header.Set("If-Match", "1")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatalf("patch response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/"+created.ID, bytes.NewBufferString(`{"display_name":"Conflict"}`))
	request.Header.Set("X-Actor-ID", "human-1")
	request.Header.Set("If-Match", "1")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !bytes.Contains(response.Body.Bytes(), []byte(`workspace_revision_conflict`)) {
		t.Fatalf("stale patch response = %d %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/"+created.ID, nil)
	request.Header.Set("X-Actor-ID", "human-1")
	request.Header.Set("If-Match", "2")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte(`"status":"detached"`)) {
		t.Fatalf("detach response = %d %s", response.Code, response.Body.String())
	}
	var audits, outbox int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE entity_id = ?", created.ID).Scan(&audits); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM event_outbox WHERE entity_id = ?", created.ID).Scan(&outbox); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if audits != 3 || outbox != 3 {
		t.Fatalf("audit/outbox counts = %d/%d, want 3/3", audits, outbox)
	}
}

func TestWorkspaceRegistryRejectsTraversalAndUnauthenticatedMutation(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqlDB.Close()
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{root}})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(`{"root_path":"`+root+`"}`))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated status = %d", response.Code)
	}

	outside := filepath.Join(root, "..")
	request = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(`{"root_path":"`+outside+`"}`))
	request.Header.Set("X-Actor-ID", "human-1")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("outside-root status = %d", response.Code)
	}
}
