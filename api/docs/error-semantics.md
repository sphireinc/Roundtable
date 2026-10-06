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

## Exact idempotency scope

The middleware applies only to POST, PATCH, PUT, and DELETE carrying a nonempty `Idempotency-Key`. Keys are used verbatim, without whitespace normalization, syntax/length validation, or automatic generation. A missing key passes through to handler-specific checks; middleware participation alone does not make a key universally required.

The cache key consists of method, complete request URI (including query string), raw `X-Actor-ID`, and idempotency key. The fingerprint hashes method, request URI, and exact body bytes. Equivalent JSON with different spacing/key order is a different payload. Different query ordering or actor-header text selects a different cache entry. Authorization token, actor role, workspace header, `If-Match`, content type, and other headers are not part of the fingerprint. Changing those headers is not detected as a different payload when the cache key remains the same.

Only requests with a nonempty body and a response status from 200 through 499 are retained. This includes client errors/conflicts, not just successful writes. A cached 4xx can continue replaying after the underlying condition is corrected. Status 500 and above is not cached. Empty-body requests are never cached, even with a key; a body of `{}` or whitespace is nonempty for this decision and is not equivalent to sending no body.

The store is an in-memory map owned by one server instance. There is no TTL, eviction policy, configured capacity, persistence, or cross-process sharing. A restart loses every entry; multiple replicas do not share replay protection. Request/response bodies and captured headers remain in memory for stored entries. The middleware reads the full request body before handler decoding, without its own byte limit, so the shared decoder's limited reader does not cap this buffering step.

## Concurrent calls and security boundary

The mutex protects cache lookup/insertion, not the entire handler execution. Two simultaneous first calls with the same key can both find no entry and both execute. There is no in-progress reservation, per-key wait, transactional deduplication record, or exactly-once guarantee. A crash after mutation but before recording the response likewise leaves an ambiguous result.

In the current middleware composition, idempotency lookup wraps the security-policy middleware. A matching cached response is replayed without re-running its bearer/role/Origin checks or handler authorization. Because credentials and roles are not in the fingerprint, cache replay must not be treated as freshly authenticated execution. This is an implementation limitation, not a recommended authorization design. Do not rely on an idempotency key as a secret or expose cached privileged results through an untrusted boundary.

On replay, stored response headers/status/body are emitted, except the captured `X-Request-ID` header is not reused. The outer request-ID middleware can provide a new header while the unchanged cached body retains the original response's request ID. Correlate both when debugging; replay is not a newly evaluated response. No explicit replay-indicator header is added.

## Client recovery rules

Retain the same key and exact request bytes when intentionally retrying a body-bearing request within the same server lifetime. Before assuming replay protection, account for server restart, replica routing, concurrent duplicates, empty bodies, and uncached server errors. If response delivery is lost, inspect authoritative entity/repository state before deciding whether to retry. Never infer rollback from a timeout, disconnected client, 5xx, or missing cache entry.

Read-only retries and mutation retries are different evidence: a repeated GET can observe newer state, while a cached mutation response can describe historical state. Preflight results also need current-state revalidation at execution time. `Idempotency-Key` does not replace `If-Match`, policy gates, resource claims, or transaction-state checks.
