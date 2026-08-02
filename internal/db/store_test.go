package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"roundtable/internal/events"
)

func TestOpenAppliesPragmasAndMigrationsIdempotently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtable.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first open failed: %v", err)
	}
	defer db1.Close()

	assertPragma(t, db1, "journal_mode", "wal")
	assertPragma(t, db1, "foreign_keys", int64(1))
	assertPragma(t, db1, "busy_timeout", int64(5000))
	assertTableExists(t, db1, "memory_entries")

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second open failed: %v", err)
	}
	defer db2.Close()

	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one schema migration row, got %d", count)
	}
}

func TestAppendEventPublishesToSubscribers(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	ch := store.Events().Subscribe(1)
	defer store.Events().Unsubscribe(ch)

	saved, err := store.AppendEvent(ctx, Event{
		RunID:       "RUN-1",
		Type:        "task.created",
		ActorID:     "A-1",
		TaskID:      "T-1",
		PayloadJSON: `{"title":"Bootstrap"}`,
	})
	if err != nil {
		t.Fatalf("append event failed: %v", err)
	}

	select {
	case published := <-ch:
		if published.ID != saved.ID || published.Type != "task.created" {
			t.Fatalf("unexpected published event: %+v", published)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event publication")
	}

	events, err := store.ListEvents(ctx, "RUN-1")
	if err != nil {
		t.Fatalf("list events failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func TestStoreRepositories(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	must(t, store.UpsertRun(ctx, Run{ID: "RUN-1", Goal: "bootstrap"}))
	run, err := store.GetRun(ctx, "RUN-1")
	must(t, err)
	if run.Goal != "bootstrap" || run.Status != "active" {
		t.Fatalf("unexpected run: %+v", run)
	}
	runs, err := store.ListRuns(ctx)
	must(t, err)
	assertLen(t, runs, 1)

	must(t, store.UpsertAgent(ctx, Agent{ID: "A-1", Role: "Chair", Name: "chair", Adapter: "claude", IsEnabled: true}))
	agent, err := store.GetAgent(ctx, "A-1")
	must(t, err)
	if !agent.IsEnabled || agent.Role != "Chair" {
		t.Fatalf("unexpected agent: %+v", agent)
	}
	agents, err := store.ListAgents(ctx)
	must(t, err)
	assertLen(t, agents, 1)

	must(t, store.UpsertAgentSession(ctx, AgentSession{ID: "S-1", AgentID: "A-1", RunID: "RUN-1", Adapter: "claude", WorkingDirectory: "/tmp/rt"}))
	session, err := store.GetAgentSession(ctx, "S-1")
	must(t, err)
	if session.WorkingDirectory != "/tmp/rt" {
		t.Fatalf("unexpected session: %+v", session)
	}
	sessions, err := store.ListAgentSessions(ctx)
	must(t, err)
	assertLen(t, sessions, 1)

	sessionEvent, err := store.AppendAgentSessionEvent(ctx, AgentSessionEvent{SessionID: "S-1", EventType: "started", PayloadJSON: `{}`})
	must(t, err)
	if sessionEvent.EventType != "started" {
		t.Fatalf("unexpected session event: %+v", sessionEvent)
	}
	sessionEvents, err := store.ListAgentSessionEvents(ctx, "S-1")
	must(t, err)
	assertLen(t, sessionEvents, 1)

	must(t, store.UpsertAdapterCapability(ctx, AdapterCapability{Adapter: "codex", SupportsResume: true, SupportsMCP: true, SupportsReadOnlyWorkspace: true, SupportsSessionCapture: true}))
	capability, err := store.GetAdapterCapability(ctx, "codex")
	must(t, err)
	if !capability.SupportsResume || !capability.SupportsMCP {
		t.Fatalf("unexpected adapter capability: %+v", capability)
	}
	capabilities, err := store.ListAdapterCapabilities(ctx)
	must(t, err)
	assertLen(t, capabilities, 1)

	must(t, store.InsertRunSnapshot(ctx, RunSnapshot{ID: "RS-1", RunID: "RUN-1", SnapshotJSON: `{"state":"ok"}`}))
	snapshots, err := store.ListRunSnapshots(ctx, "RUN-1")
	must(t, err)
	assertLen(t, snapshots, 1)

	must(t, store.UpsertTask(ctx, Task{ID: "T-1", Title: "Bootstrap", BodyMD: "body"}))
	task, err := store.GetTask(ctx, "T-1")
	must(t, err)
	if task.Status != "open" || task.Priority != 100 {
		t.Fatalf("unexpected task: %+v", task)
	}
	tasks, err := store.ListTasks(ctx)
	must(t, err)
	assertLen(t, tasks, 1)

	must(t, store.UpsertResource(ctx, Resource{ID: "R-1", Type: "file", Path: "README.md", StartLine: 1, EndLine: 10}))
	resource, err := store.GetResource(ctx, "R-1")
	must(t, err)
	if resource.Path != "README.md" {
		t.Fatalf("unexpected resource: %+v", resource)
	}
	resources, err := store.ListResources(ctx)
	must(t, err)
	assertLen(t, resources, 1)

	must(t, store.UpsertClaim(ctx, Claim{ID: "C-1", ResourceID: "R-1", AgentID: "A-1", TaskID: "T-1", ClaimType: "write", ExpiresAt: "2026-07-06T03:00:00Z", Renewable: true}))
	claim, err := store.GetClaim(ctx, "C-1")
	must(t, err)
	if claim.ClaimType != "write" || !claim.Renewable {
		t.Fatalf("unexpected claim: %+v", claim)
	}
	claims, err := store.ListClaims(ctx)
	must(t, err)
	assertLen(t, claims, 1)

	must(t, store.UpsertProposal(ctx, Proposal{ID: "P-1", TaskID: "T-1", AgentID: "A-1", Title: "Bootstrap", SummaryMD: "summary", PatchPath: ".roundtable/patches/P-1.diff"}))
	proposal, err := store.GetProposal(ctx, "P-1")
	must(t, err)
	if proposal.Status != "pending" {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	proposals, err := store.ListProposals(ctx)
	must(t, err)
	assertLen(t, proposals, 1)

	must(t, store.ReplaceProposalResources(ctx, "P-1", []string{"R-1"}))
	proposalResources, err := store.ListProposalResources(ctx, "P-1")
	must(t, err)
	if len(proposalResources) != 1 || proposalResources[0].ResourceID != "R-1" {
		t.Fatalf("unexpected proposal resources: %+v", proposalResources)
	}

	must(t, store.UpsertVote(ctx, Vote{ID: "V-1", ProposalID: "P-1", AgentID: "A-1", Vote: "approve", Confidence: 0.8, ReasonMD: "looks good"}))
	vote, err := store.GetVote(ctx, "V-1")
	must(t, err)
	if vote.Vote != "approve" {
		t.Fatalf("unexpected vote: %+v", vote)
	}
	votes, err := store.ListVotes(ctx, "P-1")
	must(t, err)
	assertLen(t, votes, 1)

	must(t, store.UpsertDecision(ctx, Decision{ID: "D-1", ProposalID: "P-1", TaskID: "T-1", Decision: "accepted", RationaleMD: "good", DecidedBy: "Chair"}))
	decision, err := store.GetDecision(ctx, "D-1")
	must(t, err)
	if decision.Decision != "accepted" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	decisions, err := store.ListDecisions(ctx)
	must(t, err)
	assertLen(t, decisions, 1)

	must(t, store.UpsertTransaction(ctx, Transaction{ID: "TX-1", ProposalID: "P-1", RunID: "RUN-1", BeforeGitHash: "abc123"}))
	transaction, err := store.GetTransaction(ctx, "TX-1")
	must(t, err)
	if transaction.Status != "pending" || transaction.AppliedBy != "orchestrator" {
		t.Fatalf("unexpected transaction: %+v", transaction)
	}
	transactions, err := store.ListTransactions(ctx)
	must(t, err)
	assertLen(t, transactions, 1)

	must(t, store.UpsertHumanApproval(ctx, HumanApproval{ID: "H-1", ProposalID: "P-1", TaskID: "T-1", Subject: "Apply high-risk patch", ReasonMD: "Auth path change", RequestedBy: "chair", Status: "approved", DecidedBy: "human", OverridePolicy: true}))
	approval, err := store.GetHumanApproval(ctx, "H-1")
	must(t, err)
	if approval.Status != "approved" || !approval.OverridePolicy {
		t.Fatalf("unexpected human approval: %+v", approval)
	}
	approvals, err := store.ListHumanApprovals(ctx, "P-1")
	must(t, err)
	assertLen(t, approvals, 1)

	must(t, store.UpsertSecurityReview(ctx, SecurityReview{ID: "SR-1", ProposalID: "P-1", TaskID: "T-1", ReviewerID: "security", Status: "approved", SummaryMD: "Reviewed", FindingsJSON: "[]"}))
	review, err := store.GetSecurityReview(ctx, "SR-1")
	must(t, err)
	if review.Status != "approved" || review.ReviewerID != "security" {
		t.Fatalf("unexpected security review: %+v", review)
	}
	reviews, err := store.ListSecurityReviews(ctx, "P-1")
	must(t, err)
	assertLen(t, reviews, 1)

	must(t, store.UpsertTestRun(ctx, TestRun{ID: "TR-1", ProposalID: "P-1", TaskID: "T-1", Command: "go test ./...", Status: "passed"}))
	testRun, err := store.GetTestRun(ctx, "TR-1")
	must(t, err)
	if testRun.Command != "go test ./..." {
		t.Fatalf("unexpected test run: %+v", testRun)
	}
	testRuns, err := store.ListTestRuns(ctx)
	must(t, err)
	assertLen(t, testRuns, 1)

	must(t, store.UpsertMemoryEntry(ctx, MemoryEntry{ID: "M-1", Scope: "project", Kind: "decision", Title: "Use SQLite", BodyMD: "SQLite is authoritative", Importance: 90}))
	memory, err := store.GetMemoryEntry(ctx, "M-1")
	must(t, err)
	if memory.Title != "Use SQLite" || memory.Importance != 90 {
		t.Fatalf("unexpected memory entry: %+v", memory)
	}
	memories, err := store.ListMemoryEntries(ctx)
	must(t, err)
	assertLen(t, memories, 1)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sqlDB, err := Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return NewStore(sqlDB, events.NewBus[Event]())
}

func assertPragma(t *testing.T, db *sql.DB, name string, want any) {
	t.Helper()
	var got any
	switch want.(type) {
	case string:
		var s string
		if err := db.QueryRow("PRAGMA " + name).Scan(&s); err != nil {
			t.Fatalf("pragma %s failed: %v", name, err)
		}
		got = s
	case int64:
		var n int64
		if err := db.QueryRow("PRAGMA " + name).Scan(&n); err != nil {
			t.Fatalf("pragma %s failed: %v", name, err)
		}
		got = n
	default:
		t.Fatalf("unsupported pragma type %T", want)
	}
	if got != want {
		t.Fatalf("pragma %s: got %v want %v", name, got, want)
	}
}

func assertTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("table existence query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected table %s to exist", table)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertLen[T any](t *testing.T, values []T, want int) {
	t.Helper()
	if len(values) != want {
		t.Fatalf("unexpected length: got %d want %d", len(values), want)
	}
}
