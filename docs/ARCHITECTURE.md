# Architecture

## Runtime components

```text
Human
  │
  ▼
Bubble Tea TUI ───────────────┐
  │                            │
  ▼                            ▼
Roundtable Orchestrator ── SQLite WAL
  │                            ▲
  │                            │
  ├── MCP Server ──────────────┘
  │     ▲
  │     │
  │  CLI Agents
  │
  ├── Policy Engine
  ├── Claim Manager
  ├── Patch Transaction Manager
  ├── Git/Repo Manager
  ├── Symbol Indexer
  ├── Memory Oracle
  └── Event Bus
```

## Principles

1. One authoritative repository.
2. Agents receive a read-only view of the repo.
3. Agents propose patches; they do not apply patches.
4. Claims protect resources before proposals.
5. Consensus and policy gates decide whether patches may be applied.
6. The transaction manager applies patches and records audit trails.
7. SQLite is the durable source of truth.
8. The TUI is a live projection of SQLite state and the event stream.
9. External CLI session state is resumable, but non-authoritative.

## Main packages

```text
cmd/roundtable
  CLI entrypoint using Cobra or urfave/cli.

internal/tui
  Bubble Tea model/update/view architecture.

internal/db
  SQLite connection, WAL mode, migrations, repositories.

internal/mcp
  MCP-compatible server, generated manifest, tool registry.

internal/agents
  role definitions, chair loop, task assignment, prompt composition.

internal/adapters
  external CLI agent process management.

internal/repo
  git status, git hash, read-only workspace, patch validation, patch apply.

internal/symbols
  tree-sitter parsing and symbol index.

internal/policy
  risk classification, consensus thresholds, human approval requirements.

internal/memory
  persistent memory entries, Oracle behavior, summaries.

internal/security
  secret scanning, dangerous commands, high-risk resources.

internal/events
  append-only events and pub/sub fanout.
```

## Orchestration loop

1. Load config, policies, project docs, and tasks.
2. Start SQLite and event bus.
3. Start MCP server.
4. Generate MCP manifest and schemas.
5. Start TUI.
6. Chair decomposes/assigns tasks.
7. Agents query table/memory.
8. Agents claim resources.
9. Implementers propose patches.
10. Reviewers, Architect, Tester, Security vote/review.
11. Policy engine evaluates consensus.
12. Human approves high-risk changes.
13. Transaction manager validates and applies patch.
14. Tests run.
15. Memory Oracle records durable decisions.
16. TUI and watch feed update.

## Data flow for a patch

```text
Agent intent
  → resource.claim
  → proposal.create
  → patch.validate
  → vote.cast
  → policy.evaluate
  → human.request_approval if needed
  → patch.apply
  → transaction recorded
  → tests run
  → decision/memory updated
```

## Read-only repo strategy

Adapters should prefer one of:

- bind mount read-only directory into sandbox/container
- copy repo to temporary read-only workspace
- use filesystem permissions to prevent writes
- allow sandbox writes but export only patch artifacts

The authoritative repo is mutated only by the orchestrator.

## Event sourcing

Every meaningful action writes an event:

- agent started
- table queried
- task created/assigned
- resource claimed/released
- proposal created
- vote cast
- decision recorded
- patch validated/applied/rejected
- test run started/completed
- human approval requested/granted/denied
- memory entry recorded/staled

Materialized tables make UI queries fast.
