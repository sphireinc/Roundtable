# Security Reviews

The local MCP tool `security.review` either scans a proposal patch or records a caller-provided review. Reviews are stored in SQLite and considered separately from votes, claim validity, and human approvals during patch validation. The implementation is in `internal/security/service.go`; the gate is evaluated by `internal/proposals/service.go`.

## Request fields

All fields are strings. The registry marks them optional, but the runtime requires at least one of `proposal_id` or `resource_id`. When both are provided, the service loads the proposal and also stores the supplied resource ID.

| Field | Default and behavior |
| --- | --- |
| `proposal_id` | Loads an existing proposal. With empty/omitted status, reads its persisted patch artifact and parses touched files before scanning. |
| `resource_id` | Associates the record with a resource. A resource-only call does not read or scan resource contents. |
| `review_id` | Generated `SR-` timestamp ID. Providing an existing ID updates that review rather than appending a new record. |
| `task_id` | Inherited from the proposal when empty; otherwise the supplied value is stored. |
| `reviewer_id` | Defaults to `security`. This is caller-provided metadata, not an authenticated identity. |
| `status` | Empty/omitted invokes the proposal scan, or defaults to `approved` for a resource-only review. Any supplied nonempty string is recorded directly and bypasses the scan. |
| `summary_md` | Uses the automatic scan summary when scanning and no summary was supplied. Otherwise defaults to `Security review recorded.` |

The response contains `review` (the saved database record) and `findings` (automatic findings for this call). Each finding has `code`, `severity`, `message`, and an optional `path`; the current heuristics do not populate paths or line numbers. Calls without a scan have no generated findings; the response/serialized findings can be `null` rather than an empty array. The tool has no argument for attaching caller-authored structured findings.

### MCP argument conversion edge cases

The local handler reads every field above only when the JSON value is a string. A number, boolean, array, object, or `null` is treated like an empty/missing string; the dispatcher does not first enforce the registry's advertised JSON Schema. Required selection checks compare against the exact empty string and do not trim whitespace. Thus `proposal_id: " "` is treated as a proposal ID and normally fails lookup, while `resource_id: " "` satisfies the at-least-one check and is stored as supplied.

This distinction is especially important for `status`: a nonempty string, including whitespace-only text, is a manual status and skips automatic patch reading/parsing/scanning for a proposal. Such a status is stored verbatim and will not satisfy the apply gate unless it exactly matches `approved` or, for the override path, `veto`. Wrongly typed or empty `status` instead selects the automatic scan when a proposal ID is present; for a resource-only call it becomes the default `approved`. Empty/wrongly typed `review_id` generates a new ID, and empty/wrongly typed `reviewer_id` defaults to `security`; whitespace values are retained. Empty/wrongly typed `task_id` inherits from a proposal, while a nonempty supplied string overrides it.

## Automatic proposal scan

Omit `status` to inspect the stored proposal patch:

```json
{"proposal_id":"P-123","reviewer_id":"security-1"}
```

The scan examines only lines starting with `+`, excluding patch headers starting with `+++`. It lowercases each added line and applies substring heuristics:

| Finding | Trigger | Severity |
| --- | --- | --- |
| `secret_logging` | A line contains `log`, `print`, or `console.` and also `token`, `secret`, `password`, `api_key`, or `private_key`. | `critical` |
| `dangerous_command` | A line contains a case-insensitive substring from the loaded policy's `commands.dangerous` list. Each matching pattern produces a finding. | `high` |

Any critical finding yields `veto`. Other findings yield `needs_changes`. No findings yields `approved`, including high/critical-risk proposals. Risk determines whether review is required; it does not force a failed scan result.

Scanning operates on the complete stored patch text, not only the paths returned by the patch metadata parser. It emits at most one `secret_logging` finding per matching added line. For dangerous commands, it emits one `dangerous_command` finding for each configured pattern found on each added line; multiple patterns or repeated matching lines can produce repeated finding codes and messages. Findings are sorted by descending severity string, then ascending code. This is lexical string ordering, not a separately defined severity rank; order among equal severity/code findings is not guaranteed. Findings contain neither source line numbers nor deduplicated path attribution, and the full matched pattern text can appear in their messages.

These checks can flag comments, variable names, and ordinary words containing a configured substring. They do not inspect removed/context lines, execute commands, verify secrets, analyze data flow, or establish that a change is secure. Perform a human/agent review appropriate to the affected feature in addition to these heuristics.

Missing proposals, unreadable patch artifacts, and malformed patches return errors before a review is stored when the automatic scan path is used. A supplied manual status skips patch reading/parsing after loading the proposal.

## Manual review records

A caller can record a completed review explicitly:

```json
{"proposal_id":"P-123","reviewer_id":"security-1","status":"approved","summary_md":"Reviewed the authentication changes and supporting tests."}
```

This call stores the supplied conclusion without running the automatic scan. The local service does not validate the status against an enum. Gate comparisons are exact: only `approved` satisfies the review gate directly; `veto` has the human-override path; `needs_changes`, unknown strings, and differently cased values fail satisfaction.

Reusing `review_id` replaces that record's proposal/resource/task associations, reviewer, status, summary, and findings. A manual update does not preserve earlier automatic findings. Its original `created_at` remains unchanged while `updated_at` advances.

The local Unix-socket MCP server has no application-level authentication or role authorization for this tool. Socket clients can supply a reviewer ID and an approval status. The gate therefore depends on trusting the callers permitted to access the socket. Restrict that access and inspect review evidence/provenance before relying on an approval. HTTP API authorization is a separate surface; it does not authenticate calls to this local tool.

## Review selection and application

Security review is required when proposal risk is exactly `high` or `critical`, touched paths match a high-risk policy pattern, or policy detects `dependency_change` or `migration`. Required-role review requests and ordinary proposal votes are separate from the persisted security-review gate.

For a proposal, the service lists reviews ordered by `created_at`, then `id`, and selects the last record. Updating an existing review changes `updated_at` but preserves its original creation time, so editing a review does not necessarily make it the selected latest review. Resource-only records are not selected for a proposal unless they also carry its proposal ID.

`patch.validate` exposes the selected `security_review`, all `security_reviews`, `security_review_required`, and `security_review_satisfied`. Its `valid` flag covers patch/claim checks separately. `patch.apply` revalidates the required security gate before changing files:

- `approved` satisfies this gate; other policy/human/claim requirements still apply.
- `veto` satisfies it only when the proposal service has a satisfying human approval with `override_policy` enabled.
- All other statuses, including an absent review, fail a required gate.
- If review is not required, validation reports this gate satisfied without requiring a record.

The veto override depends on the separate policy/human evaluation. That evaluation only selects a human approval when policy reports `NeedsHuman`; non-veto blockers prevent satisfaction, and policy vetoes require the selected approval's `override_policy` flag. A recorded approval therefore does not automatically override a security-review veto when the policy result does not require human handling. Inspect the returned policy and approval fields together.

Review findings are not bound to a patch hash by this service. After replacing or revising a proposal patch, obtain a new review of the current artifact rather than assuming an earlier approval covers the revision.

See [Governance Policies](../POLICIES.ROUNDTABLE.md), [Proposals and Transactions](TRANSACTIONS.md), and [MCP Tools](MCP_TOOLS.md) for the surrounding workflow.
