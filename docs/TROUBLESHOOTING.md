# Troubleshooting

## `roundtable run` cannot load configuration

Confirm that `.roundtable/config.yaml` exists (`roundtable init` creates it), that keys are supported by [Configuration Reference](CONFIGURATION.md), and that nested values use exactly two spaces per level. The current loader is not a full YAML parser. Inline comments and YAML-looking values can become literal string content rather than being rejected; typed integer/boolean fields reject values they cannot convert. Inspect the effective text rather than assuming valid YAML is valid Roundtable configuration.

## SQLite open or migration errors

Ensure the configured database parent directory is writable and that no incompatible process is holding a lock. SQLite runs with WAL, foreign keys, and a 5-second busy timeout. Preserve `-wal` and `-shm` files while a database is open; deleting them can lose or corrupt live state. Back up through the supported maintenance process.

## MCP socket cannot start

Check that the parent directory for `mcp.socket_path` exists or can be created, the path is not already owned by another Roundtable process, and the user can bind/remove the socket. Unix sockets are local IPC and must not be exposed by copying or forwarding them to untrusted users.

## Run stays active

An active run may be waiting for initial work, a pending agent turn, unresolved task status, a proposal review, or human action. Inspect `roundtable table`, `roundtable watch --run RUN_ID`, task/proposal state, and `agent.turn_*` events through MCP. A no-task run intentionally waits for incoming MCP work rather than completing immediately.

## Adapter command does not execute

The adapter package currently computes command/resume plans and capability metadata; the orchestrator does not launch external CLI agents or capture their session IDs. Confirm agent CLI installation only if using the command plan manually. See [Agent Adapters](ADAPTERS.md) and [Sessions and Resume](SESSIONS.md).

## UI configuration or API connection failure

Set the required `NEXT_PUBLIC_*` variables before building, verify they target the API origin and workspace you intend, and check browser network errors, API CORS policy, and API health. `NEXT_PUBLIC_*` settings are compiled into the client bundle. The UI surfaces missing configuration and normalized API problem details; it does not supply bearer credentials itself.

## Documentation build failure

Follow the full generation/preparation/build/check sequence in [Deployment](DEPLOYMENT.md), not just `mkdocs build`. MkDocs reads staged `.docs-build`, so stale content can survive in a build if preparation is omitted. Strict mode promotes emitted warnings to failures; it is not a complete coverage, semantic-link, or browser validator. Add pages to `mkdocs.yml` and run `scripts/verify-docs-navigation.py` for the separate rendered-sidebar checks. Local non-Markdown assets are not automatically copied by preparation. Task packs and prompts are intentionally excluded.

## Initialization refuses an existing file

`refusing to overwrite existing file` is expected when a starter target already exists. Initialization does not skip that file and continue. Inspect the current scaffold and configuration before retrying: earlier directories/files can remain from a partially failed attempt. Do not use `--force` merely to silence the error; it replaces customized protocol, project, policy, configuration, bootstrap task, and integration files. An already-scaffolded project normally starts with `run`, not another `init`. See [Quickstart](QUICKSTART.md).

## MCP call fails or waits indefinitely

Check whether the command uses direct mode or `--socket`. Direct mode opens local state itself; socket mode requires a listening server and has no explicit response deadline. `--args-file` is a process-local file containing the tool argument object, not a socket request envelope. A hanging client or lost response does not prove a server-side command stopped or a mutation was rolled back. Inspect records and artifacts before retrying. In particular, avoid blindly repeating proposal creation, patch application, or test execution. See [CLI Reference](CLI.md).

An exact `tool not implemented: repo.dependency_context` error is not caused by a bad manifest or missing package manager. The name is advertised but has no dispatch handler. Use the documented file-read/search alternatives; there is no resolved dependency graph behind that tool.

## Claims conflict after their displayed expiry

Elapsed wall-clock TTL does not itself change stored status. Conflict checks still include exact `active` claims until an expiration operation transitions them. Inspect `claims list` and, when authorized, run `claims expire` with the intended root/run attribution. Invalid expiry timestamps are skipped by expiration. Do not delete claim rows to force access or infer that session shutdown releases leases. Reconciliation checks content hashes, not heartbeat age or the stored resume policy. See [Resource Claims](CLAIMS.md).

## A requested agent turn never advances

Standalone `mcp serve` accepts requests but does not tick the Chair scheduler. Confirm an orchestrator is running against the same database and run ID. Drain MCP `table.watch` cursor pages and correlate the request event ID with `agent.turn_scheduled`, `agent.turn_started`, and `agent.turn_completed`. An outstanding scheduled turn blocks later scheduling even when its start acknowledgment never arrives; there is no scheduler timeout or automatic stale-owner release. Do not send repeated requests or fabricate lifecycle events for another agent. Inspect ownership and coordinate recovery with the human/Chair. Turn ownership does not launch provider processes.

## Run completion differs from task acceptance

Convergence reads database-wide tasks/proposals and uses exact status rules, not test success. Unknown task statuses count as active; unknown proposal statuses do not. Failed tasks can be terminal for convergence. Historical work can affect a new run. A bounded headless invocation can exit successfully on its iteration limit while its run remains active; this is not completion. Inspect the persisted run, individual work records, tests, and approvals separately. See [Orchestration](ORCHESTRATION.md).

## Events are missing from a live view

The TUI and CLI watch use polling; CLI follow suppresses previously printed IDs but fetches a limited recent window without a cursor. More arrivals than the window can fit between polls can be missed. Use MCP cursor polling for reliable database replay. API WebSocket live delivery uses an in-process buffered bus that can drop messages; a separate runtime writing the same database does not notify that bus automatically. Reconcile persisted sequences and replay/snapshot state rather than treating an open connection as complete history. See [Architecture](ARCHITECTURE.md) and [TUI and Watch](TUI.md).

## Resume briefing shows unexpected work

Briefings are assembled from current local rows, not frozen session-start snapshots. Task/claim/proposal selection is agent-based, decisions are database-wide, and the displayed run ID does not scope all those reads. Completed assigned tasks are not excluded, and only exact `accepted`/`rejected` proposals are omitted. Prefer an explicit session ID and verify each selected entity before acting. A heartbeat with status `active` does not clear a prior end timestamp. See [Sessions and Resume](SESSIONS.md).

## Test tool returns success with a failed test

The tool envelope reports successful command execution/persistence handling, not a passing test verdict. Read `test_run.status`: a command failure is stored as `failed` while the tool can still return `ok: true`. Missing or unreadable saved log files can yield empty log text without a tool error. Results are not bound to a patch hash or repository revision, and patch application does not automatically run expected tests. See [Tests and Evidence](TEST_EXECUTION.md).
