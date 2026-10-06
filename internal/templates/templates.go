package templates

import (
	"roundtable/internal/config"
	"roundtable/internal/mcp"
)

func StarterFiles(cfg config.Config) ([]struct {
	Path    string
	Content string
}, error) {
	registry := mcp.DefaultRegistry()
	schema, err := registry.ToolsSchemaJSON()
	if err != nil {
		return nil, err
	}
	server, err := mcp.ServerConfigJSON(cfg)
	if err != nil {
		return nil, err
	}

	files := []struct {
		Path    string
		Content string
	}{
		{Path: "AGENTS.ROUNDTABLE.md", Content: agentsDoc},
		{Path: "PROJECT.ROUNDTABLE.md", Content: projectDoc},
		{Path: "POLICIES.ROUNDTABLE.md", Content: policiesDoc},
		{Path: "TASKS.ROUNDTABLE/0001-bootstrap.md", Content: bootstrapTask},
		{Path: ".roundtable/config.yaml", Content: DefaultConfigYAML()},
		{Path: ".roundtable/memory/README.md", Content: memoryReadme},
		{Path: ".roundtable/mcp/AGENT_MCP_MANIFEST.md", Content: registry.Manifest(cfg)},
		{Path: ".roundtable/mcp/tools.schema.json", Content: schema},
		{Path: ".roundtable/mcp/server.json", Content: server},
	}
	return files, nil
}

func DefaultConfigYAML() string {
	return `version: 1
project_name: Roundtable
storage:
  sqlite_path: .roundtable/roundtable.db
  wal: true
mcp:
  transport: unix
  socket_path: .roundtable/mcp/roundtable.sock
agents:
  chair:
    role: Chair
    adapter: claude
    model: default
  architect:
    role: Architect
    adapter: claude
    model: default
  implementers:
    count: 2
    adapter: codex
    model: default
  reviewers:
    count: 1
    adapter: gemini
    model: default
  tester:
    role: Tester
    adapter: codex
    model: default
  security:
    role: Security
    adapter: claude
    model: default
  memory_oracle:
    role: MemoryOracle
    adapter: claude
    model: default
adapters:
  codex:
    command: codex
    supports_resume: true
    resume_pattern: "codex resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
  claude:
    command: claude
    supports_resume: true
    resume_pattern: "claude --resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
  gemini:
    command: gemini
    supports_resume: true
    resume_pattern: "gemini resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
  opencode:
    command: opencode
    supports_resume: false
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: false
  generic:
    command: sh
    supports_resume: false
    supports_mcp: false
    supports_readonly_workspace: true
    captures_session_id: false
`
}

const agentsDoc = `# Roundtable Agent Protocol

You are operating inside Roundtable, a shared-state multi-agent development environment.

## Authority and repository boundary

The SQLite-backed Roundtable state is authoritative. Your external CLI session memory is helpful but not authoritative. On startup or resume, inspect current table/task/claim state and query relevant project memory before continuing.

Use repository tools for read-only inspection. Do not directly modify authoritative repository files. Before proposing a patch, claim every affected resource, verify claims are compatible, and submit a unified diff through the proposal tool. The transaction/apply service is the intended code-patch mutation path. Explicit human-governed repository controls such as branch switching are separate operations. Claims are coordination metadata, not an operating-system sandbox.

## Startup checklist

1. Read table state and find your run, task, and role assignment.
2. Query memory for relevant decisions, constraints, conventions, risks, and prior failures.
3. Fetch the assigned task and inspect the affected resources and current claims.
4. Read relevant repository context through MCP tools; do not trust stale external session context over current state.
5. Claim resources before preparing a proposal.

## Priority turn requests

If you have something pressing to report or do, call agent.turn_request with run_id, agent_id, and a concise reason_md. Requests are queued in arrival order and deduplicated while outstanding. Wait for the Chair's agent.turn_scheduled event; then call agent.turn_start with the request_event_id, do the scheduled work using normal MCP tools, and call agent.turn_complete with that same request_event_id. Do not repeat a request while one is outstanding. A turn request does not preempt a running process or grant permission to bypass claim, proposal, review, or policy rules.

## Review and closeout

Record rationale with claims, proposals, votes, decisions, and releases. Follow the assigned role's review responsibilities; never treat a role label as proof that a CLI process was launched. When work is complete or blocked, report the evidence and uncertainty, update shared task/proposal state through MCP, and release claims when appropriate.

The local runtime currently coordinates turns and stores session metadata but does not automatically launch external agent CLI processes or capture their session IDs. Consult generated MCP manifest and project docs for the tool schema and current feature boundaries.
`

const projectDoc = `# Project: Roundtable

## Product definition

Roundtable is a shared-state agentic development management system. Many agents deliberate around one project state; repository mutations are governed through resource coordination, proposals, reviews/policy, and a transaction path. The durable project state, not an external agent's conversation history, is authoritative.

## Product surfaces

- Go CLI/runtime: initializes SQLite-backed state, serves local MCP tools, runs the coordinator loop, and provides table/watch/TUI projections.
- HTTP API: separate Go control-plane service with workspace-scoped REST and WebSocket/event endpoints, OpenAPI contract, governance and operational routes.
- Web UI: separate Next.js client for the API. The dashboard is wired; other shell links are not all implemented pages.
- Agent integration: MCP coordination, capability metadata, and session/briefing records exist. Automatic external CLI process launch, supervision, and session capture are not yet wired into the orchestrator.

## Implemented foundations and known boundaries

- SQLite persistence, schema initialization, WAL, event/snapshot data, task/claim/proposal/vote/decision/approval/transaction/test/memory records.
- Local MCP registry, generated manifest/schema/server metadata, repository read/search/symbol context, FIFO agent turn request events, and proposal/test/governance tools.
- File/directory/symbol claim overlap, TTL transitions, and stale base-hash reconciliation for supported resource types.
- Patch parsing, temporary-workspace validation, proposal resource coverage, policy/security/human gate evaluation, patch application, hashes, and rollback artifacts. Filesystem mutation and database persistence are not one atomic transaction; consult the transaction docs for recovery boundaries.
- Go symbol indexing uses the Go AST; TypeScript and Python use structural scanners. Tree-sitter-grade parsing is not present.
- The terminal UI is a polling snapshot layout with static command examples and limited keyboard handling, not the full command/selection/inspector experience in the design task pack.
- The browser UI currently wires the dashboard and workspace/branch context controls; the wider navigation and API contract describe broader current/planned scope. Consult the UI docs and task ledger before claiming a route is complete.

## Product invariants

1. One authoritative workspace state is shared by agents.
2. Agents use the repository read-only tool surface and submit patch artifacts; they do not directly edit the authoritative repository.
3. Resource claims coordinate overlapping work but are not an OS sandbox.
4. Policy, votes, security review, and human approvals are evaluated against persisted state before governed changes are applied.
5. The transaction/apply path records evidence and rollback material, but does not yet provide a cross-filesystem/SQLite atomic commit guarantee.
6. External agent sessions and memory support continuity; they do not override current task/claim/proposal/database state.

Human-governed workspace controls such as branch switching are separate from patch application and use API preflight/confirmation semantics.

## Technology

The current repository uses Go, SQLite, Git, Bubble Tea, a local agent-tool registry and newline-delimited JSON Unix-socket protocol, a standalone Go HTTP API, and a Next.js/React/TypeScript UI. The local socket is Roundtable-specific rather than a standards-complete JSON-RPC MCP transport. Symbol indexing currently supports Go, TypeScript/TSX, and Python with the parser approaches described in the symbol documentation.

## Product thesis

Most AI coding systems focus on one agent editing directly or multiple agents working in isolated worktrees and merging afterward. Roundtable's intended distinction is shared deliberation over common state with governed, serialized repository changes. Features named in task packs or this product thesis remain planned until their implementation and acceptance evidence are complete.
`

const policiesDoc = `# Roundtable Policies

This file defines default governance and risk policy. It should compile into runtime policy rules.

## Consensus defaults

` + "```yaml" + `
consensus:
  default:
    approvals_required: 2
    blockers_allowed: 0
    tests_required: true

  architecture:
    required_roles:
      - Architect
      - Reviewer
    approvals_required: 2
    blockers_allowed: 0

  security:
    required_roles:
      - Security
    security_veto_blocks: true
    human_approval_required: true

  dependency_change:
    required_roles:
      - Security
      - Tester
    human_approval_required: true

  destructive_command:
    human_approval_required: true
    security_approval_required: true

  migration:
    required_roles:
      - Architect
      - Security
      - Tester
    human_approval_required: true
    rollback_plan_required: true
` + "```" + `

## High-risk paths

` + "```yaml" + `
risk:
  high_paths:
    - ".env*"
    - "**/.env*"
    - "auth/**"
    - "security/**"
    - "infra/**"
    - "migrations/**"
    - "package.json"
    - "package-lock.json"
    - "pnpm-lock.yaml"
    - "yarn.lock"
    - "go.mod"
    - "go.sum"
    - "Dockerfile"
    - "docker-compose*.yml"
    - ".github/workflows/**"
` + "```" + `

## Dangerous commands

Commands requiring explicit claims and likely human approval:

` + "```yaml" + `
commands:
  dangerous:
    - "rm"
    - "chmod"
    - "chown"
    - "mv"
    - "git reset"
    - "git clean"
    - "git push"
    - "docker system prune"
    - "kubectl"
    - "terraform apply"
    - "npm install"
    - "pnpm add"
    - "yarn add"
    - "go get"
    - "pip install"
` + "```" + `

## Security veto

Security veto blocks a proposal unless Human explicitly overrides.

Security must veto if a proposal:

- exposes secrets
- logs tokens, credentials, private keys, session ids, or PII
- weakens authentication or authorization
- introduces command injection, path traversal, SQL injection, unsafe deserialization, SSRF, or XSS risk
- introduces unaudited dependency risk
- changes production infrastructure without appropriate policy
- runs destructive or irreversible commands without approval

## Human approval

Human approval is required for:

- high-risk resources
- production infrastructure
- auth/security/payment changes
- database migrations
- dependency changes
- destructive commands
- policy changes
- overriding a Security veto

## Claim conflict policy

A claim conflicts if it overlaps another active claim in a way that may cause semantic or textual conflict.

Conflict examples:

- directory claim conflicts with files below it
- file claim conflicts with symbols in that file
- symbol claims conflict if they overlap ranges
- schema claim conflicts with migrations touching that schema
- command claim conflicts with another mutating command

## Resume policy

When resuming:

- Roundtable state is authoritative.
- External CLI session memory is non-authoritative.
- Claims with stale base hashes become suspended.
- High-risk suspended claims require Human review.
- Chair decides whether to renew, revoke, or transfer stale claims.

## Runtime interpretation and format

The policy engine reads only fenced yaml blocks in this Markdown file. It supports a limited YAML-like subset with nested maps, scalar strings/integers/booleans, and string lists using space indentation; it is not a full YAML parser. If this file is missing, built-in policy defaults are used. Each parsed consensus profile replaces its entire built-in rule: omitted integers become 0, omitted booleans become false, and omitted role lists become empty. Profiles absent from the file retain their defaults. Include all fields whose behavior you want to retain. A nonempty risk.high_paths or commands.dangerous list replaces its complete default list; an omitted or empty list retains the default list.

Settings that currently affect runtime behavior:

- consensus.default.approvals_required sets the minimum approval/approve-with-notes votes; the evaluator enforces at least one.
- consensus.default.blockers_allowed limits revise/reject/veto votes. Veto additionally requires human handling when consensus.security.security_veto_blocks is true.
- consensus.security.human_approval_required makes matched high-risk paths require human approval. Proposal risk high or critical independently requires human handling.
- consensus.<profile>.required_roles contributes roles to review requests for profiles detected from paths. Current profiles are dependency_change (dependency manifests/lockfiles) and migration (migrations/ paths). High/critical risk or high-risk paths add Security; consensus.security.required_roles extends those roles.
- consensus.dependency_change.human_approval_required and consensus.migration.human_approval_required require human approval when their respective profiles are detected.
- risk.high_paths is matched against touched paths by a limited matcher, not a complete glob implementation. Matches can trigger human approval and Security review.
- commands.dangerous is matched as case-insensitive substrings in added patch lines by the security review service. It raises findings; it is not an OS command policy or a complete execution sandbox.

The starter example also contains fields that are parsed but do not currently gate patch application: profile-specific approvals_required and blockers_allowed, tests_required, security_approval_required, and rollback_plan_required. architecture and destructive_command are not automatically inferred as path profiles. Do not rely on these fields as enforced gates until implementation and tests make them so.

The prose in this file expresses human policy expectations; it does not configure runtime behavior unless explicitly described above. Claim overlap is separately implemented in docs/CLAIMS.md. Resume prose does not automatically impose extra high-risk claim approval in the current stale-claim reconciliation code.
`

const bootstrapTask = `# 0001 Bootstrap Roundtable

## Goal

Implement the initial Roundtable application skeleton.
`

const memoryReadme = `# Roundtable Memory

## Source of truth

Runtime memory entries are stored in the configured SQLite database, not as one authoritative file per memory. The Memory Oracle is the product role name; the current runtime does not launch a dedicated memory agent or automatically extract/contradiction-check memories.

## Agent use

Query project memory before work with relevant prior decisions or constraints. Record only durable, reusable facts with scope, kind, title, body, and provenance where available. Mark contradicted entries stale with a reason instead of silently reusing them. Memory is supporting context; current task, claim, proposal, vote, approval, and transaction records remain authoritative for their state.

This directory may contain optional exported summaries or generated human-readable reports. Such exports can become stale and should be checked against current SQLite state.
`
