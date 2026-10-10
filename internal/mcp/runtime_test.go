package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roundtable/internal/db"
	"roundtable/internal/policy"
	"roundtable/internal/security"
	"roundtable/internal/symbols"
)

func TestRuntimeResourceClaimAndStatus(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	result, err := rt.Call(context.Background(), "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"path":          "README.md",
		"claim_type":    "write",
	})
	if err != nil {
		t.Fatalf("resource.claim failed: %v", err)
	}
	if _, ok := result["claim"]; !ok {
		t.Fatalf("expected claim in result: %+v", result)
	}

	status, err := rt.Call(context.Background(), "resource.claim_status", map[string]any{"agent_id": "A-1"})
	if err != nil {
		t.Fatalf("resource.claim_status failed: %v", err)
	}
	claims := status["claims"].([]db.Claim)
	if len(claims) != 1 || claims[0].ID != "C-1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if claims[0].BaseHash == "" {
		t.Fatalf("expected runtime to auto-populate base hash, got %+v", claims[0])
	}
}

func TestRuntimeRepoSymbols(t *testing.T) {
	root := newRuntimeRoot(t)
	fixtures := []struct{ filename, source, language, name string }{
		{"sample.go", "package sample\nfunc Build() {}\n", "go", "Build"},
		{"sample.ts", "export function build() {}\n", "typescript", "build"},
		{"sample.tsx", "const View = () => <main />\n", "typescript", "View"},
		{"sample.py", "class MemoryOracle:\n    pass\n\nasync def query_memory():\n    pass\n", "python", "MemoryOracle"},
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	for _, fixture := range fixtures {
		t.Run(fixture.filename, func(t *testing.T) {
			sourcePath := filepath.Join(root, fixture.filename)
			if err := os.WriteFile(sourcePath, []byte(fixture.source), 0o644); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Call(context.Background(), "repo.symbols", map[string]any{"path": sourcePath})
			if err != nil {
				t.Fatalf("repo.symbols failed: %v", err)
			}
			found := result["symbols"].([]symbols.Symbol)
			if len(found) == 0 {
				t.Fatalf("expected symbols, got %+v", result)
			}
			for _, symbol := range found {
				if symbol.Name == fixture.name && symbol.Language == fixture.language {
					return
				}
			}
			t.Fatalf("expected %s (%s), got %+v", fixture.name, fixture.language, found)
		})
	}
}

func TestRuntimeTaskAndMemoryFlow(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	created, err := rt.Call(context.Background(), "task.create", map[string]any{
		"task_id":  "T-1",
		"title":    "Bootstrap runtime",
		"body_md":  "Build MCP task support",
		"priority": 10,
	})
	if err != nil {
		t.Fatalf("task.create failed: %v", err)
	}
	task := created["task"].(db.Task)
	if task.ID != "T-1" || task.Priority != 10 {
		t.Fatalf("unexpected task: %+v", task)
	}

	gotTask, err := rt.Call(context.Background(), "task.get", map[string]any{"task_id": "T-1"})
	if err != nil {
		t.Fatalf("task.get failed: %v", err)
	}
	if gotTask["task"].(db.Task).Title != "Bootstrap runtime" {
		t.Fatalf("unexpected task.get result: %+v", gotTask)
	}

	updated, err := rt.Call(context.Background(), "task.update_status", map[string]any{
		"task_id":           "T-1",
		"status":            "in_progress",
		"assigned_agent_id": "A-1",
	})
	if err != nil {
		t.Fatalf("task.update_status failed: %v", err)
	}
	if updated["task"].(db.Task).Status != "in_progress" {
		t.Fatalf("unexpected task.update_status result: %+v", updated)
	}

	recorded, err := rt.Call(context.Background(), "memory.record", map[string]any{
		"memory_id":  "M-1",
		"scope":      "project",
		"kind":       "decision",
		"title":      "Use SQLite",
		"body_md":    "SQLite is authoritative",
		"importance": 90,
	})
	if err != nil {
		t.Fatalf("memory.record failed: %v", err)
	}
	if recorded["entry"].(db.MemoryEntry).ID != "M-1" {
		t.Fatalf("unexpected memory.record result: %+v", recorded)
	}

	queryResult, err := rt.Call(context.Background(), "memory.query", map[string]any{
		"query": "sqlite",
		"limit": 5,
	})
	if err != nil {
		t.Fatalf("memory.query failed: %v", err)
	}
	entries := queryResult["entries"].([]db.MemoryEntry)
	if len(entries) != 1 || entries[0].ID != "M-1" {
		t.Fatalf("unexpected memory.query results: %+v", entries)
	}
}

func TestAgentTurnRequestsAreValidatedDeduplicatedAndQueued(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	if err := rt.store.UpsertRun(ctx, db.Run{ID: "RUN-TURNS", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	for _, agentID := range []string{"reviewer-1", "implementer-1"} {
		if err := rt.store.UpsertAgent(ctx, db.Agent{ID: agentID, Role: "agent", Name: agentID, Adapter: "generic", IsEnabled: true}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := rt.Call(ctx, "agent.turn_request", map[string]any{
		"run_id":    "RUN-TURNS",
		"agent_id":  "reviewer-1",
		"reason_md": "Found a blocking review issue",
	})
	if err != nil {
		t.Fatalf("first turn request failed: %v", err)
	}
	if first["queue_position"] != 1 || first["duplicate"] != false {
		t.Fatalf("unexpected first turn request: %+v", first)
	}
	second, err := rt.Call(ctx, "agent.turn_request", map[string]any{
		"run_id":    "RUN-TURNS",
		"agent_id":  "implementer-1",
		"reason_md": "Need to report a blocker",
	})
	if err != nil {
		t.Fatalf("second turn request failed: %v", err)
	}
	if second["queue_position"] != 2 {
		t.Fatalf("expected second request at queue position 2, got %+v", second)
	}
	duplicate, err := rt.Call(ctx, "agent.turn_request", map[string]any{
		"run_id":    "RUN-TURNS",
		"agent_id":  "reviewer-1",
		"reason_md": "Repeated request",
	})
	if err != nil {
		t.Fatalf("duplicate turn request failed: %v", err)
	}
	if duplicate["duplicate"] != true || duplicate["request_event_id"] != first["request_event_id"] || duplicate["queue_position"] != 1 {
		t.Fatalf("expected existing outstanding request to be returned, got %+v", duplicate)
	}
	events, err := rt.store.ListEvents(ctx, "RUN-TURNS")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ActorID != "reviewer-1" || events[1].ActorID != "implementer-1" {
		t.Fatalf("duplicate request should not append an event: %+v", events)
	}
	requestID := int64(first["request_event_id"].(int64))
	if _, err := rt.store.AppendEvent(ctx, db.Event{
		RunID:       "RUN-TURNS",
		Type:        "agent.turn_scheduled",
		ActorID:     "chair-1",
		PayloadJSON: fmt.Sprintf(`{"request_event_id":%d,"agent_id":"reviewer-1"}`, requestID),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Call(ctx, "agent.turn_start", map[string]any{
		"run_id": "RUN-TURNS", "agent_id": "reviewer-1", "request_event_id": requestID,
	})
	if err != nil {
		t.Fatalf("scheduled turn start failed: %v", err)
	}
	if _, err := rt.Call(ctx, "agent.turn_complete", map[string]any{
		"run_id": "RUN-TURNS", "agent_id": "reviewer-1", "request_event_id": requestID,
	}); err != nil {
		t.Fatalf("turn completion failed: %v", err)
	}
	events, err = rt.store.ListEvents(ctx, "RUN-TURNS")
	if err != nil {
		t.Fatal(err)
	}
	if events[len(events)-2].Type != "agent.turn_started" || events[len(events)-1].Type != "agent.turn_completed" {
		t.Fatalf("expected durable turn lifecycle events, got %+v", events)
	}
}

func TestTurnLifecycleToolsRequireRequestEventID(t *testing.T) {
	registry := DefaultRegistry()
	for _, name := range []string{"agent.turn_start", "agent.turn_complete"} {
		var selected *Tool
		for _, candidate := range registry.Tools() {
			if candidate.Name == name {
				selected = &candidate
				break
			}
		}
		if selected == nil {
			t.Fatalf("tool %s not registered", name)
		}
		required, ok := selected.InputSchema["required"].([]string)
		if !ok {
			t.Fatalf("tool %s has malformed required fields: %#v", name, selected.InputSchema["required"])
		}
		found := false
		for _, field := range required {
			found = found || field == "request_event_id"
		}
		if !found {
			t.Fatalf("tool %s must require request_event_id, got %v", name, required)
		}
		properties := selected.InputSchema["properties"].(map[string]any)
		if properties["request_event_id"].(map[string]any)["type"] != "integer" {
			t.Fatalf("tool %s request_event_id must be an integer", name)
		}
	}
}

func TestTableWatchSupportsIncrementalEventCursor(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := rt.store.AppendEvent(ctx, db.Event{
			RunID: "RUN-WATCH", Type: fmt.Sprintf("event.%d", i), PayloadJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := rt.Call(ctx, "table.watch", map[string]any{
		"run_id": "RUN-WATCH", "after_event_id": int64(0), "limit": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstEvents := first["events"].([]db.Event)
	if len(firstEvents) != 2 || first["has_more_events"] != true {
		t.Fatalf("expected first bounded event page, got %+v", first)
	}
	firstCursor := first["next_after_event_id"].(int64)
	if firstCursor != firstEvents[1].ID {
		t.Fatalf("expected cursor to advance to last returned event, got %+v", first)
	}

	second, err := rt.Call(ctx, "table.watch", map[string]any{
		"run_id": "RUN-WATCH", "after_event_id": firstCursor, "limit": 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondEvents := second["events"].([]db.Event)
	if len(secondEvents) != 2 || second["has_more_events"] != false || secondEvents[0].ID <= firstCursor {
		t.Fatalf("expected remaining event page without a gap, got %+v", second)
	}
	if _, err := rt.Call(ctx, "table.watch", map[string]any{"run_id": "RUN-WATCH", "after_event_id": -1}); err == nil {
		t.Fatal("expected negative event cursor to be rejected")
	}
}

func TestAgentTurnRequestRejectsDisabledAgentAndInactiveRun(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	if err := rt.store.UpsertAgent(ctx, db.Agent{ID: "disabled", Role: "agent", Name: "disabled", Adapter: "generic", IsEnabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := rt.store.UpsertRun(ctx, db.Run{ID: "RUN-CLOSED", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Call(ctx, "agent.turn_request", map[string]any{
		"run_id": "RUN-CLOSED", "agent_id": "disabled", "reason_md": "help",
	})
	if err == nil || !strings.Contains(err.Error(), "completed") {
		t.Fatalf("expected inactive run rejection, got %v", err)
	}
	if err := rt.store.UpsertRun(ctx, db.Run{ID: "RUN-OPEN", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	_, err = rt.Call(ctx, "agent.turn_request", map[string]any{
		"run_id": "RUN-OPEN", "agent_id": "disabled", "reason_md": "help",
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled agent rejection, got %v", err)
	}
}

func TestRuntimeMemorySummarizeAndMarkStale(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := rt.Call(context.Background(), "memory.record", map[string]any{
		"memory_id":  "M-1",
		"scope":      "T-1",
		"kind":       "decision",
		"title":      "Use SQLite",
		"body_md":    "SQLite is authoritative",
		"importance": 90,
	}); err != nil {
		t.Fatalf("memory.record M-1 failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "memory.record", map[string]any{
		"memory_id":  "M-2",
		"scope":      "T-1",
		"kind":       "note",
		"title":      "Follow-up",
		"body_md":    "Need rollback coverage",
		"importance": 70,
	}); err != nil {
		t.Fatalf("memory.record M-2 failed: %v", err)
	}

	summarized, err := rt.Call(context.Background(), "memory.summarize", map[string]any{
		"task_id": "T-1",
	})
	if err != nil {
		t.Fatalf("memory.summarize failed: %v", err)
	}
	if summarized["entry_count"].(int) != 2 {
		t.Fatalf("expected 2 summary entries, got %+v", summarized)
	}
	summaryMD := summarized["summary_md"].(string)
	if !strings.Contains(summaryMD, "# Memory Summary") || !strings.Contains(summaryMD, "Use SQLite") || !strings.Contains(summaryMD, "Need rollback coverage") {
		t.Fatalf("unexpected summary output: %s", summaryMD)
	}

	staled, err := rt.Call(context.Background(), "memory.mark_stale", map[string]any{
		"memory_id": "M-1",
		"reason_md": "Superseded by newer persistence decision",
	})
	if err != nil {
		t.Fatalf("memory.mark_stale failed: %v", err)
	}
	entry := staled["entry"].(db.MemoryEntry)
	if entry.Status != "stale" || !strings.Contains(entry.BodyMD, "Stale reason: Superseded by newer persistence decision") {
		t.Fatalf("unexpected stale memory entry: %+v", entry)
	}

	summarized, err = rt.Call(context.Background(), "memory.summarize", map[string]any{
		"task_id": "T-1",
	})
	if err != nil {
		t.Fatalf("memory.summarize after stale failed: %v", err)
	}
	if summarized["entry_count"].(int) != 1 {
		t.Fatalf("expected stale entry to be excluded, got %+v", summarized)
	}
	summaryMD = summarized["summary_md"].(string)
	if strings.Contains(summaryMD, "Use SQLite") || !strings.Contains(summaryMD, "Follow-up") {
		t.Fatalf("unexpected post-stale summary output: %s", summaryMD)
	}
}

func TestRuntimeTestingTools(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := rt.Call(context.Background(), "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Testing flow",
		"body_md": "Verify test tools",
		"risk":    "high",
	}); err != nil {
		t.Fatalf("task.create failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	}); err != nil {
		t.Fatalf("resource.claim failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Test change",
		"summary_md":         "Proposal for test suggestions",
		"affected_resources": []any{"file:README.md"},
		"risk":               "high",
		"patch":              testReadmePatch("hello\n", "hello tests\n"),
	}); err != nil {
		t.Fatalf("proposal.create failed: %v", err)
	}

	suggested, err := rt.Call(context.Background(), "test.suggest", map[string]any{
		"proposal_id": "P-1",
	})
	if err != nil {
		t.Fatalf("test.suggest failed: %v", err)
	}
	commands := suggested["commands"].([]string)
	if len(commands) == 0 || commands[0] == "" {
		t.Fatalf("expected suggested commands, got %+v", suggested)
	}

	runResult, err := rt.Call(context.Background(), "test.run", map[string]any{
		"test_run_id": "TR-1",
		"proposal_id": "P-1",
		"task_id":     "T-1",
		"command":     "printf 'PASS\\n'",
	})
	if err != nil {
		t.Fatalf("test.run failed: %v", err)
	}
	testRun := runResult["test_run"].(db.TestRun)
	if testRun.ID != "TR-1" || testRun.Status != "passed" || testRun.LogPath == "" {
		t.Fatalf("unexpected test run result: %+v", testRun)
	}
	if !strings.Contains(runResult["log"].(string), "PASS") {
		t.Fatalf("unexpected test log: %+v", runResult)
	}

	got, err := rt.Call(context.Background(), "test.get_result", map[string]any{
		"test_run_id": "TR-1",
	})
	if err != nil {
		t.Fatalf("test.get_result failed: %v", err)
	}
	stored := got["test_run"].(db.TestRun)
	if stored.ID != "TR-1" || stored.Status != "passed" {
		t.Fatalf("unexpected stored test run: %+v", stored)
	}
	if !strings.Contains(got["log"].(string), "PASS") {
		t.Fatalf("expected persisted log, got %+v", got)
	}
}

func TestRuntimeTableWatchProjection(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Blocked task",
		"body_md": "watch projection",
		"status":  "blocked",
	})
	mustCall(t, rt, "human.request_approval", map[string]any{
		"approval_id": "H-1",
		"subject":     "Approve risky change",
		"reason_md":   "Needed for watch output",
	})
	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-2",
		"title":   "Proposal task",
		"body_md": "proposal flow",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-2",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-2",
		"agent_id":           "A-1",
		"title":              "Pending proposal",
		"summary_md":         "watch projection",
		"affected_resources": []any{"file:README.md"},
		"patch":              testReadmePatch("hello\n", "hello watch\n"),
	})
	if _, err := rt.store.AppendEvent(context.Background(), db.Event{RunID: "RUN-1", Type: "proposal.created", ActorID: "A-1", TaskID: "T-2", PayloadJSON: `{"proposal_id":"P-1"}`}); err != nil {
		t.Fatalf("append event failed: %v", err)
	}
	if err := rt.store.UpsertTransaction(context.Background(), db.Transaction{ID: "TX-1", ProposalID: "P-1", RunID: "RUN-1", Status: "applied", AppliedBy: "chair", AppliedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("upsert transaction failed: %v", err)
	}

	result, err := rt.Call(context.Background(), "table.watch", map[string]any{
		"run_id": "RUN-1",
		"limit":  5,
	})
	if err != nil {
		t.Fatalf("table.watch failed: %v", err)
	}
	if result["active_claims"].(int) != 1 {
		t.Fatalf("expected active claim count, got %+v", result)
	}
	if len(result["blocked_tasks"].([]db.Task)) != 1 {
		t.Fatalf("expected blocked task, got %+v", result)
	}
	if len(result["pending_proposals"].([]db.Proposal)) != 1 {
		t.Fatalf("expected pending proposal, got %+v", result)
	}
	if len(result["required_approvals"].([]db.HumanApproval)) != 1 {
		t.Fatalf("expected required approval, got %+v", result)
	}
	if len(result["transactions"].([]db.Transaction)) != 1 {
		t.Fatalf("expected transaction, got %+v", result)
	}
	events := result["events"].([]db.Event)
	if len(events) < 1 || events[len(events)-1].Type != "proposal.created" {
		t.Fatalf("expected recent proposal event, got %+v", result)
	}
}

func TestRuntimeImplementsPatchPolicyHumanSecurityToolRoutes(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	cases := []struct {
		tool string
		args map[string]any
	}{
		{tool: "patch.validate", args: map[string]any{}},
		{tool: "patch.reject", args: map[string]any{}},
		{tool: "patch.apply", args: map[string]any{}},
		{tool: "human.request_approval", args: map[string]any{}},
		{tool: "human.approval_status", args: map[string]any{}},
		{tool: "security.review", args: map[string]any{}},
		{tool: "proposal.request_review", args: map[string]any{}},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			_, err := rt.Call(context.Background(), tc.tool, tc.args)
			if err == nil {
				return
			}
			if strings.Contains(err.Error(), "tool not implemented") {
				t.Fatalf("expected %s to be routed, got %v", tc.tool, err)
			}
		})
	}
}

func TestRuntimeRepoReadAndSearch(t *testing.T) {
	root := newRuntimeRoot(t)
	sourcePath := filepath.Join(root, "notes.txt")
	content := "roundtable mcp runtime\nshared state\n"
	if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	readResult, err := rt.Call(context.Background(), "repo.read_file", map[string]any{"path": "notes.txt"})
	if err != nil {
		t.Fatalf("repo.read_file failed: %v", err)
	}
	if readResult["content"].(string) != content {
		t.Fatalf("unexpected repo.read_file content: %+v", readResult)
	}

	searchResult, err := rt.Call(context.Background(), "repo.search", map[string]any{"query": "shared"})
	if err != nil {
		t.Fatalf("repo.search failed: %v", err)
	}
	matches := searchResult["matches"].([]map[string]any)
	if len(matches) == 0 {
		t.Fatalf("expected at least one search match: %+v", searchResult)
	}
}

func TestRuntimeProposalVoteDecisionFlow(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := rt.Call(context.Background(), "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Add proposal flow",
		"body_md": "Implement proposal runtime handlers",
	}); err != nil {
		t.Fatalf("task.create failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	}); err != nil {
		t.Fatalf("resource.claim failed: %v", err)
	}

	created, err := rt.Call(context.Background(), "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Update README",
		"summary_md":         "Adjust README content",
		"affected_resources": []any{"file:README.md"},
		"risk":               "normal",
		"patch":              testReadmePatch("hello\n", "hello world\n"),
	})
	if err != nil {
		t.Fatalf("proposal.create failed: %v", err)
	}
	proposal := created["proposal"].(db.Proposal)
	if proposal.ID != "P-1" {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	proposalResources := created["proposal_resources"].([]db.ProposalResource)
	if len(proposalResources) != 1 || proposalResources[0].ResourceID != "file:README.md" {
		t.Fatalf("expected inferred file proposal resource, got %+v", proposalResources)
	}
	if _, err := os.Stat(filepath.Join(root, ".roundtable/patches/P-1.diff")); err != nil {
		t.Fatalf("expected patch file to exist: %v", err)
	}

	listed, err := rt.Call(context.Background(), "proposal.list", map[string]any{"task_id": "T-1"})
	if err != nil {
		t.Fatalf("proposal.list failed: %v", err)
	}
	proposals := listed["proposals"].([]db.Proposal)
	if len(proposals) != 1 || proposals[0].ID != "P-1" {
		t.Fatalf("unexpected proposals: %+v", proposals)
	}

	voted, err := rt.Call(context.Background(), "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "A-2",
		"vote":        "approve",
		"reason_md":   "Looks good",
		"confidence":  0.8,
	})
	if err != nil {
		t.Fatalf("vote.cast failed: %v", err)
	}
	if voted["vote"].(db.Vote).ID != "V-1" {
		t.Fatalf("unexpected vote result: %+v", voted)
	}

	votes, err := rt.Call(context.Background(), "vote.list", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("vote.list failed: %v", err)
	}
	if len(votes["votes"].([]db.Vote)) != 1 {
		t.Fatalf("unexpected votes list: %+v", votes)
	}

	recorded, err := rt.Call(context.Background(), "decision.record", map[string]any{
		"decision_id":  "D-1",
		"proposal_id":  "P-1",
		"task_id":      "T-1",
		"decision":     "accepted",
		"rationale_md": "Consensus reached",
		"decided_by":   "Chair",
	})
	if err != nil {
		t.Fatalf("decision.record failed: %v", err)
	}
	if recorded["decision"].(db.Decision).ID != "D-1" {
		t.Fatalf("unexpected decision result: %+v", recorded)
	}
}

func TestRuntimeProposalAttachPatch(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := rt.Call(context.Background(), "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Attach patch",
		"body_md": "Verify attach_patch",
	}); err != nil {
		t.Fatalf("task.create failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	}); err != nil {
		t.Fatalf("resource.claim failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Initial patch",
		"summary_md":         "Create proposal first",
		"affected_resources": []any{"file:README.md"},
		"patch":              testReadmePatch("hello\n", "hello world\n"),
	}); err != nil {
		t.Fatalf("proposal.create failed: %v", err)
	}

	updatedPatch := testReadmePatch("hello\n", "hello roundtable\n")
	attached, err := rt.Call(context.Background(), "proposal.attach_patch", map[string]any{
		"proposal_id": "P-1",
		"patch":       updatedPatch,
	})
	if err != nil {
		t.Fatalf("proposal.attach_patch failed: %v", err)
	}
	if attached["proposal"].(db.Proposal).ID != "P-1" {
		t.Fatalf("unexpected attach result: %+v", attached)
	}
	proposalResources := attached["proposal_resources"].([]db.ProposalResource)
	if len(proposalResources) != 1 || proposalResources[0].ResourceID != "file:README.md" {
		t.Fatalf("unexpected proposal resources: %+v", proposalResources)
	}

	data, err := os.ReadFile(filepath.Join(root, ".roundtable/patches/P-1.diff"))
	if err != nil {
		t.Fatalf("read attached patch failed: %v", err)
	}
	if string(data) != updatedPatch {
		t.Fatalf("unexpected stored patch: %s", string(data))
	}
}

func TestRuntimeProposalRequestReview(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if err := rt.store.UpsertAgent(context.Background(), db.Agent{ID: "reviewer-1", Role: "Reviewer", Name: "Reviewer 1", Adapter: "gemini", Status: "idle", IsEnabled: true}); err != nil {
		t.Fatalf("seed reviewer failed: %v", err)
	}
	if err := rt.store.UpsertAgent(context.Background(), db.Agent{ID: "security-1", Role: "Security", Name: "Security 1", Adapter: "claude", Status: "idle", IsEnabled: true}); err != nil {
		t.Fatalf("seed security failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Request review",
		"body_md": "Verify request_review",
	}); err != nil {
		t.Fatalf("task.create failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:auth/session.txt",
		"path":          "auth/session.txt",
		"claim_type":    "write",
	}); err != nil {
		t.Fatalf("resource.claim failed: %v", err)
	}
	if _, err := rt.Call(context.Background(), "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Review auth change",
		"summary_md":         "Touches high-risk auth path",
		"affected_resources": []any{"file:auth/session.txt"},
		"risk":               "high",
		"patch": strings.Join([]string{
			"diff --git a/auth/session.txt b/auth/session.txt",
			"--- a/auth/session.txt",
			"+++ b/auth/session.txt",
			"@@ -1 +1 @@",
			"-old",
			"+new",
			"",
		}, "\n"),
	}); err != nil {
		t.Fatalf("proposal.create failed: %v", err)
	}

	result, err := rt.Call(context.Background(), "proposal.request_review", map[string]any{
		"proposal_id": "P-1",
	})
	if err != nil {
		t.Fatalf("proposal.request_review failed: %v", err)
	}
	proposal := result["proposal"].(db.Proposal)
	if proposal.Status != "in_review" {
		t.Fatalf("expected in_review proposal, got %+v", proposal)
	}
	roles := result["review_roles"].([]string)
	if len(roles) < 2 || roles[0] == "" {
		t.Fatalf("expected review roles, got %+v", roles)
	}
	reviewers := result["reviewers"].([]map[string]any)
	if len(reviewers) < 2 {
		t.Fatalf("expected reviewers, got %+v", reviewers)
	}
	if !strings.Contains(fmt.Sprint(reviewers), "reviewer-1") || !strings.Contains(fmt.Sprint(reviewers), "security-1") {
		t.Fatalf("expected mapped reviewer agents, got %+v", reviewers)
	}
	if required, _ := result["security_review_required"].(bool); !required {
		t.Fatalf("expected security review requirement, got %+v", result)
	}
	if required, _ := result["human_approval_required"].(bool); !required {
		t.Fatalf("expected human approval requirement, got %+v", result)
	}
}

func TestRuntimeProposalRejectsUnclaimedResources(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := rt.Call(context.Background(), "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Guard claims",
		"body_md": "Proposal should require claims",
	}); err != nil {
		t.Fatalf("task.create failed: %v", err)
	}

	_, err = rt.Call(context.Background(), "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Unsafe patch",
		"summary_md":         "No claim present",
		"affected_resources": []any{"file:README.md"},
		"risk":               "normal",
		"patch":              testReadmePatch("hello\n", "unsafe\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "unclaimed affected resource") {
		t.Fatalf("expected unclaimed resource error, got %v", err)
	}
}

func TestRuntimePatchValidateRejectAndApply(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Validate patches",
		"body_md": "Exercise patch lifecycle",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "README tweak",
		"summary_md":         "Safe update",
		"affected_resources": []any{"file:README.md"},
		"risk":               "normal",
		"patch":              testReadmePatch("hello\n", "hello world\n"),
	})

	validation, err := rt.Call(context.Background(), "patch.validate", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("patch.validate failed: %v", err)
	}
	if !validation["valid"].(bool) {
		t.Fatalf("expected valid proposal, got %+v", validation)
	}
	policyEval := validation["policy"].(policy.Result)
	if policyEval.Approvable {
		t.Fatalf("proposal should not be approvable before votes: %+v", policyEval)
	}

	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "architect",
		"vote":        "approve",
		"reason_md":   "good",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-2",
		"proposal_id": "P-1",
		"agent_id":    "reviewer",
		"vote":        "approve",
		"reason_md":   "good",
	})

	applied, err := rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id":    "P-1",
		"transaction_id": "TX-1",
		"run_id":         "RUN-1",
	})
	if err != nil {
		t.Fatalf("patch.apply failed: %v", err)
	}
	if applied["transaction"].(db.Transaction).ID != "TX-1" {
		t.Fatalf("unexpected transaction: %+v", applied)
	}
	if applied["proposal"].(db.Proposal).Status != "accepted" {
		t.Fatalf("unexpected proposal status after apply: %+v", applied)
	}
	readmeData, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read updated README failed: %v", err)
	}
	if string(readmeData) != "hello world\n" {
		t.Fatalf("unexpected README after apply: %q", string(readmeData))
	}

	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-2",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Bad update",
		"summary_md":         "Should be rejected",
		"affected_resources": []any{"file:README.md"},
		"risk":               "normal",
		"patch":              testReadmePatch("hello world\n", "bad update\n"),
	})
	rejected, err := rt.Call(context.Background(), "patch.reject", map[string]any{
		"proposal_id": "P-2",
		"reason_md":   "policy failure",
		"decided_by":  "Chair",
		"decision_id": "D-1",
	})
	if err != nil {
		t.Fatalf("patch.reject failed: %v", err)
	}
	if rejected["proposal"].(db.Proposal).Status != "rejected" {
		t.Fatalf("unexpected rejected proposal result: %+v", rejected)
	}
}

func TestRuntimePatchApplyBlockedByHighRiskAndVeto(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "High risk proposal",
		"body_md": "Should require human or veto block",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "High risk",
		"summary_md":         "Needs human",
		"affected_resources": []any{"file:README.md"},
		"risk":               "high",
		"patch":              testReadmePatch("hello\n", "high risk\n"),
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "architect",
		"vote":        "approve",
		"reason_md":   "good",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-2",
		"proposal_id": "P-1",
		"agent_id":    "security",
		"vote":        "veto",
		"reason_md":   "blocked",
	})

	_, err = rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id": "P-1",
		"run_id":      "RUN-1",
	})
	if err == nil || (!strings.Contains(err.Error(), "security veto") && !strings.Contains(err.Error(), "human approval")) {
		t.Fatalf("expected policy block, got %v", err)
	}
}

func TestRuntimePatchApplyBlockedByHighRiskPath(t *testing.T) {
	root := newRuntimeRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth", "session.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "High risk path proposal",
		"body_md": "Auth paths should require human approval",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:auth/session.txt",
		"path":          "auth/session.txt",
		"claim_type":    "write",
	})
	created := mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id": "P-1",
		"task_id":     "T-1",
		"agent_id":    "A-1",
		"title":       "Auth update",
		"summary_md":  "Touches auth path",
		"patch": strings.Join([]string{
			"diff --git a/auth/session.txt b/auth/session.txt",
			"--- a/auth/session.txt",
			"+++ b/auth/session.txt",
			"@@ -1 +1 @@",
			"-old",
			"+new",
			"",
		}, "\n"),
	})
	proposalResources := created["proposal_resources"].([]db.ProposalResource)
	if len(proposalResources) != 1 || proposalResources[0].ResourceID != "file:auth/session.txt" {
		t.Fatalf("unexpected proposal resources: %+v", proposalResources)
	}
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "architect",
		"vote":        "approve",
		"reason_md":   "good",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-2",
		"proposal_id": "P-1",
		"agent_id":    "reviewer",
		"vote":        "approve",
		"reason_md":   "good",
	})

	validation, err := rt.Call(context.Background(), "patch.validate", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("patch.validate failed: %v", err)
	}
	policyResult := validation["policy"].(policy.Result)
	if !policyResult.NeedsHuman || len(policyResult.HighRiskPaths) != 1 {
		t.Fatalf("expected high-risk path gate, got %+v", policyResult)
	}

	_, err = rt.Call(context.Background(), "patch.apply", map[string]any{"proposal_id": "P-1", "run_id": "RUN-1"})
	if err == nil || !strings.Contains(err.Error(), "human approval") {
		t.Fatalf("expected human approval block, got %v", err)
	}
}

func TestRuntimePatchValidateSupportsSymbolScopedResources(t *testing.T) {
	root := newRuntimeRoot(t)
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(strings.Join([]string{
		"package sample",
		"",
		"func Alpha() string {",
		`	return "a"`,
		"}",
		"",
		"func Beta() string {",
		`	return "b"`,
		"}",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Symbol-scoped patch",
		"body_md": "Only Alpha should be touched",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "symbol",
		"resource_id":   "symbol:sample.go#Alpha",
		"path":          "sample.go",
		"symbol":        "Alpha",
		"claim_type":    "write",
	})

	created := mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Update Alpha",
		"summary_md":         "Change Alpha only",
		"affected_resources": []any{"symbol:sample.go#Alpha"},
		"patch": strings.Join([]string{
			"diff --git a/sample.go b/sample.go",
			"--- a/sample.go",
			"+++ b/sample.go",
			"@@ -1,5 +1,5 @@",
			" package sample",
			" ",
			" func Alpha() string {",
			`-	return "a"`,
			`+	return "alpha"`,
			" }",
			"",
		}, "\n"),
	})
	proposalResources := created["proposal_resources"].([]db.ProposalResource)
	if len(proposalResources) != 1 || proposalResources[0].ResourceID != "symbol:sample.go#Alpha" {
		t.Fatalf("expected symbol-only proposal resources, got %+v", proposalResources)
	}

	validation, err := rt.Call(context.Background(), "patch.validate", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("patch.validate failed: %v", err)
	}
	if !validation["valid"].(bool) {
		t.Fatalf("expected symbol-scoped patch to validate, got %+v", validation)
	}
}

func TestRuntimePatchValidateRejectsChangesOutsideClaimedSymbol(t *testing.T) {
	root := newRuntimeRoot(t)
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(strings.Join([]string{
		"package sample",
		"",
		"func Alpha() string {",
		`	return "a"`,
		"}",
		"",
		"func Beta() string {",
		`	return "b"`,
		"}",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Out-of-scope symbol patch",
		"body_md": "Alpha claim should not cover Beta",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "symbol",
		"resource_id":   "symbol:sample.go#Alpha",
		"path":          "sample.go",
		"symbol":        "Alpha",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Update Beta",
		"summary_md":         "Should fail coverage",
		"affected_resources": []any{"symbol:sample.go#Alpha"},
		"patch": strings.Join([]string{
			"diff --git a/sample.go b/sample.go",
			"--- a/sample.go",
			"+++ b/sample.go",
			"@@ -6,4 +6,4 @@",
			" ",
			" func Beta() string {",
			`-	return "b"`,
			`+	return "beta"`,
			" }",
			"",
		}, "\n"),
	})

	validation, err := rt.Call(context.Background(), "patch.validate", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("patch.validate failed: %v", err)
	}
	if validation["resource_coverage"].(bool) {
		t.Fatalf("expected symbol coverage failure, got %+v", validation)
	}
	if !strings.Contains(validation["resource_error"].(string), "outside claimed symbols") {
		t.Fatalf("expected symbol coverage error, got %+v", validation)
	}
}

func TestRuntimePatchValidateSupportsRenameWithCoveredResources(t *testing.T) {
	root := newRuntimeRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "old.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Rename docs file",
		"body_md": "Ensure rename coverage validates",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "directory",
		"resource_id":   "directory:docs",
		"path":          "docs",
		"claim_type":    "write",
	})

	renamePatch := strings.Join([]string{
		"diff --git a/docs/old.txt b/docs/new.txt",
		"similarity index 100%",
		"rename from docs/old.txt",
		"rename to docs/new.txt",
		"",
	}, "\n")
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "Rename file",
		"summary_md":         "Rename old.txt to new.txt",
		"affected_resources": []any{"directory:docs"},
		"patch":              renamePatch,
	})

	validation, err := rt.Call(context.Background(), "patch.validate", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("patch.validate failed: %v", err)
	}
	if covered, _ := validation["resource_coverage"].(bool); !covered {
		t.Fatalf("expected rename patch resource coverage to validate, got %+v", validation)
	}
	if parseErr, _ := validation["patch_parse_error"].(string); parseErr != "" {
		t.Fatalf("expected rename patch to parse cleanly, got %+v", validation)
	}
}

func TestRuntimePatchValidateSuspendsStaleClaims(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Stale claim validation",
		"body_md": "Base hash drift should suspend claim",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed after claim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id":        "P-1",
		"task_id":            "T-1",
		"agent_id":           "A-1",
		"title":              "README tweak",
		"summary_md":         "Should fail on stale claim",
		"affected_resources": []any{"file:README.md"},
		"patch":              testReadmePatch("changed after claim\n", "new text\n"),
	})

	validation, err := rt.Call(context.Background(), "patch.validate", map[string]any{"proposal_id": "P-1"})
	if err != nil {
		t.Fatalf("patch.validate failed: %v", err)
	}
	if validation["claims_valid"].(bool) {
		t.Fatalf("expected stale claim to invalidate proposal, got %+v", validation)
	}
	if !strings.Contains(validation["claim_error"].(string), "stale claim base hash") {
		t.Fatalf("expected stale claim error, got %+v", validation)
	}
	staleClaims := validation["stale_claims"].([]db.Claim)
	if len(staleClaims) != 1 || staleClaims[0].Status != "suspended" {
		t.Fatalf("expected suspended stale claim, got %+v", staleClaims)
	}

	status, err := rt.Call(context.Background(), "resource.claim_status", map[string]any{"agent_id": "A-1"})
	if err != nil {
		t.Fatalf("resource.claim_status failed: %v", err)
	}
	claims := status["claims"].([]db.Claim)
	if len(claims) != 1 || claims[0].Status != "suspended" {
		t.Fatalf("expected stored claim to be suspended, got %+v", claims)
	}
}

func TestRuntimeHumanApprovalWorkflowAllowsHighRiskApply(t *testing.T) {
	root := newRuntimeRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "auth"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "auth", "session.txt"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Human approval proposal",
		"body_md": "Auth changes need approval",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:auth/session.txt",
		"path":          "auth/session.txt",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id": "P-1",
		"task_id":     "T-1",
		"agent_id":    "A-1",
		"title":       "Auth update",
		"summary_md":  "Touches auth path",
		"patch": strings.Join([]string{
			"diff --git a/auth/session.txt b/auth/session.txt",
			"--- a/auth/session.txt",
			"+++ b/auth/session.txt",
			"@@ -1 +1 @@",
			"-old",
			"+new",
			"",
		}, "\n"),
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "architect",
		"vote":        "approve",
		"reason_md":   "good",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-2",
		"proposal_id": "P-1",
		"agent_id":    "reviewer",
		"vote":        "approve",
		"reason_md":   "good",
	})

	requested, err := rt.Call(context.Background(), "human.request_approval", map[string]any{
		"approval_id":  "H-1",
		"proposal_id":  "P-1",
		"subject":      "Approve auth change",
		"reason_md":    "High-risk auth path",
		"requested_by": "chair",
	})
	if err != nil {
		t.Fatalf("human.request_approval failed: %v", err)
	}
	if requested["approval"].(db.HumanApproval).Status != "requested" {
		t.Fatalf("unexpected approval request: %+v", requested)
	}

	status, err := rt.Call(context.Background(), "human.approval_status", map[string]any{"approval_id": "H-1"})
	if err != nil {
		t.Fatalf("human.approval_status failed: %v", err)
	}
	if status["approval"].(db.HumanApproval).TaskID != "T-1" {
		t.Fatalf("expected task inference on approval request: %+v", status)
	}

	mustCall(t, rt, "human.request_approval", map[string]any{
		"approval_id":     "H-1",
		"proposal_id":     "P-1",
		"subject":         "Approve auth change",
		"reason_md":       "Granted",
		"requested_by":    "chair",
		"status":          "approved",
		"decided_by":      "human",
		"decision_md":     "Approved after review",
		"override_policy": false,
	})

	_, err = rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id":    "P-1",
		"transaction_id": "TX-1",
		"run_id":         "RUN-1",
	})
	if err == nil || !strings.Contains(err.Error(), "security review required") {
		t.Fatalf("expected security review block before apply, got %v", err)
	}

	reviewed, err := rt.Call(context.Background(), "security.review", map[string]any{
		"review_id":   "SR-1",
		"proposal_id": "P-1",
	})
	if err != nil {
		t.Fatalf("security.review failed: %v", err)
	}
	if reviewed["review"].(db.SecurityReview).Status != "approved" {
		t.Fatalf("expected approved security review, got %+v", reviewed)
	}

	applied, err := rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id":    "P-1",
		"transaction_id": "TX-1",
		"run_id":         "RUN-1",
	})
	if err != nil {
		t.Fatalf("patch.apply with human approval failed: %v", err)
	}
	if applied["human_approval"].(db.HumanApproval).ID != "H-1" {
		t.Fatalf("expected applied human approval, got %+v", applied)
	}
	data, err := os.ReadFile(filepath.Join(root, "auth", "session.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected auth file after apply: %q", string(data))
	}
}

func TestRuntimeHumanApprovalOverrideAllowsSecurityVetoApply(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "High risk proposal",
		"body_md": "Should require human override",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:README.md",
		"path":          "README.md",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id": "P-1",
		"task_id":     "T-1",
		"agent_id":    "A-1",
		"title":       "High risk",
		"summary_md":  "Needs override",
		"risk":        "high",
		"patch":       testReadmePatch("hello\n", "overridden\n"),
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-1",
		"proposal_id": "P-1",
		"agent_id":    "architect",
		"vote":        "approve",
		"reason_md":   "good",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-2",
		"proposal_id": "P-1",
		"agent_id":    "reviewer",
		"vote":        "approve",
		"reason_md":   "good",
	})
	mustCall(t, rt, "vote.cast", map[string]any{
		"vote_id":     "V-3",
		"proposal_id": "P-1",
		"agent_id":    "security",
		"vote":        "veto",
		"reason_md":   "blocked without override",
	})

	mustCall(t, rt, "human.request_approval", map[string]any{
		"approval_id":  "H-1",
		"proposal_id":  "P-1",
		"subject":      "Override security veto",
		"reason_md":    "Human accepts risk",
		"requested_by": "chair",
		"status":       "approved",
		"decided_by":   "human",
		"decision_md":  "Override granted",
	})
	mustCall(t, rt, "security.review", map[string]any{
		"review_id":   "SR-1",
		"proposal_id": "P-1",
		"status":      "veto",
		"summary_md":  "Security rejects this change without explicit human override.",
	})
	_, err = rt.Call(context.Background(), "patch.apply", map[string]any{"proposal_id": "P-1", "run_id": "RUN-1"})
	if err == nil || !strings.Contains(err.Error(), "security veto") {
		t.Fatalf("expected veto to remain blocked without override flag, got %v", err)
	}

	mustCall(t, rt, "human.request_approval", map[string]any{
		"approval_id":     "H-1",
		"proposal_id":     "P-1",
		"subject":         "Override security veto",
		"reason_md":       "Human accepts risk",
		"requested_by":    "chair",
		"status":          "approved",
		"decided_by":      "human",
		"decision_md":     "Override granted",
		"override_policy": true,
	})

	applied, err := rt.Call(context.Background(), "patch.apply", map[string]any{
		"proposal_id":    "P-1",
		"transaction_id": "TX-1",
		"run_id":         "RUN-1",
	})
	if err != nil {
		t.Fatalf("patch.apply with veto override failed: %v", err)
	}
	if applied["human_approval"].(db.HumanApproval).ID != "H-1" {
		t.Fatalf("expected override approval in result, got %+v", applied)
	}
	if applied["security_review"].(db.SecurityReview).ID != "SR-1" {
		t.Fatalf("expected security review in result, got %+v", applied)
	}
}

func TestRuntimeSecurityReviewAutoVetoesSensitiveLogging(t *testing.T) {
	root := newRuntimeRoot(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	mustCall(t, rt, "task.create", map[string]any{
		"task_id": "T-1",
		"title":   "Security scan",
		"body_md": "Detect token logging",
	})
	mustCall(t, rt, "resource.claim", map[string]any{
		"run_id":        "RUN-1",
		"claim_id":      "C-1",
		"agent_id":      "A-1",
		"task_id":       "T-1",
		"resource_type": "file",
		"resource_id":   "file:main.go",
		"path":          "main.go",
		"claim_type":    "write",
	})
	mustCall(t, rt, "proposal.create", map[string]any{
		"proposal_id": "P-1",
		"task_id":     "T-1",
		"agent_id":    "A-1",
		"title":       "Add token logging",
		"summary_md":  "Should be vetoed",
		"patch": strings.Join([]string{
			"diff --git a/main.go b/main.go",
			"--- a/main.go",
			"+++ b/main.go",
			"@@ -1,4 +1,5 @@",
			" package main",
			" ",
			" func main() {",
			"+    fmt.Println(\"token\", token)",
			" }",
			"",
		}, "\n"),
	})

	reviewed, err := rt.Call(context.Background(), "security.review", map[string]any{
		"review_id":   "SR-1",
		"proposal_id": "P-1",
	})
	if err != nil {
		t.Fatalf("security.review failed: %v", err)
	}
	review := reviewed["review"].(db.SecurityReview)
	if review.Status != "veto" {
		t.Fatalf("expected auto-veto security review, got %+v", review)
	}
	findings := reviewed["findings"].([]security.Finding)
	if len(findings) == 0 || findings[0].Code != "secret_logging" {
		t.Fatalf("expected secret logging finding, got %+v", findings)
	}
}

func mustCall(t *testing.T, rt *Runtime, tool string, args map[string]any) map[string]any {
	t.Helper()
	result, err := rt.Call(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("%s failed: %v", tool, err)
	}
	return result
}

func TestParseAndEncode(t *testing.T) {
	req, err := ParseCallRequest([]byte(`{"tool":"table.get_state","args":{"x":"y"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Tool != "table.get_state" || req.Args["x"] != "y" {
		t.Fatalf("unexpected request: %+v", req)
	}

	data, err := EncodeResponse(map[string]any{"ok": "yes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ok":true`) {
		t.Fatalf("unexpected response: %s", string(data))
	}
}

func newRuntimeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".roundtable/config.yaml"), []byte(testConfigYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	return root
}

func testReadmePatch(before, after string) string {
	before = strings.TrimSuffix(before, "\n")
	after = strings.TrimSuffix(after, "\n")
	return strings.Join([]string{
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@ -1 +1 @@",
		"-" + before,
		"+" + after,
		"",
	}, "\n")
}

const testConfigYAML = `version: 1
project_name: Roundtable
storage:
  sqlite_path: .roundtable/roundtable.db
  wal: true
mcp:
  transport: unix
  socket_path: .roundtable/mcp/roundtable.sock
agents:
  chair:
    role: Chair
    adapter: claude
    model: default
  architect:
    role: Architect
    adapter: claude
    model: default
  implementers:
    count: 2
    adapter: codex
    model: default
  reviewers:
    count: 1
    adapter: gemini
    model: default
  tester:
    role: Tester
    adapter: codex
    model: default
  security:
    role: Security
    adapter: claude
    model: default
  memory_oracle:
    role: MemoryOracle
    adapter: claude
    model: default
adapters:
  codex:
    command: codex
    supports_resume: true
    resume_pattern: "codex resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
  claude:
    command: claude
    supports_resume: true
    resume_pattern: "claude --resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
  gemini:
    command: gemini
    supports_resume: true
    resume_pattern: "gemini resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
  opencode:
    command: opencode
    supports_resume: false
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: false
  generic:
    command: sh
    supports_resume: false
    supports_mcp: false
    supports_readonly_workspace: true
    captures_session_id: false
`
