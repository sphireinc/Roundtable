package httpapi

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestVoteAPIUsesServerPolicyWeightsAndConsensus(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-votes", DisplayName: "Votes", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow("SELECT 1").Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO proposals (id, task_id, agent_id, title, summary_md, patch_path, workspace_id, status, risk) VALUES ('P-votes', '', '', 'Votes', 'summary', 'patch.diff', 'ws-votes', 'pending', 'normal')`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "security-voter", Name: "Security", Role: "security", Adapter: "test", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	post := func(body, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws-votes/proposals/P-votes/votes", bytes.NewBufferString(body))
		req.Header.Set("X-Roundtable-Orchestrator", "true")
		req.Header.Set("X-Actor-ID", "orchestrator")
		if role != "" {
			req.Header.Set("X-Actor-Role", role)
		}
		req.Header.Set("Idempotency-Key", body)
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		return res
	}
	if res := post(`{"agent_id":"security-voter","decision":"approve","rationale":"reviewed"}`, ""); res.Code != http.StatusCreated || !contains(res.Body.String(), `"policy_weight":3`) {
		t.Fatalf("vote = %d %s", res.Code, res.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-votes/proposals/P-votes/consensus", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"outcome":"pending"`) || !contains(res.Body.String(), `"approve_weight":3`) {
		t.Fatalf("consensus = %d %s", res.Code, res.Body.String())
	}
	if count := queryCount(t, sqlDB, "SELECT COUNT(*) FROM consensus_snapshots WHERE proposal_id = 'P-votes'"); count != 1 {
		t.Fatalf("snapshots = %d", count)
	}
	if res := post(`{"agent_id":"security-voter","decision":"approve","rationale":"second review"}`, "human"); res.Code != http.StatusCreated || !contains(res.Body.String(), `"outcome":"approved"`) {
		t.Fatalf("second vote = %d %s", res.Code, res.Body.String())
	}
}

func queryCount(t *testing.T, sqlDB *sql.DB, query string) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow(query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
