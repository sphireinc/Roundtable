# CLI Reference

The executable is `cmd/roundtable`; invoke it as `roundtable` after installation/build or use `go run ./cmd/roundtable`. The supported top-level commands are exactly `init`, `run`, `resume`, `sessions`, `table`, `watch`, `claims`, `symbols`, and `mcp`. This reference follows flag definitions in `internal/app/app.go`; unsupported command names are rejected.

Unless stated otherwise, `--root` defaults to `.` and selects the project root containing `.roundtable/config.yaml` and the configured SQLite file.

## Initialize a project

```sh
roundtable init [--root DIR] [--force]
```

Creates generated project protocol, policy, task, memory, and MCP files; opens/migrates the SQLite database; and synchronizes adapter capability records. Existing generated files are preserved by default. `--force` permits overwriting generated files. The starter layout is defined in `internal/templates`; it does not create every directory shown in older planning documents.

## Run and resume orchestration

```sh
roundtable run [--root DIR] [--run ID] [--goal TEXT] [--resume] [--headless]
               [--interval DURATION] [--retry-backoff DURATION]
               [--max-consecutive-errors N] [--max-iterations N]
```

| Flag | Default | Details |
| --- | --- | --- |
| `--root` | `.` | Project root. |
| `--run` | generated ID | Select/create run identifier. With `--resume`, identifies the run to reactivate; without an ID, resume selects an active run if available, otherwise the latest run. |
| `--goal` | empty | Goal stored for a newly created run. It does not parse task files or create tasks automatically. |
| `--resume` | `false` | Reactivate existing run state instead of creating a new run. |
| `--headless` | `false` | Run without starting MCP server or TUI; defaults to one cycle if no iteration bound is supplied. |
| `--interval` | `1s` | Delay between successful nonterminal cycles. |
| `--retry-backoff` | `250ms` | Delay after a failed cycle. |
| `--max-consecutive-errors` | `5` | Fail the run after this many consecutive cycle errors. |
| `--max-iterations` | `0` | Maximum cycles; zero means unlimited except headless mode's one-cycle default. |

Interactive mode runs the MCP server and TUI while the orchestration loop continues. It does not launch configured CLI agents. See [Orchestration](ORCHESTRATION.md).

`resume` prints a briefing for a registered agent session and reconciles stale claims for that agent; it does not restart the external CLI or the TUI:

```sh
roundtable resume --root DIR (--session SESSION_ID | --agent AGENT_ID)
```

## State and monitoring

```sh
roundtable table [--root DIR]
roundtable watch [--root DIR] [--run RUN_ID] [--limit N] [--follow] [--interval DURATION]
```

`table` prints run IDs/status/goals and counts for agents, tasks, claims, proposals, transactions, and memory entries. `watch` prints a snapshot of activity/blocked tasks/pending proposals/approvals/transactions/events. `--limit` defaults to `10`; `--follow` polls for updates; `--interval` defaults to `2s` and is clamped to a safe positive value. This is polling, not a push subscription.

### Watch output and polling semantics

`watch` opens the local runtime/database directly; it does not connect to the MCP socket or start an orchestrator. Each poll calls the same `table.watch` handler used by MCP, then formats the result as human-readable lines:

| Prefix | Contents |
| --- | --- |
| `WATCH` | Requested run filter plus active-agent, open-task, active-claim, and suspended-claim counts. |
| `BLOCKED` | ID and title for blocked tasks. |
| `PROPOSAL` | ID, status, and title for pending or in-review proposals. |
| `APPROVAL` | ID, status, and subject for requested human approvals. |
| `TX` | ID, status, and proposal ID for recent transactions. |
| `EVENT` | Database event ID, type, actor ID, and task ID. |

The run filter scopes only the event list. Snapshot counts and the blocked-task, proposal, and approval lists, as well as the selected transaction tail, are read from workspace-wide state. These collections are loaded with separate queries rather than one read transaction, so concurrent changes can make a single printed snapshot internally time-skewed. Treat the display as an operator overview, not as an atomic authorization or write precondition.

Without `--follow`, the command prints one poll and exits. Its event list is the newest `--limit` events for the requested run (or all runs when `--run` is empty), printed oldest-to-newest within that selected window; older omitted events are not reported. The transaction list is independently limited to the tail of the store's list order and is not filtered by `--run`. The MCP handler normalizes a nonpositive limit to `10` and caps values above `200`; the CLI does not validate or clamp the flag itself.

With `--follow`, the command polls every interval and prints each `WATCH`/projection/transaction section again. It suppresses event lines whose database IDs are no greater than the largest ID already printed, so previously visible events are not repeated. It does **not** send `after_event_id` to the handler: each poll still asks only for the newest limited window. If more than `--limit` new events arrive between polls, events that have already fallen out of that window are permanently skipped by this CLI. For lossless event consumption, call MCP `table.watch` with its `after_event_id` cursor and continue paging while `has_more_events` is true; see [MCP Tools](MCP_TOOLS.md#run-state-and-turns).

An interval of zero or less falls back to `2s`; a positive duration is used as supplied. Cancellation stops the polling loop and returns the context error, so an interrupted `--follow` invocation may exit nonzero. Poll, database, configuration, and formatting failures also terminate the command rather than being retried. The displayed enabled-agent count is configuration/state metadata, not proof that external agent processes are running.

## Claims

```sh
roundtable claims list [--root DIR] [--agent ID] [--resource RESOURCE_ID] [--status STATUS]
roundtable claims claim [--root DIR] [--run ID] [--id ID] [--agent ID] [--task ID]
                       [--resource-id ID] [--resource-type TYPE] [--path PATH]
                       [--symbol NAME] [--claim-type TYPE] [--base-hash HASH]
                       [--ttl SECONDS] [--resume-policy POLICY] [--rationale TEXT]
roundtable claims release [--root DIR] [--run ID] [--claim ID] [--actor ID] [--reason TEXT]
roundtable claims revoke  [--root DIR] [--run ID] [--claim ID] [--actor ID] [--reason TEXT]
roundtable claims expire  [--root DIR] [--run ID] [--at-unix UNIX_SECONDS]
roundtable claims reconcile [--root DIR] [--run ID] [--agent ID] [--actor ID]
```

`claim` defaults `--claim-type` to `write`, TTL to `900` seconds, and resume policy to `hold`. Provide either a resource ID or enough type/path/symbol details for resource resolution. `release` is the owner transition; `revoke` is an administrative transition. `expire` processes due claims; `--at-unix` is an optional UTC Unix-second override useful for deterministic maintenance/testing. `reconcile` suspends claims held by stale sessions and defaults actor to `system`. See [Claims](CLAIMS.md) for overlap and lifecycle semantics.

## Sessions

```sh
roundtable sessions [--root DIR]
roundtable sessions register --root DIR --id ID --agent ID --run ID --adapter NAME --cwd DIR [other flags]
roundtable sessions heartbeat --root DIR --session ID [--status STATUS] [--external-session-id ID]
roundtable sessions end --root DIR --session ID [--status STATUS]
```

Register also accepts `--provider`, `--model`, `--external-session-id`, `--resume-command`, `--mcp-socket`, and `--status` (default `active`). If no resume command is supplied and the adapter has a resume pattern plus external ID, the CLI expands `{{external_session_id}}` and saves the result. Heartbeat refreshes last-seen time and end defaults final status to `ended`. Session records do not launch agents; see [Sessions and Resume](SESSIONS.md).

## Symbols

```sh
roundtable symbols --path FILE
```

Prints extracted symbol kind/name/path/start/end line records for supported Go, TypeScript/TSX, and Python files. Unsupported extensions produce no records. See [Symbol Index](SYMBOLS.md) for parser limitations.

## MCP

```sh
roundtable mcp inspect [--root DIR] [--write]
roundtable mcp serve [--root DIR]
roundtable mcp call [--root DIR] --tool NAME [--args-file JSON_FILE] [--socket]
```

`inspect` prints registered tool names; `--write` regenerates the manifest, tool schema, and server configuration under `.roundtable/mcp/`. `serve` listens on the configured local transport until its context ends. `call` reads optional JSON object arguments from `--args-file` and invokes the tool directly against the local runtime; with `--socket`, it sends the call to the running Unix socket. See [MCP Tools](MCP_TOOLS.md).

### Inspection and generated assets

All MCP subcommands default `--root` to `.`. `inspect` loads configuration even without `--write`; a missing or malformed file prevents inspection. It prints one advertised tool name per line alphabetically, not schemas or a health report. Advertised names include the unimplemented `repo.dependency_context`.

`inspect --write` overwrites `AGENT_MCP_MANIFEST.md`, `tools.schema.json`, then `server.json` under `.roundtable/mcp`. It does not create missing parent directories, require `--force`, back up contents, or replace all files atomically. A later failure can leave earlier assets refreshed. New files request mode `0644`, subject to umask. It does not rewrite policy/configuration or start a listener. Generated-asset refresh during `run` instead uses the scaffold helper, which ensures runtime directories first.

### Call arguments and execution modes

`--tool` must be nonempty. `--args-file` resolves from the process working directory, not automatically under `--root`, and is read without the agent path guard. Supply the tool's argument object, not a complete `{tool, args}` envelope. JSON `null` decodes to a nil map; arrays and scalar strings fail. Omission supplies an empty map. There is no inline-JSON flag, stdin argument mode, file-size cap, or schema-validation step.

A direct call opens its own runtime/database, initializes schema, loads policy, executes the handler, and closes the handle. It is not a dry run: write tools can mutate state or apply eligible patches, and `test.run` can execute a command. It does not require a socket, tick the coordinator, or share the coordinator's in-memory synchronization objects.

Socket mode loads configuration to locate the listener. It has no explicit dial/read/write deadline, reconnect, or automatic retry. A server that does not return a newline can leave the response read waiting. Lost output is not proof that a mutation did not occur; inspect authoritative state before retrying non-idempotent tools.

Both modes print successful results as indented JSON plus a newline, without the socket's outer `ok`/`result` envelope. Tool errors become CLI errors rather than successful result objects. Redirected output can contain sensitive file content and metadata.

### Standalone cancellation boundary

Standalone `mcp serve` starts no coordinator or TUI. Its startup message displays the configured transport label, but the server binds a Unix listener. The dispatcher passes `context.Background()` to standalone serving, and direct calls also use a background context. These command paths do not inherit the application's caller cancellation context and expose no timeout flag. Process termination is not a guarantee of graceful context-driven cleanup or rollback. The server type's context-aware lifecycle and interactive `run` composition are separate behaviors.

## Exit behavior

Command errors are returned to the process entrypoint and produce a non-zero exit status. Invalid commands, flags, required fields, configuration, storage operations, and MCP calls are not converted into a successful empty result. For production scripting, treat output as human-readable unless a command explicitly emits JSON; `mcp call` returns JSON.
