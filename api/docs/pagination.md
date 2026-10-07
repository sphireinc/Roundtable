# Pagination and search conventions

Many collection endpoints use `limit` (default 50, accepted range 1-200) and
`cursor`, but this is not universal. Cursor formats and ordering are
endpoint-specific. Copy the returned `next_cursor` unchanged into the next
request to the same endpoint with the same workspace and filters. A null
continuation means that response has no next page, not that new records cannot
arrive later.

Some histories use timestamp/ID or sequence ordering; other lists paginate
an in-memory filtered result by offset. Invalid cursors and limits return
typed `400` problems where those parameters are implemented. Page limits
bound returned items, not necessarily the number of rows/files read to
construct the list.

When a list exposes sorting or filtering, the accepted field names are
endpoint-defined allowlists. Unknown sort fields, unsupported directions, and
invalid enum filters are rejected where the handler validates them; not every
handler rejects every unknown query parameter or arbitrary filter value. Query
parameters use `query`, `sort`, `direction`, `from`, `to`, and related entity
IDs only where declared by the endpoint contract.

## Implemented cursor families

| Surface | Cursor and ordering behavior |
| --- | --- |
| Shared in-memory offset lists | Raw, unpadded base64url encoding of a nonnegative decimal offset into the current result. This is used by session listing and most shared `writePage` collections (including activity, proposals, claims, approvals, policies, transactions, deliberations, transcripts, votes, contention, patch, phase, and session subresource lists), plus repository-entity and agent cursor parsing. An offset beyond the current result clamps to its end and returns an empty page. |
| Agent list response (`GET /workspaces/{id}/agents`) | **Cursor output mismatch:** input is decoded as raw base64url offset, but `next_cursor` is emitted as plain decimal text. Passing a non-null returned cursor back verbatim generally fails with `invalid_cursor`; encode the decimal offset as unpadded base64url to continue manually, or treat the list as non-continuable until the implementation is corrected. See [Agent diagnostics and administration](../../docs/ADAPTERS.md#http-agent-administration). |
| Memory list | Raw base64url encoding of creation timestamp plus ID; ascending `created_at`, then ID. Continuation selects tuples strictly greater than the previous final item. |
| Notification list | Timestamp/ID cursor, descending `created_at`, then ID. Continuation selects tuples strictly less than the previous final item. |
| Operational logs/audit lists | Positive decimal event/audit ID string, not base64url; descending ID with continuation strictly below that ID. |
| Workspace list | Returns all workspace rows with `next_cursor: null`; the current handler does not parse `limit` or `cursor`. |

Treat these implementation representations as diagnostics, not a client encoding API. Cursors are not signed authorization tokens, encrypted state, or proof that an item exists. Timestamp/ID decoding checks shape/nonempty fields rather than verifying a corresponding row or a canonical timestamp. Offset parsing accepts a decoded integer, not a dataset snapshot identifier. Cursor contents do not bind to workspace, actor, filter, or sort configuration; switching those settings requires restarting pagination rather than reusing a previous continuation.

## Consistency between requests

Lists are reevaluated on each request without a cross-request read snapshot. Inserts, deletes, status changes, filtering changes, and reordered data can cause offset pagination to repeat or skip items. Timestamp/ID keyset pagination avoids offset shifts, but still does not freeze the dataset or guarantee every concurrent update is observed. Newer records on a descending history generally require a fresh first-page request; continuing toward older records is not a live event subscription.

Store entity IDs to deduplicate when building a client-side collection. Do not compare cursor strings lexically, assume one endpoint accepts another endpoint's cursor, or infer total count from one page. An empty page is not evidence that a workspace has no related records under another status/filter. Event replay cursors (`last_event_id` or MCP `after_event_id`) are separate protocols from these REST collection cursors.

## Search and filter differences

Parameter names are not interchangeable. The memory implementation searches using trimmed `q`, not a universal `query` field. It applies SQL `LIKE` to title/body with surrounding `%`; user `%` and `_` retain SQL wildcard meaning rather than literal-substring escaping. Scope/kind/status are exact SQL filters and are not enum-validated there. Memory listing always excludes exact `archived` status, so requesting `status=archived` does not override that exclusion. It includes rows in the selected workspace plus legacy rows with a null workspace association.

Notifications trim recipient/category/severity values. Recipient defaults to `X-Actor-ID` when omitted; when both are empty, no recipient predicate is added. `unread` and `actionable` enable their predicates only for exact string `true`; values such as `True` do not. Actionable selection additionally requires unresolved state. These are current filtering semantics, not a general boolean-query parser or recipient authorization guarantee.

Other endpoints have their own allowed sort/search/filter fields and error behavior. Consult the [Endpoint Guide](admin-api-guide.md), [Schema Reference](schema-reference.md), and handler behavior before introducing a reusable client abstraction. The local MCP memory query uses a different substring-scoring implementation; REST search results must not be assumed to use that ranking.
