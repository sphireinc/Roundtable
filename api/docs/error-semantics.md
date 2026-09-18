# API error and retry semantics

Every error is RFC 7807 `application/problem+json` and includes the request ID
in both the `X-Request-ID` header and `request_id` body field. Conflict responses
may include machine-readable `metadata` for the UI.

Stable codes include `invalid_*`, `*_not_found`, `authentication_required`,
`forbidden`, `workspace_revision_conflict`, `invalid_*_transition`,
`claim_conflict`, `stale_base`, `policy_failure`, `approval_required`,
`transaction_busy`, `repository_degraded`, `adapter_unavailable`,
`resync_required`, and `idempotency_key_reuse`.

Clients may retry a body-bearing create/update request with the same
`Idempotency-Key` for a successful or conflict response from a mutation. The
API replays a successful response for the same method, path, actor, key, and
payload. Empty-body lifecycle transitions are re-evaluated so a second
attempt receives the current typed state conflict. Reusing a key with a
different payload is a typed `409 idempotency_key_reuse` conflict. A new key
must not be used to blindly repeat repository or transaction mutations until
the client has inspected the returned conflict metadata.

Read-only `GET` requests are safe to retry. High-impact `POST`, `PATCH`, and
`DELETE` mutations require `Idempotency-Key` where declared by OpenAPI;
preflight, validation, and diagnostics requests are also keyed because they
can create persisted records or events.
