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

func TestMemoryOracleLifecycleSearchRevisionsAndAuthorization(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-memory", DisplayName: "Memory", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	endpoint := "/api/v1/workspaces/ws-memory/memories"
	req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(`{"scope":"project","kind":"decision","title":"SQLite","body_md":"Use SQLite","tags":["architecture"],"provenance":{"source":"human"},"confidence":0.9,"reliability":0.8}`))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized create = %d", res.Code)
	}
	req = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(`{"scope":"project","kind":"decision","title":"SQLite","body_md":"Use SQLite","tags":["architecture"],"provenance":{"source":"human"},"confidence":0.9,"reliability":0.8}`))
	req.Header.Set("X-Actor-ID", "human")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	var created memoryResponse
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || created.Confidence != 0.9 || len(created.Tags) != 1 {
		t.Fatalf("created = %#v", created)
	}
	listReq := httptest.NewRequest(http.MethodGet, endpoint+"?q=SQLite&scope=project", nil)
	listRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK || !bytes.Contains(listRes.Body.Bytes(), []byte(created.ID)) {
		t.Fatalf("list = %d %s", listRes.Code, listRes.Body.String())
	}
	act := func(action string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, endpoint+"/"+created.ID+"/"+action, bytes.NewBufferString(body))
		req.Header.Set("X-Actor-ID", "human")
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		return out
	}
	if out := act("pin", ""); out.Code != http.StatusOK || !bytes.Contains(out.Body.Bytes(), []byte(`"pinned":true`)) {
		t.Fatalf("pin = %d %s", out.Code, out.Body.String())
	}
	rev := httptest.NewRequest(http.MethodPost, endpoint+"/"+created.ID+"/revisions", bytes.NewBufferString(`{"body_md":"Use SQLite authoritatively","reason":"clarified"}`))
	rev.Header.Set("X-Actor-ID", "human")
	revRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(revRes, rev)
	if revRes.Code != http.StatusOK || !bytes.Contains(revRes.Body.Bytes(), []byte(`"revision":2`)) {
		t.Fatalf("revision = %d %s", revRes.Code, revRes.Body.String())
	}
	if out := act("resynthesize", ""); out.Code != http.StatusOK || !bytes.Contains(out.Body.Bytes(), []byte(`"resynthesis_status":"requested"`)) {
		t.Fatalf("resynthesize = %d %s", out.Code, out.Body.String())
	}
	if out := act("archive", ""); out.Code != http.StatusOK || !bytes.Contains(out.Body.Bytes(), []byte(`"status":"archived"`)) {
		t.Fatalf("archive = %d %s", out.Code, out.Body.String())
	}
	var events int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM events WHERE run_id='ws-memory' AND type LIKE 'memory.%'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events < 4 {
		t.Fatalf("memory events = %d", events)
	}
}
