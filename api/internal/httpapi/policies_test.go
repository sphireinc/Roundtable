package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestPolicyAPIImmutableRevisionLifecycleAndAuthorization(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-policy", DisplayName: "Policy", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-policy/policies", bytes.NewBufferString(`{"id":"policy-test","name":"Test","scope":"workspace","selector":{"path":"api/**"},"severity":"high","enforcement_mode":"blocking","human_approval_required":true,"definition":{"threshold":2}}`))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized = %d %s", res.Code, res.Body.String())
	}
	create := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-policy/policies", bytes.NewBufferString(`{"id":"policy-test","name":"Test","scope":"workspace","selector":{"path":"api/**"},"severity":"high","enforcement_mode":"blocking","human_approval_required":true,"definition":{"threshold":2}}`))
		req.Header.Set("X-Actor-ID", "human")
		req.Header.Set("X-Actor-Role", "human")
		req.Header.Set("Idempotency-Key", "create")
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		return out
	}
	if res := create(); res.Code != http.StatusCreated || !contains(res.Body.String(), `"status":"disabled"`) {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	post := func(action string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-policy/policies/policy-test/"+action, nil)
		req.Header.Set("X-Actor-ID", "human")
		req.Header.Set("X-Actor-Role", "human")
		req.Header.Set("Idempotency-Key", action)
		out := httptest.NewRecorder()
		server.Handler().ServeHTTP(out, req)
		return out
	}
	if res := post("update-draft"); res.Code != http.StatusOK {
		t.Fatalf("draft = %d %s", res.Code, res.Body.String())
	}
	if res := post("publish"); res.Code != http.StatusOK || !contains(res.Body.String(), `"status":"active"`) {
		t.Fatalf("publish = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-policy/policies/policy-test/validate", nil)
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"valid":true`) {
		t.Fatalf("validate = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-policy/policies/policy-test/simulate", bytes.NewBufferString(`{"subject_type":"proposal","subject_id":"P-1","evidence":["test"]}`))
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"simulation":true`) {
		t.Fatalf("simulate = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-policy/policies/policy-test/evaluate", bytes.NewBufferString(`{"subject_type":"proposal","subject_id":"P-1","evidence":["test"]}`))
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "evaluate")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated || contains(res.Body.String(), `"simulation":true`) {
		t.Fatalf("evaluate = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-policy/policies/policy-test/revisions", nil)
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"version":2`) {
		t.Fatalf("revisions = %d %s", res.Code, res.Body.String())
	}
}
