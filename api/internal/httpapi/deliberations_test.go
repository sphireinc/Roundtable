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

func TestDeliberationLifecycleAndHumanMessage(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-delib", DisplayName: "Delib", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"agent-a", "agent-b"} {
		if err := store.UpsertAgent(t.Context(), db.Agent{ID: id, Name: id, Role: "implementer", Adapter: "codex", Status: "idle", IsEnabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	server := NewServer(Config{Store: store})
	headers := func(req *http.Request, key string) {
		req.Header.Set("X-Actor-ID", "human-1")
		req.Header.Set("X-Actor-Role", "human")
		req.Header.Set("Idempotency-Key", key)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-delib/deliberations", bytes.NewBufferString(`{"goal":"Resolve API shape","agent_pool":["agent-a","agent-b"],"governance_profile":"strict"}`))
	headers(req, "create")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	var created deliberationResponse
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "draft" || len(created.Participants) != 2 {
		t.Fatalf("created = %+v", created)
	}
	post := func(action, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-delib/deliberations/"+created.ID+"/"+action, nil)
		headers(req, key)
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		return res
	}
	if res := post("start", "start"); res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"status":"running"`)) {
		t.Fatalf("start = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-delib/deliberations/"+created.ID+"/messages", bytes.NewBufferString(`{"body":"approve with token=hidden"}`))
	headers(req, "message")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated || bytes.Contains(res.Body.Bytes(), []byte("hidden")) {
		t.Fatalf("message = %d %s", res.Code, res.Body.String())
	}
	if res := post("start", "start-again"); res.Code != http.StatusConflict || !bytes.Contains(res.Body.Bytes(), []byte("illegal_deliberation_transition")) {
		t.Fatalf("illegal = %d %s", res.Code, res.Body.String())
	}
	if res := post("pause", "pause"); res.Code != http.StatusOK {
		t.Fatalf("pause = %d %s", res.Code, res.Body.String())
	}
	if res := post("terminate", "terminate"); res.Code != http.StatusOK {
		t.Fatalf("terminate = %d %s", res.Code, res.Body.String())
	}
	var events int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM events WHERE type LIKE 'deliberation.%'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events < 4 {
		t.Fatalf("events = %d", events)
	}
	var audits int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE entity_type = 'deliberation'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits < 4 {
		t.Fatalf("audits = %d", audits)
	}
}
