package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"roundtable/internal/db"
)

func TestWorkspaceEventsWebSocketAuthReplayAndLiveDelivery(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-events", DisplayName: "Events", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	first, err := store.AppendEvent(t.Context(), db.Event{RunID: "ws-events", Type: "proposal.created", ActorID: "agent-1", PayloadJSON: `{"proposal_id":"P-1","token":"secret"}`})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local TCP listener unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(NewServer(Config{Store: store}).Handler())
	server.Listener = listener
	server.Start()
	defer server.Close()
	wsURL := "ws" + server.URL[len("http"):]
	_, response, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/workspaces/ws-events/events/ws", nil)
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized dial err=%v response=%v", err, response)
	}
	header := http.Header{"X-Actor-ID": []string{"human"}}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/workspaces/ws-events/events/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var hello map[string]any
	if err := conn.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if hello["kind"] != "hello" || hello["workspace_id"] != "ws-events" {
		t.Fatalf("hello = %#v", hello)
	}
	var envelope liveEventEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.EventID != first.ID || envelope.Sequence != first.ID || envelope.EntityID != "P-1" || envelope.Payload["token"] != "[REDACTED]" {
		t.Fatalf("replay = %#v", envelope)
	}
	second, err := store.AppendEvent(t.Context(), db.Event{RunID: "ws-events", Type: "proposal.approved", ActorID: "human", PayloadJSON: `{"proposal_id":"P-1"}`})
	if err != nil {
		t.Fatal(err)
	}
	var live liveEventEnvelope
	if err := conn.ReadJSON(&live); err != nil {
		t.Fatal(err)
	}
	if live.EventID != second.ID || live.Type != "proposal.approved" {
		t.Fatalf("live = %#v", live)
	}
	_ = conn.Close()
	resumed, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/workspaces/ws-events/events/ws?last_event_id="+strconv.FormatInt(first.ID, 10), header)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	_ = resumed.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resumedHello map[string]any
	if err := resumed.ReadJSON(&resumedHello); err != nil {
		t.Fatal(err)
	}
	var resumedEvent liveEventEnvelope
	if err := resumed.ReadJSON(&resumedEvent); err != nil {
		t.Fatal(err)
	}
	if resumedEvent.EventID != second.ID {
		t.Fatalf("resume = %#v", resumedEvent)
	}
}
