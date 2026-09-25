# Roundtable Policies

This file defines default governance and risk policy. It should compile into runtime policy rules.

## Consensus defaults

```yaml
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
```

## High-risk paths

```yaml
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
```

## Dangerous commands

Commands requiring explicit claims and likely human approval:

```yaml
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
```

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
- Claims with stale base hashes become `suspended`.
- High-risk suspended claims require Human review.
- Chair decides whether to renew, revoke, or transfer stale claims.

## Runtime interpretation and format

The policy engine reads only fenced `yaml` blocks in this Markdown file. It supports a limited YAML-like subset with nested maps, scalar strings/integers/booleans, and string lists using space indentation; it is not a full YAML parser. If this file is missing, built-in policy defaults are used. Parsed consensus rules merge over defaults; a nonempty `risk.high_paths` or `commands.dangerous` list replaces that default list.

Settings that currently affect runtime behavior:

- `consensus.default.approvals_required` sets the minimum approval/approve-with-notes votes; the evaluator enforces at least one.
- `consensus.default.blockers_allowed` limits revise/reject/veto votes. Veto additionally requires human handling when `consensus.security.security_veto_blocks` is true.
- `consensus.security.human_approval_required` makes matched high-risk paths require human approval. Proposal risk `high` or `critical` independently requires human handling.
- `consensus.<profile>.required_roles` contributes roles to review requests for profiles detected from paths. Current profiles are `dependency_change` (dependency manifests/lockfiles) and `migration` (`migrations/` paths). High/critical risk or high-risk paths add Security; `consensus.security.required_roles` extends those roles.
- `consensus.dependency_change.human_approval_required` and `consensus.migration.human_approval_required` require human approval when their respective profiles are detected.
- `risk.high_paths` is matched against touched paths by a limited matcher, not a complete glob implementation. Matches can trigger human approval and Security review.
- `commands.dangerous` is matched as case-insensitive substrings in added patch lines by the security review service. It raises findings; it is not an OS command policy or a complete execution sandbox.

The starter example also contains fields that are parsed but do not currently gate patch application: profile-specific `approvals_required` and `blockers_allowed`, `tests_required`, `security_approval_required`, and `rollback_plan_required`. `architecture` and `destructive_command` are not automatically inferred as path profiles. Do not rely on these fields as enforced gates until implementation and tests make them so.

The prose in this file expresses human policy expectations; it does not configure runtime behavior unless explicitly described above. Claim overlap is separately implemented in [Claims](docs/CLAIMS.md). Resume prose does not automatically impose extra high-risk claim approval in the current stale-claim reconciliation code.
