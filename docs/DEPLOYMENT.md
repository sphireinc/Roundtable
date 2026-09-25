# Deployment and Local Operations

Roundtable currently consists of distinct components: the Go coordinator/MCP/TUI, a standalone Go HTTP API under `api/`, and a Next.js admin UI under `ui/`. The API has a Docker Compose definition; the root Go runtime and browser UI require separate launch steps. These instructions describe local/single-host use, not a hardened multi-tenant production deployment.

## Go runtime

Build or run from the repository root with Go:

```sh
go build -o roundtable ./cmd/roundtable
./roundtable init --root .
./roundtable run --root . --goal "Coordinate this work"
```

The runtime stores SQLite and MCP assets under `.roundtable/` by default. The MCP server uses the configured Unix socket. Protect the project directory and database; the database contains operational state and audit/event data.

## HTTP API container

```sh
docker compose -f api/docker-compose.yml up --build
```

The compose file publishes host port `ROUNDTABLE_API_PORT` (default `8080`) to container port `8080`, mounts `.roundtable/` for state, and mounts the repository read-only at `/workspace`. The Go server defaults to `127.0.0.1:8080`, workspace root `.`, and database `.roundtable/roundtable.db`. Native runs refuse a non-loopback bind unless `-allow-remote` is explicitly passed. Remote exposure requires a deliberate network and authentication design.

Supported process flags are `-addr`, `-workspace-root`, `-database`, `-human-token`, `-agent-token`, and `-allow-remote`. Token flags default from `ROUNDTABLE_HUMAN_TOKEN` and `ROUNDTABLE_AGENT_TOKEN`; API version metadata comes from `ROUNDTABLE_API_VERSION`. Configure separate high-entropy human and agent bearer tokens outside source control. When tokens are absent, local development header mode is enabled and must not be treated as remote authentication.

## Web UI

From `ui/`, install dependencies, set `NEXT_PUBLIC_API_BASE_URL`, `NEXT_PUBLIC_WS_URL`, and `NEXT_PUBLIC_WORKSPACE_ID`, then build and run with the package scripts. These public variables are embedded at build time. The browser UI currently does not add bearer tokens to API requests; do not place privileged secrets in `NEXT_PUBLIC_*` variables.

## GitHub documentation site

The grouped docs site is built from the repository root using `python -m pip install -r requirements-docs.txt`, `ruby api/scripts/generate-admin-api-guide.rb`, `ruby api/scripts/verify-openapi.rb`, `python scripts/prepare-docs.py`, and `mkdocs build --strict`. The API generator refreshes both the endpoint inventory and component-schema catalog from `api/openapi.yaml`; verify the contract before publishing. The preparation script copies only the curated documentation pages and their linked API fixtures into ignored `.docs-build/`; it does not copy application source, TODO/DONE ledgers, or task prompts. The GitHub Actions workflow builds pull requests and publishes pushes on `main` through GitHub Pages. In repository settings, set Pages deployment source to GitHub Actions. Site output is generated into `site/` and should not be committed.

## Backups and maintenance

Use the API maintenance endpoints and [Maintenance Guide](../api/docs/maintenance.md) for supported database backup/retention procedures. Do not copy only the main SQLite file while WAL writes are active; use the supported SQLite backup path or stop writers first. Keep a tested restore procedure and protect the database, its WAL/SHM sidecars, patch artifacts, auth material, and external session metadata.
