# Project: Roundtable

## Product definition

Roundtable is a local-first, Go-based, Bubble Tea-powered TUI for shared-state agentic development.

It coordinates existing AI coding CLIs through MCP, gives every agent access to a live SQLite-backed consensus table, enforces file/symbol/resource claims, requires policy-weighted consensus before patches are applied, and mutates a single authoritative repo only through an orchestrated transaction manager.

## No web dashboard

No web dashboard is currently roadmapped. The primary interface is CLI + TUI.

## Language and stack

- Go
- SQLite WAL
- Git
- Bubble Tea
- Bubbles
- MCP-compatible server
- Tree-sitter for symbols
- CLI adapters for existing coding tools

## Required first-class features

- `roundtable init`
- `roundtable run`
- Bubble Tea TUI
- local SQLite database
- MCP server and generated MCP manifest files
- live table view
- watch feed
- human command terminal
- inspector pane
- role-based agents
- configurable implementer count
- resource claims
- symbol-level claims via tree-sitter
- patch proposals
- consensus voting
- policy-weighted governance
- human approval policies
- security vetoes
- orchestrator-applied diffs
- transaction ids
- persistent team memory
- Memory Oracle agent
- CLI agent adapters
- durable agent sessions and resume support

## Product thesis

Most AI coding systems are one of:

- one agent editing directly
- many agents working in isolated branches/worktrees and merging later

Roundtable is different:

- many agents deliberate together
- claims prevent collisions
- all mutations are serialized through a transaction manager
- governance decides what is allowed
- the repository remains authoritative and coherent
