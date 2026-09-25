# Troubleshooting

## `roundtable run` cannot load configuration

Confirm that `.roundtable/config.yaml` exists (`roundtable init` creates it), that keys are supported by [Configuration Reference](CONFIGURATION.md), and that nested values use exactly two spaces per level. The current loader is not a full YAML parser; arrays, inline comments, anchors, and unknown keys fail.

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

Install `requirements-docs.txt`, build from repository root, and use `mkdocs build --strict`. Strict mode catches missing nav sources, broken internal links, and pages omitted from the navigation. Add user-facing documentation to the explicit nav in `mkdocs.yml`; generated task packs and prompts are intentionally excluded from the public docs site.
