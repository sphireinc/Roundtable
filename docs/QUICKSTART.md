# Quickstart

This guide starts the local Go runtime. The separate HTTP API and Next.js UI have their own setup instructions in [Deployment](DEPLOYMENT.md).

## Prerequisites

- Go version declared in the root `go.mod`.
- A local loopback TCP listener for standard MCP HTTP; the compatibility socket additionally requires Unix-domain socket support.
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

## Start the Coordinator

`start` does not create runs. On a fresh project, initialize one before starting the persistent owner:

```sh
go run ./cmd/roundtable run --root . --goal "Describe the work to coordinate" --headless
```

This creates the run and executes one cycle, then exits; the run remains available for the continuous coordinator. Start the foreground owner next:

```sh
go run ./cmd/roundtable start --root .
```

This remains alive with no active runs and does not create one. Configure Codex (or another stdio-capable harness) to launch `roundtable mcp stdio --root /absolute/path/to/project`; the bridge connects to the already-running owner. The direct HTTP endpoint defaults to `http://127.0.0.1:7117/mcp`. Stop the owner with Ctrl-C; no OS service or reboot persistence is installed.

For Codex CLI, add a project-local entry to `.codex/config.toml` (do not put credentials here):

```toml
[mcp_servers.roundtable]
command = "/absolute/path/to/roundtable"
args = ["mcp", "stdio", "--root", "/absolute/path/to/project"]
```

## Interactive Alternative

```sh
go run ./cmd/roundtable run --root . --goal "Describe the work to coordinate"
```

This alternative starts the run coordinator, local MCP Unix socket, and terminal UI itself. It continues polling until convergence, cancellation, or the configured consecutive-error limit; it cannot run concurrently with `roundtable start` for the same project. To execute only one cycle and then hand an active run to `start`, use `--headless` as shown above. For an existing run, provide its ID and pass `--resume`:

```sh
go run ./cmd/roundtable run --root . --run RUN_ID --resume
```

## Inspect state

Use `roundtable table`, `roundtable watch`, `roundtable claims list`, and `roundtable mcp inspect` to inspect persisted state and the generated tool surface. See the [CLI Reference](CLI.md) for flags and subcommands.

## Before Connecting Participants

1. Confirm the intended root and inspect `.roundtable/config.yaml`, especially database and socket paths. Runtime configuration, HTTP flags/settings, and browser build variables are separate channels; pointing one component at a checkout does not align all others automatically.
2. Record current repository status and preserve existing uncommitted work. Initialization does not create a clean Git baseline, and patch application is not atomic across filesystem changes and database/artifact persistence. Do not start by resetting or deleting runtime state to make the workspace look clean.
3. Confirm the HTTP bind is loopback unless remote access is intentional and protected by TLS plus `ROUNDTABLE_MCP_TOKEN`. Socket recovery rejects non-socket and live paths and removes only a confirmed stale socket. Protect the project account and socket permissions.
4. Review the [MCP transport](MCP_TOOLS.md) and its authority: HTTP/stdio are standard MCP transports, while the compatibility Unix socket is custom Roundtable IPC. None provides cryptographic per-agent identity or an OS sandbox. A connected caller can invoke implemented mutating tools; proposal/policy/approval/transaction gates remain the application boundary.
5. If using the separate HTTP service, review [authentication limitations](../api/README.md#authentication-and-authorization) before sharing either token. An agent token is not currently a reliably isolated human-control boundary. Do not start the publicly mapped Compose service with empty credentials on a shared network or put privileged tokens in browser public variables.
6. Keep the run ID and database association explicit when requesting turns. A standalone socket server accepts tool calls but does not tick the scheduler; headless single-cycle mode does not provide a continuously listening interactive coordinator. Turn scheduling does not launch external CLI agents.

### Interpreting the First Successful Run

A successful bounded headless invocation proves only that its configured coordinator cycles returned without an unrecovered error; it can stop at the iteration limit while the stored run remains active. Inspect persisted run/task/proposal status separately. A no-task interactive run intentionally waits for incoming work. Initial agent/capability records, a visible TUI, and a bound socket do not prove provider execution, passing tests, approved patches, or complete convergence.

Use [Tasks](TASKS.md) to create authoritative task records rather than assuming Markdown task files were imported. Use [Tests and Evidence](TEST_EXECUTION.md) to interpret result status separately from tool-envelope success. Before any apply, review current claims, proposal artifact/resources, policy/security/human gates, and existing work; use [Transactions](TRANSACTIONS.md) for the exact sequence and uncertain-failure recovery. This quickstart does not perform a live provider integration or acceptance test for you.

## Data and safety

The default database is `.roundtable/roundtable.db`; the MCP socket is `.roundtable/mcp/roundtable.sock`. Treat this directory as project-local runtime state and do not commit its database, socket, WAL, SHM, patch artifacts, or secrets. Agents use the MCP surface; they must not write directly to the authoritative repository. Read the [Agent Protocol](../AGENTS.ROUNDTABLE.md) and [Governance Policies](../POLICIES.ROUNDTABLE.md) before connecting an agent.
