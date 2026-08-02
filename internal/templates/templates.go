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

Roundtable state is authoritative.

Your external CLI session memory is helpful but not authoritative. On startup or resume, query the table and memory before continuing.
`

const projectDoc = `# Project: Roundtable

Roundtable is a local-first, Go-based, Bubble Tea-powered TUI for shared-state agentic development.
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
`

const bootstrapTask = `# 0001 Bootstrap Roundtable

## Goal

Implement the initial Roundtable application skeleton.
`

const memoryReadme = `# Roundtable Memory

Runtime memory is stored in SQLite. This folder is for optional exported summaries and generated human-readable memory reports.
`
