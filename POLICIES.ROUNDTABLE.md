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
## HTTP Policy Administration

The HTTP policy catalog is workspace-scoped database state, separate from the fenced Markdown runtime configuration above. Creating, publishing, enabling, or disabling a catalog record does not rewrite this file or establish that the runtime engine has loaded its definition.

List returns policies belonging to the selected workspace, ordered oldest creation time then ID. Detail requires exact policy/workspace membership. Revision listing first checks membership, then returns that policy's revisions in descending version order. Both collections load all rows before shared offset pagination; neither provides status, scope, or selector filtering. Query/scan failures use `policy_list_failed` or `policy_revision_list_failed`; final iteration errors are not separately checked. Detail lookup errors, including database errors, become HTTP 404 `policy_not_found`.

Policy projections include ID, workspace ID, name, status, optional current revision ID, scope, selector, severity, enforcement mode, human-approval requirement, metadata, and creation/update timestamps. Revision projections include ID, policy ID, integer version, status, definition, supplied creator ID, and creation timestamp. Selector, metadata, and definition are decoded as objects; invalid, null, or non-object stored JSON becomes `{}`. These projections do not apply the observability redactor. Do not store secrets in these fields.

### Creation Defaults and Persistence

Creation requires human authorization and a nonempty `Idempotency-Key` header in addition to common authentication. The handler checks header presence, not trimmed content, and does not itself perform replay deduplication. Input accepts `id`, `name`, `scope`, `severity`, `enforcement_mode`, `selector`, `metadata`, `human_approval_required`, and `definition`. A name must be nonblank after trimming, but its original whitespace is stored. Malformed JSON and missing/blank name both return HTTP 400 `invalid_policy` with `name is required`.

An exactly empty ID generates `policy-<UnixNano>`; supplied IDs are retained without trimming. Exactly empty scope, severity, and enforcement mode default to `workspace`, `normal`, and `advisory`. Other supplied strings are not enum-validated here. Omitted human-approval requirement is false. Omitted/null definition stores `{}`; omitted/null selector and metadata store JSON null, later projected as `{}`. Creation does not invoke the validation or simulation endpoints, check selectors against repository resources, or require a nonempty definition.

The policy starts `disabled`, with current revision `<policy-id>-r1`, version 1, status `draft`. Policy, revision, and `policy.created` audit insertion share one SQL transaction. Revision creator and audit actor use the supplied `X-Actor-ID`. Begin errors return HTTP 500 `policy_create_failed`; insertion, audit, and commit failures return HTTP 409 with that code, not necessarily a duplicate-ID diagnosis. After commit, the handler ignores reload errors and returns HTTP 201 with the policy projection. It does not append a main event.

### Actions and Revision Semantics

All five actions require human authorization, a nonempty `Idempotency-Key`, and an existing policy in the selected workspace. Unknown actions return HTTP 400 `invalid_policy_action`. **These action handlers do not decode a request body**: a supplied definition, expected version, reason, or impact acknowledgement does not affect their behavior.

| Action | Database effect |
| --- | --- |
| `publish` | Marks the current revision `published` and the policy `active`. Does not validate its definition or retire older published revisions. |
| `enable` | Sets policy status `active`, without changing revision status or requiring a published revision. |
| `disable` | Sets policy status `disabled`, without changing revision status. |
| `update-draft` | Allocates `MAX(version)+1`, copies the highest-version revision's definition into a new `draft`, and points the policy at it. Does not accept edited definition content or change policy status; an active policy can consequently point at a draft. |
| `clone` | Generates a new policy ID, copies catalog fields, appends ` (clone)` to the name, starts `disabled`, and copies the source current revision's definition to a new version-1 draft. Does not copy revision history or evaluations. |

There is no source-status precondition, expected-version comparison, or checked affected-row count for these writes. Repeated publish/enable/disable calls still update timestamps and audit. Draft allocation is not a caller-controlled optimistic-concurrency protocol; write conflicts can fail the transaction. Clone/update-draft use `INSERT ... SELECT`: a missing source revision can insert zero rows without an explicit handler check. Do not infer a complete revision graph solely from a successful action response.

Each action and its `policy.<action>` audit row commit together. Clone audit identifies the **source** policy, not the newly generated clone; the response contains the clone. Begin errors return HTTP 500 `policy_transition_failed`; write/audit/commit errors return HTTP 409 with that code. Post-commit policy reload errors are ignored. Success returns HTTP 200 with `policy` and `impact`, without a main event, runtime reload, session restart, approval request, or automatic re-evaluation.

### Impact Counts Are Not a Safety Gate

The action response calculates impact after commit with four independent, unscoped database queries. Counts are global across workspaces and are not narrowed by the policy's selector or scope: sessions in `active`/`running`, claims in `active`, proposals in `pending`/`in_review`, and transactions in `pending`/`running`. Other states are excluded. Claim expiry is not checked. Query failures are ignored and can appear as zero. These counts are neither an atomic snapshot nor evidence that listed work was affected, drained, or safely migrated; no count blocks an action.

## HTTP Policy Validation and Evaluation Boundaries

The HTTP policy control plane is separate from the runtime policy engine described in this document. Its schema discovery describes an object requiring `name` and `definition`, with properties for scope, selector, severity, enforcement mode, human-approval requirement, and metadata. This discovery object does not implement comprehensive rule validation or establish runtime enforcement.

HTTP validation reads the policy's current revision and checks only three conditions: a nonblank name, a nonempty definition object, and enforcement mode equal to `advisory` or `blocking`. It returns HTTP 200 with `valid`, `result` (`pass`/`fail`), issues, policy/revision IDs, `simulation: true`, and a deterministic key. It does not execute rules, verify selectors, inspect subject resources, or persist an evaluation. A policy with no current revision has an empty definition and therefore fails the nonempty-definition check.

Simulation accepts `subject_type`, `subject_id`, and optional string-array `evidence`. It requires nonblank trimmed subject strings but retains supplied values. Persisted evaluation uses the same input shape, checks only empty strings (whitespace-only values can pass), requires human authorization and a nonempty `Idempotency-Key`, and returns HTTP 201 after persistence. Both read the current revision, not a caller-selected revision. Lookup failures are reported as `policy_not_found`; malformed input uses `invalid_simulation` or `invalid_evaluation`.

**The current HTTP evaluator does not interpret definition values as executable rules.** It reports every top-level definition key as a matched rule, in Go map iteration order, regardless of value or subject. Default result is `pass`; exact blocking mode with an empty evidence array changes it to `warn` and adds remediation to provide evidence. A nonempty array of empty strings still counts as evidence. Human-approval requirement adds remediation to obtain approval but does not itself change the result or create an approval request. Subject type/ID are labels, not validated entity references. The evaluator does not verify supplied evidence, invoke tests, discover violations, or produce `fail` from rule execution.

Responses include policy/revision IDs, subject labels, result, matched rules, evidence, remediation, human-approval requirement, deterministic key, simulation flag, and current UTC RFC3339Nano evaluation time. Simulation has no persisted ID. Evidence is echoed without the general observability redactor; do not submit secrets or internal reasoning. Empty evidence may serialize as null depending on input, while matched-rule/remediation lists are initialized as arrays.

The deterministic key is hexadecimal SHA-256 of policy ID, revision ID, subject type, and subject ID joined with `|`. It does not include evidence, definition contents, time, actor, or simulation mode, and is not a unique database constraint or authorization token. Repeated evaluations can share it; delimiters in labels can make different tuples produce the same preimage. Validation uses subject labels `validation` and the policy ID.

Persisted evaluation inserts a new timestamp-derived evaluation ID plus a `policy.evaluated` audit row in one SQL transaction. Stored reasons include matched rules, evidence, remediation, human-approval requirement, and deterministic key. The actor is taken from the supplied actor header. Transaction-begin errors return HTTP 500 `evaluation_failed`; insert/audit/commit failures return HTTP 409 with that code. This handler does not append a main event, apply a proposal, enforce a transaction gate, or run the Go runtime's policy engine. A stored `pass` from this simplified evaluator is not proof that a governed patch is safe to apply.
## HTTP Vote and Consensus Semantics

HTTP vote listing verifies exact proposal/workspace membership, reads all proposal votes ordered by creation time then ID, and applies shared offset pagination (default 50, limit 1 through 200). It returns vote/proposal/agent IDs, optional session ID, decision, policy weight/version, text-redacted rationale, optional nonzero confidence, and creation time. A failed membership query is reported as `proposal_not_found`, not distinguished from a missing proposal. Vote query/scan failures use `vote_list_failed`; final iteration errors are not separately checked.

Consensus GET recalculates from current vote rows; it does not return the latest stored snapshot or persist a new one. This HTTP algorithm has fixed quorum 2 and approval threshold 2, independent of runtime policy configuration. Stored weights below 1 are treated as 1. Exact `approve`, `reject`, `abstain`, and `veto` values contribute to their weight buckets; every row contributes to `votes_cast`, including unknown decisions. Approval requires approval weight at least 2, at least two vote rows, and zero reject/veto weight. Any reject or veto yields `blocked`; otherwise outcome is `pending`. Abstentions can satisfy the row-count quorum. There is no unique-agent quorum, confidence weighting, role-specific veto eligibility, or proposal-state transition in this calculator.

The response includes proposal ID, outcome, four weight buckets, quorum, threshold, votes cast, policy version, explanation, and current UTC calculation time. Policy version is overwritten while scanning unordered rows, so mixed stored versions do not yield a deterministic selected version; no votes leaves it empty. These fields describe this calculator, not complete runtime acceptance gates or permission to apply a patch.

Vote mutation accepts a human-authorized caller or exact `X-Roundtable-Orchestrator: true`, plus a nonblank trimmed `Idempotency-Key`. Common authentication still applies; the orchestrator header is not itself a verified coordinator identity. Input requires nonblank agent ID, decision, and rationale; decision is trimmed/lowercased and limited to the four choices. Agent ID is retained as supplied, session ID is trimmed for storage, and confidence is not range-validated. The handler does not verify agent/session ownership, eligibility, enabled status, or session/proposal association.

Weights are determined at write time from the agent's stored lowercased role: `security` gets 3, `architect`/`reviewer` get 2, all others or lookup failures get 1. Roles are not trimmed in this lookup. Version is literal `default-v1`. Supplied vote ID is trimmed; omission creates a timestamp-derived ID. IDs are global upsert keys: an existing ID updates agent/session/decision/confidence/rationale/weight/version but **does not update proposal ID**. Reusing another proposal's vote ID can therefore alter that old proposal's vote while the response and newly calculated snapshot refer to the requested proposal. Do not reuse IDs across proposals.

The vote upsert, newly calculated consensus snapshot, and `vote.cast` audit row share a SQL transaction. Snapshot approval/rejection/abstain columns hold weights, not vote counts. Each mutation creates another snapshot even when updating an existing vote; no uniqueness per agent or terminal snapshot is enforced here. After commit, a main `vote.cast` event is attempted with run ID equal to the proposal ID and its error ignored, which does not guarantee inclusion in workspace event delivery. HTTP 201 contains the submitted vote projection and calculated consensus; its vote creation timestamp is not reloaded, and session ID can reflect untrimmed input rather than stored normalization.

Begin failure uses HTTP 500 `vote_failed`; write failures use HTTP 409 `vote_conflict`, and calculation failures use `consensus_failed`. The handler does not change proposal status, apply filesystem changes, or execute runtime consensus/policy checks. Use the runtime's documented governance gates separately.
