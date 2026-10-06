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
