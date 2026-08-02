# 0001 Bootstrap Roundtable

## Goal

Implement the initial Roundtable application skeleton.

## Scope

- Go module
- CLI entrypoint
- SQLite database initialization
- migrations
- config loading
- `roundtable init`
- generated `.roundtable/mcp/*` files
- placeholder Bubble Tea TUI
- placeholder MCP server

## Acceptance criteria

- `go test ./...` passes.
- `roundtable init` creates expected files/directories.
- `roundtable run` starts the TUI and MCP server in a basic operational mode.
- SQLite WAL is enabled.
- Generated MCP manifest exists.

## Resources

- cmd/roundtable/**
- internal/db/**
- internal/tui/**
- internal/mcp/**
- .roundtable/**

## Risk

Normal
