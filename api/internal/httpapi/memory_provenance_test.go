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

func TestMemoryRevisionExpectedVersionAndProvenanceReads(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-memory-revisions", DisplayName: "Memory", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	base := "/api/v1/workspaces/ws-memory-revisions/memories"
	create := httptest.NewRequest(http.MethodPost, base, bytes.NewBufferString(`{"scope":"project","kind":"decision","title":"One","body_md":"first"}`))
	create.Header.Set("X-Actor-ID", "human")
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, create)
	if out.Code != 201 {
		t.Fatalf("create=%d %s", out.Code, out.Body.String())
	}
	var item memoryResponse
	if err := json.Unmarshal(out.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	rev := httptest.NewRequest(http.MethodPost, base+"/"+item.ID+"/revisions", bytes.NewBufferString(`{"body_md":"second","expected_revision":99}`))
	rev.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, rev)
	if out.Code != 409 {
		t.Fatalf("stale revision=%d %s", out.Code, out.Body.String())
	}
	rev = httptest.NewRequest(http.MethodPost, base+"/"+item.ID+"/revisions", bytes.NewBufferString(`{"body_md":"second","expected_revision":1}`))
	rev.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, rev)
	if out.Code != 200 {
		t.Fatalf("revision=%d %s", out.Code, out.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, base+"/"+item.ID+"/revisions", nil)
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, list)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(item.ID)) {
		t.Fatalf("revisions=%d %s", out.Code, out.Body.String())
	}
	if _, err := store.DB().Exec(`INSERT INTO memory_provenance_edges(memory_id,source_type,source_id,relation) VALUES(?,?,?,?)`, item.ID, "event", "E-1", "derived_from"); err != nil {
		t.Fatal(err)
	}
	chain := httptest.NewRequest(http.MethodGet, base+"/"+item.ID+"/source-chain", nil)
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, chain)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte("E-1")) {
		t.Fatalf("chain=%d %s", out.Code, out.Body.String())
	}
}
