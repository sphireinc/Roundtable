package adapters

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"roundtable/internal/agentauth"
	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/egress"
	"roundtable/internal/sandbox"
)

type executionAdapter interface {
	Execute(context.Context, sandbox.Runtime, StartAgentRequest, sandbox.Spec) (LaunchSession, sandbox.ExecutionResult, error)
}

type containerStopper interface {
	Stop(context.Context, string) error
}

type executionVolumeCleaner interface {
	RemoveExecutionVolumes(string) error
}

type AgentHandlerFactory func(string) (http.Handler, error)
type providerEgressSession interface {
	SocketPath() string
	Close() error
}
type providerEgressFactory func(context.Context, string, string, []string) (providerEgressSession, error)
type proposalSubmitter func(context.Context, string, map[string]any) (map[string]any, error)

var safeExecutionSessionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

type ExecutionOutcome struct {
	Session           db.AgentSession
	Process           sandbox.ExecutionResult
	PatchArtifactPath string
	Err               string
}

type ExecutionHandle struct {
	done    chan struct{}
	mu      sync.RWMutex
	outcome ExecutionOutcome
}

func (h *ExecutionHandle) Wait(ctx context.Context) (ExecutionOutcome, error) {
	select {
	case <-ctx.Done():
		return ExecutionOutcome{}, ctx.Err()
	case <-h.done:
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.outcome, nil
	}
}

type activeExecution struct {
	cancel context.CancelFunc
	handle *ExecutionHandle
}

type ExecutionSupervisor struct {
	repositoryRoot string
	store          *db.Store
	authority      *agentauth.Authority
	runtime        sandbox.Runtime
	registry       *Registry
	mcpURL         string
	agentHandler   AgentHandlerFactory
	prepareSocket  func(string) (string, string, error)
	cleanupSocket  func(string) error
	listenSocket   func(string) (net.Listener, error)
	listenEgress   providerEgressFactory
	submitProposal proposalSubmitter
	mu             sync.Mutex
	active         map[string]activeExecution
	launching      map[string]bool
}

func NewExecutionSupervisor(repositoryRoot string, store *db.Store, authority *agentauth.Authority, runtime sandbox.Runtime, registry *Registry, mcpURL string) *ExecutionSupervisor {
	return &ExecutionSupervisor{
		repositoryRoot: repositoryRoot, store: store, authority: authority, runtime: runtime, registry: registry, mcpURL: mcpURL,
		prepareSocket: func(sessionID string) (string, string, error) {
			return sandbox.PrepareAgentMCPSocketDir(repositoryRoot, sessionID)
		},
		cleanupSocket: func(sessionID string) error { return sandbox.CleanupAgentMCPSocketDir(repositoryRoot, sessionID) },
		listenSocket:  listenAgentMCPSocket,
		listenEgress: func(ctx context.Context, repositoryRoot, sessionID string, hosts []string) (providerEgressSession, error) {
			return egress.ListenSessionIn(ctx, repositoryRoot, sessionID, egress.Policy{AllowedHosts: hosts}, nil)
		},
		active: make(map[string]activeExecution), launching: make(map[string]bool),
	}
}

func (s *ExecutionSupervisor) SetAgentHandlerFactory(factory AgentHandlerFactory) {
	s.agentHandler = factory
}

func (s *ExecutionSupervisor) SetAgentSocketManager(prepare func(string) (string, string, error), cleanup func(string) error) {
	s.prepareSocket, s.cleanupSocket = prepare, cleanup
}

func (s *ExecutionSupervisor) SetAgentSocketListenerFactory(listen func(string) (net.Listener, error)) {
	s.listenSocket = listen
}

func (s *ExecutionSupervisor) SetProviderEgressFactory(factory providerEgressFactory) {
	s.listenEgress = factory
}

func (s *ExecutionSupervisor) SetProposalSubmitter(submitter func(context.Context, string, map[string]any) (map[string]any, error)) {
	s.submitProposal = submitter
}

// DispatchAssigned launches persisted task assignments without manufacturing
// assignments or treating process completion as task acceptance.
func (s *ExecutionSupervisor) DispatchAssigned(ctx context.Context, runID string) error {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return err
	}
	sessions, err := s.store.ListAgentSessions(ctx)
	if err != nil {
		return err
	}
	proposals, err := s.store.ListProposals(ctx)
	if err != nil {
		return err
	}
	seenTasks := make(map[string]bool)
	for _, proposal := range proposals {
		if ProposalBlocksTaskExecution(proposal) {
			seenTasks[proposal.TaskID] = true
		}
	}
	busyAgents := make(map[string]bool)
	for _, session := range sessions {
		var metadata struct {
			TaskID string `json:"task_id"`
		}
		_ = json.Unmarshal([]byte(session.MetadataJSON), &metadata)
		if metadata.TaskID != "" && SessionBlocksTaskRetry(session, runID) {
			seenTasks[metadata.TaskID] = true
		}
		if session.Status == "starting" || session.Status == "running" {
			busyAgents[session.AgentID] = true
		}
	}
	for _, task := range tasks {
		if task.AssignedAgentID == "" || task.Status != "in_progress" || seenTasks[task.ID] || busyAgents[task.AssignedAgentID] {
			continue
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return fmt.Errorf("generate agent execution session identity: %w", err)
		}
		sessionID := "agent-" + hex.EncodeToString(random[:])
		if _, err := s.Launch(ctx, StartAgentRequest{SessionID: sessionID, AgentID: task.AssignedAgentID, RunID: runID, TaskID: task.ID}, 30*time.Minute); err != nil {
			return fmt.Errorf("launch assigned task %s for agent %s: %w", task.ID, task.AssignedAgentID, err)
		}
		seenTasks[task.ID] = true
		busyAgents[task.AssignedAgentID] = true
	}
	return nil
}

// SessionBlocksTaskRetry reports whether a previous execution already owns,
// completed, or handed off the assigned task. Failed attempts without a
// proposal remain dispatchable so an operator can correct configuration and retry.
func SessionBlocksTaskRetry(session db.AgentSession, currentRun ...string) bool {
	switch session.Status {
	case "starting", "running":
		return true
	case "completed":
		if sessionBlocksFromProposal(session) {
			return true
		}
		return len(currentRun) == 0 || session.RunID == currentRun[0]
	case "failed":
		return sessionBlocksFromProposal(session)
	}
	return false
}

func sessionBlocksFromProposal(session db.AgentSession) bool {
	var metadata struct {
		ProposalResult json.RawMessage `json:"proposal_result"`
	}
	if json.Unmarshal([]byte(session.MetadataJSON), &metadata) == nil {
		return len(metadata.ProposalResult) > 0 && string(metadata.ProposalResult) != "null"
	}
	return false
}

func ProposalBlocksTaskExecution(proposal db.Proposal) bool {
	switch proposal.Status {
	case "pending", "in_review", "revision_requested", "accepted", "applied":
		return true
	default:
		return false
	}
}

func listenAgentMCPSocket(path string) (net.Listener, error) {
	if len(path) > 107 {
		return nil, errors.New("managed agent MCP socket path exceeds the Unix socket limit")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set agent MCP socket permissions: %w", err)
	}
	return listener, nil
}

func (s *ExecutionSupervisor) Launch(parent context.Context, req StartAgentRequest, timeout time.Duration) (_ *ExecutionHandle, launchErr error) {
	if s == nil || s.store == nil || s.authority == nil || s.runtime == nil || s.registry == nil {
		return nil, errors.New("agent execution supervisor requires persistence, capability authority, configured container runtime, and adapter registry")
	}
	if timeout <= 0 || timeout > 30*time.Minute {
		return nil, errors.New("agent execution timeout must be between 1ns and 30m to match the maximum capability lifetime")
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.TaskID) == "" {
		return nil, errors.New("agent execution requires session, agent, run, and task identities")
	}
	agent, err := s.store.GetAgent(parent, req.AgentID)
	if err != nil || !agent.IsEnabled {
		return nil, errors.New("agent execution requires an enabled, persisted agent assignment")
	}
	task, err := s.store.GetTask(parent, req.TaskID)
	if err != nil || task.Status == "completed" || task.Status == "cancelled" || task.Status == "failed" {
		return nil, errors.New("agent execution requires an active task")
	}
	// Authorization uses persisted assignment data; the request's role is descriptive only.
	req.Role = agent.Role
	if strings.EqualFold(req.Role, "implementer") {
		if task.AssignedAgentID != agent.ID || req.ProposalID != "" {
			return nil, errors.New("implementer execution requires its assigned task and cannot be proposal-bound")
		}
	} else {
		proposal, proposalErr := s.store.GetProposal(parent, req.ProposalID)
		if req.ProposalID == "" || proposalErr != nil || proposal.TaskID != task.ID || proposal.AgentID == agent.ID || proposal.Status != "pending" && proposal.Status != "in_review" {
			return nil, errors.New("review execution requires a proposal-bound assignment for another agent's active proposal")
		}
		role := strings.ToLower(strings.TrimSpace(req.Role))
		if role == "tester" && proposal.Status != "in_review" || role == "security" && proposal.Risk != "high" && proposal.Risk != "critical" || role != "reviewer" && role != "tester" && role != "security" {
			return nil, errors.New("agent role is not eligible for this proposal review")
		}
	}
	if strings.TrimSpace(s.mcpURL) == "" {
		return nil, errors.New("trusted Roundtable agent MCP endpoint is not configured; refusing to launch without scoped capabilities")
	}
	if s.agentHandler == nil {
		return nil, errors.New("session-scoped agent MCP handler is not configured; refusing to launch without a private coordinator channel")
	}
	if s.prepareSocket == nil || s.cleanupSocket == nil || s.listenSocket == nil {
		return nil, errors.New("session-scoped agent MCP socket manager is not configured")
	}
	s.mu.Lock()
	if _, exists := s.active[req.SessionID]; exists || s.launching[req.SessionID] {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent session %q is already executing", req.SessionID)
	}
	s.launching[req.SessionID] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.launching, req.SessionID)
		s.mu.Unlock()
	}()
	if _, err := s.store.GetAgentSession(parent, req.SessionID); err == nil {
		return nil, fmt.Errorf("agent session %q already exists and cannot be reused", req.SessionID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check agent session identity: %w", err)
	}
	executionRoot, err := sandbox.ManagedExecutionRoot(s.repositoryRoot)
	if err != nil {
		return nil, err
	}
	sessionRoot := filepath.Join(executionRoot, req.SessionID)
	paths := sandbox.SnapshotPaths{ExecutionRoot: sessionRoot, Source: filepath.Join(sessionRoot, "source"), Workspace: filepath.Join(sessionRoot, "workspace"), Temp: filepath.Join(sessionRoot, "temp")}
	_, socketPath, err := sandbox.AgentMCPSocketPaths(s.repositoryRoot, req.SessionID)
	if err != nil {
		return nil, err
	}
	_, egressSocketPath, err := egress.ProjectSocketPaths(s.repositoryRoot, req.SessionID)
	if err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	metadata := map[string]any{"mode": "container", "role": req.Role, "task_id": req.TaskID, "proposal_id": req.ProposalID, "cleanup_pending": true,
		"container_name": containerName(req.SessionID), "runtime": s.runtime.Name(), "workspace": paths.Workspace,
		"source": paths.Source, "temp": paths.Temp, "execution_root": paths.ExecutionRoot,
		"agent_mcp_socket": socketPath, "agent_egress_socket": egressSocketPath, "timeout_seconds": timeout.Seconds()}
	session := db.AgentSession{ID: req.SessionID, AgentID: req.AgentID, RunID: req.RunID, Status: "starting", LastSeenAt: started.Format(time.RFC3339Nano), MetadataJSON: encodeJSON(metadata)}
	if err := s.store.UpsertAgentSession(parent, session); err != nil {
		return nil, fmt.Errorf("persist agent cleanup record before provisioning: %w", err)
	}
	cleanupOnError := true
	var agentMCPServer *http.Server
	var agentMCPListener net.Listener
	var egressSession providerEgressSession
	defer func() {
		if cleanupOnError {
			cleanupErr := stopAgentMCPServer(agentMCPServer, agentMCPListener)
			if egressSession != nil {
				cleanupErr = errors.Join(cleanupErr, egressSession.Close())
			}
			if stopper, ok := s.runtime.(containerStopper); ok {
				stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				cleanupErr = errors.Join(cleanupErr, stopper.Stop(stopCtx, containerName(req.SessionID)))
				cancel()
			} else {
				cleanupErr = errors.Join(cleanupErr, errors.New("container runtime cannot verify failed launch cleanup"))
			}
			if cleaner, ok := s.runtime.(executionVolumeCleaner); ok {
				cleanupErr = errors.Join(cleanupErr, cleaner.RemoveExecutionVolumes(containerName(req.SessionID)))
			} else {
				cleanupErr = errors.Join(cleanupErr, errors.New("container runtime cannot verify failed launch volume cleanup"))
			}
			cleanupErr = errors.Join(cleanupErr, s.cleanupSocket(req.SessionID))
			cleanupErr = errors.Join(cleanupErr, egress.CleanupProjectSocket(egressSocketPath, s.repositoryRoot, req.SessionID))
			cleanupErr = errors.Join(cleanupErr, sandbox.CleanupExecution(s.repositoryRoot, paths))
			_ = s.authority.Revoke(req.SessionID)
			session.Status = "failed"
			session.EndedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if cleanupErr != nil {
				metadata["cleanup_pending"] = true
				metadata["cleanup_failure"] = cleanupErr.Error()
				launchErr = errors.Join(launchErr, fmt.Errorf("failed launch cleanup: %w", cleanupErr))
			} else {
				delete(metadata, "cleanup_pending")
				delete(metadata, "cleanup_failure")
				metadata["cleanup_completed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			}
			session.MetadataJSON = encodeJSON(metadata)
			if err := s.store.UpsertAgentSession(context.Background(), session); err != nil {
				launchErr = errors.Join(launchErr, fmt.Errorf("persist failed launch cleanup state: %w", err))
			}
		}
	}()
	if _, err := s.store.AppendAgentSessionEvent(parent, db.AgentSessionEvent{SessionID: req.SessionID, EventType: "execution.starting", PayloadJSON: encodeJSON(map[string]any{"runtime": s.runtime.Name()})}); err != nil {
		return nil, fmt.Errorf("persist agent execution event: %w", err)
	}
	paths, err = sandbox.CreateScopedSnapshot(parent, s.repositoryRoot, req.SessionID, task.ReadablePaths)
	if err != nil {
		paths = sandbox.SnapshotPaths{ExecutionRoot: sessionRoot, Source: filepath.Join(sessionRoot, "source"), Workspace: filepath.Join(sessionRoot, "workspace"), Temp: filepath.Join(sessionRoot, "temp")}
		return nil, err
	}
	adapterName, provider, model, err := s.adapterForRole(req.Role)
	if err != nil {
		return nil, err
	}
	adapter, ok := s.registry.Get(adapterName)
	if !ok {
		return nil, fmt.Errorf("configured adapter %q is unavailable", adapterName)
	}
	executor, ok := adapter.(executionAdapter)
	if !ok {
		return nil, fmt.Errorf("adapter %q does not support container execution", adapterName)
	}
	adapterConfig := s.registry.config.Adapters[adapterName]
	if len(adapterConfig.ProviderHosts) == 0 {
		return nil, fmt.Errorf("adapter %q has no exact provider_hosts allowlist; refusing execution with no provider egress policy", adapterName)
	}
	if s.listenEgress == nil {
		return nil, errors.New("session-scoped provider egress gateway is not configured")
	}
	egressSession, err = s.listenEgress(parent, s.repositoryRoot, req.SessionID, adapterConfig.ProviderHosts)
	if err != nil {
		return nil, fmt.Errorf("start session-scoped provider egress gateway: %w", err)
	}
	metadata["agent_egress_socket"] = egressSession.SocketPath()
	session.MetadataJSON = encodeJSON(metadata)
	if err := s.store.UpsertAgentSession(parent, session); err != nil {
		return nil, fmt.Errorf("persist provider egress resource identity: %w", err)
	}
	req.Provider, req.Model = provider, model
	req.WorkingDirectory = paths.Workspace
	req.BaseRevision, req.BaseStateHash = paths.Revision, paths.BaseStateHash
	req.Prompt = composeExecutionPrompt(req, task)
	req.AgentMCPURL = s.mcpURL
	token, err := s.authority.Issue(agentauth.Identity{SessionID: req.SessionID, AgentID: req.AgentID, RunID: req.RunID, TaskID: req.TaskID, ProposalID: req.ProposalID, Role: req.Role, BaseRevision: paths.Revision, BaseStateHash: paths.BaseStateHash, SnapshotRoot: paths.Source}, 30*time.Minute)
	if err != nil {
		return nil, err
	}
	req.AgentCapabilityToken = token
	handler, err := s.agentHandler(token)
	if err != nil {
		_ = s.authority.Revoke(req.SessionID)
		return nil, fmt.Errorf("configure session-scoped agent MCP handler: %w", err)
	}
	_, socketPath, err = s.prepareSocket(req.SessionID)
	if err != nil {
		_ = s.authority.Revoke(req.SessionID)
		return nil, err
	}
	agentMCPListener, err = s.listenSocket(socketPath)
	if err != nil {
		_ = s.authority.Revoke(req.SessionID)
		return nil, fmt.Errorf("listen on session-scoped agent MCP socket: %w", err)
	}
	agentMCPServer = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() { _ = agentMCPServer.Serve(agentMCPListener) }()
	session.Adapter, session.Provider, session.Model = adapterName, provider, model
	session.WorkingDirectory, session.MCPSocket = paths.Workspace, socketPath
	session.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	metadata["base_revision"], metadata["base_state_hash"] = paths.Revision, paths.BaseStateHash
	metadata["agent_egress_socket"] = egressSession.SocketPath()
	session.MetadataJSON = encodeJSON(metadata)
	if err := s.store.UpsertAgentSession(parent, session); err != nil {
		return nil, fmt.Errorf("persist agent execution revision metadata: %w", err)
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	handle := &ExecutionHandle{done: make(chan struct{})}
	s.mu.Lock()
	s.active[req.SessionID] = activeExecution{cancel: cancel, handle: handle}
	s.mu.Unlock()
	cleanupOnError = false
	go s.run(ctx, cancel, req, paths, executor, handle, session, metadata, agentMCPServer, agentMCPListener, socketPath, egressSession)
	return handle, nil
}

func composeExecutionPrompt(req StartAgentRequest, task db.Task) string {
	var prompt strings.Builder
	prompt.WriteString("Roundtable execution contract (server-assigned; follow these constraints):\n")
	prompt.WriteString("Role: " + req.Role + "\nTask ID: " + req.TaskID + "\nSession ID: " + req.SessionID + "\nBase revision: " + req.BaseRevision + "\n")
	prompt.WriteString("Repository reads are limited to operator-assigned readable paths. Acquire a Roundtable resource claim before editing each file. Work only in the disposable assigned workspace. Submit changes as a Roundtable proposal; never attempt to mutate or apply changes to the authoritative repository. Process success is not task acceptance.\n")
	prompt.WriteString("Readable paths:\n")
	for _, path := range task.ReadablePaths {
		prompt.WriteString("- " + strconv.Quote(path) + "\n")
	}
	prompt.WriteString("\nAssigned task:\n")
	prompt.WriteString(task.BodyMD)
	return prompt.String()
}

func (s *ExecutionSupervisor) adapterForRole(role string) (string, string, string, error) {
	cfg := s.registryConfig()
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "implementer":
		return cfg.Agents.Implementers.Adapter, "", cfg.Agents.Implementers.Model, nil
	case "reviewer":
		return cfg.Agents.Reviewers.Adapter, "", cfg.Agents.Reviewers.Model, nil
	case "tester":
		return cfg.Agents.Tester.Adapter, "", cfg.Agents.Tester.Model, nil
	case "security", "security_agent":
		return cfg.Agents.Security.Adapter, "", cfg.Agents.Security.Model, nil
	default:
		return "", "", "", fmt.Errorf("role %q is not eligible for external container execution", role)
	}
}

// registryConfig is set alongside the registry to keep role-to-adapter choice server-owned.
func (s *ExecutionSupervisor) registryConfig() config.Config { return s.registry.config }

func (s *ExecutionSupervisor) Cancel(sessionID string) error {
	s.mu.Lock()
	execution, ok := s.active[sessionID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("agent execution %q is not active", sessionID)
	}
	execution.cancel()
	return nil
}

// Shutdown cancels active workers and waits for terminal persistence and cleanup.
func (s *ExecutionSupervisor) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	active := make(map[string]activeExecution, len(s.active))
	for id, execution := range s.active {
		active[id] = execution
	}
	s.mu.Unlock()
	for _, execution := range active {
		execution.cancel()
	}
	var shutdownErr error
	for id, execution := range active {
		if _, err := execution.handle.Wait(ctx); err != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("wait for agent execution %s shutdown: %w", id, err))
		}
	}
	return shutdownErr
}

// Reconcile stops and cleans sessions that were active when the coordinator last exited.
func (s *ExecutionSupervisor) Reconcile(ctx context.Context) (int, error) {
	sessions, err := s.store.ListAgentSessions(ctx)
	if err != nil {
		return 0, err
	}
	reconciled := 0
	for _, session := range sessions {
		active := session.Status == "starting" || session.Status == "running"
		metadata := make(map[string]any)
		if json.Unmarshal([]byte(session.MetadataJSON), &metadata) != nil || metadata["mode"] != "container" {
			continue
		}
		cleanupPending, _ := metadata["cleanup_pending"].(bool)
		if !active && !cleanupPending {
			continue
		}
		if active || cleanupPending {
			stopper, ok := s.runtime.(containerStopper)
			if !ok {
				return reconciled, errors.New("configured container runtime cannot stop orphaned executions")
			}
			if err := stopper.Stop(ctx, containerName(session.ID)); err != nil {
				return reconciled, fmt.Errorf("stop orphaned execution %s: %w", session.ID, err)
			}
			if cleaner, ok := s.runtime.(executionVolumeCleaner); ok {
				if err := cleaner.RemoveExecutionVolumes(containerName(session.ID)); err != nil {
					return reconciled, fmt.Errorf("remove orphaned execution volumes %s: %w", session.ID, err)
				}
			} else {
				return reconciled, errors.New("configured container runtime cannot verify orphaned execution volume cleanup")
			}
		}
		managedRoot, err := sandbox.ManagedExecutionRoot(s.repositoryRoot)
		if err != nil {
			return reconciled, err
		}
		sessionRoot := filepath.Join(managedRoot, session.ID)
		if egressSocket, _ := metadata["agent_egress_socket"].(string); egressSocket != "" {
			_, projectSocket, pathErr := egress.ProjectSocketPaths(s.repositoryRoot, session.ID)
			if pathErr == nil && egressSocket == projectSocket {
				err = egress.CleanupProjectSocket(egressSocket, s.repositoryRoot, session.ID)
			} else {
				// Clean up sockets created by older releases under system temp.
				err = egress.CleanupStaleSocket(egressSocket)
			}
			if err != nil {
				return reconciled, fmt.Errorf("clean orphaned provider egress socket %s: %w", session.ID, err)
			}
		}
		if err := s.cleanupSocket(session.ID); err != nil {
			return reconciled, fmt.Errorf("clean orphaned agent MCP socket %s: %w", session.ID, err)
		}
		paths := sandbox.SnapshotPaths{ExecutionRoot: sessionRoot, Source: filepath.Join(sessionRoot, "source"), Workspace: filepath.Join(sessionRoot, "workspace"), Temp: filepath.Join(sessionRoot, "temp")}
		if err := sandbox.CleanupExecution(s.repositoryRoot, paths); err != nil {
			return reconciled, fmt.Errorf("clean orphaned execution %s: %w", session.ID, err)
		}
		if active {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			session.Status, session.EndedAt, session.LastSeenAt = "interrupted", now, now
		}
		if err := s.releaseAbandonedExecutionClaims(ctx, session); err != nil {
			return reconciled, fmt.Errorf("release abandoned claims for execution %s: %w", session.ID, err)
		}
		delete(metadata, "cleanup_pending")
		delete(metadata, "cleanup_failure")
		metadata["cleanup_completed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		session.MetadataJSON = encodeJSON(metadata)
		if err := s.store.UpsertAgentSession(ctx, session); err != nil {
			return reconciled, fmt.Errorf("persist cleanup completion for execution %s: %w", session.ID, err)
		}
		eventType := "execution.cleanup_completed"
		if active {
			eventType = "execution.interrupted"
		}
		if _, err := s.store.AppendAgentSessionEvent(ctx, db.AgentSessionEvent{SessionID: session.ID, EventType: eventType, PayloadJSON: "{}"}); err != nil {
			return reconciled, fmt.Errorf("persist cleanup event for execution %s: %w", session.ID, err)
		}
		_ = s.authority.Revoke(session.ID)
		reconciled++
	}
	return reconciled, nil
}

func (s *ExecutionSupervisor) run(ctx context.Context, cancel context.CancelFunc, req StartAgentRequest, paths sandbox.SnapshotPaths, executor executionAdapter, handle *ExecutionHandle, session db.AgentSession, metadata map[string]any, agentMCPServer *http.Server, agentMCPListener net.Listener, agentMCPSocketPath string, egressSession providerEgressSession) {
	defer cancel()
	now := time.Now().UTC()
	session.Status, session.LastSeenAt = "running", now.Format(time.RFC3339Nano)
	var runErr error
	if err := s.store.UpsertAgentSession(context.Background(), session); err != nil {
		runErr = fmt.Errorf("persist running agent session before launch: %w", err)
	} else if _, err := s.store.AppendAgentSessionEvent(context.Background(), db.AgentSessionEvent{SessionID: req.SessionID, EventType: "execution.running", PayloadJSON: "{}"}); err != nil {
		runErr = fmt.Errorf("persist agent running event before launch: %w", err)
	}
	image := ""
	if adapter, ok := s.registry.Get(session.Adapter); ok {
		if cli, ok := adapter.(*CLIAdapter); ok {
			image = cli.cfg.Image
		}
	}
	digestName := containerName(req.SessionID)
	spec := sandbox.Spec{Name: digestName, Image: image, ExecutionRoot: paths.ExecutionRoot, AgentSessionID: req.SessionID, Source: paths.Source, Workspace: paths.Workspace, Temp: paths.Temp, AgentMCPSocketPath: agentMCPSocketPath, AgentEgressSocketPath: egressSession.SocketPath(), Timeout: time.Duration(metadata["timeout_seconds"].(float64) * float64(time.Second)), OutputLimitBytes: 1 << 20}
	launch := LaunchSession{}
	process := sandbox.ExecutionResult{ExitCode: -1, StartedAt: now}
	if runErr == nil {
		launch, process, runErr = executor.Execute(ctx, s.runtime, req, spec)
	}
	if process.EndedAt.IsZero() {
		process.EndedAt = time.Now().UTC()
	}
	process.Stdout = redactCapability(process.Stdout, req.AgentCapabilityToken)
	process.Stderr = redactCapability(process.Stderr, req.AgentCapabilityToken)
	if launch.Session.ID != "" {
		session.ExternalSessionID = launch.Session.ExternalSessionID
	}
	if process.TimedOut || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		session.Status = "timed_out"
	} else if process.Canceled || errors.Is(ctx.Err(), context.Canceled) {
		session.Status = "cancelled"
	} else if runErr != nil || process.ExitCode != 0 {
		session.Status = "failed"
	} else {
		session.Status = "completed"
	}
	var artifact string
	var patchText string
	if session.Status == "completed" {
		patch, err := sandbox.CollectPatch(paths)
		if err != nil {
			runErr = errors.Join(runErr, err)
			session.Status = "failed"
		} else if patch != "" {
			patchText = patch
			artifact, runErr = s.writePatchArtifact(req.SessionID, patch)
			if runErr != nil {
				session.Status = "failed"
			}
			if runErr == nil {
				if s.submitProposal == nil {
					runErr = errors.New("patch collected but authorized proposal handoff is not configured")
				} else {
					task, taskErr := s.store.GetTask(context.Background(), req.TaskID)
					if taskErr != nil {
						runErr = fmt.Errorf("load assigned task for proposal handoff: %w", taskErr)
					} else {
						result, submitErr := s.reconcileSessionProposal(context.Background(), req, patchText)
						if !result.found {
							result.value, submitErr = s.submitProposal(context.Background(), req.AgentCapabilityToken, map[string]any{
								"task_id": req.TaskID, "agent_id": req.AgentID, "title": task.Title,
								"summary_md": "Automated container execution completed; this patch remains subject to normal Roundtable review and transaction policy.",
								"risk":       task.Risk, "patch": patchText, "base_revision": req.BaseRevision,
								"base_state_hash": req.BaseStateHash,
							})
						}
						if submitErr != nil {
							runErr = fmt.Errorf("submit collected patch through authorized proposal workflow: %w", submitErr)
						} else {
							metadata["proposal_result"] = result.value
						}
					}
				}
				if runErr != nil {
					session.Status = "failed"
				}
			}
		}
	}
	egressCleanupErr := egressSession.Close()
	if egressCleanupErr != nil {
		runErr = errors.Join(runErr, fmt.Errorf("cleanup session-scoped provider egress gateway: %w", egressCleanupErr))
		session.Status = "failed"
	}
	ended := time.Now().UTC()
	session.EndedAt, session.LastSeenAt = ended.Format(time.RFC3339Nano), ended.Format(time.RFC3339Nano)
	metadata["container_name"] = digestName
	metadata["exit_code"] = process.ExitCode
	metadata["started_at"] = process.StartedAt.Format(time.RFC3339Nano)
	metadata["ended_at"] = ended.Format(time.RFC3339Nano)
	metadata["stdout"] = process.Stdout
	metadata["stderr"] = process.Stderr
	metadata["output_truncated"] = process.OutputTruncated
	metadata["patch_artifact"] = artifact
	metadata["cleanup_pending"] = true
	if runErr != nil {
		metadata["failure"] = redactCapability(runErr.Error(), req.AgentCapabilityToken)
	}
	session.MetadataJSON = encodeJSON(metadata)
	if err := s.store.UpsertAgentSession(context.Background(), session); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("persist terminal agent session state: %w", err))
	}
	if _, err := s.store.AppendAgentSessionEvent(context.Background(), db.AgentSessionEvent{SessionID: req.SessionID, EventType: "execution." + session.Status, PayloadJSON: encodeJSON(map[string]any{"exit_code": process.ExitCode, "patch_artifact": artifact})}); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("persist terminal agent session event: %w", err))
	}
	_ = s.authority.Revoke(req.SessionID)
	cleanupErr := errors.Join(egressCleanupErr, stopAgentMCPServer(agentMCPServer, agentMCPListener))
	if stopper, ok := s.runtime.(containerStopper); ok {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		cleanupErr = errors.Join(cleanupErr, stopper.Stop(stopCtx, digestName))
		stopCancel()
	} else {
		cleanupErr = errors.Join(cleanupErr, errors.New("container runtime cannot verify execution termination"))
	}
	if cleaner, ok := s.runtime.(executionVolumeCleaner); ok {
		cleanupErr = errors.Join(cleanupErr, cleaner.RemoveExecutionVolumes(digestName))
	} else {
		cleanupErr = errors.Join(cleanupErr, errors.New("container runtime cannot verify disposable volume cleanup"))
	}
	if err := s.cleanupSocket(req.SessionID); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup session-scoped agent MCP socket: %w", err))
	}
	if err := sandbox.CleanupExecution(s.repositoryRoot, paths); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup disposable agent workspace: %w", err))
	}
	if cleanupErr != nil {
		metadata["cleanup_pending"] = true
		metadata["cleanup_failure"] = cleanupErr.Error()
		runErr = errors.Join(runErr, cleanupErr)
	} else {
		delete(metadata, "cleanup_pending")
		delete(metadata, "cleanup_failure")
		metadata["cleanup_completed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	session.MetadataJSON = encodeJSON(metadata)
	if err := s.store.UpsertAgentSession(context.Background(), session); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("persist execution cleanup result: %w", err))
	}
	if cleanupErr == nil {
		if _, err := s.store.AppendAgentSessionEvent(context.Background(), db.AgentSessionEvent{SessionID: req.SessionID, EventType: "execution.cleanup_completed", PayloadJSON: "{}"}); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("persist execution cleanup completion event: %w", err))
		}
	}
	if err := s.releaseAbandonedExecutionClaims(context.Background(), session); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("release abandoned execution claims: %w", err))
	}
	handle.mu.Lock()
	handle.outcome = ExecutionOutcome{Session: session, Process: process, PatchArtifactPath: artifact}
	if runErr != nil {
		handle.outcome.Err = redactCapability(runErr.Error(), req.AgentCapabilityToken)
	}
	handle.mu.Unlock()
	s.mu.Lock()
	delete(s.active, req.SessionID)
	s.mu.Unlock()
	close(handle.done)
}

type proposalHandoff struct {
	found bool
	value map[string]any
}

func (s *ExecutionSupervisor) reconcileSessionProposal(ctx context.Context, req StartAgentRequest, patch string) (proposalHandoff, error) {
	proposals, err := s.store.ListProposals(ctx)
	if err != nil {
		return proposalHandoff{}, err
	}
	for _, proposal := range proposals {
		if proposal.ProposerSessionID != req.SessionID {
			continue
		}
		if proposal.TaskID != req.TaskID || proposal.AgentID != req.AgentID {
			return proposalHandoff{}, errors.New("execution proposal identity does not match its assigned task and agent")
		}
		if proposal.Status != "pending" && proposal.Status != "in_review" && proposal.Status != "revision_requested" {
			return proposalHandoff{}, errors.New("execution-submitted proposal is no longer eligible for patch handoff")
		}
		root, err := os.OpenRoot(s.repositoryRoot)
		if err != nil {
			return proposalHandoff{}, err
		}
		file, err := root.Open(filepath.FromSlash(proposal.PatchPath))
		_ = root.Close()
		if err != nil {
			return proposalHandoff{}, fmt.Errorf("open execution-submitted proposal patch: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, (32<<20)+1))
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return proposalHandoff{}, err
		}
		if len(data) > 32<<20 || string(data) != patch {
			return proposalHandoff{}, errors.New("execution-submitted proposal patch differs from collected workspace patch")
		}
		return proposalHandoff{found: true, value: map[string]any{"proposal": proposal, "reused": true}}, nil
	}
	return proposalHandoff{}, nil
}

func (s *ExecutionSupervisor) releaseAbandonedExecutionClaims(ctx context.Context, session db.AgentSession) error {
	var metadata struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(session.MetadataJSON), &metadata); err != nil {
		return fmt.Errorf("decode execution metadata: %w", err)
	}
	if metadata.TaskID == "" {
		return nil
	}
	proposals, err := s.store.ListProposals(ctx)
	if err != nil {
		return err
	}
	for _, proposal := range proposals {
		if proposal.ProposerSessionID == session.ID && (proposal.Status == "pending" || proposal.Status == "in_review" || proposal.Status == "revision_requested" || proposal.Status == "accepted") {
			return nil
		}
	}
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		if claim.ExecutionSessionID != session.ID || claim.AgentID != session.AgentID || claim.TaskID != metadata.TaskID || claim.Status != "active" {
			continue
		}
		claim.Status = "released"
		if err := s.store.UpsertClaim(ctx, claim); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]string{"claim_id": claim.ID, "reason": "agent execution ended without an open proposal"})
		if _, err := s.store.AppendEvent(ctx, db.Event{RunID: session.RunID, Type: "claim.released", ActorID: session.AgentID, TaskID: metadata.TaskID, PayloadJSON: string(payload)}); err != nil {
			return err
		}
	}
	return nil
}

func stopAgentMCPServer(server *http.Server, listener net.Listener) error {
	if server == nil || listener == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown session-scoped agent MCP server: %w", err)
	}
	return nil
}

func (s *ExecutionSupervisor) writePatchArtifact(sessionID, patch string) (string, error) {
	if len(patch) > 32<<20 {
		return "", errors.New("patch artifact exceeds 32 MiB")
	}
	rel := filepath.ToSlash(filepath.Join(".roundtable", "patches", "agent-executions", sessionID+".diff"))
	if !safeExecutionSessionID.MatchString(sessionID) {
		return "", errors.New("patch artifact requires a safe execution session identifier")
	}
	root, err := os.OpenRoot(s.repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("open repository root for patch artifact: %w", err)
	}
	defer root.Close()
	directory := root
	opened := make([]*os.Root, 0, 3)
	defer func() {
		for _, child := range opened {
			_ = child.Close()
		}
	}()
	for _, component := range []string{".roundtable", "patches", "agent-executions"} {
		directory, err = openPrivateArtifactDirectory(directory, component)
		if err != nil {
			return "", err
		}
		opened = append(opened, directory)
	}
	file, err := directory.OpenFile(sessionID+".diff", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create patch artifact: %w", err)
	}
	_, writeErr := file.WriteString(patch)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = directory.Remove(sessionID + ".diff")
		return "", err
	}
	return rel, nil
}

func openPrivateArtifactDirectory(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create private patch artifact directory %q: %w", name, err)
	}
	before, err := parent.Lstat(name)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing unsafe patch artifact directory %q", name)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, fmt.Errorf("open private patch artifact directory %q: %w", name, err)
	}
	after, err := child.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = child.Close()
		return nil, fmt.Errorf("patch artifact directory %q changed during open", name)
	}
	if err := child.Chmod(".", 0o700); err != nil {
		_ = child.Close()
		return nil, fmt.Errorf("secure patch artifact directory %q: %w", name, err)
	}
	return child, nil
}

func containerName(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return "rt-agent-" + hex.EncodeToString(digest[:8])
}

func encodeJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func redactCapability(value, token string) string {
	if token == "" {
		return value
	}
	return strings.ReplaceAll(value, token, "[REDACTED_AGENT_CAPABILITY]")
}
