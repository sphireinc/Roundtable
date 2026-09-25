# Roundtable

Roundtable is a shared-state agentic development management system. Multiple agents deliberate around one project state; repository changes are intended to flow through resource claims, patch proposals, policy/review gates, and a transaction manager instead of independent agents writing to divergent copies.

The implementation has three distinct surfaces: a Go coordinator/MCP/TUI runtime, a standalone Go HTTP API, and a Next.js administration UI. Their current capabilities and unfinished boundaries are described in the [documentation site](docs/index.md); do not infer that every planned UI or agent-execution feature is complete just because it appears in a design or task specification.

## Quick start

With Go installed, from the repository root:

```sh
go run ./cmd/roundtable init --root .
go run ./cmd/roundtable run --root . --goal "Describe the work to coordinate"
```

The default runtime creates `.roundtable/config.yaml`, a SQLite database, MCP assets, and starter governance/project files. Interactive mode runs the local MCP socket, TUI, and coordinator loop. Agent CLI process launch is not yet connected; agents can use the MCP surface when separately attached. For a single coordinator cycle without the socket or TUI, add `--headless`.

## Documentation

- [Quickstart](docs/QUICKSTART.md)
- [CLI reference](docs/CLI.md)
- [Configuration reference](docs/CONFIGURATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Agent protocol](AGENTS.ROUNDTABLE.md)
- [Governance policy](POLICIES.ROUNDTABLE.md)
- [HTTP API and OpenAPI guide](api/README.md)
- [Deployment](docs/DEPLOYMENT.md)

Build the uniform grouped documentation site locally:

```sh
python -m pip install -r requirements-docs.txt
python scripts/prepare-docs.py
python -m mkdocs build --strict
```

## Repository areas

| Path | Purpose |
| --- | --- |
| `cmd/roundtable/` | Go executable entry point. |
| `internal/` | Runtime packages: config, database, MCP, orchestration, claims, proposals, policies, sessions, symbols, TUI. |
| `api/` | HTTP API server, OpenAPI contract, generated endpoint documentation, and Compose definition. |
| `ui/` | Next.js browser client for the API. |
| `docs/` | Detailed Go runtime, operator, and feature documentation. |
| `TASKS.ROUNDTABLE/` | Project and API/UI task specifications; not part of the published docs site. |
| `.roundtable/` | Local database, configuration, generated MCP assets, and runtime artifacts. Keep local state and secrets out of source control. |

## Project invariant

The database-backed shared state is authoritative. Agent conversation history and local CLI session state may help resume work, but do not replace the persisted task, claim, proposal, vote, approval, decision, transaction, or event records. Read the [Agent Protocol](AGENTS.ROUNDTABLE.md) before integrating an agent.
