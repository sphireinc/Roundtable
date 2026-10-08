package app

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"roundtable/internal/config"
	"roundtable/internal/coordinator"
	"roundtable/internal/mcp"
	"roundtable/internal/orchestrator"
	"roundtable/internal/processlock"
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
	scheduler := coordinator.NewService(store, chair.Tick)
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
		Handler:           mcp.NewStreamableHTTPHandler(mcp.NewStandardServer(runtime), httpOptions),
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
	_, _ = fmt.Fprintf(stdout, "roundtable coordinator ready; project=%s http=%s/mcp socket=%s interval=%s\n", runtime.Root(), httpBaseURL(options), cfg.MCP.SocketPath, interval.String())
	return stopCoordinator(ctx, cancelRun, httpDone, coordinatorDone, httpServer, stderr, stdout)
}

func stopCoordinator(ctx context.Context, cancelRun context.CancelFunc, httpDone <-chan error, coordinatorDone <-chan error, httpServer *http.Server, stderr, stdout io.Writer) error {
	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-httpDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("MCP HTTP server stopped: %w", err)
		}
	}
	cancelRun()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()
	}
	select {
	case <-coordinatorDone:
	case <-shutdownCtx.Done():
		_, _ = fmt.Fprintln(stderr, "roundtable coordinator shutdown drain timed out")
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
