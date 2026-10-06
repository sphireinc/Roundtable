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
