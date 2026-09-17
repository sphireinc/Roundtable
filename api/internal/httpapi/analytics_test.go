package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"roundtable/internal/db"
)

func TestConsensusAnalyticsUsesBoundedWindowAndExplicitRates(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-analytics", DisplayName: "Analytics", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO proposals (id, task_id, agent_id, title, summary_md, patch_path, workspace_id, status, risk) VALUES ('P-analytics', '', '', 'Analytics', 'summary', 'patch.diff', 'ws-analytics', 'pending', 'normal')`); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertVote(t.Context(), db.Vote{ID: "V-analytics", ProposalID: "P-analytics", AgentID: "agent-a", Vote: "abstain", ReasonMD: "needs more evidence"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO consensus_snapshots (id, proposal_id, status, approval_count, rejection_count, abstain_count, snapshot_json) VALUES ('S-analytics', 'P-analytics', 'pending', 0, 0, 1, '{}')`); err != nil {
		t.Fatal(err)
	}
	from := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	to := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-analytics/analytics/consensus?from="+from+"&to="+to, nil)
	res := httptest.NewRecorder()
	NewServer(Config{Store: store}).Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), `"proposal_count":1`) || !contains(res.Body.String(), `"abstention_rate":1`) || !contains(res.Body.String(), `"quorum_failures":1`) {
		t.Fatalf("analytics = %d %s", res.Code, res.Body.String())
	}
	tooWide := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-analytics/analytics/consensus?from=2020-01-01T00:00:00Z&to=2021-01-01T00:00:00Z", nil)
	tooWideRes := httptest.NewRecorder()
	NewServer(Config{Store: store}).Handler().ServeHTTP(tooWideRes, tooWide)
	if tooWideRes.Code != http.StatusBadRequest {
		t.Fatalf("wide window = %d %s", tooWideRes.Code, tooWideRes.Body.String())
	}
}
