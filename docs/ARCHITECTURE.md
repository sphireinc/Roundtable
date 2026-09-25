# Architecture

Roundtable's target model is many agents deliberating over one shared project state, with repository mutation governed by proposal review and a transaction path. The implementation is split into three executable surfaces; they share concepts and SQLite storage but are not one monolithic process.

## Components

| Component | Source | Responsibility | Current boundary |
| --- | --- | --- | --- |
| Go CLI/runtime | `cmd/roundtable`, `internal/` | Initialization, local SQLite-backed state, MCP tools/server, orchestration loop, claims/proposals/policy, terminal table/watch/TUI. | Does not launch agent CLI processes. |
| HTTP API | `api/cmd/server`, `api/internal/httpapi` | Versioned REST/WebSocket control plane, API-specific auth/CORS/workspace enforcement, dashboard and administration surfaces. | Separate server process; contract is `api/openapi.yaml`. |
| Web UI | `ui/src` | Browser admin shell and API client. | Dashboard is the only wired product page; many shell links represent unfinished task-pack scope. |

## Go runtime package map

- `internal/app`: command parsing and composition of runtime services.
- `internal/config`: defaults and supported `.roundtable/config.yaml` scalar parser.
- `internal/db`: SQLite connection, WAL/foreign-key/busy-timeout pragmas, schema migration, persistence repositories.
- `internal/state`: read-model snapshot assembled from persisted tables.
- `internal/mcp`: central tool registry, local runtime handlers, generated tool schema/manifest, Unix socket server.
- `internal/orchestrator`: agent-record synchronization, task assignment, proposal review requests, FIFO agent turn scheduling, retrying run loop, convergence decisions.
- `internal/claims`: resource normalization, overlap/conflict checks, TTL, transitions, and stale-session reconciliation.
- `internal/symbols`: Go AST parsing and structural TypeScript/Python extraction for symbol resources.
- `internal/proposals`, `internal/repo`: proposal lifecycle, patch parsing/validation/application, resource checks, and transaction records.
- `internal/policy`, `internal/security`: policy loading/evaluation and security-review records/checks used by proposal governance.
- `internal/sessions`, `internal/adapters`: session persistence/briefings and adapter metadata/command plans.
- `internal/events`: in-process event publication/subscription primitives.
- `internal/tui`: terminal projection of coordinator snapshots/watch feed.

These package roles are not equivalent to full automatic agent execution: in particular adapter command plans are not a process supervisor, and the TUI is not the browser UI. Details and limitations are documented per feature in the navigation.

## State and data flow

SQLite is the durable store for runs, agents/sessions, tasks, resources/claims, proposals/patches, votes/decisions, approvals/security reviews, test-run records, memory, and events. `db.Open` enables WAL, foreign keys, and a 5-second busy timeout before migrations. The optional config field `storage.wal` is currently parsed but does not turn WAL on/off.

The implemented high-level agent workflow is:

1. The coordinator exposes state and tools over MCP.
2. Agents read/search repository context, record tasks or claims, and submit proposals/patch artifacts through tools.
3. Runtime services validate proposals, claims, votes/reviews, and policy gates against current shared state.
4. Eligible patch application is performed by the Roundtable service path, which records transaction status and events.
5. The UI/TUI/CLI project persisted state; projections are not authoritative mutations.

The exact gate sequence and failure semantics depend on proposal status, policy, approval, and patch validation. See [Proposals and Transactions](TRANSACTIONS.md); do not infer the aspirational end-to-end flow in task specs as universally automatic today.

## Event and turn coordination

Durable domain events are persisted in SQLite and used by snapshots/watch queries. Agent turn requests are represented as durable scheduling events: outstanding requests are ordered FIFO, an agent starts its scheduled turn, and explicit completion permits the next request to be scheduled. Requests are deduplicated while outstanding. This is coordination metadata and does not itself execute the requested work.

The Go event bus provides in-process fanout; the standalone API has its own WebSocket/event snapshot implementation. Those are separate delivery paths and should not be assumed to share one process-local subscription.

## Trust boundaries

- The project database and policy files are authoritative local state.
- Agent-facing MCP operations do not grant agents direct authority to write the repository; proposals are the intended write boundary.
- The HTTP API enforces workspace roots and human/agent control surfaces independently of the local MCP runtime.
- Browser configuration is public build-time configuration. No privileged bearer token belongs in a `NEXT_PUBLIC_*` variable.
- A Unix socket is a local trust boundary, not a network authentication protocol.

## Related references

- [Configuration](CONFIGURATION.md) documents actual parser keys and defaults.
- [MCP Tools](MCP_TOOLS.md) inventories agent-callable tools.
- [Database](DATABASE.md) documents schema and persistence.
- [HTTP API](../api/README.md) covers service deployment and links to OpenAPI.
- [Web UI](UI.md) distinguishes current pages from planned navigation.
