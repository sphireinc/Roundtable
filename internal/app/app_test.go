package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"roundtable/internal/db"
	"roundtable/internal/repo"
)

func TestInitCreatesExpectedScaffold(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	expected := []string{
		".roundtable/roundtable.db",
		".roundtable/config.yaml",
		".roundtable/mcp/AGENT_MCP_MANIFEST.md",
		".roundtable/mcp/tools.schema.json",
		".roundtable/mcp/server.json",
		".roundtable/sessions/agents",
		".roundtable/sessions/runs",
		".roundtable/memory/README.md",
		".roundtable/patches",
		".roundtable/logs",
		".roundtable/runs",
		"AGENTS.ROUNDTABLE.md",
		"PROJECT.ROUNDTABLE.md",
		"POLICIES.ROUNDTABLE.md",
		"TASKS.ROUNDTABLE/0001-bootstrap.md",
	}

	for _, rel := range expected {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}
}

func TestInitRefusesOverwriteWithoutForce(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.ROUNDTABLE.md"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	err := Run(context.Background(), []string{"init", "--root", root}, &output, &output)
	if err == nil {
		t.Fatal("expected overwrite refusal")
	}
	if !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMCPInspectWritesGeneratedRegistryAssets(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	output.Reset()

	if err := Run(context.Background(), []string{"mcp", "inspect", "--root", root, "--write"}, &output, &output); err != nil {
		t.Fatalf("mcp inspect failed: %v", err)
	}

	written := mustReadFile(t, filepath.Join(root, ".roundtable/mcp/tools.schema.json"))
	if !strings.Contains(written, `"name": "table.watch"`) {
		t.Fatalf("expected registry-generated schema to include table.watch, got: %s", written)
	}
	manifest := mustReadFile(t, filepath.Join(root, ".roundtable/mcp/AGENT_MCP_MANIFEST.md"))
	if !strings.Contains(manifest, "not itself a standards-complete MCP transport") {
		t.Fatalf("expected generated manifest to state the socket protocol boundary, got: %s", manifest)
	}
	if !strings.Contains(output.String(), "proposal.create") {
		t.Fatalf("expected inspect output to list tool names, got: %s", output.String())
	}
}

func TestMCPCallInProcess(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	argsPath := filepath.Join(root, "claim.json")
	if err := os.WriteFile(argsPath, []byte(`{"run_id":"RUN-1","claim_id":"C-1","agent_id":"A-1","task_id":"T-1","resource_type":"file","path":"README.md","claim_type":"write"}`), 0o644); err != nil {
		t.Fatalf("write args failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{"mcp", "call", "--root", root, "--tool", "resource.claim", "--args-file", argsPath}, &output, &output); err != nil {
		t.Fatalf("mcp call failed: %v", err)
	}
	if !strings.Contains(output.String(), `"ID": "C-1"`) {
		t.Fatalf("unexpected mcp call output: %s", output.String())
	}
}

func TestInitSeedsAdapterCapabilities(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, nil)
	capability, err := store.GetAdapterCapability(context.Background(), "codex")
	if err != nil {
		t.Fatalf("get adapter capability failed: %v", err)
	}
	if !capability.SupportsResume || !capability.SupportsMCP || !capability.SupportsSessionCapture {
		t.Fatalf("unexpected codex capability: %+v", capability)
	}
	if !strings.Contains(capability.MetadataJSON, `"resume_pattern":"codex resume {{external_session_id}}"`) {
		t.Fatalf("expected resume pattern in metadata, got %s", capability.MetadataJSON)
	}

	generic, err := store.GetAdapterCapability(context.Background(), "generic")
	if err != nil {
		t.Fatalf("get generic adapter capability failed: %v", err)
	}
	if generic.SupportsResume || generic.SupportsSessionCapture {
		t.Fatalf("unexpected generic capability: %+v", generic)
	}
}

func TestSessionsCommandListsStoredSessions(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{
		ID:                    "S-1",
		AgentID:               "implementer-1",
		RunID:                 "RUN-1",
		Adapter:               "codex",
		ExternalSessionID:     "ext-123",
		ExternalResumeCommand: "codex resume ext-123",
		WorkingDirectory:      root,
		Status:                "active",
	}); err != nil {
		t.Fatalf("seed session failed: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{"sessions", "--root", root}, &output, &output); err != nil {
		t.Fatalf("sessions failed: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "S-1 implementer-1 active codex codex resume ext-123") {
		t.Fatalf("unexpected sessions output: %s", got)
	}
}

func TestSessionsLifecycleCommandsPersistSessionState(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"sessions", "register",
		"--root", root,
		"--id", "S-1",
		"--agent", "implementer-1",
		"--run", "RUN-1",
		"--adapter", "codex",
		"--external-session-id", "ext-123",
		"--cwd", root,
	}, &output, &output); err != nil {
		t.Fatalf("sessions register failed: %v", err)
	}
	if !strings.Contains(output.String(), "S-1 implementer-1 active codex resume ext-123") {
		t.Fatalf("unexpected register output: %s", output.String())
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"sessions", "heartbeat",
		"--root", root,
		"--session", "S-1",
		"--status", "idle",
	}, &output, &output); err != nil {
		t.Fatalf("sessions heartbeat failed: %v", err)
	}
	if !strings.Contains(output.String(), "S-1 idle") {
		t.Fatalf("unexpected heartbeat output: %s", output.String())
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"sessions", "end",
		"--root", root,
		"--session", "S-1",
	}, &output, &output); err != nil {
		t.Fatalf("sessions end failed: %v", err)
	}
	if !strings.Contains(output.String(), "S-1 ended") {
		t.Fatalf("unexpected end output: %s", output.String())
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	session, err := store.GetAgentSession(context.Background(), "S-1")
	if err != nil {
		t.Fatalf("get session failed: %v", err)
	}
	if session.Status != "ended" || session.ExternalResumeCommand != "codex resume ext-123" || session.EndedAt == "" {
		t.Fatalf("unexpected stored session: %+v", session)
	}
	events, err := store.ListAgentSessionEvents(context.Background(), "S-1")
	if err != nil {
		t.Fatalf("list session events failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 session events, got %d", len(events))
	}
}

func TestResumeCommandPrintsBriefing(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	ctx := context.Background()
	mustApp(t, store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Refresh tokens", BodyMD: "body", Status: "in_progress", AssignedAgentID: "implementer-1"}))
	mustApp(t, store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "implementer-1", TaskID: "T-1", ClaimType: "write", Status: "active", ExpiresAt: "2099-01-01T00:00:00Z", Renewable: true, ResumePolicy: "hold"}))
	mustApp(t, store.UpsertProposal(ctx, db.Proposal{ID: "P-1", TaskID: "T-1", AgentID: "implementer-1", Title: "Patch", SummaryMD: "summary", PatchPath: ".roundtable/patches/P-1.diff", Status: "pending"}))
	mustApp(t, store.UpsertDecision(ctx, db.Decision{ID: "D-1", ProposalID: "P-1", TaskID: "T-1", Decision: "accepted", RationaleMD: "Tokens must never be logged.", DecidedBy: "Chair"}))
	mustApp(t, store.UpsertAgentSession(ctx, db.AgentSession{
		ID:                    "S-1",
		AgentID:               "implementer-1",
		RunID:                 "RUN-1",
		Adapter:               "codex",
		ExternalSessionID:     "ext-123",
		ExternalResumeCommand: "codex resume ext-123",
		WorkingDirectory:      root,
		Status:                "active",
	}))
	mustApp(t, sqlDB.Close())

	output.Reset()
	if err := Run(context.Background(), []string{"resume", "--root", root, "--session", "S-1"}, &output, &output); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"# Roundtable Resume Briefing",
		"You are resuming as implementer-1.",
		"Run: RUN-1",
		"External session: ext-123",
		"Current task:",
		"T-1 Refresh tokens",
		"Your active claims:",
		"- file:README.md",
		"Pending proposals:",
		"- P-1 from you: pending",
		"Latest decisions:",
		"- D-1: Tokens must never be logged.",
		"Required next action:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected resume output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestResumeCommandSuspendsStaleClaims(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	baseHash, err := repo.FileHash(root, "README.md")
	if err != nil {
		t.Fatalf("hash file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("mutate file failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	ctx := context.Background()
	mustApp(t, store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}))
	mustApp(t, store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "implementer-1", TaskID: "T-1", ClaimType: "write", BaseHash: baseHash, Status: "active", ExpiresAt: "2099-01-01T00:00:00Z", Renewable: true, ResumePolicy: "hold"}))
	mustApp(t, store.UpsertAgentSession(ctx, db.AgentSession{
		ID:                    "S-1",
		AgentID:               "implementer-1",
		RunID:                 "RUN-1",
		Adapter:               "codex",
		ExternalSessionID:     "ext-123",
		ExternalResumeCommand: "codex resume ext-123",
		WorkingDirectory:      root,
		Status:                "active",
	}))
	mustApp(t, sqlDB.Close())

	output.Reset()
	if err := Run(context.Background(), []string{"resume", "--root", root, "--session", "S-1"}, &output, &output); err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if !strings.Contains(output.String(), "Stale claims suspended on resume:") || !strings.Contains(output.String(), "- file:README.md (C-1)") {
		t.Fatalf("unexpected resume output: %s", output.String())
	}

	sqlDB, err = db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	defer sqlDB.Close()
	store = db.NewStore(sqlDB, nil)
	claim, err := store.GetClaim(context.Background(), "C-1")
	if err != nil {
		t.Fatalf("get claim failed: %v", err)
	}
	if claim.Status != "suspended" {
		t.Fatalf("expected suspended claim, got %+v", claim)
	}
}

func mustApp(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTablePrintsSnapshotSummary(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertRun(context.Background(), db.Run{ID: "RUN-1", Goal: "bootstrap", Status: "active"}); err != nil {
		t.Fatalf("seed run failed: %v", err)
	}
	if err := store.UpsertTask(context.Background(), db.Task{ID: "T-1", Title: "Bootstrap", BodyMD: "body"}); err != nil {
		t.Fatalf("seed task failed: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{"table", "--root", root}, &output, &output); err != nil {
		t.Fatalf("table failed: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "RUNS 1") || !strings.Contains(got, "RUN-1 [active] bootstrap") || !strings.Contains(got, "TASKS 1") {
		t.Fatalf("unexpected table output: %s", got)
	}
}

func TestWatchPrintsProjectionSummary(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	ctx := context.Background()
	mustApp(t, store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Blocked task", BodyMD: "body", Status: "blocked"}))
	mustApp(t, store.UpsertProposal(ctx, db.Proposal{ID: "P-1", TaskID: "T-1", AgentID: "A-1", Title: "Pending proposal", SummaryMD: "summary", PatchPath: ".roundtable/patches/P-1.diff", Status: "pending"}))
	mustApp(t, store.UpsertHumanApproval(ctx, db.HumanApproval{ID: "H-1", Subject: "Approve P-1", ReasonMD: "needed", Status: "requested"}))
	mustApp(t, store.UpsertTransaction(ctx, db.Transaction{ID: "TX-1", ProposalID: "P-1", RunID: "RUN-1", Status: "applied", AppliedBy: "chair", AppliedAt: "2026-01-01T00:00:00Z"}))
	_, err = store.AppendEvent(ctx, db.Event{RunID: "RUN-1", Type: "proposal.created", ActorID: "A-1", TaskID: "T-1", PayloadJSON: `{"proposal_id":"P-1"}`})
	mustApp(t, err)
	mustApp(t, sqlDB.Close())

	output.Reset()
	if err := Run(context.Background(), []string{"watch", "--root", root, "--run", "RUN-1"}, &output, &output); err != nil {
		t.Fatalf("watch failed: %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"WATCH run=RUN-1",
		"BLOCKED T-1 Blocked task",
		"PROPOSAL P-1 pending Pending proposal",
		"APPROVAL H-1 requested Approve P-1",
		"TX TX-1 applied P-1",
		"EVENT 1 proposal.created A-1 T-1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected watch output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestWatchFollowPrintsLateEvents(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	ctx := context.Background()
	_, err = store.AppendEvent(ctx, db.Event{RunID: "RUN-1", Type: "proposal.created", ActorID: "A-1", TaskID: "T-1", PayloadJSON: `{"proposal_id":"P-1"}`})
	mustApp(t, err)

	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(watchCtx, []string{"watch", "--root", root, "--run", "RUN-1", "--follow", "--interval", "10ms"}, &output, &output)
	}()

	time.Sleep(30 * time.Millisecond)
	_, err = store.AppendEvent(ctx, db.Event{RunID: "RUN-1", Type: "approval.requested", ActorID: "chair", TaskID: "T-1", PayloadJSON: `{"approval_id":"H-1"}`})
	mustApp(t, err)
	time.Sleep(40 * time.Millisecond)
	cancel()

	err = <-done
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("watch follow failed unexpectedly: %v", err)
	}

	got := output.String()
	if strings.Count(got, "EVENT 1 proposal.created A-1 T-1") != 1 {
		t.Fatalf("expected initial event exactly once, got:\n%s", got)
	}
	if !strings.Contains(got, "EVENT 2 approval.requested chair T-1") {
		t.Fatalf("expected late event in follow output, got:\n%s", got)
	}
}

func TestEndToEndRunResumeWatchSessionsFlow(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	configPath := filepath.Join(root, ".roundtable/config.yaml")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config failed: %v", err)
	}
	socketPath := "/tmp/roundtable-app-test.sock"
	_ = os.Remove(socketPath)
	configText := strings.Replace(string(configData), ".roundtable/mcp/roundtable.sock", socketPath, 1)
	if err := os.WriteFile(configPath, []byte(configText), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-1", "--goal", "bootstrap", "--headless"}, &output, &output); err != nil {
		t.Fatalf("run failed unexpectedly: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"sessions", "register",
		"--root", root,
		"--id", "S-1",
		"--agent", "implementer-1",
		"--run", "RUN-1",
		"--adapter", "codex",
		"--external-session-id", "ext-123",
		"--cwd", root,
	}, &output, &output); err != nil {
		t.Fatalf("sessions register failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	ctx := context.Background()
	mustApp(t, store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Bootstrap", BodyMD: "body", Status: "in_progress", AssignedAgentID: "implementer-1"}))
	mustApp(t, store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}))
	mustApp(t, store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "implementer-1", TaskID: "T-1", ClaimType: "write", Status: "active", ExpiresAt: "2099-01-01T00:00:00Z", Renewable: true, ResumePolicy: "hold"}))
	mustApp(t, os.WriteFile(filepath.Join(root, ".roundtable/patches/P-1.diff"), []byte(testReadmePatch("hello\n", "hello world\n")), 0o644))
	mustApp(t, store.UpsertProposal(ctx, db.Proposal{ID: "P-1", TaskID: "T-1", AgentID: "implementer-1", Title: "Pending patch", SummaryMD: "summary", PatchPath: ".roundtable/patches/P-1.diff", Status: "pending"}))
	mustApp(t, store.UpsertTransaction(ctx, db.Transaction{ID: "TX-1", ProposalID: "P-1", RunID: "RUN-1", Status: "applied", AppliedBy: "chair", AppliedAt: "2026-01-01T00:00:00Z"}))
	_, err = store.AppendEvent(ctx, db.Event{RunID: "RUN-1", Type: "proposal.created", ActorID: "implementer-1", TaskID: "T-1", PayloadJSON: `{"proposal_id":"P-1"}`})
	mustApp(t, err)
	mustApp(t, sqlDB.Close())

	output.Reset()
	if err := Run(context.Background(), []string{"run", "--root", root, "--resume", "--run", "RUN-1", "--headless"}, &output, &output); err != nil {
		t.Fatalf("resume run failed unexpectedly: %v", err)
	}

	sqlDB, err = db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	defer sqlDB.Close()
	store = db.NewStore(sqlDB, nil)
	run, err := store.GetRun(context.Background(), "RUN-1")
	if err != nil {
		t.Fatalf("get run failed: %v", err)
	}
	if run.Goal != "bootstrap" || run.Status != "active" {
		t.Fatalf("unexpected run: %+v", run)
	}
	snapshots, err := store.ListRunSnapshots(context.Background(), "RUN-1")
	if err != nil {
		t.Fatalf("list snapshots failed: %v", err)
	}
	if len(snapshots) < 2 {
		t.Fatalf("expected at least 2 run snapshots, got %d", len(snapshots))
	}

	output.Reset()
	if err := Run(context.Background(), []string{"resume", "--root", root, "--session", "S-1"}, &output, &output); err != nil {
		t.Fatalf("resume command failed: %v", err)
	}
	if !strings.Contains(output.String(), "# Roundtable Resume Briefing") || !strings.Contains(output.String(), "- P-1 from you: in_review") {
		t.Fatalf("unexpected resume output: %s", output.String())
	}

	output.Reset()
	if err := Run(context.Background(), []string{"watch", "--root", root, "--run", "RUN-1"}, &output, &output); err != nil {
		t.Fatalf("watch failed: %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"WATCH run=RUN-1",
		"PROPOSAL P-1 in_review Pending patch",
		"TX TX-1 applied P-1",
		"EVENT",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected end-to-end watch output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestRunHeadlessExecutesOrchestrationTick(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	store := db.NewStore(sqlDB, nil)
	ctx := context.Background()
	mustApp(t, store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Open task", BodyMD: "body", Status: "open"}))
	mustApp(t, sqlDB.Close())

	output.Reset()
	if err := Run(context.Background(), []string{"run", "--root", root, "--run", "RUN-1", "--goal", "bootstrap", "--headless"}, &output, &output); err != nil {
		t.Fatalf("headless run failed: %v", err)
	}

	sqlDB, err = db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("reopen db failed: %v", err)
	}
	defer sqlDB.Close()
	store = db.NewStore(sqlDB, nil)
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents failed: %v", err)
	}
	if len(agents) < 7 {
		t.Fatalf("expected orchestrator to seed agents, got %+v", agents)
	}
	task, err := store.GetTask(context.Background(), "T-1")
	if err != nil {
		t.Fatalf("get task failed: %v", err)
	}
	if task.AssignedAgentID == "" || task.Status != "in_progress" {
		t.Fatalf("expected orchestrator-assigned task, got %+v", task)
	}
}

func TestRunHeadlessLeavesActiveRunForForegroundCoordinator(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder
	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := Run(context.Background(), []string{"run", "--root", root, "--goal", "coordinate follow-up work", "--headless"}, &output, &output); err != nil {
		t.Fatalf("headless run failed: %v", err)
	}
	sqlDB, err := db.Open(filepath.Join(root, ".roundtable/roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	defer sqlDB.Close()
	runs, err := db.NewStore(sqlDB, nil).ListRuns(context.Background())
	if err != nil {
		t.Fatalf("list initialized runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "active" {
		t.Fatalf("headless handoff runs = %+v, want one active run", runs)
	}
}

func TestClaimsCLIWorkflow(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"claims", "claim",
		"--root", root,
		"--run", "RUN-1",
		"--id", "C-1",
		"--agent", "A-1",
		"--task", "T-1",
		"--resource-type", "file",
		"--path", "README.md",
		"--claim-type", "write",
	}, &output, &output); err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if !strings.Contains(output.String(), "C-1 write active file:README.md") {
		t.Fatalf("unexpected claim output: %s", output.String())
	}

	output.Reset()
	if err := Run(context.Background(), []string{"claims", "list", "--root", root}, &output, &output); err != nil {
		t.Fatalf("claims list failed: %v", err)
	}
	if !strings.Contains(output.String(), "C-1 A-1 write active file:README.md") {
		t.Fatalf("unexpected claims list output: %s", output.String())
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"claims", "release",
		"--root", root,
		"--run", "RUN-1",
		"--claim", "C-1",
		"--actor", "A-1",
		"--reason", "done",
	}, &output, &output); err != nil {
		t.Fatalf("claim release failed: %v", err)
	}
	if !strings.Contains(output.String(), "C-1 released") {
		t.Fatalf("unexpected release output: %s", output.String())
	}
}

func TestClaimsReconcileCLIWorkflow(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder

	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	baseHash, err := repo.FileHash(root, "README.md")
	if err != nil {
		t.Fatalf("hash file failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"claims", "claim",
		"--root", root,
		"--run", "RUN-1",
		"--id", "C-1",
		"--agent", "A-1",
		"--task", "T-1",
		"--resource-type", "file",
		"--path", "README.md",
		"--claim-type", "write",
		"--base-hash", baseHash,
	}, &output, &output); err != nil {
		t.Fatalf("claim failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("mutate file failed: %v", err)
	}

	output.Reset()
	if err := Run(context.Background(), []string{
		"claims", "reconcile",
		"--root", root,
		"--run", "RUN-1",
		"--agent", "A-1",
		"--actor", "A-1",
	}, &output, &output); err != nil {
		t.Fatalf("claims reconcile failed: %v", err)
	}
	if !strings.Contains(output.String(), "C-1 A-1 suspended") {
		t.Fatalf("unexpected reconcile output: %s", output.String())
	}
}

func TestSymbolsCLI(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "sample.py")
	if err := os.WriteFile(sourcePath, []byte("class MemoryOracle:\n    pass\n\ndef query_memory():\n    pass\n"), 0o644); err != nil {
		t.Fatalf("write source failed: %v", err)
	}

	var output strings.Builder
	if err := Run(context.Background(), []string{"symbols", "--path", sourcePath}, &output, &output); err != nil {
		t.Fatalf("symbols failed: %v", err)
	}
	got := output.String()
	if !strings.Contains(got, "class MemoryOracle") || !strings.Contains(got, "function query_memory") {
		t.Fatalf("unexpected symbols output: %s", got)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func testReadmePatch(before, after string) string {
	return strings.Join([]string{
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@ -1 +1 @@",
		"-" + strings.TrimSuffix(before, "\n"),
		"+" + strings.TrimSuffix(after, "\n"),
		"",
	}, "\n")
}
