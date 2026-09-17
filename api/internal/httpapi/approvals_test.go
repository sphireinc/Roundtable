package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestApprovalAPIRequiresReasonsAndAdvancesProposalTransactionally(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-approval", DisplayName: "Approval", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO proposals(id,task_id,agent_id,title,summary_md,patch_path,workspace_id,status,risk) VALUES('P-approval','','','Approval','summary','patch.diff','ws-approval','in_review','high')`); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	body := `{"id":"A-approval","proposal_id":"P-approval","subject":"Approve high risk patch","reason":"security review required","risk":"high","required_role":"human","override_policy":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-approval/approvals", bytes.NewBufferString(body))
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unauthorized=%d", res.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-approval/approvals", bytes.NewBufferString(body))
	req.Header.Set("X-Roundtable-Orchestrator", "true")
	req.Header.Set("X-Actor-ID", "orchestrator")
	req.Header.Set("Idempotency-Key", "request")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("request=%d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-approval/approvals/A-approval/reject", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "reject")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing reason=%d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-approval/approvals/A-approval/reject", bytes.NewBufferString(`{"reason":"unresolved security finding"}`))
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "reject-real")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject=%d %s", res.Code, res.Body.String())
	}
	var state string
	if err := sqlDB.QueryRow(`SELECT approval_state FROM proposals WHERE id='P-approval'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "rejected" {
		t.Fatalf("proposal state=%s", state)
	}
}
