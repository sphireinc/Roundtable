package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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

func TestViewSanitizesTerminalControlsAcrossPanes(t *testing.T) {
	m := newModel(Options{
		Goal:      "inspect",
		RunID:     "RUN-1\x1b[2J",
		Snapshot:  state.Snapshot{Tasks: []db.Task{{ID: "T-1", Title: "task\x1b]0;title\x07", Status: "blocked"}}},
		WatchFeed: []string{"event\x1b[2J"},
	})
	m.width = 100
	m.height = 24
	m.commandOutput = []string{"command output\x1b]0;title\x07"}
	view := m.View()
	if strings.ContainsAny(view, "\x1b\x07") {
		t.Fatalf("view contains terminal control characters: %q", view)
	}
	if !strings.Contains(view, "task") || !strings.Contains(view, "event") || !strings.Contains(view, "command output") {
		t.Fatalf("sanitization removed ordinary visible content:\n%s", view)
	}
}

func TestInspectorSelectionCyclesThroughSupportedEntities(t *testing.T) {
	m := newModel(Options{Snapshot: state.Snapshot{
		Tasks:           []db.Task{{ID: "T-1", Title: "Inspect task", Status: "blocked"}},
		Proposals:       []db.Proposal{{ID: "P-1", Title: "Inspect proposal", Status: "pending"}},
		Claims:          []db.Claim{{ID: "C-1", TaskID: "T-1", AgentID: "A-1", ResourceID: "R-1", Status: "active"}},
		Agents:          []db.Agent{{ID: "A-1", Name: "Implementer", Role: "implementer", Adapter: "codex", Status: "working", IsEnabled: true}},
		Memories:        []db.MemoryEntry{{ID: "M-1", Kind: "fact", Title: "Inspect memory", Status: "active"}},
		TestRuns:        []db.TestRun{{ID: "TR-1", Command: "go test", Status: "passed", ReportedOutcome: "success"}},
		SecurityReviews: []db.SecurityReview{{ID: "SR-1", Status: "findings", SummaryMD: "Inspect security"}},
	}})
	want := []struct{ header, detail string }{
		{"Selected item: Task T-1", "Title: Inspect task"},
		{"Selected item: Proposal P-1", "Title: Inspect proposal"},
		{"Selected item: Claim C-1", "Resource: R-1"},
		{"Selected item: Agent A-1", "Name: Implementer"},
		{"Selected item: Memory M-1", "Title: Inspect memory"},
		{"Selected item: Test TR-1", "Command: go test"},
		{"Selected item: Security SR-1", "Summary: Inspect security"},
	}
	for _, expected := range want {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(model)
		lines := strings.Join(m.inspectorLines(), "\n")
		if !strings.Contains(lines, expected.header) || !strings.Contains(lines, expected.detail) {
			t.Fatalf("inspector selection missing header/detail %q / %q:\n%s", expected.header, expected.detail, lines)
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if got := m.inspectorLines()[0]; got != want[0].header {
		t.Fatalf("forward selection did not wrap: got %q, want %q", got, want[0].header)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	if got := m.inspectorLines()[0]; got != want[len(want)-1].header {
		t.Fatalf("reverse selection did not wrap: got %q, want %q", got, want[len(want)-1].header)
	}
}

func TestInspectorSelectionPersistsAcrossRefreshAndClearsWhenRemoved(t *testing.T) {
	snapshot := state.Snapshot{Tasks: []db.Task{{ID: "T-1", Title: "Inspect task", Status: "blocked"}}}
	m := newModel(Options{Snapshot: snapshot})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	updated, _ = m.Update(refreshMsg{snapshot: snapshot})
	m = updated.(model)
	if got := m.inspectorLines()[0]; got != "Selected item: Task T-1" {
		t.Fatalf("refresh lost selected entity: %q", got)
	}
	updated, _ = m.Update(refreshMsg{snapshot: state.Snapshot{}})
	m = updated.(model)
	if got := m.inspectorLines()[0]; got != "Selected item: auto" {
		t.Fatalf("removed entity retained stale selection: %q", got)
	}
}

func TestInspectorNormalizesMultilineValues(t *testing.T) {
	m := newModel(Options{Snapshot: state.Snapshot{
		Tasks:          []db.Task{{ID: "T-1\nInjected", Title: "Task title\nInjected\x1b[2J", Status: "blocked", AssignedAgentID: "A-1\nInjected"}},
		Proposals:      []db.Proposal{{ID: "P-1\nInjected", Title: "Proposal title\nInjected\x1b]0;evil\x07", Status: "pending", TaskID: "T-1\nInjected", Risk: "high\nInjected"}},
		Transactions:   []db.Transaction{{ID: "TX-1\nInjected", Status: "applied", AppliedBy: "chair\nInjected"}},
		HumanApprovals: []db.HumanApproval{{ID: "H-1\nInjected", Status: "requested"}},
	}})
	for _, tc := range []struct {
		key  tea.KeyType
		want string
	}{
		{tea.KeyTab, "Selected item: Task T-1 Injected"},
		{tea.KeyTab, "Selected item: Proposal P-1 Injected"},
	} {
		updated, _ := m.Update(tea.KeyMsg{Type: tc.key})
		m = updated.(model)
		lines := m.inspectorLines()
		if !strings.Contains(strings.Join(lines, "\n"), tc.want) {
			t.Fatalf("multiline selection was not normalized to %q: %q", tc.want, lines)
		}
		for _, line := range lines {
			if strings.ContainsAny(line, "\r\n\x1b\x07") {
				t.Fatalf("inspector line contains a terminal control: %q", line)
			}
		}
	}
	m.hasSelection = false
	for _, line := range m.inspectorLines() {
		if strings.ContainsAny(line, "\r\n") {
			t.Fatalf("automatic inspector line contains a newline: %q", line)
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
	batch, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected command and spinner batch, got %T", command())
	}
	for _, run := range batch {
		updated, _ = m.Update(run())
		m = updated.(model)
	}
	if executed != "claims list" {
		t.Fatalf("executed command = %q", executed)
	}
	view := m.View()
	if !strings.Contains(view, "$ claims list") || !strings.Contains(view, "claim C-1 active") || strings.Contains(view, "working...") {
		t.Fatalf("command result not reflected in terminal:\n%s", view)
	}
}

func TestCommandTerminalUsesSpinnerWhileCommandIsRunning(t *testing.T) {
	m := newModel(Options{ExecuteCommand: func(context.Context, string) (string, error) {
		return "done", nil
	}})
	idleFrame := m.spinner.View()
	updated, idleTick := m.Update(spinner.TickMsg{ID: m.spinner.ID()})
	m = updated.(model)
	if idleTick != nil || m.spinner.View() != idleFrame {
		t.Fatal("idle spinner tick should not update or schedule another frame")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("table")})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if !m.commandBusy {
		t.Fatal("command should be running")
	}
	lines := strings.Join(m.commandLines(), "\n")
	if !strings.Contains(lines, "working") || !strings.Contains(lines, m.spinner.View()) {
		t.Fatalf("running command indicator does not include the spinner component: %q", lines)
	}
	before := m.spinner.View()
	updated, tick := m.Update(spinner.TickMsg{ID: m.spinner.ID()})
	m = updated.(model)
	if tick == nil || m.spinner.View() == before {
		t.Fatalf("spinner tick did not advance the running indicator: before=%q after=%q", before, m.spinner.View())
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
