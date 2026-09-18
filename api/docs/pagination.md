# Pagination and search conventions

Collection endpoints use `limit` (default 50, maximum 200) and an opaque
base64url `cursor`. A response includes `next_cursor: null` when there is no
next page. Clients must treat cursors as opaque values and must not construct,
decode, or persist assumptions about their internal representation.

History and high-volume resources use stable ordering with a deterministic
tie-breaker: the primary timestamp or sequence is followed by the immutable
row ID. Invalid cursors and limits return typed `400` problems. Text search is
bounded by the same page limit; callers should narrow by workspace and related
entity before requesting subsequent pages.

When a list exposes sorting or filtering, the accepted field names are
endpoint-defined allowlists. Unknown sort fields, unsupported directions, and
invalid enum filters are rejected rather than interpolated into SQL. Query
parameters use `query`, `sort`, `direction`, `from`, `to`, and related entity
IDs only where declared by the endpoint contract.
