package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestRunControlSeparatesRunStateFromSessions(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-run", DisplayName: "Run", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	post := func(action string, body string, actor bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-run/runs/"+action, bytes.NewBufferString(body))
		if actor {
			req.Header.Set("X-Actor-ID", "human")
			req.Header.Set("X-Actor-Role", "human")
		}
		req.Header.Set("Idempotency-Key", action)
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		return out
	}
	if res := post("start", `{"goal":"govern changes"}`, false); res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized start=%d", res.Code)
	}
	res := post("start", `{"goal":"govern changes"}`, true)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"active"`) {
		t.Fatalf("start=%d %s", res.Code, res.Body.String())
	}
	if res = post("pause", `{}`, true); res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"paused-intake"`) {
		t.Fatalf("pause=%d %s", res.Code, res.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-run/runs/status", nil)
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, req)
	if out.Code != http.StatusOK || !contains(out.Body.String(), `"state":"paused-intake"`) {
		t.Fatalf("status=%d %s", out.Code, out.Body.String())
	}
	if res = post("resume", `{}`, true); res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"active"`) {
		t.Fatalf("resume=%d %s", res.Code, res.Body.String())
	}
	if res = post("stop", `{}`, true); res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"stopped"`) {
		t.Fatalf("stop=%d %s", res.Code, res.Body.String())
	}
}
