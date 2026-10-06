# Deployment and Local Operations

Roundtable currently consists of distinct components: the Go coordinator/MCP/TUI, a standalone Go HTTP API under `api/`, and a Next.js admin UI under `ui/`. The API has a Docker Compose definition; the root Go runtime and browser UI require separate launch steps. These instructions describe local/single-host use, not a hardened multi-tenant production deployment.

## Go runtime

Build or run from the repository root with Go:

```sh
go build -o roundtable ./cmd/roundtable
./roundtable run --root . --goal "Coordinate this work"
```

This assumes an already-scaffolded project with a readable `.roundtable/config.yaml`. For a new project, initialize its target root first as described in [Quickstart](QUICKSTART.md); `init` refuses existing starter files, while `--force` overwrites customizations. The runtime stores SQLite and MCP assets under `.roundtable/` by default. The MCP server uses the configured Unix socket. Protect the project directory and database; the database contains operational state and audit/event data.

## HTTP API container

```sh
docker compose -f api/docker-compose.yml up --build
```

The compose file publishes host port `ROUNDTABLE_API_PORT` (default `8080`) to container port `8080`, mounts `.roundtable/` for state, and mounts the repository read-only at `/workspace`. The Go server defaults to `127.0.0.1:8080`, workspace root `.`, and database `.roundtable/roundtable.db`. Native runs refuse a non-loopback bind unless `-allow-remote` is explicitly passed. Remote exposure requires a deliberate network and authentication design.

Supported process flags are `-addr`, `-workspace-root`, `-database`, `-human-token`, `-agent-token`, and `-allow-remote`. Token flags default from `ROUNDTABLE_HUMAN_TOKEN` and `ROUNDTABLE_AGENT_TOKEN`; API version metadata comes from `ROUNDTABLE_API_VERSION`. Configure separate high-entropy human and agent bearer tokens outside source control. When tokens are absent, local development header mode is enabled and must not be treated as remote authentication.

## Web UI

From `ui/`, install dependencies, set `NEXT_PUBLIC_API_BASE_URL`, `NEXT_PUBLIC_WS_URL`, and `NEXT_PUBLIC_WORKSPACE_ID`, then build and run with the package scripts. These public variables are embedded at build time. The browser UI currently does not add bearer tokens to API requests; do not place privileged secrets in `NEXT_PUBLIC_*` variables.

## GitHub documentation site

After the strict build, run `python scripts/verify-docs-navigation.py`. The GitHub workflow runs this check before uploading the Pages artifact. It compares the primary sidebar on every generated HTML page, including the 404 page, against the labels, order, and page targets in `mkdocs.yml`. It also verifies that each configured page has a rendered output file. Missing entries, changed order, wrong link targets, missing pages, or multiple/missing primary sidebars fail the command. The checker supports the configured directory URLs and Material sidebar markup; update it when changing themes or URL mode.

The sidebar taxonomy is maintained in the single `nav` mapping in `mkdocs.yml`. Governance pages share a Runtime subgroup, and database pages share a Database subgroup. Keep explicit page entries visible; `navigation.indexes` is disabled because it folds README overview pages into section labels. Active-page and table-of-contents controls may differ between pages, while the complete primary navigation remains uniform. The HTML check verifies labels and targets; use a browser to assess responsive layout, keyboard interaction, and visual appearance.

The grouped docs site is built from the repository root using `python -m pip install -r requirements-docs.txt`, `ruby api/scripts/generate-admin-api-guide.rb`, `ruby api/scripts/verify-openapi.rb`, `python scripts/prepare-docs.py`, and `mkdocs build --strict`. The API generator refreshes both the endpoint inventory and component-schema catalog from `api/openapi.yaml`; verify the contract before publishing. The preparation script copies only the curated documentation pages and their linked API fixtures into ignored `.docs-build/`; it does not copy application source, TODO/DONE ledgers, or task prompts. The GitHub Actions workflow builds pull requests and publishes pushes on `main` through GitHub Pages. In repository settings, set Pages deployment source to GitHub Actions. Site output is generated into `site/` and should not be committed.

### Reproducible documentation build

Run from the repository root with Python, pip, and Ruby available:

```sh
python3 -m pip install -r requirements-docs.txt
ruby api/scripts/generate-admin-api-guide.rb
ruby api/scripts/verify-openapi.rb
python3 scripts/prepare-docs.py
python3 -m mkdocs build --strict
python3 scripts/verify-docs-navigation.py
```

`mkdocs build` alone does not refresh generated API Markdown or assemble current sources. The configured `docs_dir` is `.docs-build`, not `docs`; skipping preparation can build stale staged content. The generator writes tracked API reference pages, so inspect their diff after changes to OpenAPI. The verifier is a static contract check, not evidence that a running API implements every route correctly.

Preparation determines the repository root from the script's own location. It deletes the entire existing `.docs-build` tree and recreates it; do not place hand-authored content or valuable files there. It copies four root documents (`README.md`, `AGENTS.ROUNDTABLE.md`, `PROJECT.ROUNDTABLE.md`, `POLICIES.ROUNDTABLE.md`), every `*.md` recursively under `docs` and `api/docs`, and the explicit assets `api/README.md`, `api/openapi.yaml`, `api/examples/dashboard-fixtures.json`, and `examples/resume-briefing.md`. Original relative paths are preserved. Non-Markdown images, stylesheets, downloads, and other files under `docs` are not automatically included. Add asset-copy support deliberately before linking new local assets.

This is a curated source pipeline, not an automatic secret scanner. Any Markdown added under either copied documentation tree becomes a build input, even without a navigation entry. Keep runtime dumps, credentials, private notes, and generated operational data outside those trees. Missing explicitly copied files or copy errors fail preparation after any earlier staging work; the staging tree is not atomically replaced.

### CI triggers and publication evidence

The `Documentation` workflow runs on pull requests, pushes to `main`, and manual dispatch. It uses Python 3.12 on `ubuntu-latest` and executes generation, contract verification, preparation, strict build, and navigation verification in order. Its concurrency group is `pages-${github.ref}`, with older in-progress runs for that ref cancelled when a new run starts.

Pull requests build and validate without configuring Pages, uploading its artifact, or deploying. Non-pull-request builds configure Pages and upload `site`; deployment additionally requires exact ref `refs/heads/main`. A manual run on another branch can build/upload but does not satisfy that deployment condition. The deploy job depends on the build, uses the `github-pages` environment, and receives `pages: write` and `id-token: write` permissions; the workflow-wide content permission is read-only.

A passing local build proves neither hosted CI success nor publication. Verify the particular workflow run and deployment output URL separately after a push. The Pages source setting must be GitHub Actions. Neither `site` nor `.docs-build` should be committed. The navigation verifier establishes common rendered labels/order/targets, not mobile appearance, keyboard accessibility, client-side search behavior, or availability at the deployed URL.

## Backups and maintenance

Use the API maintenance endpoints and [Maintenance Guide](../api/docs/maintenance.md) for supported database backup/retention procedures. Do not copy only the main SQLite file while WAL writes are active; use the supported SQLite backup path or stop writers first. Keep a tested restore procedure and protect the database, its WAL/SHM sidecars, patch artifacts, auth material, and external session metadata.
