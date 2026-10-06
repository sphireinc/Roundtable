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

The policy engine reads only fenced `yaml` blocks in this Markdown file. It supports a limited YAML-like subset with nested maps, scalar strings/integers/booleans, and string lists using space indentation; it is not a full YAML parser. If this file is missing, built-in policy defaults are used.

Consensus overrides are applied by profile name. Each profile present in the parsed file replaces that entire built-in rule; fields do not inherit individually. Omitted integer fields become `0`, omitted booleans become `false`, and omitted role lists become empty. Profiles absent from the file retain their built-in rules. For example, overriding `consensus.security` with only `required_roles` also sets `security_veto_blocks` and `human_approval_required` to `false`. Include every field whose behavior you want to retain. An overridden default rule with no approval count still requires at least one approval because the evaluator clamps that count.

A nonempty `risk.high_paths` or `commands.dangerous` list replaces its complete default list. An omitted or empty list retains the built-in list; it cannot clear the defaults. These list rules differ from consensus profile replacement.

### Multiple blocks and parser details

Only a fence whose trimmed opening line is exactly three backticks followed by `yaml` is read; `yml`, uppercase labels, and unlabeled fences are ignored. The closing line must trim to exactly three backticks. An unterminated recognized fence returns an error. Prose outside these fences does not configure the engine.

Recognized blocks are combined in document order before decoding: nested maps merge recursively and later scalar/list values replace earlier values at the same key. This is a separate operation from replacing built-in profile rules. Appending a second `consensus.security` block with just `required_roles` retains boolean fields already present in an earlier block in the same file. Replacing the entire file with only that partial profile resets those booleans when the combined file is decoded and applied to built-in defaults.

The parser counts space indentation; child maps/lists can use a deeper indentation level, and siblings must agree on the selected level. Tabs do not count as indentation. Blank lines and full-line comments beginning with `#` after trimming are skipped. Inline comments remain value text. The first colon separates a map key from its value; list entries use `- ` and support scalar values only.

Scalar parsing trims whitespace and edge double-quote characters, then attempts Go integer and boolean parsing. It does not decode string escapes or single-quoted strings. Quoted numeric/boolean text can still become a number or boolean. List decoding retains only nonempty string items; numeric/boolean items are discarded. Inline arrays/maps, anchors, aliases, multiline strings, and general YAML semantics are unsupported. Invalid integer/boolean field values decode to `0`/`false`; unknown fields are ignored. A consensus profile that is present with a scalar instead of a map returns an error.

### Path matching and profiles

The matcher converts platform separators to `/`, trims pattern whitespace, and matches case-sensitively. Its special cases are limited:

| Pattern | Runtime interpretation |
| --- | --- |
| Empty pattern | Never matches. |
| `auth/**` | Matches `auth` itself and paths starting with `auth/`; does not match `api/auth/file.go`. |
| `**/.env*` | Uses a basename-prefix special case and matches `.env`-prefixed basenames at any depth. The special case is not general recursive glob syntax. |
| Pattern without `/`, such as `Dockerfile` or `.env*` | Tries Go `filepath.Match` against the complete path and then its basename. |
| Other patterns | Uses Go `filepath.Match`; malformed patterns do not match. |

Dependency profiles use exact basenames `go.mod`, `go.sum`, `package.json`, `package-lock.json`, `pnpm-lock.yaml`, or `yarn.lock`, including nested files. Migration profiles require a path starting with `migrations/` at the root; `api/migrations/001.sql` does not automatically receive that profile. A custom high-risk pattern can still make such a nested path high risk.

Review requests always include Reviewer and add configured profile roles. High/critical proposal risk or matched high-risk paths add Security and its configured required roles. These requests select enabled agents by role, case-insensitively, excluding the proposal author. They do not constrain vote counting: the policy evaluator counts stored `approve`/`approve_with_notes` votes as approvals and `revise`/`reject`/`veto` votes as blockers, without checking the voter's role. A veto also increments the separate veto count. Role requirements and vote thresholds therefore describe distinct runtime operations.

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

See [Security Reviews](docs/SECURITY_REVIEW.md) for the automatic heuristics, manual status recording, and how persisted review records affect patch application.
