package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestClaimsAPIConflictOwnershipAndForceRelease(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("claim me"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open(filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-claims", DisplayName: "Claims", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "agent-claims", Name: "Agent", Role: "implementer", Adapter: "codex", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-claims/claims/internal", bytes.NewBufferString(`{"agent_id":"agent-claims","task_id":"task-claims","resource_type":"file","path":"file.txt","mode":"exclusive","run_id":"run-claims"}`))
	req.Header.Set("X-Roundtable-Orchestrator", "true")
	req.Header.Set("Idempotency-Key", "claim-1")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	var created claimResponse
	if err := decodeResponse(res, &created); err != nil {
		t.Fatal(err)
	}
	if res := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-claims/claims/internal", bytes.NewBufferString(`{"agent_id":"agent-claims","task_id":"task-2","resource_type":"file","path":"file.txt","mode":"exclusive"}`))
		req.Header.Set("X-Roundtable-Orchestrator", "true")
		req.Header.Set("Idempotency-Key", "claim-2")
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		return out
	}(); res.Code != http.StatusConflict || !contains(res.Body.String(), "claim_conflict") {
		t.Fatalf("conflict = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-claims/claims/contentions", nil)
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	var contentionPage struct {
		Items []contentionResponse `json:"items"`
	}
	if res.Code != http.StatusOK || decodeResponse(res, &contentionPage) != nil || len(contentionPage.Items) != 1 {
		t.Fatalf("contentions = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-claims/claims/contentions/"+contentionPage.Items[0].ID+"/resolve", bytes.NewBufferString(`{"resolution":"keep_owner"}`))
	req.Header.Set("X-Actor-ID", "agent-claims")
	req.Header.Set("Idempotency-Key", "resolve-unauthorized")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized resolve = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-claims/claims/contentions/"+contentionPage.Items[0].ID+"/resolve", bytes.NewBufferString(`{"resolution":"keep_owner","reason":"owner remains"}`))
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "resolve")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"resolved"`) {
		t.Fatalf("resolve = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-claims/claims/"+created.ID+"/extend", bytes.NewBufferString(`{"ttl_seconds":120}`))
	req.Header.Set("X-Actor-ID", "agent-claims")
	req.Header.Set("Idempotency-Key", "extend")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("extend = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-claims/claims/"+created.ID+"/force-release?reason=cleanup", nil)
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "release")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"released"`) {
		t.Fatalf("release = %d %s", res.Code, res.Body.String())
	}
}

func decodeResponse(res *httptest.ResponseRecorder, target any) error {
	return json.Unmarshal(res.Body.Bytes(), target)
}
