package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"roundtable/api/internal/httpapi"
	"roundtable/internal/db"
)

func isLoopbackAddress(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	root := flag.String("workspace-root", ".", "allowed workspace root")
	databasePath := flag.String("database", ".roundtable/roundtable.db", "SQLite database path")
	humanToken := flag.String("human-token", os.Getenv("ROUNDTABLE_HUMAN_TOKEN"), "bearer token for browser/human control requests")
	agentToken := flag.String("agent-token", os.Getenv("ROUNDTABLE_AGENT_TOKEN"), "bearer token for agent/MCP control requests")
	allowRemote := flag.Bool("allow-remote", false, "explicitly allow a non-loopback listen address")
	flag.Parse()
	if !*allowRemote && !isLoopbackAddress(*addr) {
		fmt.Fprintln(os.Stderr, "refusing non-loopback bind; pass -allow-remote explicitly")
		os.Exit(2)
	}

	sqlDB, err := db.Open(*databasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	version := os.Getenv("ROUNDTABLE_API_VERSION")
	server := httpapi.NewServer(httpapi.Config{Version: version, Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{*root}, HumanToken: *humanToken, AgentToken: *agentToken})
	if err := server.Serve(ctx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
