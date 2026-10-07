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

### Dependency and Build Environment

`requirements-docs.txt` pins the top-level theme package to `mkdocs-material==9.7.7`. It does not separately pin MkDocs, Markdown, PyYAML, or other transitive packages, include hashes, or constitute a full dependency lock. A fresh installation can therefore resolve different permitted transitive versions. Python packages are acquired by pip; API generation/verification additionally needs Ruby and its standard-library YAML/JSON support. These tools are not installed by the preparation script. Prefer an isolated Python environment rather than altering a shared interpreter, and record actual versions when diagnosing differing builds.

The workflow selects Python 3.12 with `actions/setup-python@v5`, but does not install or pin Ruby explicitly; it relies on the `ubuntu-latest` runner environment. Checkout uses `actions/checkout@v4`; Pages configuration/upload/deploy use major action versions v5/v4/v4 respectively, not immutable commit-SHA pins. There is no dependency cache, matrix of Python/platform versions, browser installation, application-service startup, or deployment smoke test in this workflow. It does not execute Go tests, frontend tests/builds, live API requests, keyboard/mobile acceptance, or a dependency-vulnerability audit.

All preparation input names are explicit except recursively discovered lowercase `*.md` files in the two documentation trees. Other root Markdown, task packs, prompts, UI source, runtime databases, and patch artifacts are not copied by this script. `shutil.copy2` copies files and metadata; it is not a content sanitizer, Markdown rewriter, link repairer, or assertion that local symlink targets are safe to publish. Review the actual staged tree when adding generated or externally supplied documentation. The script has no dry-run, destination flag, incremental mode, or CLI configuration; rerunning replaces `.docs-build` in the repository determined from its location.

### CI triggers and publication evidence

The `Documentation` workflow runs on pull requests, pushes to `main`, and manual dispatch. It uses Python 3.12 on `ubuntu-latest` and executes generation, contract verification, preparation, strict build, and navigation verification in order. Its concurrency group is `pages-${github.ref}`, with older in-progress runs for that ref cancelled when a new run starts.

Pull requests build and validate without configuring Pages, uploading its artifact, or deploying. Non-pull-request builds configure Pages and upload `site`; deployment additionally requires exact ref `refs/heads/main`. A manual run on another branch can build/upload but does not satisfy that deployment condition. The deploy job depends on the build, uses the `github-pages` environment, and receives `pages: write` and `id-token: write` permissions; the workflow-wide content permission is read-only.

A passing local build proves neither hosted CI success nor publication. Verify the particular workflow run and deployment output URL separately after a push. The Pages source setting must be GitHub Actions. Neither `site` nor `.docs-build` should be committed. The navigation verifier establishes common rendered labels/order/targets, not mobile appearance, keyboard accessibility, client-side search behavior, or availability at the deployed URL.

## Backups and maintenance

Use the API maintenance endpoints and [Maintenance Guide](../api/docs/maintenance.md) for supported database backup/retention procedures. Do not copy only the main SQLite file while WAL writes are active; use the supported SQLite backup path or stop writers first. Keep a tested restore procedure and protect the database, its WAL/SHM sidecars, patch artifacts, auth material, and external session metadata.
## Developer Verification Scope

This repository has a root `go.mod` declaring Go 1.26.0 and no Makefile. The runtime and API Go packages belong to that one module; there is no separate `api/go.mod`. Run Go commands from the repository root:

```sh
go build ./...
go test ./...
go vet ./...
go test -race ./...
```

These are distinct checks, not interchangeable evidence. Build checks compilation; tests execute the Go test suite; vet performs its static diagnostics; race tests instrument executed test paths for data races. A successful race run does not prove unexecuted concurrent paths safe. The SQLite dependency uses `github.com/mattn/go-sqlite3`, so use a compatible C toolchain and CGO-enabled environment for meaningful database execution. Toolchain/dependency downloads and platform permissions can affect these commands independently of product behavior.

Root Go tests include API packages but do not run the Next.js UI build, frontend unit tests, Playwright browser suite, OpenAPI verification, or MkDocs navigation checks. Run those documented toolchains separately. Tests using temporary databases or local HTTP servers are not proof that a deployed service, remote agent CLI, container mount, or GitHub Pages publication works. Record the command, platform, revision, and relevant live-service configuration when reporting acceptance.

The current `.github/workflows` directory contains the documentation workflow, not a general runtime/API/UI CI workflow. Local success must not be presented as hosted CI evidence for those components. Documentation build validation likewise does not certify runtime behavior or comprehensive feature coverage.
## Documentation Site Configuration

`mkdocs.yml` is the authoritative site and navigation configuration. Site name is `Roundtable Documentation`; description is `Product, runtime, API, and operations documentation for Roundtable.` Sources are assembled under `.docs-build`, output is written to `site`, and `use_directory_urls: true` produces directory-style page URLs. Edit original Markdown and configuration, not these generated output directories. The configuration does not set a canonical `site_url`, repository/edit links, custom CSS/JavaScript, analytics integration, locale, or theme color palette.

The Material theme enables `navigation.sections` (grouped sidebar sections), `navigation.top` (back-to-top control), `toc.follow` (following the active in-page heading), and `content.code.copy` (code-block copy controls). Navigation indexes, instant navigation, navigation tabs, and expansion of every branch are not enabled here. The page table of contents is distinct from the shared left navigation: current-page headings can differ while the same logical navigation tree is retained.

Markdown extensions are admonitions, attribute lists, definition lists, tables, a table of contents with heading permalinks, collapsible details, SuperFences, and alternate-style tabbed content. Their availability does not mean arbitrary embedded HTML or scripts are safe to publish. Search is not explicitly configured in this file; do not describe custom indexing, ranking, or access-control behavior that the repository has not configured.

The central `nav` tree groups Product, Getting Started, Runtime, Agent Interface, HTTP API, Web UI, and Operations. Runtime has nested Governance and Database groups; HTTP API has nested API Reference and Operations and Administration groups. Labels and ordering come from this single tree, not duplicated per-page menus. Add new public pages to the appropriate group and retain existing nesting instead of introducing a page-specific sidebar. The navigation verifier checks the rendered common tree across pages; it does not certify mobile layout, keyboard interaction, visual wrapping, search behavior, or documentation completeness.

Preparation recursively copies Markdown from `docs/` and `api/docs/`, plus four named root documents and explicitly selected API/OpenAPI/example assets. It does not copy arbitrary images, scripts, source trees, task packs, or all examples. A newly referenced non-Markdown asset needs an intentional preparation-rule update; merely placing it beside a source page does not make it available in the assembled site. Preparation preserves relative source paths and replaces the entire staging directory each run. Always rerun preparation after source edits before claiming a fresh build result.
### Prepared Source Inventory

`scripts/prepare-docs.py` resolves the repository root from its own location, not the caller's working directory, and always targets root `.docs-build`. It accepts no command-line flags, environment-based destination override, incremental mode, or dry run. The copy set is:

| Source | Prepared destination/selection |
| --- | --- |
| Root `README.md`, `AGENTS.ROUNDTABLE.md`, `PROJECT.ROUNDTABLE.md`, `POLICIES.ROUNDTABLE.md` | Same filenames at staging root. |
| `docs/` and `api/docs/` | Recursive `*.md` matches, sorted within each tree, retaining repository-relative paths. |
| `api/README.md` and `api/openapi.yaml` | Same repository-relative paths. |
| `api/examples/dashboard-fixtures.json` | Same repository-relative path; no general JSON fixture glob. |
| `examples/resume-briefing.md` | Same repository-relative path; no general example-directory copy. |

The script deletes an existing staging tree with `shutil.rmtree`, then recreates it before copying. This is not an atomic replacement and does not preserve a last-known-good staging tree on failure. `shutil.copy2` copies file content and supported metadata; it is not a sanitizer or symlink-containment check and normally follows a selected file symlink. Markdown glob matching follows the platform's path-matching behavior; do not assume every differently cased extension is portable. It does not preprocess Markdown, rewrite links, generate API references, validate navigation membership, or verify linked assets. A nonexistent recursively scanned tree can contribute no matches rather than independently failing a required-directory check, while missing explicitly named files fail their copy operation.

Keep source documents and selected assets authoritative; files edited only inside `.docs-build` are lost on the next preparation. Run the generator and contract checks separately, then preparation, strict build, and navigation verification. Inspect build diagnostics for missing anchors/assets even when a command exits successfully; these checks are not a complete link crawler or publication acceptance test.

### Navigation Verification Contract

Local browser spot-check on 2026-10-06: Chrome rendered the project overview with grouped desktop navigation, expanded Runtime/Governance, and followed Claims into its nested page while retaining the shared groups. At a 390-by-844 viewport, Claims content reflowed and the navigation drawer displayed the Governance submenu with readable links. The temporary viewport override was reset afterward. This is representative local browser evidence, not an exhaustive all-page interaction test, keyboard/accessibility audit, or verification of the published GitHub Pages deployment. The separate static verifier covers configured label/target uniformity across every rendered page.

Run `python3 scripts/verify-docs-navigation.py` after preparation and site build, with PyYAML available in that Python environment. The verifier reads root `mkdocs.yml`, requires directory URLs, maps README/index sources to directory `index.html` outputs, and checks every configured page exists. It then scans **all** rendered HTML files under the configured site directory, including error pages, not only pages listed in navigation.

For each page it requires exactly one Material primary navigation element and compares the sequence of rendered entries against the central navigation configuration: normalized labels, resolved link targets, and the full ancestor-label path for each entry. Group labels are included as entries without targets, so moving a child under another sibling group fails even if flattened labels, ordering, and nesting depth stay unchanged. It excludes the secondary in-page table of contents and rejects sidebar links pointing off-site or to fragments. Relative links are resolved against each rendered page; whitespace in labels is normalized. Missing output, no rendered pages, unexpected targets, or mismatches produce a nonzero exit with diagnostic page names.

Before scanning rendered HTML, the verifier also derives a page target for every prepared Markdown file and compares that complete set with the leaf targets in `mkdocs.yml`. It fails if a Markdown page copied into `.docs-build` is omitted from navigation or if a menu leaf has no corresponding prepared Markdown source. This makes navigation membership part of the contract, not merely equality of the sidebar on pages MkDocs happened to render. The inventory is taken from the prepared tree, so rerun `scripts/prepare-docs.py` before verification; otherwise stale or missing staging files can make the comparison misleading.

This check proves prepared-page/navigation coverage plus consistent labels, ordering, group entries, destinations, and ancestor relationships in parsed HTML across the generated pages. It does not compare CSS presentation, expanded/collapsed state, active-page highlighting, accessibility semantics, mobile drawer behavior, or keyboard focus. Keep the central nested `nav` configuration and perform desktop/mobile browser inspection before claiming the full sidebar requirement is accepted. Counts printed by the script describe the current build and must be refreshed after adding pages.
