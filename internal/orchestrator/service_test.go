package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"roundtable/internal/config"
	"roundtable/internal/db"
)

func TestSyncAgentsSeedsConfiguredRoles(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, nil)
	service, err := NewService(root, config.Default(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncAgents(context.Background()); err != nil {
		t.Fatalf("sync agents failed: %v", err)
	}
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents failed: %v", err)
	}
	if len(agents) < 7 {
		t.Fatalf("expected seeded agents, got %+v", agents)
	}
}

func TestTickAssignsOpenTasksAndRequestsReview(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable", "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "POLICIES.ROUNDTABLE.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	service, err := NewService(root, cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncAgents(context.Background()); err != nil {
		t.Fatalf("sync agents failed: %v", err)
	}

	ctx := context.Background()
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Implement feature", BodyMD: "body", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-2", Title: "Review patch", BodyMD: "body", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "implementer-1", TaskID: "T-2", ClaimType: "write", Status: "active", ExpiresAt: "2099-01-01T00:00:00Z", Renewable: true, ResumePolicy: "hold"}); err != nil {
		t.Fatal(err)
	}
	patch := strings.Join([]string{
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n")
	if err := os.WriteFile(PatchPath(root, "P-1"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProposal(ctx, db.Proposal{ID: "P-1", TaskID: "T-2", AgentID: "implementer-1", Title: "Pending patch", SummaryMD: "summary", PatchPath: filepath.ToSlash(filepath.Join(".roundtable/patches", "P-1.diff")), Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceProposalResources(ctx, "P-1", []string{"file:README.md"}); err != nil {
		t.Fatal(err)
	}

	if err := service.Tick(ctx, "RUN-1"); err != nil {
		t.Fatalf("tick failed: %v", err)
	}

	task, err := store.GetTask(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.AssignedAgentID == "" || task.Status != "in_progress" {
		t.Fatalf("expected assigned in-progress task, got %+v", task)
	}
	proposal, err := store.GetProposal(ctx, "P-1")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "in_review" {
		t.Fatalf("expected in_review proposal, got %+v", proposal)
	}
	events, err := store.ListEvents(ctx, "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 {
		t.Fatalf("expected orchestration events, got %+v", events)
	}
}

func TestRunRepeatsUntilTaskAndProposalWorkConverges(t *testing.T) {
	root, store, service := newTestService(t)
	defer store.DB().Close()
	ctx := context.Background()
	if err := store.UpsertRun(ctx, db.Run{ID: "RUN-LOOP", Goal: "loop", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-LOOP", Title: "work", Status: "in_progress", AssignedAgentID: "implementer-1"}); err != nil {
		t.Fatal(err)
	}
	cycles := 0
	service.tick = func(ctx context.Context, runID string) error {
		cycles++
		if cycles == 3 {
			task, err := store.GetTask(ctx, "T-LOOP")
			if err != nil {
				return err
			}
			task.Status = "completed"
			if err := store.UpsertTask(ctx, task); err != nil {
				return err
			}
		}
		return nil
	}

	outcome, err := service.Run(ctx, "RUN-LOOP", LoopOptions{Interval: time.Millisecond})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if cycles != 3 || outcome.Iterations != 3 || outcome.Status != "completed" {
		t.Fatalf("expected three cycles and completed outcome, got cycles=%d outcome=%+v", cycles, outcome)
	}
	run, err := store.GetRun(ctx, "RUN-LOOP")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.EndedAt == "" {
		t.Fatalf("expected completed run status, got %+v", run)
	}
	events, err := store.ListEvents(ctx, "RUN-LOOP")
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-1].Type != "orchestrator.completed" {
		t.Fatalf("expected convergence event, got %+v", events)
	}
	_ = root
}

func TestRunRetriesCycleErrorsWithBoundedAttempts(t *testing.T) {
	_, store, service := newTestService(t)
	defer store.DB().Close()
	ctx := context.Background()
	if err := store.UpsertRun(ctx, db.Run{ID: "RUN-RETRY", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-DONE", Title: "done", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	service.tick = func(context.Context, string) error {
		calls++
		if calls < 3 {
			return errors.New("temporary database busy")
		}
		return nil
	}
	outcome, err := service.Run(ctx, "RUN-RETRY", LoopOptions{
		Interval:             time.Millisecond,
		RetryBackoff:         time.Millisecond,
		MaxConsecutiveErrors: 3,
	})
	if err != nil {
		t.Fatalf("expected retry to recover and converge, got %v", err)
	}
	if calls != 3 || outcome.Status != "completed" {
		t.Fatalf("expected recovery on third attempt, calls=%d outcome=%+v", calls, outcome)
	}
	events, err := store.ListEvents(ctx, "RUN-RETRY")
	if err != nil {
		t.Fatal(err)
	}
	retries := 0
	for _, event := range events {
		if event.Type == "orchestrator.retry" {
			retries++
		}
	}
	if retries != 2 {
		t.Fatalf("expected two durable retry events, got %d: %+v", retries, events)
	}
}

func TestRunStopsFailedAfterRetryLimit(t *testing.T) {
	_, store, service := newTestService(t)
	defer store.DB().Close()
	ctx := context.Background()
	if err := store.UpsertRun(ctx, db.Run{ID: "RUN-FAIL", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	service.tick = func(context.Context, string) error { return errors.New("persistent failure") }
	outcome, err := service.Run(ctx, "RUN-FAIL", LoopOptions{
		RetryBackoff:         time.Millisecond,
		MaxConsecutiveErrors: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "2 consecutive errors") {
		t.Fatalf("expected bounded retry failure, got outcome=%+v error=%v", outcome, err)
	}
	run, getErr := store.GetRun(ctx, "RUN-FAIL")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if run.Status != "failed" {
		t.Fatalf("expected failed run status, got %+v", run)
	}
}

func TestRunConvergesToBlockedWhenOnlyBlockedTasksRemain(t *testing.T) {
	_, store, service := newTestService(t)
	defer store.DB().Close()
	ctx := context.Background()
	if err := store.UpsertRun(ctx, db.Run{ID: "RUN-BLOCKED", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-BLOCKED", Title: "blocked", Status: "blocked"}); err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Run(ctx, "RUN-BLOCKED", LoopOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "blocked" || outcome.Reason != "all_remaining_tasks_blocked" {
		t.Fatalf("unexpected blocked outcome: %+v", outcome)
	}
}

func TestTickSchedulesTurnRequestsInFIFOOrder(t *testing.T) {
	_, store, service := newTestService(t)
	defer store.DB().Close()
	ctx := context.Background()
	if err := store.UpsertRun(ctx, db.Run{ID: "RUN-TURNS", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ agentID, reason string }{
		{"reviewer-1", "urgent review"},
		{"implementer-1", "report blocker"},
	} {
		body := `{"reason_md":"` + request.reason + `"}`
		if _, err := store.AppendEvent(ctx, db.Event{RunID: "RUN-TURNS", Type: "agent.turn_requested", ActorID: request.agentID, PayloadJSON: body}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Tick(ctx, "RUN-TURNS"); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEvents(ctx, "RUN-TURNS")
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1]; got.Type != "agent.turn_scheduled" || got.ActorID != "chair-1" || !strings.Contains(got.PayloadJSON, `"agent_id":"reviewer-1"`) {
		t.Fatalf("expected oldest request to be scheduled first, got %+v", got)
	}
	requestID := events[0].ID
	for _, eventType := range []string{"agent.turn_started", "agent.turn_completed"} {
		body := `{"request_event_id":` + fmt.Sprint(requestID) + `}`
		if _, err := store.AppendEvent(ctx, db.Event{RunID: "RUN-TURNS", Type: eventType, ActorID: "reviewer-1", PayloadJSON: body}); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Tick(ctx, "RUN-TURNS"); err != nil {
		t.Fatal(err)
	}
	events, err = store.ListEvents(ctx, "RUN-TURNS")
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1]; got.Type != "agent.turn_scheduled" || !strings.Contains(got.PayloadJSON, `"agent_id":"implementer-1"`) {
		t.Fatalf("expected next request to be scheduled second, got %+v", got)
	}
}

func newTestService(t *testing.T) (string, *db.Store, *Service) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "POLICIES.ROUNDTABLE.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(sqlDB, nil)
	service, err := NewService(root, config.Default(), store)
	if err != nil {
		sqlDB.Close()
		t.Fatal(err)
	}
	return root, store, service
}
