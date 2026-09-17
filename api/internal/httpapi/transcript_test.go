package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestDeliberationTranscriptDeepLinkAndVisibility(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	root := t.TempDir()
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-transcript", DisplayName: "Transcript", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "agent-transcript", Name: "Agent", Role: "implementer", Adapter: "codex", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO deliberations (id, workspace_id, title, status, created_by, metadata_json) VALUES ('delib-transcript','ws-transcript','Goal','running','human','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`INSERT INTO deliberation_messages (id, deliberation_id, actor, message_type, body, sequence) VALUES ('msg-visible','delib-transcript','agent','reasoning_summary','Visible summary',0),('msg-hidden','delib-transcript','agent','chain_of_thought','Hidden reasoning',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(t.Context(), db.Event{RunID: "delib-transcript", Type: "deliberation.started", ActorID: "human", PayloadJSON: `{"proposal_id":"P-1"}`}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-transcript/deliberations/delib-transcript/transcript?limit=10", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || string(res.Body.Bytes()) == "" {
		t.Fatalf("list = %d %s", res.Code, res.Body.String())
	}
	if contains(res.Body.String(), "Hidden reasoning") {
		t.Fatalf("hidden transcript leaked: %s", res.Body.String())
	}
	var page struct {
		Items []transcriptEntry `json:"items"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-transcript/deliberations/delib-transcript/transcript/msg-visible", nil)
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !contains(res.Body.String(), "Visible summary") {
		t.Fatalf("deep link = %d %s", res.Code, res.Body.String())
	}
}
