package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"roundtable/internal/app"
	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/mcp"
	"roundtable/internal/orchestrator"
)

func TestAcceptanceNormalPatchLifecycle(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))
	must(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644))
	output.Reset()
	must(t, app.Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-1", "--goal", "acceptance", "--headless"}, &output, &output))

	rt, cleanup := openRuntime(t, root)
	defer cleanup()
	ctx := context.Background()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id":  "T-1",
		"title":    "Update README",
		"body_md":  "Exercise normal patch lifecycle",
		"priority": 1,
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "implementer-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "implementer-1",
		"title":              "Patch README",
		"summary_md":         "Normal acceptance patch",
		"affected_resources": []any{"file:README.md"},
		"patch":              filePatch("README.md", "hello", "hello world"),
	})
	mustCall(t, rt, "proposal.request_review", map[string]any{"proposal_id": "P-1"})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "architect-1",
		"vote":        "approve",
		"reason_md":   "Looks coherent",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-2",
		"proposal_id": "P-1",
		"agent_id":    "reviewer-1",
		"vote":        "approve",
		"reason_md":   "Looks correct",
	})

	validation := mustCall(t, rt, "patch.validate", map[string]any{"proposal_id": "P-1"})
	if valid, _ := validation["valid"].(bool); !valid {
		t.Fatalf("expected valid patch lifecycle, got %+v", validation)
	}

	applied := mustCall(t, rt, "patch.apply", map[string]any{
		"proposal_id":    "P-1",
		"run_id":         "RUN-1",
		"transaction_id": "TX-1",
	})
	if applied["transaction"].(db.Transaction).ID != "TX-1" {
		t.Fatalf("unexpected apply result: %+v", applied)
	}

	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\n" {
		t.Fatalf("unexpected repo content: %q", string(data))
	}

	watch := mustCall(t, rt, "table.watch", map[string]any{"run_id": "RUN-1", "limit": 10})
	if len(watch["transactions"].([]db.Transaction)) == 0 {
		t.Fatalf("expected transaction in watch view, got %+v", watch)
	}
	if len(watch["events"].([]db.Event)) == 0 {
		t.Fatalf("expected events in watch view, got %+v", watch)
	}
	_ = ctx
}

func TestAcceptanceHighRiskApprovalLifecycle(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))
	must(t, os.MkdirAll(filepath.Join(root, "auth"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "auth", "session.txt"), []byte("old\n"), 0o644))
	output.Reset()
	must(t, app.Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-2", "--goal", "high-risk acceptance", "--headless"}, &output, &output))

	rt, cleanup := openRuntime(t, root)
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-2",
		"title":   "Patch auth",
		"body_md": "Exercise human/security acceptance path",
		"risk":    "high",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-2",
		"claim_id":      "C-2",
		"agent_id":      "implementer-1",
		"task_id":       "T-2",
		"resource_type": "file",
		"resource_id":   "file:auth/session.txt",
		"path":          "auth/session.txt",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-2",
		"task_id":            "T-2",
		"agent_id":           "implementer-1",
		"title":              "Patch auth path",
		"summary_md":         "High-risk acceptance patch",
		"affected_resources": []any{"file:auth/session.txt"},
		"risk":               "high",
		"patch":              filePatch("auth/session.txt", "old", "new"),
	})
	mustCall(t, rt, "proposal.request_review", map[string]any{"proposal_id": "P-2"})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-3",
		"proposal_id": "P-2",
		"agent_id":    "architect-1",
		"vote":        "approve",
		"reason_md":   "Architecture holds",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-4",
		"proposal_id": "P-2",
		"agent_id":    "reviewer-1",
		"vote":        "approve",
		"reason_md":   "Review complete",
	})

	validation := mustCall(t, rt, "patch.validate", map[string]any{"proposal_id": "P-2"})
	if required, _ := validation["security_review_required"].(bool); !required {
		t.Fatalf("expected security review requirement, got %+v", validation)
	}
	if required, _ := validation["human_approval_applied"].(bool); required {
		t.Fatalf("expected no human approval yet, got %+v", validation)
	}

	mustCall(t, rt, "security.review", map[string]any{
		"review_id":   "SR-1",
		"proposal_id": "P-2",
		"reviewer_id": "security-1",
		"status":      "approved",
		"summary_md":  "Security reviewed",
	})
	mustCall(t, rt, "human.request_approval", map[string]any{
		"approval_id":     "H-1",
		"proposal_id":     "P-2",
		"subject":         "Approve high-risk auth change",
		"reason_md":       "Acceptance flow",
		"status":          "approved",
		"requested_by":    "chair-1",
		"decided_by":      "human-1",
		"decision_md":     "Approved for acceptance test",
		"override_policy": false,
	})

	applied := mustCall(t, rt, "patch.apply", map[string]any{
		"proposal_id":    "P-2",
		"run_id":         "RUN-2",
		"transaction_id": "TX-2",
	})
	if applied["human_approval"].(db.HumanApproval).ID != "H-1" {
		t.Fatalf("expected human approval on apply result, got %+v", applied)
	}
	if applied["security_review"].(db.SecurityReview).ID != "SR-1" {
		t.Fatalf("expected security review on apply result, got %+v", applied)
	}

	data, err := os.ReadFile(filepath.Join(root, "auth", "session.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected high-risk repo content: %q", string(data))
	}
}

func TestAcceptanceClaimContentionRejectsConcurrentWriteClaims(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))
	must(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644))
	output.Reset()
	must(t, app.Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-3", "--goal", "claim contention", "--headless"}, &output, &output))

	rt, cleanup := openRuntime(t, root)
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-3",
		"title":   "First writer",
		"body_md": "Acquire initial claim",
	})
	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-4",
		"title":   "Second writer",
		"body_md": "Attempt conflicting claim",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-3",
		"claim_id":      "C-3",
		"agent_id":      "implementer-1",
		"task_id":       "T-3",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})

	_, err := rt.Call(context.Background(), "resource.claim", map[string]any{
		"run_id":        "RUN-3",
		"claim_id":      "C-4",
		"agent_id":      "reviewer-1",
		"task_id":       "T-4",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	if err == nil || !strings.Contains(err.Error(), "claim conflict") {
		t.Fatalf("expected claim conflict, got %v", err)
	}

	store := openStore(t, root)
	defer store.close(t)
	claims, err := store.store.ListClaims(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].ID != "C-3" {
		t.Fatalf("expected only the original claim to persist, got %+v", claims)
	}
}

func TestAcceptanceLongRunningSessionLifecycle(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))
	output.Reset()
	must(t, app.Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-4", "--goal", "session lifecycle", "--headless"}, &output, &output))

	output.Reset()
	must(t, app.Run(context.Background(), []string{
		"sessions", "register",
		"--root", root,
		"--id", "S-4",
		"--agent", "implementer-1",
		"--run", "RUN-4",
		"--adapter", "codex",
		"--external-session-id", "ext-4",
		"--cwd", root,
	}, &output, &output))
	time.Sleep(10 * time.Millisecond)
	output.Reset()
	must(t, app.Run(context.Background(), []string{
		"sessions", "heartbeat",
		"--root", root,
		"--session", "S-4",
		"--status", "busy",
		"--external-session-id", "ext-4a",
	}, &output, &output))
	time.Sleep(10 * time.Millisecond)
	output.Reset()
	must(t, app.Run(context.Background(), []string{
		"sessions", "heartbeat",
		"--root", root,
		"--session", "S-4",
		"--status", "idle",
	}, &output, &output))
	output.Reset()
	must(t, app.Run(context.Background(), []string{
		"sessions", "end",
		"--root", root,
		"--session", "S-4",
	}, &output, &output))

	store := openStore(t, root)
	defer store.close(t)
	session, err := store.store.GetAgentSession(context.Background(), "S-4")
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "ended" || session.ExternalSessionID != "ext-4a" || session.EndedAt == "" {
		t.Fatalf("unexpected final session state: %+v", session)
	}
	events, err := store.store.ListAgentSessionEvents(context.Background(), "S-4")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("expected register, 2 heartbeats, and end events; got %+v", events)
	}
	if events[0].EventType != "registered" || events[1].EventType != "heartbeat" || events[2].EventType != "heartbeat" || events[3].EventType != "ended" {
		t.Fatalf("unexpected session event sequence: %+v", events)
	}
}

func TestAcceptanceHighRiskApplyRejectedWithoutApprovals(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))
	must(t, os.MkdirAll(filepath.Join(root, "auth"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "auth", "session.txt"), []byte("old\n"), 0o644))
	output.Reset()
	must(t, app.Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-5", "--goal", "rejection path", "--headless"}, &output, &output))

	rt, cleanup := openRuntime(t, root)
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-5",
		"title":   "Patch auth",
		"body_md": "Exercise missing-approval rejection path",
		"risk":    "high",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-5",
		"claim_id":      "C-5",
		"agent_id":      "implementer-1",
		"task_id":       "T-5",
		"resource_type": "file",
		"resource_id":   "file:auth/session.txt",
		"path":          "auth/session.txt",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-5",
		"task_id":            "T-5",
		"agent_id":           "implementer-1",
		"title":              "Patch auth path",
		"summary_md":         "Rejected high-risk patch",
		"affected_resources": []any{"file:auth/session.txt"},
		"risk":               "high",
		"patch":              filePatch("auth/session.txt", "old", "new"),
	})
	mustCall(t, rt, "proposal.request_review", map[string]any{"proposal_id": "P-5"})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-5",
		"proposal_id": "P-5",
		"agent_id":    "architect-1",
		"vote":        "approve",
		"reason_md":   "Architecture holds",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-6",
		"proposal_id": "P-5",
		"agent_id":    "reviewer-1",
		"vote":        "approve",
		"reason_md":   "Review complete",
	})

	_, err := rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id":    "P-5",
		"run_id":         "RUN-5",
		"transaction_id": "TX-5",
	})
	if err == nil || !strings.Contains(err.Error(), "proposal not approvable") {
		t.Fatalf("expected approval/policy rejection, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "auth", "session.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\n" {
		t.Fatalf("expected repo to remain unchanged, got %q", string(data))
	}

	store := openStore(t, root)
	defer store.close(t)
	transactions, err := store.store.ListTransactions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 0 {
		t.Fatalf("expected no transactions on rejected apply, got %+v", transactions)
	}
}

func TestAcceptanceInterruptedApplyDoesNotPersistTransaction(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))
	must(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644))
	for i := 0; i < 400; i++ {
		path := filepath.Join(root, "fixtures", "blob-"+strconv.Itoa(i)+".txt")
		must(t, os.MkdirAll(filepath.Dir(path), 0o755))
		must(t, os.WriteFile(path, []byte(strings.Repeat("payload\n", 32)), 0o644))
	}
	output.Reset()
	must(t, app.Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-6", "--goal", "interrupted apply", "--headless"}, &output, &output))

	rt, cleanup := openRuntime(t, root)
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-6",
		"title":   "Patch README",
		"body_md": "Exercise interrupted apply path",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-6",
		"claim_id":      "C-6",
		"agent_id":      "implementer-1",
		"task_id":       "T-6",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-6",
		"task_id":            "T-6",
		"agent_id":           "implementer-1",
		"title":              "Patch README",
		"summary_md":         "Interrupted apply patch",
		"affected_resources": []any{"file:README.md"},
		"patch":              filePatch("README.md", "hello", "hello world"),
	})
	mustCall(t, rt, "proposal.request_review", map[string]any{"proposal_id": "P-6"})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-7",
		"proposal_id": "P-6",
		"agent_id":    "architect-1",
		"vote":        "approve",
		"reason_md":   "Looks coherent",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-8",
		"proposal_id": "P-6",
		"agent_id":    "reviewer-1",
		"vote":        "approve",
		"reason_md":   "Looks correct",
	})

	validation := mustCall(t, rt, "patch.validate", map[string]any{"proposal_id": "P-6"})
	if valid, _ := validation["valid"].(bool); !valid {
		t.Fatalf("expected proposal to validate before interruption, got %+v", validation)
	}

	stopMutator := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMutator:
				return
			case <-ticker.C:
				_ = os.WriteFile(filepath.Join(root, "README.md"), []byte("concurrent mutation\n"), 0o644)
			}
		}
	}()

	_, err := rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id":    "P-6",
		"run_id":         "RUN-6",
		"transaction_id": "TX-6",
	})
	close(stopMutator)
	if err == nil || (!strings.Contains(err.Error(), "proposal validation failed") && !strings.Contains(err.Error(), "patch apply failed")) {
		t.Fatalf("expected apply failure under concurrent mutation, got %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "hello world\n" {
		t.Fatalf("expected proposed patch not to land after interrupted apply, got %q", string(data))
	}

	store := openStore(t, root)
	defer store.close(t)
	transactions, err := store.store.ListTransactions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(transactions) != 0 {
		t.Fatalf("expected no transaction persisted on interrupted apply, got %+v", transactions)
	}
	proposal, err := store.store.GetProposal(context.Background(), "P-6")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "in_review" {
		t.Fatalf("expected proposal to remain unapplied, got %+v", proposal)
	}
}

func TestAcceptanceContinuousOrchestratorMCPTurnLifecycle(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder
	must(t, app.Run(context.Background(), []string{"init", "--root", root}, &output, &output))

	projectConfig, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	opened := openStore(t, root)
	defer opened.close(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := opened.store.UpsertRun(ctx, db.Run{ID: "RUN-LOOP", Goal: "continuous loop", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := opened.store.UpsertTask(ctx, db.Task{ID: "T-LOOP", Title: "Long running task", BodyMD: "body", Status: "in_progress", AssignedAgentID: "implementer-1"}); err != nil {
		t.Fatal(err)
	}
	chair, err := orchestrator.NewService(root, projectConfig, opened.store)
	if err != nil {
		t.Fatal(err)
	}
	if err := chair.SyncAgents(ctx); err != nil {
		t.Fatal(err)
	}
	runtime, cleanup, err := mcp.OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	runDone := make(chan struct {
		outcome orchestrator.RunOutcome
		err     error
	}, 1)
	go func() {
		outcome, err := chair.Run(ctx, "RUN-LOOP", orchestrator.LoopOptions{Interval: 5 * time.Millisecond})
		runDone <- struct {
			outcome orchestrator.RunOutcome
			err     error
		}{outcome: outcome, err: err}
	}()

	request, err := runtime.Call(ctx, "agent.turn_request", map[string]any{
		"run_id":    "RUN-LOOP",
		"agent_id":  "implementer-1",
		"reason_md": "I have a blocker to report before continuing",
	})
	if err != nil {
		t.Fatal(err)
	}
	requestEventID := int64(request["request_event_id"].(int64))
	waitUntil(t, 3*time.Second, func() (bool, error) {
		events, err := opened.store.ListEvents(ctx, "RUN-LOOP")
		if err != nil {
			return false, err
		}
		for _, event := range events {
			if event.Type != "agent.turn_scheduled" {
				continue
			}
			var payload struct {
				RequestEventID int64 `json:"request_event_id"`
			}
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return false, err
			}
			if payload.RequestEventID == requestEventID {
				return true, nil
			}
		}
		return false, nil
	})
	if _, err := runtime.Call(ctx, "agent.turn_start", map[string]any{
		"run_id": "RUN-LOOP", "agent_id": "implementer-1", "request_event_id": requestEventID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Call(ctx, "agent.turn_complete", map[string]any{
		"run_id": "RUN-LOOP", "agent_id": "implementer-1", "request_event_id": requestEventID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Call(ctx, "task.update_status", map[string]any{"task_id": "T-LOOP", "status": "completed"}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-runDone:
		if result.err != nil || result.outcome.Status != "completed" {
			t.Fatalf("continuous loop did not converge: outcome=%+v err=%v", result.outcome, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for continuous orchestration to converge")
	}
}

func openRuntime(t *testing.T, root string) (*mcp.Runtime, func()) {
	t.Helper()
	rt, cleanup, err := mcp.OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	return rt, cleanup
}

func mustCall(t *testing.T, rt *mcp.Runtime, tool string, args map[string]any) map[string]any {
	t.Helper()
	result, err := rt.Call(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("%s failed: %v", tool, err)
	}
	return result
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type testStore struct {
	db    interface{ Close() error }
	store *db.Store
}

func openStore(t *testing.T, root string) testStore {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	return testStore{
		db:    sqlDB,
		store: db.NewStore(sqlDB, nil),
	}
}

func (s testStore) close(t *testing.T) {
	t.Helper()
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitUntil(t *testing.T, timeout time.Duration, check func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ok, err := check()
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func filePatch(path, before, after string) string {
	return strings.Join([]string{
		"diff --git a/" + path + " b/" + path,
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1 +1 @@",
		"-" + before,
		"+" + after,
		"",
	}, "\n")
}
