# Proposals, Patch Validation, and Transactions

Agents submit patch-bearing proposals. Roundtable records proposal metadata and affected resources, validates the patch against the current repository and governance state, and exposes an orchestrator-side apply operation. A successful apply creates a transaction record and a rollback artifact. This is the repository's governed mutation path; it is not a single ACID transaction across SQLite and filesystem writes.

## Proposal lifecycle

`proposal.create` stores task/agent/title/summary/risk, affected resources, expected tests/rollback notes, and a patch artifact beneath `.roundtable/patches/`. `proposal.attach_patch` can attach or replace patch content for an existing proposal. Review requests, votes, decisions, security reviews, and human approvals are persisted separately. Proposal status is updated through proposal/patch operations; exact eligible states and gates should be read from the service, not inferred from the design task pack.

## Local Proposal Reads

`proposal.list` accepts optional actual string `task_id` and `status`. Empty/missing or non-string values impose no filter; nonempty values compare exactly without whitespace trimming or case normalization. It loads all database proposals ordered by creation time then ID and filters in memory. There is no run/workspace/agent restriction, cursor, page size, newest-first mode, or automatic exclusion of terminal records. The response is `{proposals: [...]}` with an empty array when no matches survive, not a shared HTTP page envelope.

The underlying list selects ID, task/agent IDs, title, summary Markdown, patch path, status, risk, and creation time. It does not populate the newer HTTP control-plane substates, workspace metadata, base revision, or validation summary merely because those database columns exist. Local stored-record serialization is distinct from the HTTP response schema; do not infer field parity from shared tables. List query, scan, and final iteration errors propagate rather than returning a verified empty result.

`proposal.get` requires a nonempty string proposal ID, loads that record by ID, and separately reads its resource links sorted by resource ID. It returns `proposal` and `proposal_resources`; an empty association slice can be null. The links contain proposal/resource IDs, not expanded resource definitions, claims, or symbols. A resource-read error fails the call after proposal lookup. These independent reads are not an atomic snapshot.

Neither read validates artifact existence, downloads patch bytes, redacts all stored metadata, recalculates policy, loads votes/decisions/security/human/test evidence, or proves current apply eligibility. Use the corresponding tools and current validation for those tasks. Proposal path/status metadata alone is not proof of a readable patch or a completed transaction; HTTP list/detail membership and pagination are separately described below.

## Local Patch Attachment and Replacement

`proposal.attach_patch` requires actual nonempty string `proposal_id` and `patch`, loads the proposal by ID, parses touched files, merges optional `affected_resources` with parsed file resources, and validates those claims against the **stored proposal author**. It does not authenticate that the caller is that author or require a pending/in-review proposal status. Whitespace is not independently trimmed by these required-string checks. Malformed patch/resource arguments or missing claims fail before the file write; this is not full apply validation, policy acceptance, or test execution.

If the proposal already has a patch path, attachment reuses it; otherwise it constructs `.roundtable/patches/<proposal-id>.diff` and saves that path. The helper joins the stored path to the service root without a separate symlink/containment check at this write site. It creates parent directories with requested mode `0755` and writes complete patch bytes with requested mode `0644`, subject to umask/existing modes. This is an overwrite, not append, exclusive creation, immutable artifact revision, or atomic temporary-file rename. Preserve the previous patch yourself through the governed evidence workflow when comparison is needed.

After writing, it upserts the existing proposal, ensures resource records, replaces proposal-resource links with the merged list, and reloads proposal/resources for the response. These steps are not one filesystem/database transaction. Failure can leave new patch bytes with old or partial resource metadata; a late response error does not restore the previous artifact. No expected proposal revision, patch hash, or compare-and-swap guards concurrent replacements.

Attachment does not clear existing votes, decisions, human approvals, security-review records, test runs, or prior transaction evidence; it does not reset status/risk/base metadata or request fresh review. Those records are not automatically rebound to the new bytes. **Reassess existing acceptance evidence after every replacement** rather than treating an earlier approval or passing test as approval of the changed artifact. The response contains the saved proposal and resource list, without an immutable patch-version ID or dedicated replacement event from this handler.

## Votes and decision records

### Local Review Requests and Rejection

`proposal.request_review` requires a nonempty proposal ID, loads proposal/resources/votes, and reads/parses the current artifact to evaluate policy. Missing/unreadable/malformed artifacts fail the request; it does not perform temporary apply or execute tests. Target roles always include `Reviewer`, add configured required roles from detected profiles, and add `Security` for exact high/critical risk or high-risk paths, extending it with configured security roles. Role labels are deduplicated case-sensitively and sorted; candidate matching to stored agent roles is case-insensitive but does not trim stored roles.

Every enabled matching agent except the exact stored proposal author is returned with role, agent ID, adapter, and stored status. Readiness, session availability, workspace association, concurrency limits, or a current turn are not checked. When a role has no candidate, its response entry contains only the role; this is not an error or proof that review occurred. Proposal status changes to `in_review` only from empty or exact `pending`; other statuses still return review context without a transition. The response includes proposal/resources, roles/candidates, policy result, and human/security requirement flags.

This local handler does not persist review assignments, append a review-request event, contact candidates, create approval records, run a security scan, schedule turns, or apply a patch. Its role selection does not enforce voter eligibility. A successful response is a computed review plan, not completed review evidence or an asynchronous job receipt. HTTP request-review uses its separately documented transition behavior rather than this full local response.

`patch.reject` requires nonempty `proposal_id` and `reason_md`, loads the proposal, and unconditionally upserts status `rejected` without prior-state or caller-ownership checks. It then upserts a rejected decision associated with the proposal/task. Unadvertised `decision_id` defaults to a generated `D-` timestamp ID; `decided_by` defaults to `orchestrator`. Supplied nonempty values and reason text are stored without general text redaction or authenticated actor validation. Reusing a decision ID updates that row rather than creating an immutable history entry.

Status write and decision write/reloads are separate operations, not one transaction. A decision failure can leave the proposal rejected without the intended decision record; repeated calls can create additional decisions. Rejection does not revert applied files, delete patch/resources, release claims, invalidate votes/approvals, cancel sessions, or create compensation. It returns saved `proposal` and `decision`, with no dedicated rejection event emitted by this handler. In particular, rejecting an already accepted proposal changes metadata, not the repository mutation it previously represented.

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

### Temporary Workspace Copy

Validation creates an OS temporary directory named `roundtable-patch-*`; apply separately creates `roundtable-apply-*` and repeats the copy/application check. Both schedule recursive cleanup on return, with cleanup errors ignored. There is no configured temporary-root, copy-size limit, disk-space preflight, or retained dry-run workspace for inspection.

`CopyWorkspace` walks the current filesystem, not a Git commit/index export. It skips directories whose basename is exactly `.git` or `.roundtable` at **any depth**, but does not read `.gitignore`, skip dependency/build trees, filter secrets, or exclude ordinary hidden files. Untracked and already-modified files are copied. Large `node_modules`, build output, and local environment files can therefore increase work and copy sensitive material into temporary storage. A file named `.git` (as in some worktree layouts) is not excluded by the directory-only test.

Directories are created with requested mode `0755`; files are opened and copied into newly created regular files. Original executable bits, permissions, ownership, timestamps, and extended attributes are not preserved. File symlinks are opened through their targets and copied as regular file contents; directory symlinks are not traversed as directories by this walk and can cause a copy failure when opened as a file. There is no symlink-containment check in this helper. Other special filesystem entries are not explicitly filtered. Copy/close errors can fail the dry run for reasons unrelated to patch validity.

The walk is not a snapshot or a context-aware bounded operation: concurrent external writes can produce a mixed-time copy, and request cancellation does not independently interrupt its file-copy loop. Successful patch application to this copy proves applicability to that copied view, not preservation of filesystem metadata, clean Git state, test success, or absence of secrets. It also does not eliminate the race before authoritative application. Review the actual worktree and recorded gates separately.

`patch.apply` repeats validation immediately before mutation. It refuses invalid patches, policy results that are not approvable without the required human override, and unsatisfied required security review. It then:

1. Reads the stored patch and parses touched files.
2. Captures pre-apply file contents for a rollback artifact.
3. Copies the workspace into a temporary directory and verifies patch application there.
4. Computes a before-state hash, applies the patch to the authoritative workspace, and computes an after-state hash.
5. Marks the proposal accepted, writes a rollback JSON artifact under `.roundtable/patches/`, and persists a transaction with proposal/run/applier, hashes, status, timestamps, rollback path, and metadata.

`repo.RepoStateHash` combines Git HEAD (when available) with a SHA-256 of sorted workspace files, excluding `.git` and `.roundtable`; outside a Git repository it uses the workspace hash. This is not solely a Git tree hash and may include pre-existing uncommitted files.

## Repository-State Hash Format

`WorkspaceHash` walks the selected root and skips directories named exactly `.git` or `.roundtable` at any depth. It includes other nondirectory paths regardless of Git tracking/ignore rules, slash-normalizes and sorts their relative names, then hashes each relative path, a NUL byte, full file bytes, and another NUL byte in that order with SHA-256. The result is lowercase hexadecimal. Empty directories contribute nothing. Permissions, executable bits, ownership, timestamps, extended attributes, symlink identity, and Git staging state are not encoded. File symlinks are read through their targets; unreadable paths or directory symlinks read as files can fail hashing.

`RepoStateHash` first runs Git discovery/HEAD commands, then computes that filesystem hash. Outside Git it returns only the hexadecimal workspace hash. Inside Git it returns `git:<HEAD>:<workspace-hash>`; an unborn HEAD recognized by its error-message checks uses `no-head` in place of HEAD. Those checks inspect specific command-error text rather than a structured Git status. Other Git failures abort hashing. Git commands have no context timeout in this helper, and the discovered top-level path is not used to broaden hashing beyond the supplied root. A root inside a larger repository can therefore combine that repository's HEAD with only the selected subtree's files.

Branch name, remote/upstream, merge state, reflog, and clean-worktree status are not separate hash components. Two branches at the same commit with identical included files can match. Mode-only changes or empty-directory changes can match too. Conversely, ignored build/dependency/environment files can change the hash without a proposal modification. A `.git` file is not excluded by the directory-only rule. Treat transaction `before_git_hash`/`after_git_hash` as these mixed Git/filesystem labels, not pure Git object IDs, signatures, or a full filesystem backup.

Hashing reads the current filesystem sequentially with no snapshot lock, size cap, cancellation checks, or per-file manifest output. Concurrent changes can yield mixed-time evidence, and a failure after authoritative apply can prevent recording the after hash. Hash equality does not prove tests passed, files are safe, or later work may be overwritten. Preserve current diffs, transaction records, and recovery artifacts alongside hashes before any manual restoration.

## Important guarantees and gaps

### External Patch Executor

Temporary and authoritative application both call `exec.Command("patch", "-p1")` with the chosen root as working directory and the full patch text on stdin. The executable must be available on the Roundtable process's PATH. This is the system `patch` utility, not `git apply`, a Go-native diff engine, a configured adapter command, or a shell pipeline. `-p1` strips one leading pathname component according to that installed utility's behavior. No explicit batch, forward-only, fuzz, reject-file, backup-file, or binary-mode options are passed; parser recognition of a change does not guarantee that the installed executor supports all its Git diff metadata.

The command inherits the process environment and has no context cancellation, timeout, resource limit, sandbox, or output-byte cap in this helper. It collects combined stdout/stderr in memory. Success discards that output and returns only nil error; failure includes the exit error and trimmed combined output in `patch apply failed: ...`, without general secret redaction here. Review who can read tool errors and artifacts. Different platform implementations/versions can behave differently; a documentation build does not probe this executable.

The helper does not stage files, commit, reset, switch branches, enforce a clean worktree, run tests, or inspect/reverse partial effects. A nonzero exit can follow earlier file/hunk changes; the caller performs no automatic filesystem rollback. Temporary checks reduce ordinary context failures but do not establish an atomic authoritative apply or eliminate concurrent-write races. Inspect changed files and any executor-created artifacts after an uncertain failure before retrying. Missing `patch` fails command startup rather than activating a fallback implementation.

- The required security-review gate reads persisted statuses. The local MCP caller can provide both a manual status and a reviewer ID; supplying a status skips the automatic patch scan. Reviewer identity is not authenticated by the Unix-socket tool server. Restrict socket access to trusted participants and inspect review provenance. See [Security Reviews](SECURITY_REVIEW.md) for exact scan and gate behavior.
- Temporary-apply validation detects many malformed/context-mismatched diffs before the authoritative apply.
- The apply service does not itself run `expected_tests`; use the testing tools/records separately. A stored test result is not proof that the current patch was tested unless the association and timing are checked.
- The before/after hashes and rollback JSON are recovery evidence, not an automatic rollback engine.
- Filesystem mutation and subsequent proposal/rollback/transaction persistence are separate operations. A database or artifact-write error after patch application can leave changed files without a complete transaction record.
- Validation and actual apply are not protected by a single workspace lock/SQLite transaction in this service; concurrent external writers can create a time-of-check/time-of-use race.
- Git worktree status and uncommitted user edits matter. Do not interpret the before hash as a clean baseline.

## Recovery

Before retrying an uncertain apply, inspect the workspace diff, proposal state, transaction rows, and `.roundtable/patches/*-rollback.json`. Preserve the rollback artifact and database/WAL files. Restore only after reviewing the exact changed paths and confirming no later work would be overwritten. The service does not expose a general automatic rollback command today.

## Rollback Artifact Format

Runtime apply captures rollback entries before authoritative mutation, using the parsed touched-file list. The eventual `.roundtable/patches/<transaction-id>-rollback.json` is an indented JSON array, not a reverse diff or an automatically executable restore plan:

```json
[
  {"path": "src/example.go", "existed": true, "content": "previous text\n"},
  {"path": "src/new.go", "existed": false, "content": ""}
]
```

`path` is the supplied parsed relative path. Successful `os.ReadFile` records `existed: true` and converts all bytes to a Go string. A not-exist error records false with empty content; other read errors abort capture. Empty existing files are distinguishable from missing files through the flag. This helper does not capture directory trees, file permissions, ownership, timestamps, executable bits, symlink identity/target metadata, extended attributes, or Git index state. Reading a symlink follows its target. JSON string serialization can replace invalid UTF-8, so the artifact is not a byte-exact binary-file backup.

Writing creates parent directories with requested mode `0755` and writes the artifact with requested mode `0644`, subject to umask/existing permissions. It uses an ordinary direct write rather than exclusive creation, atomic rename, fsync, checksum validation, encryption, or restrictive secret-specific permissions. Reusing a transaction ID can reuse/overwrite its artifact path. File contents are not redacted; preserve access controls and do not publish recovery artifacts in documentation or Git.

Capture is in memory before apply, but artifact writing occurs after files have changed and the proposal has been marked accepted. A later artifact-write or transaction-persistence failure can therefore leave mutation without a complete saved recovery record. There is no schema version, before/after hash embedded in this array, later-work conflict check, or general restore handler. Compare transaction hashes, current diffs, and every affected path before creating a governed recovery patch. The `existed: false` flag describes the capture-time state, not authorization to delete a file now containing later work.

## HTTP phase history and recovery advice

The API exposes GET `/api/v1/workspaces/{id}/transactions/{transaction_id}/phases` and `/recovery`. Both resolve the workspace and require a transaction associated with that workspace; lookup failure returns 404 `transaction_not_found`. Neither endpoint executes a phase, retries an apply, restores files, or invokes the runtime transaction manager.

Phase history reads `transaction_phases` rows for the transaction, ordered by `created_at` then ID ascending. Items include ID, transaction ID, phase, status, actor, optional reason/request ID, inputs/outputs objects, optional log reference/failure code, recovery state, optional start/end timestamps, and creation time. Null recovery state becomes `recoverable`; this fallback is not a verified recovery assessment. Null, malformed, or non-object inputs/outputs become empty objects. Reason receives text redaction, but inputs/outputs are decoded **without payload redaction**. Actor, log reference, failure code, and other phase columns are not independently text-redacted here. Do not place secrets or internal reasoning in these records. A log reference is metadata, not an automatic log download.

All phase rows are loaded before shared offset pagination, with default limit 50 and accepted range 1 through 200. Query/scan failures return `phase_list_failed`; final iteration errors are not separately checked. A page boundary is not a transaction snapshot, and stored phases are not proof that filesystem changes and audit records were committed atomically.

Recovery advice returns `transaction_id`, stored `status`, `repository_state`, an `actions` array, and optional `blocked_reason`. Its entire decision is a fixed mapping from transaction status:

| Stored status | Advertised actions | Repository state |
| --- | --- | --- |
| `pending` | `stage`, `validate`, `cancel` | `unknown` |
| `staged` | `validate`, `apply`, `cancel` | `unknown` |
| `failed` | `retry`, `cancel`, `compensate` | `unknown` |
| `cancelled` | `compensate` | `unknown` |
| `applied` | Empty | `verified` |
| Any other value | Empty; reason `transaction state has no safe recovery action` | `unknown` |

The literal `verified` for `applied` does not result from inspecting Git, file hashes, rollback artifacts, phase outputs, or current repository integrity. Action labels are advice, not authorization, capability discovery, or a guarantee that an executable endpoint exists for each label. An unfamiliar stored state, including a runtime-specific terminal state, can receive no recommended action even if manual investigation remains possible. Always inspect actual repository state and relevant runtime recovery evidence before retrying or compensating.

## HTTP Transaction Control

Transaction list/detail membership is determined by joining the transaction's proposal to the selected workspace, not by transaction `run_id`. Orphaned transactions or transactions whose proposal belongs elsewhere are not returned. List orders by transaction ID and loads all rows before shared offset pagination; there are no status/run filters in this handler. Query/scan errors return HTTP 500 `transaction_list_failed`; final iteration errors are not checked separately. Detail lookup errors become HTTP 404 `transaction_not_found`, including underlying database failures.

The projection contains ID, selected workspace ID, proposal/run IDs, before hash, optional after hash, status, applier, optional application time and rollback path, and metadata. Metadata uses object decoding with `{}` for null/malformed/non-object JSON, without payload redaction. Hash fields retain their historical `git_hash` names but have the runtime hash semantics explained above.

Every control action requires human authorization and a nonempty `Idempotency-Key` header, plus common authentication. This handler checks header presence rather than trimmed content and has no local replay cache. It does not decode a JSON request body. `reason` for status actions comes exclusively from the URL query and receives limited text redaction; avoid sensitive reasons in URLs, which can also appear in access logs.

### Status Actions and Validation

`stage`, `retry`, and `cancel` map to stored statuses `staged`, `pending`, and `cancelled`. They do not copy a patch, schedule a worker, rerun validation, stop a process, restore files, or update proposal transaction substates. The SQL update excludes rows already `applied` or `cancelled`, but **does not check affected-row count**. Thus an action on either terminal status can succeed, leave status unchanged, and still add a `completed` phase and audit row. Other existing statuses are not subjected to action-specific preconditions.

The status update, phase insertion, and `transaction.<action>` audit insertion share one SQL transaction. Phase actor is the supplied actor header; start/end timestamps are the same current UTC RFC3339Nano value. No executable inputs/outputs or failure record are populated by this action. Begin errors use HTTP 500 `transaction_transition_failed`; write/commit failures use HTTP 409. A commit-only failure can produce an uninformative detail because its error is not retained. Post-commit reload errors are ignored; success returns HTTP 200 with the transaction projection and does not append a main event.

`validate` loads the workspace-owned transaction and calls runtime `PatchValidate` for its stored proposal ID. Empty proposal ID returns HTTP 409 `transaction_invalid`; service errors return `transaction_validation_failed`. Success returns the runtime validation object with HTTP 200, not the transaction projection. It does not advance status, persist a phase, run expected tests, or guarantee apply eligibility; inspect the separate policy/security/human fields described above. Unknown actions, after transaction lookup, return HTTP 400 `invalid_transaction_action`.

### Apply Routing and Authorization Boundaries

`apply` additionally requires exact `X-Roundtable-Orchestrator: true`. This supplied header is a routing guard, not independent verification of a transaction-manager identity. Proposal ID comes from a route parameter when present, otherwise the `proposal_id` query parameter; missing ID returns HTTP 400 `proposal_id_required`. JSON body fields are ignored.

Unlike other actions, apply branches **before** the workspace-scoped transaction lookup. It passes the supplied proposal ID and route transaction ID directly to runtime `PatchApply`, using the selected workspace root, workspace ID as `run_id`, and actor header as `applied_by`. This HTTP branch does not verify that the supplied proposal belongs to the selected workspace, matches an existing transaction, or is associated with the route transaction ID. The service loads proposals by ID and runs its own governance checks; this is not equivalent to the missing HTTP association checks. Do not rely on list/detail membership filtering as authorization for apply.

The service performs the actual repository mutation sequence documented above, not a queued phase transition. It can create/upsert a transaction using the supplied ID rather than requiring a previously staged transaction. Success returns HTTP 200 with runtime proposal, transaction, policy, human-approval, and security-review objects. Every returned service error maps to HTTP 409 `transaction_apply_blocked`; that label does not prove files remained unchanged, since failures can occur after authoritative mutation. Preserve and inspect repository state before retrying.

### Compensation Is Proposal Creation Only

`compensate` requires a workspace-owned transaction and a nonempty stored rollback path, otherwise HTTP 409 `compensation_unavailable`. It does not check transaction status, current hashes, artifact existence, artifact contents, or whether later work would be overwritten. It generates `compensating-<UnixNano>` and inserts a new high-risk, pending proposal with empty task ID, actor header as agent ID, title/summary naming the original transaction, approval state `pending`, transaction state `not_started`, and patch path copied verbatim from the rollback path. No affected-resource rows are created here.

The runtime's rollback artifact is JSON containing recovery entries, **not a unified reverse patch**. This endpoint does not convert it; the resulting proposal therefore does not by itself provide a patch suitable for normal validation/application. Review recovery evidence and prepare a valid governed patch with resources before attempting restoration. No automatic rollback occurs and the original transaction is unchanged.

Proposal insertion and audit are separate writes, with audit errors ignored. No main event, phase, approval request, or deduplication is created. Repetition can create multiple compensating proposals. Insert failure returns HTTP 409 `compensation_failed`; success returns HTTP 201 with proposal ID, original transaction ID, copied patch path, and literal `pending` status.

## HTTP Patch Preview Boundaries

Proposal patch metadata, files, diff, and symbols endpoints load a workspace-owned proposal and read its stored patch path from disk on each request. The path must be nonblank and repository-relative; lexical normalization rejects root/parent escapes and absolute paths. The helper does **not** resolve symlinks, so a lexically contained path is not proof that the actual read target stays inside the workspace. Patch contents are read entirely into memory and parsed before pagination; there is no configured patch-byte limit or immutable preview snapshot.

Proposal lookup failures map to 404 `proposal_not_found`; other path/read/parser failures map to 400 `patch_unavailable`. A successful preview does not perform `git apply --check`, verify leases, evaluate policy, run tests, or authorize transaction application.

Metadata returns proposal ID, stored patch path/base revision, inspected current HEAD, stale-base flag, file count, added/removed counts, and binary count. Repository inspection errors are ignored. Stale-base is true when a nonempty stored base differs from the returned HEAD, including an empty HEAD after inspection failure. **The line-count labels are not actual diff-line statistics:** added lines counts parsed change-range entries, removed lines counts deleted-file entries, and binary count remains zero in this handler. Do not use these fields as code churn or risk measurements.

File items include path, old/new paths, operation, binary flag, and added/removed counts. Path prefers new path, falling back to old. Operation precedence is rename, create, delete, then modify. Added/removed counts are left at zero. Binary detection is a simple search for `Binary files a/<path>` or `Binary files b/<path>` in the whole patch, not comprehensive Git binary-patch decoding. Lists use shared offset pagination, default 50, limits 1 through 200.

Diff returns paginated strings from splitting the text-redacted patch on newline, not structured hunks or HTML. A trailing newline produces a final empty string. Limited text redaction can modify diff content, so the response is not an exact downloadable patch or a suitable source for applying changes. It does not sanitize arbitrary secret content or validate Markdown/HTML rendering.

Symbols indexes every currently readable touched file in the **existing worktree**, not the proposed post-patch contents or only declarations overlapping changed ranges. Unsafe paths and indexing errors are silently skipped. Deleted, unsupported, or not-yet-created files can contribute no symbols without an error response. Items expose path, resource ID, kind, name, and line range, potentially using absolute indexer paths. Treat them as current-file context subject to [Symbol Index](SYMBOLS.md) limitations, not a verified semantic impact analysis.

Separate preview requests can observe different patch bytes, repository HEADs, or file contents. Preserve authoritative patch artifacts and use the actual validation/application pipeline before any governed mutation.

## Related tools

`proposal.create`, `proposal.attach_patch`, `proposal.request_review`, `vote.cast`, `decision.record`, `security.review`, `human.request_approval`, `patch.validate`, `patch.apply`, and `patch.reject` are documented in [MCP Tools](MCP_TOOLS.md). Policy syntax is documented in [Governance Policies](../POLICIES.ROUNDTABLE.md).
## HTTP Proposal State Transitions

### Creation and Listing

Internal HTTP creation requires exact `X-Roundtable-Orchestrator: true` and a nonblank trimmed idempotency key in addition to common authentication. This supplied marker is not proof of a verified coordinator identity. Title and patch path are trimmed; summary is text-redacted but not trimmed, so whitespace-only summary can pass. All three must be nonempty after that processing. Path checks reject any `..` substring (including benign filenames containing it) or leading `/`; they do not inspect patch existence, resolve symlinks, parse the patch, or establish claim coverage. Preview and actual validation perform separate checks.

Omitted proposal ID receives a timestamp-derived value; supplied ID is used without trimming. Empty risk defaults to `normal`, while arbitrary nonempty risk strings are accepted. Deliberation ID, proposer session ID, base revision, and resource IDs are stored without handler-level association/eligibility checks. Task and agent IDs are inserted as empty strings. Initial main status and vote/policy/approval substates are `pending`; transaction state is `not_started`. No vote, policy evaluation, approval request, review assignment, or execution begins automatically.

Proposal insert, supplied resource-link inserts, and creation audit share a transaction. Resource IDs are not deduplicated by the handler, so database constraints can reject duplicates or invalid links. Begin failure uses `proposal_create_failed`; insert/link/audit/commit failures use `proposal_create_conflict`. After commit, reload and separate main-event errors are ignored. HTTP 201 is therefore not proof of a populated response or workspace event delivery; the event run ID is the proposal ID.

List selects exact workspace membership, oldest creation time then ID, and performs a separate detail read per row before offset pagination. It has no status/risk/agent/search filters. Per-row detail errors silently omit records, while initial query/scan/final-iteration failures return `proposal_list_failed`. Default limit is 50, maximum 200; small pages do not bound the initial collection load. Detail lookup errors use `proposal_not_found`. Neither read recalculates governance substates or verifies the stored patch artifact.

### Transition Effects

The HTTP proposal transition handler requires human authorization, a nonblank trimmed `Idempotency-Key`, and exact proposal/workspace membership. It recognizes only these actions:

| Action | Allowed source states | Target state |
| --- | --- | --- |
| `request-review` | `pending` | `in_review` |
| `reject` | `pending`, `in_review` | `rejected` |
| `withdraw` | `pending`, `in_review` | `withdrawn` |

Unknown actions return `invalid_proposal_transition`; disallowed source states return 409 `illegal_proposal_transition`. This handler does not decode a request body or require a rejection/withdrawal reason. Request-review does not assign reviewers, cast votes, evaluate policy, acquire claims, or start tests. Reject/withdraw do not release claims or restore files. Approval/application use separate surfaces; action labels alone are not runtime transaction operations.

State update and before/after audit insertion share a SQL transaction. The update includes the previously read status in its predicate, but affected-row count is not checked: a concurrent state change can cause zero updated rows while audit is still inserted and the response succeeds. Begin failure uses HTTP 500 `proposal_transition_failed`; SQL/commit failure uses HTTP 409 `proposal_transition_conflict`. Vote, policy, approval, and transaction substate columns are not reset or recalculated by this handler.

After commit, proposal reload and separate main-event append errors are ignored. Main event run ID is the proposal ID, and event type uses the raw hyphenated action (`proposal.request-review`), while audit action replaces hyphens with underscores (`proposal.request_review`). This is not guaranteed workspace live-stream membership. HTTP 200 returns the reloaded projection when available, not evidence that review work or event delivery occurred. Proposal reads expose stored substates and text-redact title, summary, and patch path; those values are labels, not verified up-to-date consensus or policy outcomes.
