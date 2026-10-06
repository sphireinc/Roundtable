# Quickstart

This guide starts the local Go runtime. The separate HTTP API and Next.js UI have their own setup instructions in [Deployment](DEPLOYMENT.md).

## Prerequisites

- Go version declared in the root `go.mod`.
- A POSIX environment for the default Unix-domain MCP socket.
- SQLite support provided by the Go dependencies; no external database server is required.
- Optional agent CLIs (`codex`, `claude`, `gemini`, `opencode`) for integrations. The current adapter layer advertises capabilities and constructs launch/resume plans; it does not yet supervise real agent processes.

## Initialize

For a project root that does not already contain the starter files:

```sh
go run ./cmd/roundtable init --root .
```

Initialization writes the default project protocol/policy files and `.roundtable/` runtime configuration, opens the SQLite database, applies schema migrations, and synchronizes built-in adapter capabilities. Without `--force`, an existing starter file causes an error; it is not silently skipped. This repository already contains starter documents, so do not rerun `init --root .` expecting a no-op. Review the generated configuration before starting a run.

### Created directories and files

The scaffold first ensures these directories exist: `.roundtable`, `.roundtable/mcp`, `.roundtable/sessions`, `.roundtable/sessions/agents`, `.roundtable/sessions/runs`, `.roundtable/memory`, `.roundtable/patches`, `.roundtable/logs`, `.roundtable/runs`, and `TASKS.ROUNDTABLE`. It then writes starter files in this order:

1. `AGENTS.ROUNDTABLE.md`: agent protocol.
2. `PROJECT.ROUNDTABLE.md`: project model.
3. `POLICIES.ROUNDTABLE.md`: starter governance policy.
4. `TASKS.ROUNDTABLE/0001-bootstrap.md`: Markdown bootstrap task artifact.
5. `.roundtable/config.yaml`: complete default configuration.
6. `.roundtable/memory/README.md`: memory-directory guidance.
7. `.roundtable/mcp/AGENT_MCP_MANIFEST.md`: derived tool manifest.
8. `.roundtable/mcp/tools.schema.json`: derived registry schemas.
9. `.roundtable/mcp/server.json`: derived local transport metadata.

Only after scaffolding succeeds does initialization open the default `.roundtable/roundtable.db`, ensure its schema, and upsert built-in adapter capabilities. It does not load a preexisting configuration to select a custom database path or adapter map. It does not start a run, synchronize agent rows, create a socket listener, launch provider processes, import the bootstrap Markdown as a database task, or create a Git repository.

### Existing files and partial failures

`--force` overwrites all listed starter files, including customized protocol/policy documents and configuration, with built-in templates. It is not a selective generated-MCP-assets refresh. Back up and review customizations before using it. To refresh only integration assets from the current configuration, use `roundtable mcp inspect --root . --write`; that command has a different scope from project initialization.

Scaffolding is sequential, not transactional: a directory or file written before a later failure remains in place. There is no preflight check of every target, temporary-file/rename transaction, or automatic cleanup. A retry without `--force` can fail on a file created by the previous partial attempt. A database migration or capability-write failure can likewise occur after all starter files exist. Inspect the reported target and current filesystem before retrying rather than assuming the failed invocation changed nothing.

Directory creation requests mode `0755`; new starter files request `0644`, subject to umask and existing permissions. Existing file modes are not explicitly tightened. The overwrite check uses `os.Stat` and subsequent writes follow ordinary filesystem behavior; this scaffold is not a secure symlink-resistant installer. Initialize only a trusted project directory, keep secrets out of starter documents, and apply account/filesystem protections separately.

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
