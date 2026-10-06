# Proposals, Patch Validation, and Transactions

Agents submit patch-bearing proposals. Roundtable records proposal metadata and affected resources, validates the patch against the current repository and governance state, and exposes an orchestrator-side apply operation. A successful apply creates a transaction record and a rollback artifact. This is the repository's governed mutation path; it is not a single ACID transaction across SQLite and filesystem writes.

## Proposal lifecycle

`proposal.create` stores task/agent/title/summary/risk, affected resources, expected tests/rollback notes, and a patch artifact beneath `.roundtable/patches/`. `proposal.attach_patch` can attach or replace patch content for an existing proposal. Review requests, votes, decisions, security reviews, and human approvals are persisted separately. Proposal status is updated through proposal/patch operations; exact eligible states and gates should be read from the service, not inferred from the design task pack.

## Votes and decision records

`vote.cast` requires an existing `proposal_id` and nonempty `agent_id`, `vote`, and `reason_md`. It returns the saved `vote`. `confidence` defaults to 0 and is not range-clamped by the local handler. The handler also accepts `vote_id` (not advertised in the registry); missing/empty generates a `V-` timestamp ID, while an existing ID replaces that vote row's fields and retains its creation time. Caller agent IDs are not authenticated or checked against review roles by this handler. Policy counts stored recognized vote strings, without using confidence or policy-weight metadata in its current approval count.

Use `approve`, `approve_with_notes`, `revise`, `reject`, `veto`, or `abstain` as the workflow vocabulary. The local handler accepts any nonempty string; unrecognized or differently cased strings are stored but do not contribute to approval/blocker counts. `vote.list` requires a proposal ID and returns its votes in ascending creation-time/ID order. The returned list can be `null` when empty. These tools do not automatically apply a patch after votes arrive.

`decision.record` requires nonempty `decision`, `rationale_md`, and `decided_by`; `proposal_id` and `task_id` are optional associations. It additionally recognizes an unadvertised `decision_id`, generating a `D-` timestamp ID when empty. An existing ID updates its row rather than creating an immutable revision. The handler stores decision strings and actor metadata directly, without checking the actor identity or loading the associated task/proposal first. It returns `decision`; recording it does not itself update task/proposal status, grant approval, or apply a patch. Lifecycle operations such as `patch.reject` have their own associated state changes.

## Validation performed by `patch.validate`

Validation reports separate results for:

1. The proposal's active-claim validation for its declared affected resources.
2. Patch artifact existence and unified-diff parse success.
3. Resource coverage: the touched paths must be covered by proposal resources.
4. Applying the patch to a temporary workspace copy of the current project.
5. Policy evaluation based on proposal risk, touched paths, and stored votes.
6. Human approval satisfaction and whether a security review is required/satisfied.

The returned `valid` field covers claim validation, artifact presence, patch parsing, resource coverage, and successful temporary apply. Policy approvability and security/human gates are reported separately and are enforced again by `patch.apply`.

## Apply sequence

`patch.apply` repeats validation immediately before mutation. It refuses invalid patches, policy results that are not approvable without the required human override, and unsatisfied required security review. It then:

1. Reads the stored patch and parses touched files.
2. Captures pre-apply file contents for a rollback artifact.
3. Copies the workspace into a temporary directory and verifies patch application there.
4. Computes a before-state hash, applies the patch to the authoritative workspace, and computes an after-state hash.
5. Marks the proposal accepted, writes a rollback JSON artifact under `.roundtable/patches/`, and persists a transaction with proposal/run/applier, hashes, status, timestamps, rollback path, and metadata.

`repo.RepoStateHash` combines Git HEAD (when available) with a SHA-256 of sorted workspace files, excluding `.git` and `.roundtable`; outside a Git repository it uses the workspace hash. This is not solely a Git tree hash and may include pre-existing uncommitted files.

## Important guarantees and gaps

- The required security-review gate reads persisted statuses. The local MCP caller can provide both a manual status and a reviewer ID; supplying a status skips the automatic patch scan. Reviewer identity is not authenticated by the Unix-socket tool server. Restrict socket access to trusted participants and inspect review provenance. See [Security Reviews](SECURITY_REVIEW.md) for exact scan and gate behavior.
- Temporary-apply validation detects many malformed/context-mismatched diffs before the authoritative apply.
- The apply service does not itself run `expected_tests`; use the testing tools/records separately. A stored test result is not proof that the current patch was tested unless the association and timing are checked.
- The before/after hashes and rollback JSON are recovery evidence, not an automatic rollback engine.
- Filesystem mutation and subsequent proposal/rollback/transaction persistence are separate operations. A database or artifact-write error after patch application can leave changed files without a complete transaction record.
- Validation and actual apply are not protected by a single workspace lock/SQLite transaction in this service; concurrent external writers can create a time-of-check/time-of-use race.
- Git worktree status and uncommitted user edits matter. Do not interpret the before hash as a clean baseline.

## Recovery

Before retrying an uncertain apply, inspect the workspace diff, proposal state, transaction rows, and `.roundtable/patches/*-rollback.json`. Preserve the rollback artifact and database/WAL files. Restore only after reviewing the exact changed paths and confirming no later work would be overwritten. The service does not expose a general automatic rollback command today.

## Related tools

`proposal.create`, `proposal.attach_patch`, `proposal.request_review`, `vote.cast`, `decision.record`, `security.review`, `human.request_approval`, `patch.validate`, `patch.apply`, and `patch.reject` are documented in [MCP Tools](MCP_TOOLS.md). Policy syntax is documented in [Governance Policies](../POLICIES.ROUNDTABLE.md).
