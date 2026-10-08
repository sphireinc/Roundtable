package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"roundtable/internal/claims"
	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/mcp"
	"roundtable/internal/orchestrator"
	"roundtable/internal/processlock"
	"roundtable/internal/scaffold"
	"roundtable/internal/sessions"
	"roundtable/internal/state"
	"roundtable/internal/symbols"
	"roundtable/internal/templates"
	"roundtable/internal/tui"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usage(stderr)
	}

	switch args[0] {
	case "init":
		return runInit(args[1:], stdout)
	case "run":
		return runRoundtable(ctx, args[1:], stdout)
	case "start":
		return runStart(ctx, args[1:], stdout, stderr)
	case "resume":
		return runResume(ctx, args[1:], stdout)
	case "sessions":
		return runSessions(ctx, args[1:], stdout)
	case "table":
		return runTable(ctx, args[1:], stdout)
	case "watch":
		return runWatch(ctx, args[1:], stdout)
	case "claims":
		return runClaims(ctx, args[1:], stdout)
	case "symbols":
		return runSymbols(args[1:], stdout)
	case "mcp":
		return runMCP(ctx, args[1:], stdout, stderr)
	default:
		return usage(stderr)
	}
}

func usage(w io.Writer) error {
	_, _ = fmt.Fprintln(w, "usage: roundtable <init|run|start|resume|sessions|table|watch|claims|symbols|mcp>")
	return errors.New("unknown command")
}

func runInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	force := fs.Bool("force", false, "overwrite generated files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := config.Default()
	starterFiles, err := templates.StarterFiles(cfg)
	if err != nil {
		return err
	}
	files := make([]scaffold.FileSpec, 0, len(starterFiles))
	for _, file := range starterFiles {
		files = append(files, scaffold.FileSpec(file))
	}

	if err := scaffold.EnsureLayout(*root, *force, files); err != nil {
		return err
	}

	dbPath := filepath.Join(*root, cfg.Storage.SQLitePath)
	sqlDB, err := db.Open(dbPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := syncAdapterCapabilities(context.Background(), store, cfg); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "initialized roundtable in %s\n", *root)
	return nil
}

func runRoundtable(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	runID := fs.String("run", "", "run id")
	goal := fs.String("goal", "", "goal description")
	resume := fs.Bool("resume", false, "resume an existing run")
	headless := fs.Bool("headless", false, "initialize run state without starting MCP server or TUI")
	interval := fs.Duration("interval", time.Second, "orchestration polling interval")
	retryBackoff := fs.Duration("retry-backoff", 250*time.Millisecond, "delay before retrying a failed orchestration cycle")
	maxErrors := fs.Int("max-consecutive-errors", 5, "stop after this many consecutive orchestration errors")
	maxIterations := fs.Int("max-iterations", 0, "stop after this many cycles; zero runs until convergence")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ownerLock, err := processlock.Acquire(*root)
	if err != nil {
		return err
	}
	defer ownerLock.Close()

	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}
	runtime, runtimeCleanup, err := mcp.OpenRuntime(*root)
	if err != nil {
		return err
	}
	defer runtimeCleanup()
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
	activeRun, err := ensureRunState(ctx, store, *runID, *goal, *resume)
	if err != nil {
		return err
	}
	if err := writeGeneratedMCPAssets(*root, cfg); err != nil {
		return err
	}
	loopOptions := orchestrator.LoopOptions{
		Interval:             *interval,
		RetryBackoff:         *retryBackoff,
		MaxConsecutiveErrors: *maxErrors,
		MaxIterations:        *maxIterations,
	}
	if *headless {
		if loopOptions.MaxIterations == 0 {
			loopOptions.MaxIterations = 1
		}
		_, loopErr := chair.Run(ctx, activeRun.ID, loopOptions)
		if loopErr != nil && !errors.Is(loopErr, orchestrator.ErrIterationLimit) {
			return loopErr
		}
		return insertRunSnapshot(ctx, store, activeRun.ID)
	}

	if err := insertRunSnapshot(ctx, store, activeRun.ID); err != nil {
		return err
	}
	snapshot, err := state.LoadSnapshot(ctx, store)
	if err != nil {
		return err
	}
	watchFeed, err := loadWatchFeed(ctx, store, activeRun.ID, cfg.MCP.SocketPath, 6)
	if err != nil {
		return err
	}
	server := mcp.NewServerWithRuntime(runtime, resolveSocketPath(*root, cfg.MCP.SocketPath), mcp.DefaultRegistry())
	if err := server.Start(ctx); err != nil {
		return err
	}
	defer server.Close()

	loopCtx, cancelLoop := context.WithCancel(ctx)
	defer cancelLoop()
	loopDone := make(chan error, 1)
	go func() {
		_, err := chair.Run(loopCtx, activeRun.ID, loopOptions)
		if errors.Is(err, context.Canceled) && loopCtx.Err() != nil {
			err = nil
		}
		loopDone <- err
	}()
	tuiDone := make(chan error, 1)
	go func() {
		tuiDone <- tui.Run(loopCtx, stdout, tui.Options{
			Goal:       activeRun.Goal,
			SocketPath: cfg.MCP.SocketPath,
			RunID:      activeRun.ID,
			Snapshot:   snapshot,
			WatchFeed:  watchFeed,
			Refresh: func(refreshCtx context.Context) (state.Snapshot, []string, error) {
				nextSnapshot, err := state.LoadSnapshot(refreshCtx, store)
				if err != nil {
					return state.Snapshot{}, nil, err
				}
				nextWatchFeed, err := loadWatchFeed(refreshCtx, store, activeRun.ID, cfg.MCP.SocketPath, 6)
				if err != nil {
					return state.Snapshot{}, nil, err
				}
				return nextSnapshot, nextWatchFeed, nil
			},
		})
	}()
	select {
	case err := <-loopDone:
		cancelLoop()
		if err != nil {
			return err
		}
		return nil
	case err := <-tuiDone:
		return err
	}
}

func runMCP(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: roundtable mcp <inspect|serve|stdio|call>")
	}
	switch args[0] {
	case "inspect":
		return runMCPInspect(args[1:], stdout)
	case "serve":
		return runMCPServe(ctx, args[1:], stdout)
	case "stdio":
		return runMCPStdio(ctx, args[1:], stdout, stderr)
	case "call":
		return runMCPCall(args[1:], stdout)
	default:
		return errors.New("unsupported mcp command")
	}
}

func runMCPStdio(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mcp stdio", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	endpoint := fs.String("url", "", "Streamable HTTP MCP endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}
	url := *endpoint
	if url == "" {
		scheme := "http"
		if cfg.MCP.TLSCertFile != "" && cfg.MCP.TLSKeyFile != "" {
			scheme = "https"
		}
		url = scheme + "://" + cfg.MCP.HTTPAddress + "/mcp"
	}
	return mcp.RunStdioBridge(ctx, os.Stdin, stdout, stderr, url, mcp.HTTPAuth{Token: os.Getenv("ROUNDTABLE_MCP_TOKEN")})
}

func runMCPInspect(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	write := fs.Bool("write", false, "rewrite generated MCP files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}
	registry := mcp.DefaultRegistry()
	schema, err := registry.ToolsSchemaJSON()
	if err != nil {
		return err
	}
	server, err := mcp.ServerConfigJSON(cfg)
	if err != nil {
		return err
	}

	if *write {
		files := []scaffold.FileSpec{
			{Path: ".roundtable/mcp/AGENT_MCP_MANIFEST.md", Content: registry.Manifest(cfg)},
			{Path: ".roundtable/mcp/tools.schema.json", Content: schema},
			{Path: ".roundtable/mcp/server.json", Content: server},
		}
		for _, file := range files {
			path := filepath.Join(*root, file.Path)
			if err := os.WriteFile(path, []byte(file.Content), 0o644); err != nil {
				return fmt.Errorf("write %s: %w", file.Path, err)
			}
		}
	}

	for _, name := range registry.StandardToolNames() {
		_, _ = fmt.Fprintln(stdout, name)
	}
	return nil
}

func runMCPServe(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ownerLock, err := processlock.Acquire(*root)
	if err != nil {
		return err
	}
	defer ownerLock.Close()

	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}

	runtime, cleanup, err := mcp.OpenRuntime(*root)
	if err != nil {
		return err
	}
	defer cleanup()
	server := mcp.NewServerWithRuntime(runtime, resolveSocketPath(*root, cfg.MCP.SocketPath), mcp.DefaultRegistry())
	if err := server.Start(ctx); err != nil {
		return err
	}
	defer server.Close()

	_, _ = fmt.Fprintf(stdout, "roundtable mcp serve listening on %s://%s\n", cfg.MCP.Transport, cfg.MCP.SocketPath)
	<-ctx.Done()
	return nil
}

func runMCPCall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	toolName := fs.String("tool", "", "tool name")
	argsPath := fs.String("args-file", "", "JSON file with tool arguments")
	useSocket := fs.Bool("socket", false, "call through unix socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *toolName == "" {
		return errors.New("tool is required")
	}

	payload := map[string]any{}
	if *argsPath != "" {
		var err error
		payload, err = mcp.ReadJSONFile(*argsPath)
		if err != nil {
			return err
		}
	}

	if *useSocket {
		return callMCPSocket(*root, *toolName, payload, stdout)
	}

	runtime, cleanup, err := mcp.OpenRuntime(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	result, err := runtime.Call(context.Background(), *toolName, payload)
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runTable(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("table", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(filepath.Join(*root, cfg.Storage.SQLitePath))
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, nil)
	snapshot, err := state.LoadSnapshot(ctx, store)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "RUNS %d\n", len(snapshot.Runs))
	for _, run := range snapshot.Runs {
		goal := run.Goal
		if goal == "" {
			goal = "(no goal)"
		}
		_, _ = fmt.Fprintf(stdout, "%s [%s] %s\n", run.ID, run.Status, goal)
	}
	_, _ = fmt.Fprintf(stdout, "AGENTS %d\n", len(snapshot.Agents))
	_, _ = fmt.Fprintf(stdout, "TASKS %d\n", len(snapshot.Tasks))
	_, _ = fmt.Fprintf(stdout, "CLAIMS %d\n", len(snapshot.Claims))
	_, _ = fmt.Fprintf(stdout, "PROPOSALS %d\n", len(snapshot.Proposals))
	_, _ = fmt.Fprintf(stdout, "TRANSACTIONS %d\n", len(snapshot.Transactions))
	_, _ = fmt.Fprintf(stdout, "MEMORY %d\n", len(snapshot.Memories))
	return nil
}

func runWatch(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	runID := fs.String("run", "", "run id filter")
	limit := fs.Int("limit", 10, "max recent events/transactions")
	follow := fs.Bool("follow", false, "poll and print updates continuously")
	interval := fs.Duration("interval", 2*time.Second, "poll interval when --follow is set")
	if err := fs.Parse(args); err != nil {
		return err
	}

	runtime, cleanup, err := mcp.OpenRuntime(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	lastEventID := int64(0)
	for {
		result, err := runtime.Call(ctx, "table.watch", map[string]any{
			"run_id": *runID,
			"limit":  *limit,
		})
		if err != nil {
			return err
		}

		lastEventID = printWatchSnapshot(stdout, result, lastEventID, *follow)
		if !*follow {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(clampWatchInterval(*interval)):
		}
	}
}

func runSessions(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "register":
			return runSessionsRegister(ctx, args[1:], stdout)
		case "heartbeat":
			return runSessionsHeartbeat(ctx, args[1:], stdout)
		case "end":
			return runSessionsEnd(ctx, args[1:], stdout)
		}
	}
	return runSessionsList(ctx, args, stdout)
}

func runSessionsList(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	service, cleanup, cfg, err := openSessionService(*root)
	if err != nil {
		return err
	}
	_ = cfg
	defer cleanup()
	found, err := service.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, session := range found {
		_, _ = fmt.Fprintf(stdout, "%s %s %s %s %s\n", session.ID, session.AgentID, session.Status, session.Adapter, session.ExternalResumeCommand)
	}
	return nil
}

func runSessionsRegister(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sessions register", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	id := fs.String("id", "", "session id")
	agentID := fs.String("agent", "", "agent id")
	runID := fs.String("run", "", "run id")
	adapter := fs.String("adapter", "", "adapter name")
	provider := fs.String("provider", "", "provider")
	model := fs.String("model", "", "model")
	externalSessionID := fs.String("external-session-id", "", "external session id")
	resumeCommand := fs.String("resume-command", "", "external resume command")
	workingDirectory := fs.String("cwd", "", "working directory")
	mcpSocket := fs.String("mcp-socket", "", "mcp socket path")
	status := fs.String("status", "active", "session status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *agentID == "" || *runID == "" || *adapter == "" || *workingDirectory == "" {
		return errors.New("id, agent, run, adapter, and cwd are required")
	}
	service, cleanup, cfg, err := openSessionService(*root)
	if err != nil {
		return err
	}
	defer cleanup()
	if *resumeCommand == "" {
		if adapterCfg, ok := cfg.Adapters[*adapter]; ok && adapterCfg.ResumePattern != "" && *externalSessionID != "" {
			*resumeCommand = strings.ReplaceAll(adapterCfg.ResumePattern, "{{external_session_id}}", *externalSessionID)
		}
	}
	session, err := service.Register(ctx, sessions.RegisterRequest{
		ID:                    *id,
		AgentID:               *agentID,
		RunID:                 *runID,
		Adapter:               *adapter,
		Provider:              *provider,
		Model:                 *model,
		ExternalSessionID:     *externalSessionID,
		ExternalResumeCommand: *resumeCommand,
		WorkingDirectory:      *workingDirectory,
		MCPSocket:             *mcpSocket,
		Status:                *status,
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s %s %s\n", session.ID, session.AgentID, session.Status, session.ExternalResumeCommand)
	return nil
}

func runSessionsHeartbeat(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sessions heartbeat", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	sessionID := fs.String("session", "", "session id")
	status := fs.String("status", "", "session status")
	externalSessionID := fs.String("external-session-id", "", "external session id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" {
		return errors.New("session is required")
	}
	service, cleanup, _, err := openSessionService(*root)
	if err != nil {
		return err
	}
	defer cleanup()
	session, err := service.Heartbeat(ctx, *sessionID, *status, *externalSessionID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s %s\n", session.ID, session.Status, session.LastSeenAt)
	return nil
}

func runSessionsEnd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sessions end", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	sessionID := fs.String("session", "", "session id")
	status := fs.String("status", "ended", "final session status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" {
		return errors.New("session is required")
	}
	service, cleanup, _, err := openSessionService(*root)
	if err != nil {
		return err
	}
	defer cleanup()
	session, err := service.End(ctx, *sessionID, *status)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s %s\n", session.ID, session.Status, session.EndedAt)
	return nil
}

func runResume(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	sessionID := fs.String("session", "", "agent session id")
	agentID := fs.String("agent", "", "agent id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *sessionID == "" && *agentID == "" {
		return errors.New("resume requires --session or --agent")
	}

	cfg, err := config.Load(*root)
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(filepath.Join(*root, cfg.Storage.SQLitePath))
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	service := sessions.NewService(db.NewStore(sqlDB, nil))
	claimService := claims.NewService(db.NewStore(sqlDB, nil))
	if *sessionID == "" {
		session, err := service.FindSessionByAgent(ctx, *agentID)
		if err != nil {
			return err
		}
		*sessionID = session.ID
	}
	session, err := service.GetSession(ctx, *sessionID)
	if err != nil {
		return err
	}
	suspended, err := claimService.ReconcileStaleClaims(ctx, claims.ReconcileRequest{
		Root:    *root,
		RunID:   session.RunID,
		AgentID: session.AgentID,
		ActorID: session.AgentID,
	})
	if err != nil {
		return err
	}
	briefing, err := service.BuildResumeBriefing(ctx, *sessionID)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(stdout, briefing.Markdown)
	if len(suspended) > 0 {
		_, _ = fmt.Fprintf(stdout, "\nStale claims suspended on resume:\n")
		for _, claim := range suspended {
			_, _ = fmt.Fprintf(stdout, "- %s (%s)\n", claim.ResourceID, claim.ID)
		}
	}
	return nil
}

func runClaims(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: roundtable claims <list|claim|release|revoke|expire|reconcile>")
	}
	switch args[0] {
	case "list":
		return runClaimsList(ctx, args[1:], stdout)
	case "claim":
		return runClaimsCreate(ctx, args[1:], stdout)
	case "release":
		return runClaimsRelease(ctx, args[1:], stdout)
	case "revoke":
		return runClaimsRevoke(ctx, args[1:], stdout)
	case "expire":
		return runClaimsExpire(ctx, args[1:], stdout)
	case "reconcile":
		return runClaimsReconcile(ctx, args[1:], stdout)
	default:
		return errors.New("unsupported claims command")
	}
}

func runClaimsList(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("claims list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	agentID := fs.String("agent", "", "agent id filter")
	resourceID := fs.String("resource", "", "resource id filter")
	status := fs.String("status", "", "status filter")
	if err := fs.Parse(args); err != nil {
		return err
	}

	service, cleanup, err := openClaimService(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	claims, err := service.List(ctx, claims.StatusFilter{
		AgentID:    *agentID,
		ResourceID: *resourceID,
		Status:     *status,
	})
	if err != nil {
		return err
	}
	for _, claim := range claims {
		_, _ = fmt.Fprintf(stdout, "%s %s %s %s %s\n", claim.ID, claim.AgentID, claim.ClaimType, claim.Status, claim.ResourceID)
	}
	return nil
}

func runClaimsCreate(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("claims claim", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	runID := fs.String("run", "", "run id")
	id := fs.String("id", "", "claim id")
	agentID := fs.String("agent", "", "agent id")
	taskID := fs.String("task", "", "task id")
	resourceID := fs.String("resource-id", "", "resource id")
	resourceType := fs.String("resource-type", "", "resource type")
	path := fs.String("path", "", "resource path")
	symbolName := fs.String("symbol", "", "resource symbol name")
	claimType := fs.String("claim-type", "write", "claim type")
	baseHash := fs.String("base-hash", "", "resource base hash")
	ttlSeconds := fs.Int("ttl", 900, "claim ttl in seconds")
	resumePolicy := fs.String("resume-policy", "hold", "resume policy")
	rationale := fs.String("rationale", "", "claim rationale")
	if err := fs.Parse(args); err != nil {
		return err
	}

	service, cleanup, err := openClaimService(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	claim, err := service.Create(ctx, claims.CreateRequest{
		ID:           *id,
		RunID:        *runID,
		AgentID:      *agentID,
		TaskID:       *taskID,
		ResourceID:   *resourceID,
		ResourceType: *resourceType,
		ResourcePath: *path,
		SymbolName:   *symbolName,
		ClaimType:    *claimType,
		BaseHash:     *baseHash,
		TTL:          time.Duration(*ttlSeconds) * time.Second,
		Renewable:    true,
		ResumePolicy: *resumePolicy,
		RationaleMD:  *rationale,
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s %s %s\n", claim.ID, claim.ClaimType, claim.Status, claim.ResourceID)
	return nil
}

func runClaimsRelease(ctx context.Context, args []string, stdout io.Writer) error {
	return runClaimsTransition(ctx, args, stdout, "release")
}

func runClaimsRevoke(ctx context.Context, args []string, stdout io.Writer) error {
	return runClaimsTransition(ctx, args, stdout, "revoke")
}

func runClaimsTransition(ctx context.Context, args []string, stdout io.Writer, mode string) error {
	fs := flag.NewFlagSet("claims "+mode, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	runID := fs.String("run", "", "run id")
	claimID := fs.String("claim", "", "claim id")
	actorID := fs.String("actor", "", "actor id")
	reason := fs.String("reason", "", "transition reason")
	if err := fs.Parse(args); err != nil {
		return err
	}

	service, cleanup, err := openClaimService(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	var claim db.Claim
	switch mode {
	case "release":
		claim, err = service.Release(ctx, *runID, *claimID, *actorID, *reason)
	case "revoke":
		claim, err = service.Revoke(ctx, *runID, *claimID, *actorID, *reason)
	default:
		return errors.New("unsupported claim transition")
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "%s %s\n", claim.ID, claim.Status)
	return nil
}

func runClaimsExpire(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("claims expire", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	runID := fs.String("run", "", "run id")
	atUnix := fs.String("at-unix", "", "optional unix timestamp override")
	if err := fs.Parse(args); err != nil {
		return err
	}

	service, cleanup, err := openClaimService(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	now := time.Now().UTC()
	if *atUnix != "" {
		value, err := strconv.ParseInt(*atUnix, 10, 64)
		if err != nil {
			return err
		}
		now = time.Unix(value, 0).UTC()
	}

	expired, err := service.ExpireDueClaims(ctx, *runID, now)
	if err != nil {
		return err
	}
	for _, claim := range expired {
		_, _ = fmt.Fprintf(stdout, "%s %s\n", claim.ID, claim.Status)
	}
	return nil
}

func runClaimsReconcile(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("claims reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	root := fs.String("root", ".", "project root")
	runID := fs.String("run", "", "run id")
	agentID := fs.String("agent", "", "agent id filter")
	actorID := fs.String("actor", "system", "actor id")
	if err := fs.Parse(args); err != nil {
		return err
	}

	service, cleanup, err := openClaimService(*root)
	if err != nil {
		return err
	}
	defer cleanup()

	suspended, err := service.ReconcileStaleClaims(ctx, claims.ReconcileRequest{
		Root:    *root,
		RunID:   *runID,
		AgentID: *agentID,
		ActorID: *actorID,
	})
	if err != nil {
		return err
	}
	for _, claim := range suspended {
		_, _ = fmt.Fprintf(stdout, "%s %s %s\n", claim.ID, claim.AgentID, claim.Status)
	}
	return nil
}

func openClaimService(root string) (*claims.Service, func(), error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.Open(filepath.Join(root, cfg.Storage.SQLitePath))
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = sqlDB.Close() }
	return claims.NewService(db.NewStore(sqlDB, nil)), cleanup, nil
}

func openSessionService(root string) (*sessions.Service, func(), config.Config, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, config.Config{}, err
	}
	sqlDB, err := db.Open(filepath.Join(root, cfg.Storage.SQLitePath))
	if err != nil {
		return nil, nil, config.Config{}, err
	}
	cleanup := func() { _ = sqlDB.Close() }
	return sessions.NewService(db.NewStore(sqlDB, nil)), cleanup, cfg, nil
}

func callMCPSocket(root, toolName string, payload map[string]any, stdout io.Writer) error {
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", resolveSocketPath(root, cfg.MCP.SocketPath))
	if err != nil {
		return err
	}
	defer conn.Close()

	request, err := json.Marshal(mcp.CallRequest{Tool: toolName, Args: payload})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(request, '\n')); err != nil {
		return err
	}

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}
	var response mcp.CallResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return err
	}
	if !response.OK {
		return errors.New(response.Error)
	}
	return writeJSON(stdout, response.Result)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func ensureRunState(ctx context.Context, store *db.Store, runID, goal string, resume bool) (db.Run, error) {
	if resume {
		if runID != "" {
			run, err := store.GetRun(ctx, runID)
			if err != nil {
				return db.Run{}, err
			}
			return reactivateRun(ctx, store, run)
		}
		runs, err := store.ListRuns(ctx)
		if err != nil {
			return db.Run{}, err
		}
		if len(runs) == 0 {
			return db.Run{}, errors.New("no run available to resume")
		}
		selected := runs[len(runs)-1]
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].Status == "active" {
				selected = runs[i]
				break
			}
		}
		return reactivateRun(ctx, store, selected)
	}

	if runID == "" {
		runID = generatedRunID()
	}
	run := db.Run{
		ID:     runID,
		Goal:   goal,
		Status: "active",
	}
	if err := store.UpsertRun(ctx, run); err != nil {
		return db.Run{}, err
	}
	return store.GetRun(ctx, runID)
}

func reactivateRun(ctx context.Context, store *db.Store, run db.Run) (db.Run, error) {
	if run.Status == "active" && run.EndedAt == "" {
		return run, nil
	}
	run.Status = "active"
	run.EndedAt = ""
	if err := store.UpsertRun(ctx, run); err != nil {
		return db.Run{}, err
	}
	return store.GetRun(ctx, run.ID)
}

func writeGeneratedMCPAssets(root string, cfg config.Config) error {
	registry := mcp.DefaultRegistry()
	schema, err := registry.ToolsSchemaJSON()
	if err != nil {
		return err
	}
	server, err := mcp.ServerConfigJSON(cfg)
	if err != nil {
		return err
	}
	files := []scaffold.FileSpec{
		{Path: ".roundtable/mcp/AGENT_MCP_MANIFEST.md", Content: registry.Manifest(cfg)},
		{Path: ".roundtable/mcp/tools.schema.json", Content: schema},
		{Path: ".roundtable/mcp/server.json", Content: server},
	}
	return scaffold.EnsureLayout(root, true, files)
}

func insertRunSnapshot(ctx context.Context, store *db.Store, runID string) error {
	snapshot, err := state.LoadSnapshot(ctx, store)
	if err != nil {
		return err
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return store.InsertRunSnapshot(ctx, db.RunSnapshot{
		ID:           "RS-" + time.Now().UTC().Format("20060102T150405.000000000"),
		RunID:        runID,
		SnapshotJSON: string(body),
	})
}

func loadWatchFeed(ctx context.Context, store *db.Store, runID, socketPath string, limit int) ([]string, error) {
	events, err := store.ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	lines := make([]string, 0, len(events)+2)
	for _, event := range events {
		lines = append(lines, fmt.Sprintf("%s %s %s", event.Type, blankCLI(event.ActorID, "system"), blankCLI(event.TaskID, "-")))
	}
	if len(lines) == 0 {
		lines = append(lines, "MCP server listening")
	}
	lines = append(lines, fmt.Sprintf("Socket: %s", blankCLI(socketPath, "not set")))
	lines = append(lines, "Press q to quit")
	return lines, nil
}

func printWatchSnapshot(stdout io.Writer, result map[string]any, lastEventID int64, incremental bool) int64 {
	_, _ = fmt.Fprintf(stdout, "WATCH run=%s active_agents=%v open_tasks=%v active_claims=%v suspended_claims=%v\n",
		valueOrAny(result, "run_id"),
		valueOrAny(result, "active_agents"),
		valueOrAny(result, "open_tasks"),
		valueOrAny(result, "active_claims"),
		valueOrAny(result, "suspended_claims"),
	)
	for _, task := range result["blocked_tasks"].([]db.Task) {
		_, _ = fmt.Fprintf(stdout, "BLOCKED %s %s\n", task.ID, task.Title)
	}
	for _, proposal := range result["pending_proposals"].([]db.Proposal) {
		_, _ = fmt.Fprintf(stdout, "PROPOSAL %s %s %s\n", proposal.ID, proposal.Status, proposal.Title)
	}
	for _, approval := range result["required_approvals"].([]db.HumanApproval) {
		_, _ = fmt.Fprintf(stdout, "APPROVAL %s %s %s\n", approval.ID, approval.Status, approval.Subject)
	}
	for _, tx := range result["transactions"].([]db.Transaction) {
		_, _ = fmt.Fprintf(stdout, "TX %s %s %s\n", tx.ID, tx.Status, tx.ProposalID)
	}

	maxEventID := lastEventID
	for _, event := range result["events"].([]db.Event) {
		if incremental && event.ID <= lastEventID {
			continue
		}
		_, _ = fmt.Fprintf(stdout, "EVENT %d %s %s %s\n", event.ID, event.Type, event.ActorID, event.TaskID)
		if event.ID > maxEventID {
			maxEventID = event.ID
		}
	}
	return maxEventID
}

func clampWatchInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 2 * time.Second
	}
	return interval
}

func valueOrAny(values map[string]any, key string) any {
	if value, ok := values[key]; ok {
		return value
	}
	return ""
}

func generatedRunID() string {
	return "RUN-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func blankCLI(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func resolveSocketPath(root, socketPath string) string {
	if filepath.IsAbs(socketPath) {
		return socketPath
	}
	return filepath.Join(root, socketPath)
}

func syncAdapterCapabilities(ctx context.Context, store *db.Store, cfg config.Config) error {
	for name, adapter := range cfg.Adapters {
		metadata, err := json.Marshal(map[string]any{
			"command":        adapter.Command,
			"resume_pattern": adapter.ResumePattern,
		})
		if err != nil {
			return err
		}
		if err := store.UpsertAdapterCapability(ctx, db.AdapterCapability{
			Adapter:                   name,
			SupportsResume:            adapter.SupportsResume,
			SupportsMCP:               adapter.SupportsMCP,
			SupportsReadOnlyWorkspace: adapter.SupportsReadOnlyWorkspace,
			SupportsSessionCapture:    adapter.CapturesSessionID,
			MetadataJSON:              string(metadata),
		}); err != nil {
			return err
		}
	}
	return nil
}

func runSymbols(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("symbols", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "source file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errors.New("symbols requires --path")
	}

	indexer := symbols.NewIndexer()
	found, err := indexer.IndexPath(*path)
	if err != nil {
		return err
	}
	for _, symbol := range found {
		_, _ = fmt.Fprintf(stdout, "%s %s %s %d %d\n", symbol.Kind, symbol.Name, symbol.Path, symbol.StartLine, symbol.EndLine)
	}
	return nil
}
