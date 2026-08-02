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
