# API-27: WebSocket Live Event Stream

## Objective

Implement **WebSocket Live Event Stream** as part of the authoritative Roundtable control-plane API.

## Non-negotiable invariants

- The transaction manager is the sole writer to the authoritative repository.
- Agent processes do not receive repository write APIs.
- Proposal patches are immutable artifacts until a governed transaction applies them.
- Claims, votes, policy evaluation, approvals, and transaction state transitions are authoritative server-side.
- Every state-changing human/admin action is audited.
- All repository paths are canonicalized and constrained to the workspace root.
- Secret values are never returned to the admin UI.

## Detailed requirements

- Implement `/api/v1/workspaces/{workspace_id}/events/ws` with authenticated connection, initial hello containing schema version/current sequence, heartbeat/ping, and monotonic workspace sequence numbers.
- Events use stable envelope: event_id, workspace_id, sequence, type, occurred_at, actor, entity_type, entity_id, payload.
- Support resume from last seen sequence when retained. If resume is impossible, return an explicit resync-required control message.
- Backpressure policy must be defined. Never block transaction/governance processing indefinitely on a slow browser client.
- Event publication should use an outbox/commit-after-DB pattern so UI does not observe impossible states.

## Transport and response requirements

- Base API prefix: `/api/v1`.
- Use `application/json`; errors use `application/problem+json`.
- Every request gets a `request_id`; return it in headers and problem bodies.
- Timestamps are UTC RFC3339Nano.
- High-volume lists use cursor pagination.
- Mutating operations validate current state inside the same database transaction used to change state.
- Return explicit typed conflicts instead of generic `500`.
- High-impact/retry-prone POSTs support `Idempotency-Key`.

## Persistence requirements

- Foreign keys enabled.
- Required indexes documented with the query they support.
- Immutable history/audit rows are append-only.
- State-machine transitions record actor, reason when applicable, request/correlation ID, and timestamp.
- Database writes and live-event publication use an outbox/after-commit pattern so clients cannot observe rolled-back state.

## Security requirements

- Authorize the human actor for every mutation.
- Reject path traversal and unsafe repository-relative paths.
- Redact secrets before storage/serialization where possible.
- Treat adapter output, logs, patch text, filenames, and agent text as untrusted data.
- Never shell-concatenate user-provided strings.

## Acceptance criteria

- OpenAPI contract updated with schemas, examples, and error responses.
- Unit tests cover validation and state transitions.
- Integration tests cover persistence and emitted domain events.
- Authorization tests cover allowed and forbidden roles.
- Concurrency/conflict tests exist for mutable revisioned resources.
- API behavior supports the corresponding admin UI without client-side guesswork.
