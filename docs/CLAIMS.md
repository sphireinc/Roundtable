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

## Safe usage sequence

1. Read current table, assigned task, and claim status.
2. Claim every resource the proposed diff will touch, using stable resource IDs and the current base hash where available.
3. Do not edit or propose resources with an incompatible active claim.
4. Include affected resources in the proposal so patch validation can compare proposal coverage against the actual diff.
5. Release claims when work ends; rely on TTL/reconciliation as recovery, not normal cleanup.

The CLI syntax and defaults are in [CLI Reference](CLI.md). Agent tools are in [MCP Tools](MCP_TOOLS.md).
