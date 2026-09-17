package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestSessionObservabilityIsPaginatedAndRedacted(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	root := t.TempDir()
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-observe", DisplayName: "Observe", RootPath: root, Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent(t.Context(), db.Agent{ID: "agent-observe", Name: "Worker", Role: "implementer", Adapter: "codex", Status: "idle", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgentSession(t.Context(), db.AgentSession{ID: "session-observe", AgentID: "agent-observe", RunID: "run-observe", Adapter: "codex", WorkingDirectory: root, Status: "active", MetadataJSON: `{"workspace_id":"ws-observe","usage":{"input_tokens":12,"secret_value":99}}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendAgentSessionEvent(t.Context(), db.AgentSessionEvent{SessionID: "session-observe", EventType: "output", PayloadJSON: `{"message":"ok","token":"dont-return"}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(t.Context(), db.Event{RunID: "run-observe", Type: "tool.mcp_call", PayloadJSON: `{"tool":"repo.read_file","action":"read","password":"dont-return"}`}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-observe/sessions/session-observe/logs?limit=1", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("logs = %d %s", response.Code, response.Body.String())
	}
	if string(response.Body.Bytes()) == "" || contains(response.Body.String(), "dont-return") {
		t.Fatalf("logs leaked secret: %s", response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-observe/sessions/session-observe/tool-calls", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !contains(response.Body.String(), "repo.read_file") || contains(response.Body.String(), "dont-return") {
		t.Fatalf("tools = %d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/ws-observe/sessions/session-observe/metrics", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var metrics map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &metrics); err != nil {
		t.Fatal(err)
	}
	usage, _ := metrics["usage"].(map[string]any)
	if usage["input_tokens"] != float64(12) || usage["secret_value"] != nil {
		t.Fatalf("metrics = %+v", metrics)
	}
}

func contains(value, fragment string) bool {
	return len(value) >= len(fragment) && (value == fragment || indexOf(value, fragment) >= 0)
}
func indexOf(value, fragment string) int {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return i
		}
	}
	return -1
}
