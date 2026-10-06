# Roundtable

Roundtable coordinates multiple coding agents around one shared project state. Agents deliberate, claim resources, create proposals, review and vote, while a controlled transaction path governs changes to the authoritative repository.

This documentation describes the implementation in this repository. It distinguishes working behavior from planned surfaces, especially where an API or UI contract is broader than the currently implemented runtime.

## Start here

- [Quickstart](QUICKSTART.md) initializes a local workspace and runs the Go coordinator.
- [CLI Reference](CLI.md) documents the commands and flags implemented by the executable.
- [Configuration Reference](CONFIGURATION.md) describes every currently parsed key, default, and limitation of `.roundtable/config.yaml`.
- [Architecture](ARCHITECTURE.md) maps the Go runtime, standalone HTTP API, and web UI.

## System areas

- [Runtime and orchestration](ORCHESTRATION.md) explains run lifecycle, cycles, turn scheduling, retry behavior, and convergence.
- [Tasks and assignments](TASKS.md) documents local task storage, ordering, updates, and assignment behavior.
- [MCP tools](MCP_TOOLS.md) describes the agent-facing local tool boundary.
- [Proposals and transactions](TRANSACTIONS.md) explains how proposed changes are validated and applied.
- [Security reviews](SECURITY_REVIEW.md) covers automatic findings, manual review records, and the security gate used during application.
- [Human approvals](HUMAN_APPROVALS.md) explains persisted decisions and the policy selection rules used during application.
- [Tests and evidence](TEST_EXECUTION.md) documents command execution, captured logs, result status, and validation limits.
- [HTTP API](../api/README.md) links to the OpenAPI contract and endpoint guide.
- [Web UI](UI.md) records implemented routes separately from navigation concepts that are not yet implemented.
- [Operations](DEPLOYMENT.md) covers local deployment and runtime configuration.

## Documentation conventions

“Implemented” means the behavior is present in this checkout; “contract” means an API/schema or design artifact specifies a behavior; “planned” means the implementation is not complete. CLI flags and configuration tables are maintained against source code. The generated endpoint guide is maintained against `api/openapi.yaml`.

## Reference Directory

The left navigation is generated from one `mkdocs.yml` tree on every page. Its groups are Product, Getting Started, Runtime, Agent Interface, HTTP API, Web UI, and Operations. Runtime contains Governance and Database subgroups. The current page's heading table of contents is separate and intentionally varies by page; on mobile, open the navigation drawer to reach the shared groups.

### Product and Startup

| Reference | Use it for |
| --- | --- |
| [Project overview](../README.md) | Repository purpose, implementation scope, and entry points. |
| [Product model](../PROJECT.ROUNDTABLE.md) | Shared-state concepts and intended coordination model. |
| [Architecture](ARCHITECTURE.md) | Process boundaries and which component owns each operation. |
| [Agent protocol](../AGENTS.ROUNDTABLE.md) | Participation and governed repository-work expectations. |
| [Quickstart](QUICKSTART.md) | Local initialization and first execution. |
| [CLI](CLI.md) | Command syntax, flags, defaults, and output behavior. |
| [Runtime configuration](CONFIGURATION.md) | Parsed YAML settings; not HTTP configuration revisions or browser build variables. |

### Runtime and Agent Interface

| Reference | Use it for |
| --- | --- |
| [Orchestration](ORCHESTRATION.md) | Turns, pressing turn requests, run lifecycle, scheduling, and HTTP run-control boundaries. |
| [Tasks](TASKS.md) | Task metadata, assignments, and ordering. |
| [Symbols](SYMBOLS.md) | Resource identities, indexing coverage, and HTTP repository-entity limitations. |
| [Policies](../POLICIES.ROUNDTABLE.md) | Markdown policy parsing, runtime gates, HTTP policy administration, evaluation, votes, and consensus differences. |
| [Claims](CLAIMS.md) | Leases, overlap, ownership, HTTP acquisition, and contention resolution. |
| [Transactions](TRANSACTIONS.md) | Proposals, patch previews, validation/application, phase records, and recovery/compensation limitations. |
| [Security reviews](SECURITY_REVIEW.md) | Heuristics, persisted review decisions, and application gates. |
| [Human approvals](HUMAN_APPROVALS.md) | Local and HTTP approval fields, transitions, selection, and trust boundaries. |
| [Tests and evidence](TEST_EXECUTION.md) | Executed commands, captured results, and HTTP validation-stage semantics. |
| [Sessions and resume](SESSIONS.md) | Session lifecycle, external resume metadata, and HTTP observability scopes. |
| [Adapters](ADAPTERS.md) | Provider declarations, executable probes, enablement, and diagnostics. |
| [TUI and watch](TUI.md) | Terminal display and refresh behavior. |
| [Persistence overview](DATABASE.md) | SQLite operation, migrations, and table roles. |
| [Schema detail](DATABASE_SCHEMA.md) | Final/control-plane columns, constraints, and migration-specific differences. |
| [Memory Oracle](MEMORY_ORACLE.md) | Memory creation/search, revisions, provenance, merge, and lifecycle controls. |
| [MCP tools](MCP_TOOLS.md) | Advertised tool inputs and implemented local handlers. |

### HTTP API and Browser

| Reference | Use it for |
| --- | --- |
| [API overview](../api/README.md) | Server flags/environment, authentication defects and limits, CORS, metrics, health, repository/branch controls, deliberations, events, logs, audit, analytics, and dashboard projections. |
| [Endpoint guide](../api/docs/admin-api-guide.md) | Generated method/path inventory, declared permissions, and schema links. Permission labels are contract metadata, not independent enforcement. |
| [Schema reference](../api/docs/schema-reference.md) | Generated component properties, required flags, and declared response shapes; not runtime conformance proof. |
| [Error and retry semantics](../api/docs/error-semantics.md) | JSON decoding, attribution, actual authorization checks, idempotency scope, and recovery after ambiguous responses. |
| [Pagination and search](../api/docs/pagination.md) | Endpoint-specific cursor formats, ordering, filters, and consistency limits. |
| [Workspace settings](../api/docs/settings.md) | Defaults, revisions, confirmation/version handling, and settings not wired into running components. |
| [Notifications](../api/docs/notifications.md) | Recipient selection, unread/actionable counts, acknowledgment, and broadcast behavior. |
| [Maintenance](../api/docs/maintenance.md) | Global database backup, integrity, checkpoint, and destructive notification retention. |
| [Web UI](UI.md) | Implemented browser routes, configuration, client behavior, and unfinished navigation surfaces. |

### Operations and Evidence

| Reference | Use it for |
| --- | --- |
| [Deployment](DEPLOYMENT.md) | Local/process deployment, GitHub Pages preparation/navigation checks, and verification scopes. |
| [Troubleshooting](TROUBLESHOOTING.md) | Diagnosis and recovery without assuming mutation rollback. |
| [Resume briefing example](../examples/resume-briefing.md) | Example agent context, not authoritative state or a guaranteed exact generated format. |

Read behavioral references alongside generated contracts when calling an endpoint: implementation limitations can differ from declared fields and permission labels. A successful documentation build proves rendering/link checks, not live API correctness, safe repository mutation, exhaustive coverage, or publication of the current checkout. The source-grounded coverage audit remains ongoing.
