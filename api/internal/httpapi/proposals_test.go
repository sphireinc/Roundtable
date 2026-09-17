package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestProposalAPIOrchestratorCreationAndGovernedTransitions(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-proposal", DisplayName: "Proposal", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-proposal/proposals/internal", bytes.NewBufferString(`{"title":"Safe proposal","summary":"summary","patch_path":".roundtable/patches/P.diff","base_revision":"abc"}`))
	req.Header.Set("Idempotency-Key", "create-1")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized create = %d", res.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-proposal/proposals/internal", bytes.NewBufferString(`{"proposal_id":"P-test","title":"Safe proposal","summary":"summary token=hidden","patch_path":".roundtable/patches/P.diff","base_revision":"abc"}`))
	req.Header.Set("X-Roundtable-Orchestrator", "true")
	req.Header.Set("X-Actor-ID", "orchestrator")
	req.Header.Set("Idempotency-Key", "create-1")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated || contains(res.Body.String(), "hidden") {
		t.Fatalf("create = %d %s", res.Code, res.Body.String())
	}
	post := func(action string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-proposal/proposals/P-test/"+action, nil)
		req.Header.Set("X-Actor-ID", "human")
		req.Header.Set("X-Actor-Role", "human")
		req.Header.Set("Idempotency-Key", action)
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		return res
	}
	if res := post("request-review"); res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"in_review"`) {
		t.Fatalf("review = %d %s", res.Code, res.Body.String())
	}
	if res := post("request-review"); res.Code != http.StatusConflict || !contains(res.Body.String(), "illegal_proposal_transition") {
		t.Fatalf("illegal = %d %s", res.Code, res.Body.String())
	}
	if res := post("reject"); res.Code != http.StatusOK || !contains(res.Body.String(), `"state":"rejected"`) {
		t.Fatalf("reject = %d %s", res.Code, res.Body.String())
	}
	var audits int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE entity_type = 'proposal'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits < 3 {
		t.Fatalf("audits = %d", audits)
	}
}
