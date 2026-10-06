# Human Approval Records

The local tools `human.request_approval` and `human.approval_status` store and read approval records. The proposal service selects these records during policy evaluation. Requesting a record does not send a notification or wait for an interactive human response in these handlers.

## Request and decision fields

`human.request_approval` requires nonempty string `subject` and `reason_md` on every call, including updates. The remaining fields are optional:

| Field | Behavior |
| --- | --- |
| `approval_id` | Generated `H-` timestamp ID. An existing ID replaces that record's supplied/defaulted fields. |
| `proposal_id` | Loads an existing proposal when nonempty. A missing proposal returns an error. |
| `task_id` | Inherited from the proposal when omitted/empty; otherwise caller-supplied. A task-only record is allowed. |
| `status` | Defaults to `requested`. Nonempty supplied strings are stored without validating an enum. The apply selection compares exactly to `approved`. |
| `requested_by` | Defaults to `orchestrator`; supplied value is metadata. |
| `decision_md` | Optional decision explanation; defaults empty. |
| `decided_by` | Optional decision actor; defaults empty. It is caller-supplied metadata. |
| `override_policy` | Boolean, default `false`. String `"true"` is not a boolean and falls back to `false`. |

```json
{"subject":"Dependency change","reason_md":"The proposal updates a dependency manifest.","proposal_id":"P-123"}
```

The response contains `approval`, including its generated ID. A trusted human/operator records the decision by submitting that ID again with the full desired record:

```json
{"approval_id":"H-123","subject":"Dependency change","reason_md":"The proposal updates a dependency manifest.","proposal_id":"P-123","status":"approved","decided_by":"human","decision_md":"Reviewed the dependency and test evidence.","override_policy":false}
```

Updates are replacements rather than partial patches. Omitting status resets it to `requested`; omitted associations and decision fields are cleared/defaulted; omitted override becomes `false`. Creation time is retained and update time advances. Preserve the values you intend to retain. `human.approval_status` requires `approval_id`, returns the saved `approval`, and errors for an unknown record.

## How application selects approval

For a proposal, approvals are ordered by ascending `created_at`, then `id`. The service scans backward for a satisfying record:

1. If policy does not report `NeedsHuman`, no human record is selected as satisfying.
2. If blocker count exceeds veto count, a human record cannot satisfy the policy result.
3. The record must have exact status `approved`.
4. If policy votes include a veto, the record must also have `override_policy: true`.

The newest eligible approved record is selected. A later rejected/requested record does not automatically invalidate an earlier eligible approval. Editing a row changes `updated_at` but not its position in creation-time order. Task-only approvals do not satisfy a proposal's gate; selection is by proposal association.

`patch.validate` reports `human_approvals`, the selected `human_approval`, and `human_approval_applied`, alongside the policy result. Application uses those results with claim, patch, and security checks. A satisfying human record can allow the proposal service's apply path when policy is not ordinarily approvable; inspect all returned gates. For a separate security-review veto, the selected human record must also have `override_policy` enabled. See [Security Reviews](SECURITY_REVIEW.md).

## Authority and evidence

The local Unix-socket MCP service does not authenticate the caller as a human or authorize these fields by role. A socket caller can submit `status: approved`, a decision actor, and an override flag. These records provide persisted coordination evidence for trusted participants; their labels alone do not prove human identity or consent. Control socket access and verify decision provenance. The HTTP API's human/agent authorization rules are separate from this local MCP handler.

These records are not bound to a patch hash, and this handler does not revoke them automatically when a proposal patch changes. Recheck the decision against the current proposal artifact and record updated approval evidence when needed. The handler does not itself append a decision event or create an immutable approval revision.

See [Governance Policies](../POLICIES.ROUNDTABLE.md), [MCP Tools](MCP_TOOLS.md), and [Proposals and Transactions](TRANSACTIONS.md).
## HTTP Approval Control Plane

HTTP approval list/detail scope is derived by joining the approval's proposal to the URL workspace. Task-only or proposal-less approvals are not discoverable through these reads, even though the request handler permits an omitted proposal ID. Lists load all qualifying rows oldest-first by creation time/ID before offset pagination (default 50, limit 1 through 200). There is no automatic expiration, expiry field, status filter, or background reminder in these handlers. Query/scan errors use `approval_list_failed`; lookup failures use `approval_not_found`.

Responses expose approval/workspace/proposal/task IDs, subject, reason, proposal risk (default `normal`), stored status, literal `required_role: "human"`, requested/deciding actors, override-policy flag, timestamps, and an empty decision-metadata object. Stored decision Markdown is not selected into the response's optional `decision` field. Request-supplied risk and required role are not persisted by this handler. Decision metadata is saved in audit payloads, not restored into approval responses. Reads do not independently redact subject/reason/actor strings, although request and decision reasons are text-redacted on those write paths.

Request creation accepts a human-authorized caller or exact orchestrator header, requires a nonempty (not trimmed) `Idempotency-Key`, and requires nonblank subject/reason. A supplied proposal must belong to the workspace; task ID is not similarly validated. Optional ID is used verbatim, with timestamp-derived ID on omission. It inserts status `requested` and an audit row in one transaction, but does not initialize proposal approval state, emit a main event, create a notification, or schedule a human turn. After commit, reload errors are ignored; a proposal-less request can therefore return HTTP 201 with an empty response projection despite a persisted approval row.

### HTTP Input Shapes and Normalization

Request JSON accepts `id`, `proposal_id`, `task_id`, `subject`, `reason`, `risk`, `required_role`, `decision_metadata`, and `override_policy`. Unlike the local MCP tool, the HTTP reason key is `reason`, not `reason_md`; an unknown typed field is rejected by the shared decoder. Malformed input and blank subject/reason all return HTTP 400 `invalid_approval` with the same required-fields detail. Subject and reason are checked after trimming but stored with their original whitespace (reason also receives limited text redaction). There is no subject-length, risk-enum, required-role, or actor-identity validation in this handler.

Exactly empty request ID generates `approval-<UnixNano>`; whitespace IDs are retained. A nonempty proposal ID is looked up without trimming, so whitespace-only proposal input fails membership rather than being treated as omitted. Task association is not inherited from the proposal on this HTTP path. Empty/whitespace-only task IDs become SQL null; nonblank values retain their whitespace. The literal orchestrator-header branch does not itself require a nonblank actor ID; common bearer authentication remains separate, and the header is not a verified coordinator credential.

Decision JSON accepts only `reason`, `decision_metadata`, and boolean `override`. Action is selected by the route, not a body `decision` or `status` field. Type mismatches and unknown fields return HTTP 400 `invalid_decision`. Reject/request-changes with blank trimmed reason return `reason_required`; a supplied nonblank reason retains whitespace after limited redaction. There is no decision-specific confidence, expiry, expected revision, patch hash, or evidence-verification input.

Request `risk` and `required_role` are accepted but unused: risk is later read from the joined proposal and required role is always projected as `human`. Both creation and decision `decision_metadata` values are marshaled directly into audit payloads without recursive observability redaction; omission/null can store JSON null. They do not become immutable approval revisions, runtime evidence checks, or response metadata. Protect raw audit access and do not submit secrets in that object. The HTTP handler's human-role checks have the authenticated-context limitation described in [API Authorization](../api/README.md#authentication-and-authorization).

### Decision Effects

Decisions require human authorization, a nonempty idempotency key, and exact prior status `requested`. Actions are `approve`, `reject`, `request-changes`, and `defer`, producing `approved`, `rejected`, `changes_requested`, and `deferred`. Reject and request-changes require a nonblank reason; approve/defer do not. Deferred and changes-requested approvals are no longer decidable through this same path, because it only accepts `requested`.

An input `override: true` requires the approval's existing override-policy flag and exact `X-Override-Permission: true`; these checks do not establish a distinct force-override role or external permission grant. The decision does not change the stored override-policy flag. Thus that flag reflects what was requested, not proof that the deciding request actively chose an override.

Approval update and decision audit share a transaction. Proposal `approval_state` updates only when its prior value is `pending` or `requested`. Only request-changes attempts to change main proposal status to `in_review`; approve/reject/defer do not set main proposal status to their named outcome. The secondary proposal-status update ignores SQL errors. Affected-row counts are not checked, so concurrent decisions or proposal-state changes can leave persisted effects different from the caller's intended transition.

After commit, approval reload and a separate main event append are attempted with errors ignored. No filesystem apply, runtime acceptance, compensation, or test execution follows. Begin errors use HTTP 500 `approval_request_failed`/`approval_decision_failed`; write/commit failures use HTTP 409 with those codes. Re-read authoritative approval and proposal state before continuing, and do not treat a decision response as proof that all governance gates passed.

Successful decision returns HTTP 200 with the approval projection; creation returns HTTP 201. Main decision events use run ID equal to the workspace ID and type `approval.<route-action>`, preserving the hyphen in `request-changes`; payload contains approval ID and reloaded status. A reload failure can therefore also distort the attempted event payload. Commit-only failures can have an uninformative error detail because the commit error is not retained. List handlers do not separately check final row-iteration errors. Detail/database lookup failures are classified as HTTP 404, not consistently separated from absent approvals.
