# API error and retry semantics

Ordinary errors emitted through the problem helpers use RFC 7807
`application/problem+json`, with the effective request ID in the header and
`request_id` body field. This is not universal: blocked branch switches return
the preflight object, router-generated errors can use plain text, and WebSocket
failures after upgrade cannot become ordinary HTTP problem responses. Conflict
problems may include machine-readable `metadata` for the UI.

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

Read-only `GET` requests can be retried, but may observe newer state. Mutation
key requirements are handler-specific; OpenAPI declarations are not independent
enforcement. For example, settings preflight/update and notification acknowledgment
do not require a key in their handlers, while proposal validation and agent
diagnostics do. Inspect the endpoint's implementation-grounded reference before
assuming a request requires or benefits from replay caching.

## Shared JSON Decoding

Handlers that call `decodeJSON` use Go's JSON decoder over `io.LimitReader` with a 1 MiB (`1 << 20` bytes) read limit and `DisallowUnknownFields`. This rejects unknown fields when decoding typed structs, but does not reject arbitrary keys in map-based settings inputs. It is not full OpenAPI/JSON Schema validation: required fields, enums, ranges, identity associations, and permissions depend on each handler's subsequent checks. The decoder does not inspect `Content-Type` or demand an Accept header.

It decodes one value, then requires a second decode to return EOF. Additional JSON values or malformed trailing data within the limited view produce `request body must contain one JSON object`. That error wording does not enforce an object root independently: decoding JSON null into a struct can succeed with zero fields, and map targets accept null; handler validation determines what happens next. Struct field matching follows Go JSON rules, including case-insensitive matches and later duplicate-key updates, rather than strict schema property matching.

The limit is **not** `http.MaxBytesReader`, does not detect every oversized body, and does not drain/reject all remaining transport bytes. A valid first value followed by sufficient whitespace to exhaust the limited reader can appear complete even with additional bytes beyond it. A value truncated inside the limit normally fails JSON decoding. There is no common automatic HTTP 413 response: handlers select their own problem code/status, and some intentionally ignore decode errors and use defaults. Keyed requests can already have their full body buffered by idempotency middleware before this decoder runs. Do not treat 1 MiB as an end-to-end memory or request-size protection.

Handlers that never call the decoder ignore supplied JSON options. An empty body normally produces EOF at the initial decode, but some lifecycle actions accept it because they do not decode, and some validation/run actions ignore that error. These are endpoint-specific behaviors, not a universal empty-object substitution rule.

## Actor and Request Attribution

The generic `authorized` helper checks only that trimmed `X-Actor-ID` is nonempty. It does not look up an agent/human record, establish ownership, inspect role, or verify that the ID belongs to a bearer token. The separate `humanAuthorized` helper additionally accepts normalized roles `human`, `admin`, `chair`, `view`, `operate`, `approve`, `govern`, `administer`, and `force-override`. Thus even role label `view` satisfies that helper; it is not a granular privilege hierarchy. Common security middleware remains a separate boundary, and cached-response bypass limitations are described below.

Both helpers inspect original headers rather than the authenticated actor context. Accepting an agent token changes context identity to agent but leaves a supplied human-role header intact; that request can still pass `humanAuthorized`. An allowlisted header is therefore not proof of authenticated human kind. See [Authentication and Authorization](../README.md#authentication-and-authorization) for this known boundary defect. Do not use generated permission labels or successful bearer authentication alone as evidence of endpoint-level privilege isolation.

Request-ID middleware trims a supplied `X-Request-ID` and replaces it when empty, longer than 128 bytes, or containing CR/LF. Generated IDs are `req-` plus 16 random bytes encoded as hexadecimal, with Unix-nanosecond fallback on random-source failure. The effective ID is placed in response headers and request context; the original request header is not rewritten. A handler reading the raw incoming header can therefore persist attribution different from the effective context ID. Request IDs are correlation labels, not idempotency keys or verified actor identities.

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
