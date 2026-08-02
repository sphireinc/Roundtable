package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"roundtable/internal/db"
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
		"TX TX-1 [applied]",
		"WATCH FEED",
		"proposal.created implementer-1 T-1",
		"COMMAND / HUMAN TERMINAL",
		"> show tx TX-0001",
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
		Refresh: func(context.Context) (state.Snapshot, []string, error) {
			return state.Snapshot{}, nil, nil
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
		watchFeed: []string{"vote.cast reviewer-1 T-2"},
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
	cmd := scheduleRefresh(time.Millisecond, func(context.Context) (state.Snapshot, []string, error) {
		return state.Snapshot{}, []string{"tick"}, nil
	})
	if cmd == nil {
		t.Fatal("expected refresh tick command")
	}
	msg := cmd()
	if _, ok := msg.(refreshMsg); !ok {
		t.Fatalf("expected refreshMsg, got %T", msg)
	}
}
