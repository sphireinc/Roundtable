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

func TestGovernanceFlowPersistsAuditAndEventBoundaries(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{root}})
	create := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(`{"display_name":"Flow","root_alias":"flow","root_path":"`+root+`","default_branch":"main"}`))
	create.Header.Set("X-Actor-ID", "human")
	created := httptest.NewRecorder()
	server.Handler().ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("workspace = %d %s", created.Code, created.Body.String())
	}
	var workspace workspaceResponse
	if err := json.Unmarshal(created.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	proposalPath := "/api/v1/workspaces/" + workspace.ID + "/proposals/internal"
	browser := httptest.NewRequest(http.MethodPost, proposalPath, bytes.NewBufferString(`{"title":"Browser bypass","summary":"forbidden","patch_path":".roundtable/patches/p.diff"}`))
	browser.Header.Set("X-Actor-ID", "human")
	browser.Header.Set("Idempotency-Key", "browser-proposal")
	browserResult := httptest.NewRecorder()
	server.Handler().ServeHTTP(browserResult, browser)
	if browserResult.Code != http.StatusForbidden {
		t.Fatalf("browser proposal bypass = %d %s", browserResult.Code, browserResult.Body.String())
	}
	proposal := httptest.NewRequest(http.MethodPost, proposalPath, bytes.NewBufferString(`{"proposal_id":"P-flow","title":"Governed patch","summary":"submitted by orchestrator","patch_path":".roundtable/patches/p.diff","base_revision":"head"}`))
	proposal.Header.Set("X-Roundtable-Orchestrator", "true")
	proposal.Header.Set("X-Actor-ID", "agent-flow")
	proposal.Header.Set("Idempotency-Key", "proposal-flow")
	proposalResult := httptest.NewRecorder()
	server.Handler().ServeHTTP(proposalResult, proposal)
	if proposalResult.Code != http.StatusCreated {
		t.Fatalf("proposal = %d %s", proposalResult.Code, proposalResult.Body.String())
	}
	var audits, events int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM audit_events WHERE workspace_id = ?", workspace.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM events WHERE run_id = ?", "P-flow").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if audits < 2 || events < 1 {
		t.Fatalf("governance evidence audits=%d events=%d", audits, events)
	}
}
