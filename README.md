# Roundtable

Roundtable is a local-first shared-state agentic development orchestrator.

Multiple AI agents deliberate together, claim resources, propose patches, vote under policy-weighted governance, and mutate one authoritative repository only through a transaction manager.

Roundtable is not parallel AI coding in silos. It is many agents at one table, one computer, one repo, one governed stream of changes.

## Category

Shared-State Agentic Development

## Core model

> Model C: Many agents deliberate together, but mutate one shared repo through a transaction manager.

## Primary goals

- Coordinate existing CLI coding agents such as Codex CLI, Claude Code, Gemini CLI, OpenCode, Cursor CLI, and generic shell agents.
- Force all agents through a local MCP-compatible tool surface.
- Keep the repository read-only to agents.
- Require patch-only proposals.
- Apply patches only through the orchestrator after claim validation, consensus, policy gates, and human approval where required.
- Maintain a literal live SQLite-backed consensus table.
- Support file, directory, symbol, command, schema, endpoint, and other resource claims.
- Provide a Bubble Tea TUI with live table, watch feed, human terminal, and inspector panes.
- Persist agent sessions so runs can be resumed, including adapter-specific resume commands such as `codex resume <session-id>`.
- Maintain persistent team memory through a Memory Oracle agent.

## Initial CLI flow

```bash
roundtable init
roundtable run --goal "Refactor auth to support refresh-token rotation without breaking login"
```

`roundtable init` creates the project scaffolding, including `.roundtable/mcp/*` files.

`roundtable run` starts the MCP server, launches/resumes agents, brings up the TUI, and starts orchestration.

## Important commands

```bash
roundtable init
roundtable run --goal "..."
roundtable run --task TASKS.ROUNDTABLE/0001-bootstrap.md
roundtable tui
roundtable watch
roundtable table
roundtable mcp serve
roundtable mcp inspect
roundtable mcp inspect --write
roundtable agents
roundtable claims
roundtable proposals
roundtable approve P-0001
roundtable reject P-0001 --reason "..."
roundtable veto P-0001 --reason "..."
roundtable resume
roundtable resume --agent implementer-1
roundtable sessions
roundtable memory query "why did we avoid Redis?"
```

## Repository layout

```text
cmd/roundtable/             CLI entrypoint
internal/tui/               Bubble Tea application and panes
internal/db/                SQLite, migrations, repositories, event log
internal/mcp/               MCP-compatible server and tool registry
internal/agents/            Role prompts, chair loop, orchestration policies
internal/adapters/          CLI adapters for codex/claude/gemini/opencode/generic
internal/repo/              Git integration, read-only workspace, patch validation/apply
internal/symbols/           Tree-sitter parsing and symbol index
internal/policy/            Consensus, risk, human approval, security gates
internal/memory/            Memory Oracle and durable memory store
internal/security/          Secret scanning, dangerous command/path checks
internal/events/            Pub/sub event bus for TUI/watch feed
migrations/                 SQLite migrations
TASKS.ROUNDTABLE/           Bite-size task files
.roundtable/                Local runtime state
```

## Non-negotiable rule

Agents never directly write to the authoritative repository.

Agents may read, discuss, claim resources, propose patches, vote, review, test, and query memory. The orchestrator alone applies mutations.
