# Project: Roundtable

## Product definition

Roundtable is a shared-state agentic development management system. Many agents deliberate around one project state; repository mutations are governed through resource coordination, proposals, reviews/policy, and a transaction path. The durable project state, not an external agent's conversation history, is authoritative.

## Product surfaces

- Go CLI/runtime: initializes SQLite-backed state, serves local MCP tools, runs the coordinator loop, and provides table/watch/TUI projections.
- HTTP API: separate Go control-plane service with workspace-scoped REST and WebSocket/event endpoints, OpenAPI contract, governance and operational routes.
- Web UI: separate Next.js client for the API. The dashboard is wired; other shell links are not all implemented pages.
- Agent integration: MCP coordination, capability metadata, and session/briefing records exist. Automatic external CLI process launch, supervision, and session capture are not yet wired into the orchestrator.

## Implemented foundations and known boundaries

- SQLite persistence, schema initialization, WAL, event/snapshot data, task/claim/proposal/vote/decision/approval/transaction/test/memory records.
- Local MCP registry, generated manifest/schema/server metadata, repository read/search/symbol context, FIFO agent turn request events, and proposal/test/governance tools.
- File/directory/symbol claim overlap, TTL transitions, and stale base-hash reconciliation for supported resource types.
- Patch parsing, temporary-workspace validation, proposal resource coverage, policy/security/human gate evaluation, patch application, hashes, and rollback artifacts. Filesystem mutation and database persistence are not one atomic transaction; consult the transaction docs for recovery boundaries.
- Go symbol indexing uses the Go AST; TypeScript and Python use structural scanners. Tree-sitter-grade parsing is not present.
- The terminal UI is a polling snapshot layout with static command examples and limited keyboard handling, not the full command/selection/inspector experience in the design task pack.
- The browser UI currently wires the dashboard and workspace/branch context controls; the wider navigation and API contract describe broader current/planned scope. Consult the UI docs and task ledger before claiming a route is complete.

## Product invariants

1. One authoritative workspace state is shared by agents.
2. Agents use the repository read-only tool surface and submit patch artifacts; they do not directly edit the authoritative repository.
3. Resource claims coordinate overlapping work but are not an OS sandbox.
4. Policy, votes, security review, and human approvals are evaluated against persisted state before governed changes are applied.
5. The transaction/apply path records evidence and rollback material, but does not yet provide a cross-filesystem/SQLite atomic commit guarantee.
6. External agent sessions and memory support continuity; they do not override current task/claim/proposal/database state.

## Technology

The current repository uses Go, SQLite, Git, Bubble Tea, a local agent-tool registry and newline-delimited JSON Unix-socket protocol, a standalone Go HTTP API, and a Next.js/React/TypeScript UI. The local socket is Roundtable-specific rather than a standards-complete JSON-RPC MCP transport. Symbol indexing currently supports Go, TypeScript/TSX, and Python with the parser approaches described in the symbol documentation.

## Product thesis

Most AI coding systems focus on one agent editing directly or multiple agents working in isolated worktrees and merging afterward. Roundtable's intended distinction is shared deliberation over common state with governed, serialized repository changes. Features named in task packs or this product thesis remain planned until their implementation and acceptance evidence are complete.
