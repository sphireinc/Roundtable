package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestTransactionAPIRejectsDirectAgentControlAndAuditsHumanTransition(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-tx", DisplayName: "Transactions", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO proposals(id,task_id,agent_id,title,summary_md,patch_path,workspace_id,status,risk) VALUES('P-tx','','','Tx','summary','patch.diff','ws-tx','in_review','normal')`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO transactions(id,proposal_id,run_id,before_git_hash,status,applied_by,rollback_patch_path) VALUES('TX-test','P-tx','run','before','pending','orchestrator','.roundtable/patches/rollback.json')`); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-tx/transactions/TX-test/cancel", nil)
	req.Header.Set("X-Actor-ID", "agent")
	req.Header.Set("Idempotency-Key", "cancel-agent")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("agent control=%d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-tx/transactions/TX-test/cancel?reason=stop", nil)
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "cancel-human")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("human control=%d %s", res.Code, res.Body.String())
	}
	var audits int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='transaction.cancel'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audits=%d", audits)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-tx/transactions/TX-test/compensate", nil)
	req.Header.Set("X-Actor-ID", "human")
	req.Header.Set("X-Actor-Role", "human")
	req.Header.Set("Idempotency-Key", "compensate")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated || !contains(res.Body.String(), `"status":"pending"`) {
		t.Fatalf("compensate=%d %s", res.Code, res.Body.String())
	}
}
