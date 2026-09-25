# Quickstart

This guide starts the local Go runtime. The separate HTTP API and Next.js UI have their own setup instructions in [Deployment](DEPLOYMENT.md).

## Prerequisites

- Go version declared in the root `go.mod`.
- A POSIX environment for the default Unix-domain MCP socket.
- SQLite support provided by the Go dependencies; no external database server is required.
- Optional agent CLIs (`codex`, `claude`, `gemini`, `opencode`) for integrations. The current adapter layer advertises capabilities and constructs launch/resume plans; it does not yet supervise real agent processes.

## Initialize

From the repository root:

```sh
go run ./cmd/roundtable init --root .
```

Initialization writes the default project protocol/policy files and `.roundtable/` runtime configuration, opens the SQLite database, applies schema migrations, and synchronizes configured adapter capabilities. Existing generated files are not overwritten unless `--force` is supplied. Review the generated configuration before starting a run.

## Start a run

```sh
go run ./cmd/roundtable run --root . --goal "Describe the work to coordinate"
```

An interactive run starts the coordinator, local MCP Unix socket, and terminal UI. It continues polling until convergence, cancellation, or the configured consecutive-error limit. To initialize and execute one coordinator cycle without starting the MCP server or TUI:

```sh
go run ./cmd/roundtable run --root . --goal "Describe the work" --headless
```

`--headless` defaults to one cycle when `--max-iterations` is zero. For an existing run, provide its ID and pass `--resume`:

```sh
go run ./cmd/roundtable run --root . --run RUN_ID --resume
```

## Inspect state

Use `roundtable table`, `roundtable watch`, `roundtable claims list`, and `roundtable mcp inspect` to inspect persisted state and the generated tool surface. See the [CLI Reference](CLI.md) for flags and subcommands.

## Data and safety

The default database is `.roundtable/roundtable.db`; the MCP socket is `.roundtable/mcp/roundtable.sock`. Treat this directory as project-local runtime state and do not commit its database, socket, WAL, SHM, patch artifacts, or secrets. Agents use the MCP surface; they must not write directly to the authoritative repository. Read the [Agent Protocol](../AGENTS.ROUNDTABLE.md) and [Governance Policies](../POLICIES.ROUNDTABLE.md) before connecting an agent.
