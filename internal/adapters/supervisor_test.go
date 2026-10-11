package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"roundtable/internal/agentauth"
	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/egress"
	roundmcp "roundtable/internal/mcp"
	"roundtable/internal/sandbox"
)

func TestExecutionSupervisorPersistsCancellationRevokesCapabilityAndCleansWorkspace(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	authority := agentauth.New(nil)
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), waitForCancel: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, authority, runtime)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{
		SessionID: "S-cancel", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Reviewer", Prompt: "task",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("container runner did not start")
	}
	session, err := store.GetAgentSession(context.Background(), "S-cancel")
	if err != nil || (session.Status != "starting" && session.Status != "running") {
		t.Fatalf("session was not durably recorded before execution: %+v err=%v", session, err)
	}
	if err := supervisor.Cancel("S-cancel"); err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Session.Status != "cancelled" || !outcome.Process.Canceled {
		t.Fatalf("cancellation did not become terminal state: %+v", outcome)
	}
	if _, err := authority.Resolve(runtime.token); err == nil {
		t.Fatal("agent capability remained valid after cancellation")
	}
	metadata := map[string]any{}
	if err := json.Unmarshal([]byte(outcome.Session.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["role"] != "implementer" {
		t.Fatalf("capability role trusted caller input instead of persisted agent assignment: %v", metadata["role"])
	}
	if metadata["capability_token"] != nil {
		t.Fatal("capability token was persisted in session metadata")
	}
	if runtime.agentEgressSocket == "" || !runtime.egressClosed {
		t.Fatalf("session provider egress gateway was not mounted and cleaned: path=%q closed=%v", runtime.agentEgressSocket, runtime.egressClosed)
	}
	metadataJSON, _ := json.Marshal(metadata)
	if strings.Contains(string(metadataJSON), runtime.token) {
		t.Fatal("agent output leaked its capability into persisted session metadata")
	}
	workspace, _ := metadata["workspace"].(string)
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatalf("execution workspace was not cleaned after cancellation: %s (%v)", workspace, err)
	}
	events, err := store.ListAgentSessionEvents(context.Background(), "S-cancel")
	if err != nil || len(events) < 2 {
		t.Fatalf("execution lifecycle events were not persisted: count=%d err=%v", len(events), err)
	}
}

func TestExecutionSupervisorCollectsPatchWithoutChangingAuthoritativeRepository(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), editWorkspace: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{
		SessionID: "S-patch", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Implementer", Prompt: "task",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("container runner did not start")
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Session.Status != "completed" || outcome.PatchArtifactPath == "" {
		t.Fatalf("successful execution did not produce a governed patch artifact: %+v", outcome)
	}
	patch, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(outcome.PatchArtifactPath)))
	if err != nil || !strings.Contains(string(patch), "+agent change") {
		t.Fatalf("patch artifact missing workspace edit: %q err=%v", patch, err)
	}
	authoritative, err := os.ReadFile(filepath.Join(repository, "README.md"))
	if err != nil || string(authoritative) != "base\n" {
		t.Fatalf("external execution changed authoritative repository: %q err=%v", authoritative, err)
	}
	if _, err := os.Stat(filepath.Join(repository, ".roundtable", "sessions", "agent-executions", "S-patch")); !os.IsNotExist(err) {
		t.Fatalf("successful execution workspace was not cleaned: %v", err)
	}
	if _, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-patch", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Implementer"}, time.Minute); err == nil {
		t.Fatal("completed execution session identifier was reused")
	}
	persisted, err := store.GetAgentSession(context.Background(), "S-patch")
	if err != nil || persisted.Status != "completed" {
		t.Fatalf("rejected session reuse changed persisted execution: status=%s err=%v", persisted.Status, err)
	}
}

func TestExecutionSupervisorPersistsCapturedProviderSessionAndResumeCommand(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), stdout: "{\"type\":\"thread.started\",\"thread_id\":\"thread-123\"}\n"}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	cfg := supervisorConfig()
	adapter := cfg.Adapters["codex"]
	adapter.SupportsResume = true
	adapter.CapturesSessionID = true
	adapter.ResumePattern = "codex exec resume {{external_session_id}} --json"
	cfg.Adapters["codex"] = adapter
	supervisor.registry = NewRegistry(cfg)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-capture-persist", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Implementer"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.GetAgentSession(context.Background(), "S-capture-persist")
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Session.ExternalSessionID != "thread-123" || persisted.ExternalSessionID != "thread-123" || persisted.ExternalResumeCommand != "codex exec resume thread-123 --json" {
		t.Fatalf("captured provider session was not persisted: outcome=%+v stored=%+v", outcome.Session, persisted)
	}
}

func TestExecutionSupervisorSubmitsCollectedPatchThroughAuthorizedProposalTool(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), editWorkspace: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	var submitted map[string]any
	supervisor.SetProposalSubmitter(func(_ context.Context, token string, args map[string]any) (map[string]any, error) {
		if token == "" {
			t.Fatal("proposal handoff omitted execution capability")
		}
		submitted = args
		return map[string]any{"proposal": db.Proposal{ID: "P-handoff"}}, nil
	})
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{
		SessionID: "S-proposal-handoff", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Implementer",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Session.Status != "completed" || submitted == nil {
		t.Fatalf("successful patch was not handed to proposal workflow: outcome=%+v args=%v", outcome, submitted)
	}
	if submitted["task_id"] != "T-1" || submitted["agent_id"] != "A-1" || submitted["base_revision"] == "" || submitted["base_state_hash"] == "" || !strings.Contains(submitted["patch"].(string), "+agent change") {
		t.Fatalf("proposal handoff omitted identity, base revision, or patch: %#v", submitted)
	}
}

func TestSupervisedPatchCompletesGovernedTransactionWorkflow(t *testing.T) {
	repository := supervisorGitRepoAt(t, "/tmp")
	if err := os.MkdirAll(filepath.Join(repository, ".roundtable"), 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(repository, ".roundtable", "config.yaml")
	if err := os.WriteFile(configFile, []byte("adapters:\n  codex:\n    image: agent:test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-f", ".roundtable/config.yaml"}, {"commit", "-qm", "fixture config"}} {
		command := exec.Command("git", args...)
		command.Dir = repository
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	rt, closeRuntime, err := roundmcp.OpenRuntime(repository)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntime()
	store := rt.Store()
	ctx := context.Background()
	if err := store.UpsertAgent(ctx, db.Agent{ID: "A-1", Role: "implementer", Name: "Worker", Adapter: "codex", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Update README", BodyMD: "Make the assigned update", Status: "in_progress", AssignedAgentID: "A-1", ReadablePaths: []string{"README.md"}}); err != nil {
		t.Fatal(err)
	}
	authority := agentauth.New(nil)
	rt.SetAgentAuthority(authority)
	fake := &supervisorFakeRuntime{started: make(chan struct{}), editWorkspace: true}
	fake.duringRun = func(callCtx context.Context, spec sandbox.Spec) error {
		_, err := rt.CallAgent(callCtx, spec.Env["ROUNDTABLE_AGENT_CAPABILITY"], "resource.claim", map[string]any{
			"path": "README.md", "resource_type": "file", "claim_type": "write",
		})
		return err
	}
	supervisor := NewExecutionSupervisor(repository, store, authority, fake, NewRegistry(supervisorConfig()), "http://127.0.0.1:7117/agent/mcp")
	supervisor.SetAgentHandlerFactory(func(token string) (http.Handler, error) {
		return roundmcp.NewScopedAgentHTTPHandler(rt, roundmcp.HTTPOptions{Address: "127.0.0.1:7117"}, token)
	})
	supervisor.SetAgentSocketListenerFactory(func(string) (net.Listener, error) { return newSupervisorTestListener(), nil })
	supervisor.SetProviderEgressFactory(func(_ context.Context, root, sessionID string, _ []string) (providerEgressSession, error) {
		_, socket, err := egress.ProjectSocketPaths(root, sessionID)
		if err != nil {
			return nil, err
		}
		closed := false
		return supervisorFakeEgressSession{path: socket, closed: &closed}, nil
	})
	supervisor.SetProposalSubmitter(func(callCtx context.Context, token string, args map[string]any) (map[string]any, error) {
		return rt.CallAgent(callCtx, token, "proposal.create", args)
	})
	handle, err := supervisor.Launch(ctx, StartAgentRequest{SessionID: "S-transaction", AgentID: "A-1", RunID: "R-1", TaskID: "T-1"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(ctx)
	if err != nil || outcome.Session.Status != "completed" || outcome.Err != "" {
		t.Fatalf("supervised execution did not hand off cleanly: %+v err=%v", outcome, err)
	}
	proposals, err := store.ListProposals(ctx)
	if err != nil || len(proposals) != 1 || proposals[0].ProposerSessionID != "S-transaction" || proposals[0].Status != "pending" {
		t.Fatalf("execution patch was not persisted as its pending proposal: %+v err=%v", proposals, err)
	}
	proposalID := proposals[0].ID
	if _, err := rt.Call(ctx, "proposal.request_review", map[string]any{"proposal_id": proposalID}); err != nil {
		t.Fatal(err)
	}
	for index, reviewer := range []string{"architect-1", "reviewer-1"} {
		if _, err := rt.Call(ctx, "vote.cast", map[string]any{"proposal_id": proposalID, "agent_id": reviewer, "vote": "approve", "reason_md": "reviewed"}); err != nil {
			t.Fatal(err)
		}
		_ = index
	}
	validation, err := rt.Call(ctx, "patch.validate", map[string]any{"proposal_id": proposalID})
	if err != nil || validation["valid"] != true {
		t.Fatalf("supervisor proposal did not pass normal validation: result=%+v err=%v", validation, err)
	}
	applied, err := rt.Call(ctx, "patch.apply", map[string]any{"proposal_id": proposalID, "transaction_id": "TX-agent-e2e", "run_id": "R-1"})
	if err != nil {
		votes, _ := store.ListVotes(ctx, proposalID)
		t.Fatalf("approved supervisor proposal did not apply through the transaction workflow: %v (votes=%+v validation=%+v)", err, votes, validation)
	}
	if transaction := applied["transaction"].(db.Transaction); transaction.ID != "TX-agent-e2e" || transaction.Status != "applied" {
		t.Fatalf("unexpected governed transaction: %+v", transaction)
	}
	readme, err := os.ReadFile(filepath.Join(repository, "README.md"))
	if err != nil || string(readme) != "base\nagent change\n" {
		t.Fatalf("transaction did not apply the supervised workspace patch: %q err=%v", readme, err)
	}
}

func TestExecutionPromptContainsServerAssignedRoleAndScope(t *testing.T) {
	prompt := composeExecutionPrompt(StartAgentRequest{Role: "implementer", TaskID: "T-1", SessionID: "S-1", BaseRevision: "revision-1"}, db.Task{
		BodyMD: "Change the assigned file.", ReadablePaths: []string{"internal/allowed"},
	})
	for _, expected := range []string{"Role: implementer", "Task ID: T-1", "Session ID: S-1", "Base revision: revision-1", "internal/allowed", "resource claim before editing", "disposable assigned workspace", "Roundtable proposal", "Change the assigned file."} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("execution prompt missing %q", expected)
		}
	}
}

func TestExecutionSupervisorLaunchesProposalBoundMemoryOracleInContainer(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	ctx := context.Background()
	if err := store.UpsertAgent(ctx, db.Agent{ID: "A-oracle", Role: "MemoryOracle", Name: "Memory Oracle", Adapter: "codex", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Review memory", BodyMD: "Review the proposal against prior decisions.", Status: "in_progress", AssignedAgentID: "A-implementer", ReadablePaths: []string{"README.md"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProposal(ctx, db.Proposal{ID: "P-1", TaskID: "T-1", AgentID: "A-implementer", Title: "Proposed change", SummaryMD: "Change workflow", PatchPath: ".roundtable/patches/P-1.diff", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), duringRun: func(_ context.Context, spec sandbox.Spec) error {
		prompt := strings.Join(spec.Command, " ")
		for _, expected := range []string{"Role: MemoryOracle", "Memory Oracle review contract", "memory.task_history", "memory.oracle_report", "persist it with memory.record", "advisory"} {
			if !strings.Contains(prompt, expected) {
				return fmt.Errorf("container command omitted Memory Oracle prompt text %q", expected)
			}
		}
		return nil
	}}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	handle, err := supervisor.Launch(ctx, StartAgentRequest{SessionID: "S-oracle", AgentID: "A-oracle", RunID: "R-1", TaskID: "T-1", ProposalID: "P-1", Role: "caller-supplied"}, time.Minute)
	if err != nil {
		t.Fatalf("launch Memory Oracle through sandbox supervisor: %v", err)
	}
	if _, err := handle.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchAssignedRunsOneTaskPerAgentAndDoesNotDuplicate(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	if err := store.UpsertTask(context.Background(), db.Task{ID: "T-2", Title: "Second", BodyMD: "task", Status: "in_progress", AssignedAgentID: "A-1", ReadablePaths: []string{"README.md"}}); err != nil {
		t.Fatal(err)
	}
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), waitForCancel: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	if err := supervisor.DispatchAssigned(context.Background(), "R-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatcher did not start assigned container execution")
	}
	if err := supervisor.DispatchAssigned(context.Background(), "R-1"); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListAgentSessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("dispatcher created duplicate or concurrent session for one agent: sessions=%d err=%v", len(sessions), err)
	}
	if err := supervisor.Cancel(sessions[0].ID); err != nil {
		t.Fatal(err)
	}
	supervisor.mu.Lock()
	handle := supervisor.active[sessions[0].ID].handle
	supervisor.mu.Unlock()
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchAssignedRetriesTaskAfterFailedExecution(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{
		ID: "S-failed-attempt", AgentID: "A-1", RunID: "R-1", Adapter: "codex", Status: "failed",
		MetadataJSON: `{"task_id":"T-1","mode":"container","failure":"image unavailable"}`,
	}); err != nil {
		t.Fatal(err)
	}
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	if err := supervisor.DispatchAssigned(context.Background(), "R-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("failed execution permanently stranded its assigned task")
	}
}

func TestDispatchAssignedDoesNotDuplicateTaskWithOpenProposal(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	if err := store.UpsertProposal(context.Background(), db.Proposal{ID: "P-open", TaskID: "T-1", AgentID: "A-1", Title: "Existing", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	if err := supervisor.DispatchAssigned(context.Background(), "R-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
		t.Fatal("dispatcher duplicated an assigned task that already has an open proposal")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestExecutionHandoffReusesOnlyItsMatchingProposalAndPreservesOpenClaims(t *testing.T) {
	root := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	patch := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-base\n+agent change\n"
	proposalPath := filepath.Join(root, ".roundtable", "patches", "agent-submitted.diff")
	if err := os.MkdirAll(filepath.Dir(proposalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proposalPath, []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProposal(context.Background(), db.Proposal{ID: "P-session", TaskID: "T-1", AgentID: "A-1", Title: "Submitted in session", PatchPath: ".roundtable/patches/agent-submitted.diff", Status: "in_review", ProposerSessionID: "S-handoff"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(context.Background(), db.Claim{ID: "C-session", ExecutionSessionID: "S-handoff", ResourceID: "file:README.md", AgentID: "A-1", TaskID: "T-1", ClaimType: "write", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(context.Background(), db.Claim{ID: "C-other-session", ExecutionSessionID: "S-other", ResourceID: "file:other.go", AgentID: "A-1", TaskID: "T-1", ClaimType: "write", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	supervisor := &ExecutionSupervisor{repositoryRoot: root, store: store}
	reused, err := supervisor.reconcileSessionProposal(context.Background(), StartAgentRequest{SessionID: "S-handoff", AgentID: "A-1", TaskID: "T-1"}, patch)
	if err != nil || !reused.found {
		t.Fatalf("expected existing session proposal to be reused: %+v err=%v", reused, err)
	}
	if _, err := supervisor.reconcileSessionProposal(context.Background(), StartAgentRequest{SessionID: "S-handoff", AgentID: "A-1", TaskID: "T-1"}, patch+"different"); err == nil {
		t.Fatal("handoff accepted a collected patch that differed from the submitted proposal")
	}
	session := db.AgentSession{ID: "S-handoff", AgentID: "A-1", RunID: "R-1", MetadataJSON: `{"task_id":"T-1"}`}
	if err := supervisor.releaseAbandonedExecutionClaims(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	claim, err := store.GetClaim(context.Background(), "C-session")
	if err != nil || claim.Status != "active" {
		t.Fatalf("open proposal lost its required claim: %+v err=%v", claim, err)
	}
	if err := store.UpsertProposal(context.Background(), db.Proposal{ID: "P-session", TaskID: "T-1", AgentID: "A-1", Title: "Submitted in session", PatchPath: ".roundtable/patches/agent-submitted.diff", Status: "rejected", ProposerSessionID: "S-handoff"}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.releaseAbandonedExecutionClaims(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	claim, err = store.GetClaim(context.Background(), "C-session")
	if err != nil || claim.Status != "released" {
		t.Fatalf("rejected proposal left its execution claim active: %+v err=%v", claim, err)
	}
	otherClaim, err := store.GetClaim(context.Background(), "C-other-session")
	if err != nil || otherClaim.Status != "active" {
		t.Fatalf("execution cleanup changed another session's claim: %+v err=%v", otherClaim, err)
	}
}

func TestCompletedNoProposalAttemptCanRetryInANewerRun(t *testing.T) {
	session := db.AgentSession{RunID: "R-old", Status: "completed", MetadataJSON: `{"task_id":"T-1"}`}
	if SessionBlocksTaskRetry(session, "R-current") {
		t.Fatal("successful no-proposal execution permanently blocked retry in a new run")
	}
	if !SessionBlocksTaskRetry(session, "R-old") {
		t.Fatal("successful no-proposal execution would be relaunched repeatedly in its current run")
	}
}

func TestSessionBlocksTaskRetryOnlyForActiveOrHandedOffExecution(t *testing.T) {
	tests := []struct {
		status   string
		metadata string
		want     bool
	}{
		{"starting", `{}`, true},
		{"running", `{}`, true},
		{"completed", `{}`, true},
		{"failed", `{}`, false},
		{"failed", `{"proposal_result":{"proposal":{"id":"P-1"}}}`, true},
		{"cancelled", `{}`, false},
		{"timed_out", `{}`, false},
		{"interrupted", `{}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.status+tt.metadata, func(t *testing.T) {
			got := SessionBlocksTaskRetry(db.AgentSession{Status: tt.status, MetadataJSON: tt.metadata})
			if got != tt.want {
				t.Fatalf("SessionBlocksTaskRetry(%q, %s) = %v, want %v", tt.status, tt.metadata, got, tt.want)
			}
		})
	}
}

func TestProposalBlocksTaskExecutionByWorkflowState(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"pending", true},
		{"in_review", true},
		{"revision_requested", true},
		{"accepted", true},
		{"applied", true},
		{"rejected", false},
		{"withdrawn", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := ProposalBlocksTaskExecution(db.Proposal{Status: tt.status}); got != tt.want {
				t.Fatalf("ProposalBlocksTaskExecution(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestExecutionSupervisorShutdownCancelsAndJoinsActiveContainers(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), waitForCancel: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	if _, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-shutdown", AgentID: "A-1", RunID: "R-1", TaskID: "T-1"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("container runner did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := supervisor.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := store.GetAgentSession(context.Background(), "S-shutdown")
	if err != nil || session.Status != "cancelled" {
		t.Fatalf("shutdown returned before terminal session state persisted: status=%q err=%v", session.Status, err)
	}
}

func TestExecutionSupervisorRefusesSymlinkedPatchArtifactDirectory(t *testing.T) {
	repository := supervisorGitRepo(t)
	outside := t.TempDir()
	patches := filepath.Join(repository, ".roundtable", "patches")
	if err := os.MkdirAll(filepath.Dir(patches), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, patches); err != nil {
		t.Fatal(err)
	}
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), editWorkspace: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{
		SessionID: "S-patch-symlink", AgentID: "A-1", RunID: "R-1", TaskID: "T-1",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Session.Status != "failed" || outcome.Err == "" {
		t.Fatalf("symlinked patch destination did not fail closed: %+v", outcome)
	}
	if _, err := os.Lstat(filepath.Join(outside, "agent-executions", "S-patch-symlink.diff")); !os.IsNotExist(err) {
		t.Fatalf("supervisor wrote a patch artifact outside the repository: %v", err)
	}
}

func TestExecutionSupervisorPersistsTimeoutAsTerminalState(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), waitForCancel: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{
		SessionID: "S-timeout", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Implementer", Prompt: "task",
	}, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Session.Status != "timed_out" || !outcome.Process.TimedOut {
		t.Fatalf("execution timeout was not persisted distinctly from cancellation: %+v", outcome)
	}
}

func TestExecutionSupervisorScopesAndClosesAgentMCPUnixSocket(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	var socketListener *supervisorTestListener
	supervisor.SetAgentSocketListenerFactory(func(string) (net.Listener, error) {
		socketListener = newSupervisorTestListener()
		return socketListener, nil
	})
	supervisor.SetAgentHandlerFactory(func(token string) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}), nil
	})
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-mcp-socket", AgentID: "A-1", RunID: "R-1", TaskID: "T-1"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if runtime.agentMCPSocket == "" {
		t.Fatal("agent MCP socket was not mounted in the execution temp directory")
	}
	if socketListener == nil || !socketListener.isClosed() {
		t.Fatal("session-scoped agent MCP listener remained open after execution ended")
	}
}

func TestExecutionSupervisorReconcilesInterruptedContainerSessions(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	root, err := sandbox.ManagedExecutionRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	sessionRoot := filepath.Join(root, "S-orphan")
	for _, path := range []string{filepath.Join(sessionRoot, "source"), filepath.Join(sessionRoot, "workspace"), filepath.Join(sessionRoot, "temp")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{ID: "S-orphan", AgentID: "A-1", RunID: "R-1", Adapter: "generic", WorkingDirectory: filepath.Join(sessionRoot, "workspace"), Status: "running", MetadataJSON: `{"mode":"container"}`}); err != nil {
		t.Fatal(err)
	}
	count, err := supervisor.Reconcile(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("reconcile count=%d err=%v", count, err)
	}
	session, err := store.GetAgentSession(context.Background(), "S-orphan")
	if err != nil || session.Status != "interrupted" {
		t.Fatalf("orphan session status=%q err=%v", session.Status, err)
	}
	if runtime.stopped != containerName("S-orphan") {
		t.Fatalf("unexpected container terminated: %q", runtime.stopped)
	}
	if _, err := os.Stat(sessionRoot); !os.IsNotExist(err) {
		t.Fatalf("orphan session directory not cleaned: %v", err)
	}
}

func TestExecutionSupervisorReconcilesCleanupPendingTerminalSession(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := NewExecutionSupervisor(repository, store, agentauth.New(nil), runtime, NewRegistry(supervisorConfig()), "")
	managedRoot, err := sandbox.ManagedExecutionRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "S-cleanup-pending"
	sessionRoot := filepath.Join(managedRoot, sessionID)
	for _, path := range []string{filepath.Join(sessionRoot, "source"), filepath.Join(sessionRoot, "workspace"), filepath.Join(sessionRoot, "temp")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{
		ID: sessionID, AgentID: "A-1", RunID: "R-1", Adapter: "generic", Status: "completed",
		MetadataJSON: `{"mode":"container","cleanup_pending":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	count, err := supervisor.Reconcile(context.Background())
	if err != nil || count != 1 {
		t.Fatalf("cleanup reconciliation count=%d err=%v", count, err)
	}
	session, err := store.GetAgentSession(context.Background(), sessionID)
	if err != nil || session.Status != "completed" || strings.Contains(session.MetadataJSON, `"cleanup_pending":true`) {
		t.Fatalf("cleanup recovery changed terminal outcome or retained pending flag: session=%+v err=%v", session, err)
	}
	if _, err := os.Stat(sessionRoot); !os.IsNotExist(err) {
		t.Fatalf("terminal session workspace remains after cleanup recovery: %v", err)
	}
	if runtime.stopped != containerName(sessionID) {
		t.Fatalf("cleanup-pending terminal session did not verify container termination: %q", runtime.stopped)
	}
}

func TestExecutionSupervisorKeepsCleanupPendingWhenEgressCloseFails(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	supervisor.SetProviderEgressFactory(func(context.Context, string, string, []string) (providerEgressSession, error) {
		return supervisorFakeEgressSession{path: filepath.Join(t.TempDir(), "gateway.sock"), closed: &runtime.egressClosed, closeErr: errors.New("simulated gateway cleanup failure")}, nil
	})
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-egress-cleanup-failure", AgentID: "A-1", RunID: "R-1", TaskID: "T-1"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(outcome.Session.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["cleanup_pending"] != true || metadata["cleanup_failure"] == nil || outcome.Err == "" {
		t.Fatalf("failed egress cleanup was marked complete: outcome=%+v metadata=%+v", outcome, metadata)
	}
}

func TestExecutionSupervisorKeepsCleanupPendingWhenContainerRemovalFails(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), stopErr: errors.New("simulated container removal failure")}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-container-cleanup-failure", AgentID: "A-1", RunID: "R-1", TaskID: "T-1"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(outcome.Session.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["cleanup_pending"] != true || metadata["cleanup_failure"] == nil || outcome.Err == "" {
		t.Fatalf("failed container removal was marked complete: outcome=%+v metadata=%+v", outcome, metadata)
	}
}

func TestExecutionSupervisorDoesNotClearCleanupPendingWithoutVolumeCleaner(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	runtime := &supervisorRuntimeWithoutVolumeCleaner{base: &supervisorFakeRuntime{started: make(chan struct{})}}
	supervisor := NewExecutionSupervisor(repository, store, agentauth.New(nil), runtime, NewRegistry(supervisorConfig()), "")
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{ID: "S-no-volume-cleaner", AgentID: "A-1", RunID: "R-1", Adapter: "codex", Status: "failed", MetadataJSON: `{"mode":"container","cleanup_pending":true}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Reconcile(context.Background()); err == nil {
		t.Fatal("reconciliation claimed cleanup completed without a volume cleanup capability")
	}
	session, err := store.GetAgentSession(context.Background(), "S-no-volume-cleaner")
	if err != nil || !strings.Contains(session.MetadataJSON, `"cleanup_pending":true`) {
		t.Fatalf("unverified volume cleanup was not kept pending: session=%+v err=%v", session, err)
	}
}

func TestExecutionSupervisorPersistsCleanupFailureBeforeProvisioningCompletes(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	actualEgressPath := filepath.Join(t.TempDir(), "gateway.sock")
	supervisor.SetProviderEgressFactory(func(context.Context, string, string, []string) (providerEgressSession, error) {
		return supervisorFakeEgressSession{path: actualEgressPath, closed: &runtime.egressClosed, closeErr: errors.New("simulated provider socket cleanup failure")}, nil
	})
	supervisor.SetAgentHandlerFactory(func(string) (http.Handler, error) { return nil, errors.New("simulated scoped MCP setup failure") })
	if _, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-setup-cleanup-failure", AgentID: "A-1", RunID: "R-1", TaskID: "T-1"}, time.Minute); err == nil {
		t.Fatal("expected scoped MCP setup failure")
	}
	session, err := store.GetAgentSession(context.Background(), "S-setup-cleanup-failure")
	if err != nil {
		t.Fatalf("failed provisioning left no durable recovery record: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(session.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if session.Status != "failed" || metadata["cleanup_pending"] != true || metadata["cleanup_failure"] == nil {
		t.Fatalf("failed setup cleanup was not durably marked pending: session=%+v metadata=%+v", session, metadata)
	}
	if metadata["agent_egress_socket"] != actualEgressPath {
		t.Fatalf("cleanup record stored predicted rather than actual egress socket: got=%v want=%s", metadata["agent_egress_socket"], actualEgressPath)
	}
}

func TestExecutionSupervisorDoesNotReconcileLegacySessionsAsContainers(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	runtime := &supervisorFakeRuntime{started: make(chan struct{})}
	supervisor := NewExecutionSupervisor(repository, store, agentauth.New(nil), runtime, NewRegistry(supervisorConfig()), "")
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{ID: "legacy-session", AgentID: "A-1", RunID: "R-1", Adapter: "generic", WorkingDirectory: repository, Status: "running", MetadataJSON: `{"mode":"legacy"}`}); err != nil {
		t.Fatal(err)
	}
	count, err := supervisor.Reconcile(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("legacy sessions should not be reconciled as containers: count=%d err=%v", count, err)
	}
	session, err := store.GetAgentSession(context.Background(), "legacy-session")
	if err != nil || session.Status != "running" || runtime.stopped != "" {
		t.Fatalf("legacy session was mutated or stopped: session=%+v stopped=%q err=%v", session, runtime.stopped, err)
	}
}

func TestExecutionSupervisorReportsLifecyclePersistenceFailure(t *testing.T) {
	repository := supervisorGitRepo(t)
	store := supervisorStore(t)
	supervisorAssignTask(t, store, "implementer")
	runtime := &supervisorFakeRuntime{started: make(chan struct{}), waitForCancel: true}
	supervisor := newSupervisorWithTestMCP(t, repository, store, agentauth.New(nil), runtime)
	handle, err := supervisor.Launch(context.Background(), StartAgentRequest{SessionID: "S-db-fail", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "Implementer"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runtime.started:
	case <-time.After(5 * time.Second):
		t.Fatal("container runner did not start")
	}
	if err := store.DB().Close(); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Cancel("S-db-fail"); err != nil {
		t.Fatal(err)
	}
	outcome, err := handle.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outcome.Err, "persist") {
		t.Fatalf("execution did not report lifecycle persistence failure: %q", outcome.Err)
	}
}

type supervisorFakeRuntime struct {
	started           chan struct{}
	waitForCancel     bool
	editWorkspace     bool
	token             string
	stopped           string
	agentMCPSocket    string
	agentEgressSocket string
	egressClosed      bool
	stopErr           error
	duringRun         func(context.Context, sandbox.Spec) error
	stdout            string
}

type supervisorRuntimeWithoutVolumeCleaner struct{ base *supervisorFakeRuntime }

func (r *supervisorRuntimeWithoutVolumeCleaner) Name() string { return r.base.Name() }
func (r *supervisorRuntimeWithoutVolumeCleaner) Stop(ctx context.Context, name string) error {
	return r.base.Stop(ctx, name)
}
func (r *supervisorRuntimeWithoutVolumeCleaner) Run(ctx context.Context, spec sandbox.Spec) (sandbox.ExecutionResult, error) {
	return r.base.Run(ctx, spec)
}

func (r *supervisorFakeRuntime) Name() string { return "fake-container" }
func (r *supervisorFakeRuntime) Stop(_ context.Context, name string) error {
	r.stopped = name
	return r.stopErr
}
func (r *supervisorFakeRuntime) RemoveExecutionVolumes(_ string) error { return nil }
func (r *supervisorFakeRuntime) Run(ctx context.Context, spec sandbox.Spec) (sandbox.ExecutionResult, error) {
	r.token = spec.Env["ROUNDTABLE_AGENT_CAPABILITY"]
	if spec.AgentMCPSocketPath != "" {
		r.agentMCPSocket = spec.AgentMCPSocketPath
	}
	r.agentEgressSocket = spec.AgentEgressSocketPath
	close(r.started)
	started := time.Now().UTC()
	if r.duringRun != nil {
		if err := r.duringRun(ctx, spec); err != nil {
			return sandbox.ExecutionResult{ExitCode: -1, StartedAt: started, EndedAt: time.Now().UTC()}, err
		}
	}
	if r.waitForCancel {
		<-ctx.Done()
		result := sandbox.ExecutionResult{ExitCode: -1, StartedAt: started, EndedAt: time.Now().UTC(), Stdout: "echo " + r.token}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
		} else {
			result.Canceled = true
		}
		return result, ctx.Err()
	}
	if r.editWorkspace {
		if err := os.WriteFile(filepath.Join(spec.Workspace, "README.md"), []byte("base\nagent change\n"), 0o644); err != nil {
			return sandbox.ExecutionResult{}, err
		}
	}
	stdout := r.stdout
	if stdout == "" {
		stdout = "done"
	}
	return sandbox.ExecutionResult{ExitCode: 0, StartedAt: started, EndedAt: time.Now().UTC(), Stdout: stdout}, nil
}

func supervisorConfig() config.Config {
	cfg := config.Default()
	cfg.Adapters["codex"] = config.AdapterConfig{Image: "agent:test", Command: "codex", SupportsMCP: true, ProviderHosts: []string{"api.example.com"}}
	cfg.Agents.Implementers.Adapter = "codex"
	cfg.Agents.Reviewers.Adapter = "codex"
	cfg.Agents.MemoryOracle.Adapter = "codex"
	return cfg
}

func newSupervisorWithTestMCP(t *testing.T, repository string, store *db.Store, authority *agentauth.Authority, runtime sandbox.Runtime) *ExecutionSupervisor {
	t.Helper()
	supervisor := NewExecutionSupervisor(repository, store, authority, runtime, NewRegistry(supervisorConfig()), "http://127.0.0.1:7117/agent/mcp")
	fakeRuntime, ok := runtime.(*supervisorFakeRuntime)
	if !ok {
		t.Fatal("test supervisor requires fake runtime")
	}
	supervisor.SetProviderEgressFactory(func(_ context.Context, _, _ string, hosts []string) (providerEgressSession, error) {
		if len(hosts) != 1 || hosts[0] != "api.example.com" {
			return nil, errors.New("unexpected provider host policy")
		}
		return supervisorFakeEgressSession{path: filepath.Join(t.TempDir(), "gateway.sock"), closed: &fakeRuntime.egressClosed}, nil
	})
	socketDirs := make(map[string]string)
	supervisor.SetAgentSocketManager(func(sessionID string) (string, string, error) {
		testTemp := t.TempDir()
		tempRoot := filepath.Dir(filepath.Dir(testTemp))
		directory, err := os.MkdirTemp(tempRoot, "mcp-")
		if err != nil {
			return "", "", err
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return "", "", err
		}
		t.Cleanup(func() { _ = os.RemoveAll(directory) })
		socketDirs[sessionID] = directory
		return directory, filepath.Join(directory, "s"), nil
	}, func(sessionID string) error {
		directory := socketDirs[sessionID]
		if directory == "" {
			return nil
		}
		_ = os.Remove(filepath.Join(directory, "s"))
		delete(socketDirs, sessionID)
		return nil
	})
	supervisor.SetAgentHandlerFactory(func(token string) (http.Handler, error) {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}), nil
	})
	supervisor.SetAgentSocketListenerFactory(func(string) (net.Listener, error) {
		return newSupervisorTestListener(), nil
	})
	supervisor.SetProposalSubmitter(func(context.Context, string, map[string]any) (map[string]any, error) {
		return map[string]any{"proposal": db.Proposal{ID: "P-test"}}, nil
	})
	return supervisor
}

type supervisorFakeEgressSession struct {
	path     string
	closed   *bool
	closeErr error
}

func (s supervisorFakeEgressSession) SocketPath() string { return s.path }
func (s supervisorFakeEgressSession) Close() error       { *s.closed = true; return s.closeErr }

type supervisorTestListener struct {
	closed chan struct{}
	once   sync.Once
}

func newSupervisorTestListener() *supervisorTestListener {
	return &supervisorTestListener{closed: make(chan struct{})}
}

func (l *supervisorTestListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *supervisorTestListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (*supervisorTestListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "test-agent-mcp", Net: "unix"}
}

func (l *supervisorTestListener) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

func supervisorStore(t *testing.T) *db.Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db.NewStore(sqlDB, nil)
}

func supervisorAssignTask(t *testing.T, store *db.Store, role string) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertAgent(ctx, db.Agent{ID: "A-1", Role: role, Name: "Worker", Adapter: "generic", IsEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Task", BodyMD: "task", Status: "in_progress", AssignedAgentID: "A-1", ReadablePaths: []string{"README.md"}}); err != nil {
		t.Fatal(err)
	}
}

func supervisorGitRepo(t *testing.T) string {
	return supervisorGitRepoAt(t, ".")
}

func supervisorGitRepoAt(t *testing.T, parent string) string {
	t.Helper()
	root, err := os.MkdirTemp(parent, ".supervisor-repo-test-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "roundtable-test@example.invalid"}, {"config", "user.name", "Roundtable Test"}, {"config", "commit.gpgsign", "false"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".roundtable/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "add", ".")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	command = exec.Command("git", "commit", "-qm", "fixture")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	return root
}
