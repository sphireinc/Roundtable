package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"roundtable/internal/adapters"
	"roundtable/internal/agentauth"
	"roundtable/internal/config"
	"roundtable/internal/coordinator"
	"roundtable/internal/db"
	"roundtable/internal/mcp"
	"roundtable/internal/orchestrator"
	"roundtable/internal/processlock"
	"roundtable/internal/sandbox"
)

func runStart(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	interval := fs.Duration("interval", time.Second, "active-run coordinator tick interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *interval <= 0 {
		return errors.New("coordinator interval must be positive")
	}
	canonicalRoot, err := canonicalProjectRoot(*root)
	if err != nil {
		return err
	}
	*root = canonicalRoot
	ownerLock, err := processlock.Acquire(*root)
	if err != nil {
		return err
	}
	defer ownerLock.Close()
	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}
	options := mcp.HTTPOptions{
		Address: cfg.MCP.HTTPAddress, Token: os.Getenv("ROUNDTABLE_MCP_TOKEN"),
		TLSCertFile: rooted(*root, cfg.MCP.TLSCertFile), TLSKeyFile: rooted(*root, cfg.MCP.TLSKeyFile),
	}
	if err := options.Validate(); err != nil {
		return err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if options.TLSCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(options.TLSCertFile, options.TLSKeyFile)
		if err != nil {
			return fmt.Errorf("load MCP HTTP TLS certificate and key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	runtime, cleanup, err := mcp.OpenRuntime(*root)
	if err != nil {
		return err
	}
	defer cleanup()
	agentAuthority := agentauth.New(nil)
	runtime.SetAgentAuthority(agentAuthority)
	store := runtime.Store()
	if err := syncAdapterCapabilities(ctx, store, cfg); err != nil {
		return err
	}
	chair, err := orchestrator.NewService(*root, cfg, store)
	if err != nil {
		return err
	}
	if err := chair.SyncAgents(ctx); err != nil {
		return err
	}
	reconciled, err := reconcileContainerExecutions(ctx, *root, store, agentAuthority, cfg, sandbox.Detect)
	if err != nil {
		return err
	}
	if reconciled > 0 {
		_, _ = fmt.Fprintf(stdout, "roundtable coordinator: reconciled %d interrupted container execution(s)\n", reconciled)
	}
	var dispatchMu sync.Mutex
	var supervisorMu sync.RWMutex
	var supervisor *adapters.ExecutionSupervisor
	dispatchAssigned := func(dispatchCtx context.Context, runID string) error {
		tasks, err := store.ListTasks(dispatchCtx)
		if err != nil {
			return err
		}
		sessions, err := store.ListAgentSessions(dispatchCtx)
		if err != nil {
			return err
		}
		proposals, err := store.ListProposals(dispatchCtx)
		if err != nil {
			return err
		}
		seenTasks, busyAgents := make(map[string]bool), make(map[string]bool)
		for _, proposal := range proposals {
			if adapters.ProposalBlocksTaskExecution(proposal) {
				seenTasks[proposal.TaskID] = true
			}
		}
		for _, session := range sessions {
			var metadata struct {
				TaskID string `json:"task_id"`
			}
			_ = json.Unmarshal([]byte(session.MetadataJSON), &metadata)
			if metadata.TaskID != "" && adapters.SessionBlocksTaskRetry(session, runID) {
				seenTasks[metadata.TaskID] = true
			}
			if session.Status == "starting" || session.Status == "running" {
				busyAgents[session.AgentID] = true
			}
		}
		hasDispatchableWork := false
		for _, task := range tasks {
			if task.AssignedAgentID != "" && task.Status == "in_progress" && !seenTasks[task.ID] && !busyAgents[task.AssignedAgentID] {
				hasDispatchableWork = true
				break
			}
		}
		if !hasDispatchableWork {
			return nil
		}
		dispatchMu.Lock()
		defer dispatchMu.Unlock()
		if supervisor == nil {
			containerRuntime, err := sandbox.Detect(*root)
			if err != nil {
				return err
			}
			created := adapters.NewExecutionSupervisor(*root, store, agentAuthority, containerRuntime, adapters.NewRegistry(cfg), "http://127.0.0.1:7117/agent/mcp")
			created.SetAgentHandlerFactory(func(token string) (http.Handler, error) {
				return mcp.NewScopedAgentHTTPHandler(runtime, mcp.HTTPOptions{Address: "127.0.0.1:7117"}, token)
			})
			created.SetProposalSubmitter(func(callCtx context.Context, token string, args map[string]any) (map[string]any, error) {
				return runtime.CallAgent(callCtx, token, "proposal.create", args)
			})
			supervisorMu.Lock()
			supervisor = created
			supervisorMu.Unlock()
		}
		return supervisor.DispatchAssigned(dispatchCtx, runID)
	}
	schedulerTick := func(tickCtx context.Context, runID string) error {
		if err := chair.Tick(tickCtx, runID); err != nil {
			return err
		}
		return dispatchAssigned(tickCtx, runID)
	}
	scheduler := coordinator.NewService(store, schedulerTick)
	scheduler.SetLogger(func(message string) { _, _ = fmt.Fprintf(stdout, "roundtable coordinator: %s\n", message) })
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	server := mcp.NewServerWithRuntime(runtime, resolveSocketPath(*root, cfg.MCP.SocketPath), nil)
	if err := server.Start(runCtx); err != nil {
		return err
	}
	defer func() {
		cancelRun()
		if err := server.Close(); err != nil {
			_, _ = fmt.Fprintf(stderr, "roundtable: %v\n", err)
		}
	}()

	listener, err := net.Listen("tcp", cfg.MCP.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen for MCP HTTP: %w", err)
	}
	httpOptions := options
	httpOptions.Health = func() string { return scheduler.Health().Status }
	httpServer := &http.Server{
		Handler:           mcp.NewRoundtableHTTPHandler(runtime, httpOptions),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		TLSConfig:         tlsConfig,
	}
	httpDone := make(chan error, 1)
	go func() {
		var serveErr error
		if options.TLSCertFile != "" || options.TLSKeyFile != "" {
			serveErr = httpServer.ServeTLS(listener, "", "")
		} else {
			serveErr = httpServer.Serve(listener)
		}
		httpDone <- serveErr
	}()
	coordinatorDone := make(chan error, 1)
	go func() { coordinatorDone <- scheduler.Run(runCtx, *interval) }()
	_, _ = fmt.Fprintf(stdout, "roundtable coordinator ready; project=%s admin_http=%s/mcp agent_http=%s/agent/mcp socket=%s interval=%s\n", runtime.Root(), httpBaseURL(options), httpBaseURL(options), cfg.MCP.SocketPath, interval.String())
	return stopCoordinator(ctx, cancelRun, httpDone, coordinatorDone, httpServer, func() *adapters.ExecutionSupervisor {
		supervisorMu.RLock()
		defer supervisorMu.RUnlock()
		return supervisor
	}, stderr, stdout)
}

func canonicalProjectRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve project root symlinks: %w", err)
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("project root %q must be an existing directory", root)
	}
	return canonical, nil
}

func reconcileContainerExecutions(ctx context.Context, root string, store *db.Store, authority *agentauth.Authority, cfg config.Config, detectRuntime func(string) (sandbox.Runtime, error)) (int, error) {
	sessions, err := store.ListAgentSessions(ctx)
	if err != nil {
		return 0, fmt.Errorf("inspect persisted agent sessions before startup: %w", err)
	}
	needsRecovery := false
	for _, session := range sessions {
		var metadata struct {
			Mode           string `json:"mode"`
			CleanupPending bool   `json:"cleanup_pending"`
		}
		active := session.Status == "starting" || session.Status == "running"
		if json.Unmarshal([]byte(session.MetadataJSON), &metadata) == nil && metadata.Mode == "container" && (active || metadata.CleanupPending) {
			needsRecovery = true
			break
		}
	}
	if !needsRecovery {
		return 0, nil
	}
	containerRuntime, err := detectRuntime(root)
	if err != nil {
		return 0, fmt.Errorf("recover interrupted container executions before coordinator startup: %w", err)
	}
	supervisor := adapters.NewExecutionSupervisor(root, store, authority, containerRuntime, adapters.NewRegistry(cfg), "")
	reconciled, err := supervisor.Reconcile(ctx)
	if err != nil {
		return reconciled, fmt.Errorf("recover interrupted container executions before coordinator startup: %w", err)
	}
	return reconciled, nil
}

func stopCoordinator(ctx context.Context, cancelRun context.CancelFunc, httpDone <-chan error, coordinatorDone <-chan error, httpServer *http.Server, getSupervisor func() *adapters.ExecutionSupervisor, stderr, stdout io.Writer) error {
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-httpDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("MCP HTTP server stopped: %w", err)
		}
	}
	cancelRun()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
	}
	select {
	case <-coordinatorDone:
	case <-shutdownCtx.Done():
		_, _ = fmt.Fprintln(stderr, "roundtable coordinator shutdown drain timed out")
	}
	var supervisor *adapters.ExecutionSupervisor
	if getSupervisor != nil {
		supervisor = getSupervisor()
	}
	if supervisor != nil {
		if err := supervisor.Shutdown(shutdownCtx); err != nil {
			_, _ = fmt.Fprintf(stderr, "roundtable agent shutdown: %v\n", err)
			if serveErr == nil {
				serveErr = err
			}
		}
	}
	_, _ = fmt.Fprintln(stdout, "roundtable coordinator stopped")
	return serveErr
}

func rooted(root, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(root, value)
}

func httpBaseURL(options mcp.HTTPOptions) string {
	scheme := "http"
	if options.TLSCertFile != "" && options.TLSKeyFile != "" {
		scheme = "https"
	}
	return scheme + "://" + options.Address
}
