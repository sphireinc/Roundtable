package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"roundtable/internal/db"
	"roundtable/internal/events"
	"roundtable/internal/state"
)

func TestViewRendersSnapshotDrivenPanes(t *testing.T) {
	m := newModel(Options{
		Goal:       "ship roundtable",
		SocketPath: ".roundtable/mcp/roundtable.sock",
		RunID:      "RUN-1",
		Snapshot: state.Snapshot{
			Runs: []db.Run{
				{ID: "RUN-1", Goal: "ship roundtable", Status: "active"},
			},
			Agents: []db.Agent{
				{ID: "A-1", IsEnabled: true},
				{ID: "A-2", IsEnabled: true},
			},
			Tasks: []db.Task{
				{ID: "T-1", Title: "Bootstrap", Status: "in_progress"},
				{ID: "T-2", Title: "Review", Status: "blocked"},
			},
			Claims: []db.Claim{
				{ID: "C-1", Status: "active"},
				{ID: "C-2", Status: "suspended"},
			},
			Proposals: []db.Proposal{
				{ID: "P-1", Title: "Pending patch", Status: "pending", CreatedAt: "2026-01-01T00:00:00Z"},
			},
			VoteTallyProposalID: "P-1",
			VoteTally:           db.VoteTally{Approvals: 1, Vetoes: 1, Other: 1},
			Transactions: []db.Transaction{
				{ID: "TX-1", Status: "applied", AppliedBy: "chair", AppliedAt: "2026-01-01T00:00:00Z"},
			},
			HumanApprovals: []db.HumanApproval{
				{ID: "H-1", Status: "requested", UpdatedAt: "2026-01-01T00:00:00Z"},
			},
		},
		WatchFeed: []string{
			"proposal.created implementer-1 T-1",
			"vote.cast reviewer-1 T-1",
		},
	})
	m.width = 100
	m.height = 24

	view := m.View()
	for _, want := range []string{
		"LIVE TABLE",
		"Run: RUN-1 [active]",
		"Goal: ship roundtable",
		"Tasks: 1 active, 1 blocked",
		"Claims: 1 active, 1 suspended",
		"Proposals: 1 pending",
		"VOTES P-1 [pending]: A1 B0 V1 O1",
		"BLOCKED T-2 Review",
		"TX TX-1 [applied]",
		"WATCH FEED",
		"proposal.created implementer-1 T-1",
		"COMMAND / HUMAN TERMINAL",
		"rt> ",
		"INSPECTOR",
		"Proposal: P-1 [pending]",
		"Transaction: TX-1 [applied]",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestViewFallsBackWithoutSnapshotData(t *testing.T) {
	m := newModel(Options{
		Goal:       "",
		SocketPath: ".roundtable/mcp/roundtable.sock",
	})
	m.width = 80
	m.height = 20

	view := m.View()
	for _, want := range []string{
		"Goal: not set",
		"MCP server listening",
		"Socket: .roundtable/mcp/roundtable.s",
		"Roundtable commands; no shell",
		"No proposals, transactions, or appro",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected fallback view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestRefreshMessageUpdatesSnapshotAndWatchFeed(t *testing.T) {
	m := newModel(Options{
		Goal: "ship roundtable",
		Snapshot: state.Snapshot{
			Tasks: []db.Task{{ID: "T-1", Title: "Bootstrap", Status: "in_progress"}},
		},
		WatchFeed: []string{"proposal.created implementer-1 T-1"},
		Refresh: func(context.Context) (state.Snapshot, []string, int64, error) {
			return state.Snapshot{}, nil, 0, nil
		},
		RefreshInterval: time.Millisecond,
	})

	updated, cmd := m.Update(refreshMsg{
		snapshot: state.Snapshot{
			Tasks: []db.Task{
				{ID: "T-1", Title: "Bootstrap", Status: "in_progress"},
				{ID: "T-2", Title: "Review", Status: "blocked"},
			},
		},
		watchFeed:     []string{"vote.cast reviewer-1 T-2"},
		latestEventID: 1,
	})

	next, ok := updated.(model)
	if !ok {
		t.Fatalf("expected updated model type, got %T", updated)
	}
	if len(next.opts.Snapshot.Tasks) != 2 {
		t.Fatalf("expected refreshed tasks, got %+v", next.opts.Snapshot.Tasks)
	}
	if len(next.opts.WatchFeed) != 1 || next.opts.WatchFeed[0] != "vote.cast reviewer-1 T-2" {
		t.Fatalf("unexpected refreshed watch feed: %+v", next.opts.WatchFeed)
	}
	if cmd == nil {
		t.Fatal("expected next refresh command")
	}
	if _, ok := cmd().(refreshMsg); !ok {
		t.Fatalf("expected refresh command to produce refreshMsg")
	}
}

func TestScheduleRefreshReturnsTickCommand(t *testing.T) {
	cmd := scheduleRefresh(time.Millisecond, func(context.Context) (state.Snapshot, []string, int64, error) {
		return state.Snapshot{}, []string{"tick"}, 1, nil
	})
	if cmd == nil {
		t.Fatal("expected refresh tick command")
	}
	msg := cmd()
	if _, ok := msg.(refreshMsg); !ok {
		t.Fatalf("expected refreshMsg, got %T", msg)
	}
}

func TestCommandTerminalAcceptsInputAndShowsCommandResult(t *testing.T) {
	var executed string
	m := newModel(Options{ExecuteCommand: func(_ context.Context, command string) (string, error) {
		executed = command
		return "claim C-1 active", nil
	}})
	for _, r := range []rune("claims list") {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(model)
	}
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if command == nil || !m.commandBusy {
		t.Fatal("enter should schedule command execution")
	}
	if got := m.commandInput; got != "" {
		t.Fatalf("command input after submit = %q, want empty", got)
	}
	updated, _ = m.Update(command())
	m = updated.(model)
	if executed != "claims list" {
		t.Fatalf("executed command = %q", executed)
	}
	view := m.View()
	if !strings.Contains(view, "$ claims list") || !strings.Contains(view, "claim C-1 active") || strings.Contains(view, "working...") {
		t.Fatalf("command result not reflected in terminal:\n%s", view)
	}
}

func TestCommandTerminalBackspaceAndEmptyEnter(t *testing.T) {
	m := newModel(Options{})
	for _, r := range []rune("abc") {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(model)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	if m.commandInput != "ab" {
		t.Fatalf("input after backspace = %q", m.commandInput)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if cmd != nil || m.commandBusy || m.commandInput != "" {
		t.Fatalf("empty submit changed command state: %+v", m)
	}
}

func TestWatchEventStreamAppendsOnlyNewEventsForActiveRun(t *testing.T) {
	stream := make(chan db.Event, 1)
	m := newModel(Options{
		RunID:         "RUN-1",
		WatchFeed:     []string{"older event"},
		LatestEventID: 10,
		EventStream:   stream,
	})
	updated, _ := m.Update(watchEventMsg{event: db.Event{ID: 11, RunID: "RUN-1", Type: "task.created", ActorID: "chair", TaskID: "T-1"}, ok: true})
	m = updated.(model)
	if m.opts.LatestEventID != 11 || m.opts.WatchFeed[len(m.opts.WatchFeed)-1] != "task.created chair T-1" {
		t.Fatalf("new event not appended to watch feed: %+v", m.opts)
	}
	updated, _ = m.Update(watchEventMsg{event: db.Event{ID: 11, RunID: "RUN-1", Type: "task.created"}, ok: true})
	m = updated.(model)
	if len(m.opts.WatchFeed) != 2 {
		t.Fatalf("duplicate event changed feed: %+v", m.opts.WatchFeed)
	}
	updated, _ = m.Update(watchEventMsg{event: db.Event{ID: 12, RunID: "RUN-OTHER", Type: "task.created"}, ok: true})
	m = updated.(model)
	if m.opts.LatestEventID != 11 || len(m.opts.WatchFeed) != 2 {
		t.Fatalf("other-run event changed feed: %+v", m.opts)
	}
}

func TestWatchEventStreamReadsFromSubscription(t *testing.T) {
	bus := events.NewBus[db.Event]()
	stream := bus.Subscribe(1)
	defer bus.Unsubscribe(stream)
	m := newModel(Options{RunID: "RUN-1", EventStream: stream})
	bus.Publish(db.Event{ID: 1, RunID: "RUN-1", Type: "proposal.created"})
	updated, command := m.Update(mustReadWatchEvent(t, waitForWatchEvent(stream)))
	m = updated.(model)
	if len(m.opts.WatchFeed) != 1 || m.opts.WatchFeed[0] != "proposal.created system -" || command == nil {
		t.Fatalf("subscription event was not delivered: model=%+v command=%v", m.opts, command)
	}
}

func mustReadWatchEvent(t *testing.T, command tea.Cmd) tea.Msg {
	t.Helper()
	if command == nil {
		t.Fatal("expected watch-event wait command")
	}
	return command()
}

func TestOlderRefreshDoesNotOverwriteLiveWatchEvent(t *testing.T) {
	m := newModel(Options{
		RunID:         "RUN-1",
		LatestEventID: 12,
		WatchFeed:     []string{"event 12"},
		Refresh: func(context.Context) (state.Snapshot, []string, int64, error) {
			return state.Snapshot{}, []string{"stale event 11"}, 11, nil
		},
	})
	updated, _ := m.Update(refreshMsg{watchFeed: []string{"stale event 11"}, latestEventID: 11})
	m = updated.(model)
	if len(m.opts.WatchFeed) != 1 || m.opts.WatchFeed[0] != "event 12" || m.opts.LatestEventID != 12 {
		t.Fatalf("stale refresh rewound live event feed: %+v", m.opts)
	}
}
