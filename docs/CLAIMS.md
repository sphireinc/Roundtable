# Resource Claims

Claims are persisted leases that coordinate access to resources. A claim is a conflict-management record; it is not an operating-system permission, sandbox, or guarantee that an agent cannot write a file outside the MCP boundary.

## Resource representation

A resource record contains ID, type, optional path/symbol/language and line span, current hash, and metadata. If no resource ID is supplied, IDs are normalized from type and path; symbol IDs use `symbol:<path>#<name>`. Paths are slash-normalized. Symbol line spans are populated when indexing succeeds.

The service accepts type strings and stores resource metadata, but conflict detection only defines overlap for identical IDs and these pairs: symbol/file in the same path; symbol/directory containing that path; file/directory containment; overlapping/nested directories; identical symbols in the same path. Other resource categories may be recorded but do not automatically receive semantic overlap detection. Do not assume endpoint/schema/command/design resources have specialized conflict rules without adding them.

## Claim types and conflict matrix

Valid claim types are `read`, `write`, `review`, and `exclusive`.

| Existing/requested modes | Conflict behavior |
| --- | --- |
| Either side is `exclusive` | Conflicts when resources overlap. |
| Either side is `read` (unless exclusive applies) | No conflict under current mode logic. |
| `review` with `review` | No conflict. |
| `review` with `write` | No conflict. |
| `write` with `write` | Conflict when resources overlap. |

Only active claims participate in conflict checks. A failed claim attempt records a contention entry before returning a conflict error.

## Lifecycle and defaults

Claim creation requires agent ID, task ID, resource type, valid claim type, and resource ID or path. Missing claim IDs are generated. TTL defaults to 15 minutes when the request duration is nonpositive; the CLI default is 900 seconds. Missing resume policy defaults to `hold`; arbitrary nonempty resume-policy strings are currently accepted, not validated against a finite enum. `renewable` is persisted; CLI-created claims set it true.

State transitions record statuses such as active, released, revoked, suspended, and expired. Expiration processes active claims whose RFC3339 expiry is at or before the supplied time. The CLI can use `--at-unix` for a time override. Extension renews expiry and heartbeat; not every runtime surface exposes every transition.

## Hashes and resume reconciliation

On resume, stale reconciliation inspects active claims with a nonempty base hash, excluding read and review claims. File claims compare file SHA-256; directory claims compare directory hashes; symbol claims compare the current indexed line-range hash. A changed nonempty hash suspends a claim. Other resource types have no content hash in the current implementation. Missing/unresolvable resource content can produce an empty hash and is not equivalent to proof that the resource is unchanged.

## Creation and contention boundaries

The service checks required strings for emptiness, not whitespace-only content, and validates claim mode against the four exact values above. It does not authenticate the caller or verify that supplied agent/task/run IDs identify authorized participants. A supplied resource ID is retained verbatim rather than checked against the derived type/path/symbol ID. Use consistent IDs and metadata; an ID label alone is not a canonical filesystem identity.

Creation upserts resource metadata **before** checking conflicts. A rejected request can therefore still create or update a resource row. Symbol span discovery uses the supplied resource path directly and silently ignores indexing errors; a claim can exist without a verified declaration span. Conflict detection reads active claim and resource rows separately, with no transaction encompassing conflict-check plus insert and no unique overlapping-lease constraint. Concurrent creators can race; a successful claim is not a database-enforced exclusive filesystem lock.

Each detected conflict causes a separate contention insert before the conflict error is returned. Partial contention inserts remain if a later insert fails. Repeated failed attempts can create multiple contention rows; there is no automatic queue, owner notification, approval, or resolution in this creation path. Existing active claims from the same agent are not excluded from conflict checks, so an agent can conflict with its own overlapping write/exclusive claim.

Caller-supplied claim IDs are upsert keys rather than guaranteed-new identifiers. A successful creation can replace an existing claim row under that ID. Creation and release/revoke/suspend ignore event-append errors after saving: a successful response does not prove the corresponding audit event exists. Claim state, resource updates, contention rows, and event writes are not one atomic transaction.

## Expiration, renewal, and transitions

Conflict checks use exact status `active`, not the current wall-clock expiry. A due claim continues participating until an expiration operation changes its status. Listing likewise does not expire records as a side effect. Expiration scans all local claims, not a run-specific lease set; its supplied run ID is used for emitted events. Invalid RFC3339 expiry strings are skipped, and inactive claims are untouched. Earlier expiration changes remain if processing a later claim fails.

The service-level `Extend` operation requires exact active status, but does not inspect `renewable`, ownership, or whether the current expiry has already elapsed. It sets expiration to **now plus** the requested TTL, not old expiration plus TTL, so extending with a shorter duration can shorten a lease. Nonpositive TTL becomes 15 minutes. Heartbeat is refreshed separately with a current UTC timestamp. An extension-event failure is returned after the claim has already been saved. There is no `resource.extend` tool or `claims extend` CLI in the current local surface; this describes the service method, not an available agent command.

Release, revoke, and suspend overwrite status without checking a permitted prior-state transition or matching actor ownership. Repeating a transition is not rejected; changing an already-terminal claim is possible through these service methods. They preserve expiry, base hash, heartbeat, and rationale fields; transition reason is event payload content rather than replacement claim rationale. Local caller actor strings are attribution, not authorization. API-specific checks are a separate surface.

## Reconciliation selection details

Despite the name, stale-claim reconciliation does not use agent-session heartbeat age, session status, `heartbeat_at`, `expires_at`, `renewable`, or `resume_policy` to select or recover leases. It scans active claims with nonempty base hashes, optionally filters exact agent ID, and skips read/review modes. The run ID scopes emitted events, not selection of claim rows. Stored `hold` or custom resume policies do not implement automatic renew/release behavior here.

For each selected claim it reloads the resource, computes the current content hash, and persists that resource hash even when no suspension follows. Missing resource rows or propagated hash/storage errors abort the operation after any earlier changes. An empty current hash leaves the claim active; it is not a verified unchanged result. Symbol reconciliation attempts to rediscover the named declaration, but indexing failure or a missing name retains the previously stored span. Consult [Symbol Index](SYMBOLS.md) before treating that span as a precise semantic boundary.

## HTTP Claim Acquisition and Transitions

Claim list/detail require exact stored `claims.workspace_id` membership; legacy unbound rows are not automatically assigned to the URL workspace. Lists load all claims before membership queries and offset pagination. They do not filter by status, agent, or expiry. Resource lookup errors are ignored when building responses, potentially leaving type/path/symbol empty. Response session ID is not populated, even though creation accepts a `session_id` input. Rationale receives text redaction; resource metadata and identifiers are returned without equivalent independent redaction.

HTTP creation requires exact `X-Roundtable-Orchestrator: true` plus a nonblank trimmed idempotency key, in addition to common authentication. The header is a supplied marker, not a verified coordinator process identity. Agent/task/resource-type/path/mode are trimmed; required fields are agent, task, resource type, and mode. Nonempty path undergoes lexical workspace-relative checks but not symlink resolution. Agent/task/session/run associations are not verified here. The local claim service performs acquisition with its documented conflict and persistence limitations, then a separate SQL update binds the claim to the URL workspace. Binding failure returns `claim_persist_failed` after acquisition has already occurred. Session ID input is ignored. Service errors use `claim_conflict`, with HTTP 400 selected by error-text fragments `required`/`invalid`, otherwise 409.

Transitions accept the raw nonempty actor header exactly matching claim agent ID, or a human-authorized caller, plus a nonblank idempotency key. This owner comparison uses supplied attribution, not independently verified ownership credentials. Actions are `extend`, `release`, and `force-release`. Force-release uses the same owner/human boundary and release service as ordinary release; it adds a required nonblank `reason` query parameter, not a distinct force-override permission check. It does not revoke via a separate service path.

Extend reads optional body `ttl_seconds` but ignores body decode errors, so malformed/empty input can use the service's default TTL. Ordinary release may have no reason; query reason is text-redacted before the service call. These operations inherit the local service's transition/renewal behavior. Their emitted service-event run label is supplied as the claim's task ID, not an independently resolved run or workspace. Failures use `claim_transition_conflict` and can occur after a service write.

Non-owner transitions attempt an extra audit insert with `forced: true`, ignoring its error. That insert uses the raw query reason rather than the text-redacted service reason. Owner transitions do not make that additional audit record. Re-read current claim state and do not infer complete audit coverage, actual process ownership, or an automatic contention resolution from HTTP success.

## HTTP Contention Listing and Resolution

Contention listing resolves the workspace, reads local `claim_contentions` joined to the challenged claim, then filters by the challenged claim's workspace membership. Optional `resource_id` is exact and untrimmed; there is no status filter. Rows are ordered oldest creation time then ID, loaded before shared offset pagination (default 50, limits 1 through 200). A missing challenged claim can cause a null-to-string scan failure rather than being silently omitted. Query/scan failures return `contention_list_failed`; final iteration errors are not separately checked.

Items include contention/workspace/resource IDs, requested resource/agent/task/mode/path, challenged claim and current owner agent, contention state/reason, creation time as `acquired_at`, current challenged-claim expiry as `lease_expires_at`, and `allowed_resolutions`. The acquisition timestamp describes the contention record, not necessarily the owner's original lease acquisition. The list does not apply general payload or text redaction to these fields. Every row advertises `keep_owner`, `transfer`, `extend_lease`, and `reject_contender`, even when already resolved; this is a static list, not a current authorization or feasibility verdict.

Resolution requires human authorization, a nonblank trimmed `Idempotency-Key`, and JSON `resolution` plus optional `reason`. Resolution names must match exact strings; whitespace and case are not normalized. `split` and `narrow` are recognized but return 409 `resolution_not_supported` because resource-specific semantics are absent. Unknown/empty input returns `invalid_resolution`. Lookup selects a contention whose resource appears among this workspace's claims, a different scope check from the list's challenged-claim membership check. Lookup failures return `contention_not_found`; a status other than exact `open` returns 409 `contention_already_resolved`.

| Resolution | Actual effect |
| --- | --- |
| `keep_owner` | Marks contention resolved; leaves the challenged claim unchanged. |
| `reject_contender` | Marks contention resolved; does not create or separately reject a requested claim. |
| `transfer` | For an active challenged claim, overwrites agent ID, task ID, and claim type with stored requested values. Keeps the same claim/resource ID, expiry, base hash, heartbeat, and other lease metadata. Does not validate the new participant, requested mode, or overlapping claims again. |
| `extend_lease` | For an active challenged claim, sets expiry to now plus 15 minutes, not old expiry plus 15 minutes. Does not check renewable flag, refresh heartbeat, or accept a caller TTL. Can shorten a longer lease. |

Claim updates, contention resolution, and `claim.contention_resolved` audit insertion share one SQL transaction. The update statements do not check affected-row counts: a claim that is no longer active can remain unchanged while the contention is reported resolved, and concurrent resolvers are not ruled out solely by the earlier open-state read. Transaction-begin failure returns HTTP 500; later SQL/commit failures return HTTP 409 `contention_resolution_failed`. The optional reason is text-redacted in audit fields, not stored as a new claim rationale.

After commit, a separate main event is attempted and its error ignored. HTTP 200 returns contention ID, `state: "resolved"`, and resolution, not a reloaded claim or proof of event delivery. No filesystem ownership enforcement, agent notification, automatic scheduling, new requested-resource lease, or patch application follows from this handler. Re-read claim state and inspect current conflicts before the contender continues governed work.

## Safe usage sequence

1. Read current table, assigned task, and claim status.
2. Claim every resource the proposed diff will touch, using stable resource IDs and the current base hash where available.
3. Do not edit or propose resources with an incompatible active claim.
4. Include affected resources in the proposal so patch validation can compare proposal coverage against the actual diff.
5. Release claims when work ends; rely on TTL/reconciliation as recovery, not normal cleanup.

The CLI syntax and defaults are in [CLI Reference](CLI.md). Agent tools are in [MCP Tools](MCP_TOOLS.md).
