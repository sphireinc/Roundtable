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

func TestSessionLifecycleStateMachineAndWorkspaceBoundary(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-session", DisplayName: "Session", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "agent-session", Name: "Worker", Role: "implementer", Adapter: "codex", Status: "idle", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-session/sessions", bytes.NewBufferString(`{"agent_id":"agent-session","run_id":"run-1","adapter":"codex","external_session_id":"ext-1"}`))
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "create-1")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	var created sessionResponse
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "starting" || created.ResumeCommandTemplate != "codex resume <external_session_id>" {
		t.Fatalf("created = %+v", created)
	}

	path := "/api/v1/workspaces/ws-session/sessions/" + created.ID + "/pause"
	req = httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"status":"paused"`)) {
		t.Fatalf("pause = %d %s", res.Code, res.Body.String())
	}

	path = "/api/v1/workspaces/ws-session/sessions/" + created.ID + "/resume"
	req = httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "resume-1")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"status":"resuming"`)) {
		t.Fatalf("resume = %d %s", res.Code, res.Body.String())
	}

	path = "/api/v1/workspaces/ws-session/sessions/" + created.ID + "/stop"
	req = httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(`"status":"stopped"`)) {
		t.Fatalf("stop = %d %s", res.Code, res.Body.String())
	}

	path = "/api/v1/workspaces/ws-session/sessions/" + created.ID + "/pause"
	req = httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("X-Actor-Role", "human")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusConflict || !bytes.Contains(res.Body.Bytes(), []byte("illegal_session_transition")) {
		t.Fatalf("illegal transition = %d %s", res.Code, res.Body.String())
	}
}
