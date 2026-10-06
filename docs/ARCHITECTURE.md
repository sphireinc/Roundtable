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

### Durable append versus live notification

`Store.AppendEvent` inserts an `events` row, obtains its generated integer ID, reloads that row, and only then publishes it on the store's in-memory bus. The insert is not enclosed in a transaction with the subsequent reload/publication. An error after insertion can therefore leave a durable event even though the caller receives an error and subscribers receive no notification. Retrying is not automatically deduplicated by this general append method. Whether an entity mutation and its event are atomic depends on the particular service path, not this method's name.

`ListEvents` reads persisted rows in ascending ID order and optionally filters by exact run ID. Empty run ID reads all runs. The bus does not hold historical events, acknowledge delivery, persist subscriber positions, retry missed messages, or synthesize events for arbitrary SQL updates. Each `NewStore` creates its own bus when one is not supplied; stores share notifications only when explicitly given the same bus instance. Sharing a database file does not share a bus across stores or processes.

### In-process bus lifecycle

`Subscribe(buffer)` creates a channel of the requested capacity. Negative capacity is invalid and can panic; zero capacity creates an unbuffered channel. `Publish` attempts one nonblocking send per subscriber, dropping that subscriber's copy when it cannot immediately accept the value. A slow subscriber does not block all producers while waiting for space. There is no drop counter, overflow error, retry queue, or guaranteed delivery to an unbuffered subscriber. Map iteration does not define subscriber ordering, and concurrent publishers do not establish a total delivery order equivalent to database event IDs.

`Unsubscribe` removes and closes the matching channel; unknown channels are ignored. `Close` closes all subscribed channels and is idempotent. Publishing after close does nothing; subscribing after close returns an already-closed channel. Buffered values can still be read before a closed channel is drained. Closing a bus does not delete database events or close the SQLite handle. Consumers must handle channel closure and use persisted events for recovery rather than treating a channel receive as authoritative complete history.

### API WebSocket implications

The API's event WebSocket subscribes to its store bus with capacity 64 and separately reads historical workspace events using the requested cursor. Live bus notifications are therefore best-effort hints after the historical replay, not an exactly-once stream. There is no bus overflow signal when more notifications arrive than the buffer can hold. An event inserted by the separate Go runtime is not automatically published through the API process's bus; a shared SQLite file alone does not create cross-process live fanout.

The WebSocket sends a periodic ping containing the current persisted workspace sequence, which can reveal that durable state advanced beyond received notifications. Reconnect/replay or fetch the documented event snapshot when reconciliation is needed. Do not interpret a live connection, `resumable: true`, or an outbox table's existence as proof of complete real-time delivery. Cursor retention and replay rules are API-specific; consult the [Endpoint Guide](../api/docs/admin-api-guide.md) rather than applying MCP cursor fields to the WebSocket protocol.

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
