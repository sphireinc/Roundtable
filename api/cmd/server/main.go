package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"roundtable/api/internal/httpapi"
	"roundtable/internal/db"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	root := flag.String("workspace-root", ".", "allowed workspace root")
	databasePath := flag.String("database", ".roundtable/roundtable.db", "SQLite database path")
	flag.Parse()

	sqlDB, err := db.Open(*databasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := httpapi.NewServer(httpapi.Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{*root}})
	if err := server.Serve(ctx, *addr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
