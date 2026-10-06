# Persistent Memory

Roundtable currently provides a SQLite-backed memory-entry store and MCP tools for querying, recording, summarizing, and marking entries stale. “Memory Oracle” is the product concept/role name; the current Go runtime does not launch a dedicated Memory Oracle agent, automatically extract memories, detect contradictions, or use embeddings.

## Entry fields

The record includes ID, scope, kind, title, Markdown body, optional source event ID, importance, status, created/updated timestamps, and additional schema fields for workspace, tags, provenance, source session/proposal, confidence/reliability, pinning, revision/archive/resynthesis. The MCP registry advertises scope, kind, title, body, source event ID, and importance. The handler also recognizes `memory_id` and `status`, although they are not advertised. The additional schema fields are not configurable through this local handler and are not part of its returned core memory projection. A missing/empty ID generates an `M-` timestamp ID.

| Record input | Behavior |
| --- | --- |
| `scope`, `kind`, `title`, `body_md` | Required nonempty strings. Scope and kind are free-form values; use consistent conventions. |
| `source_event_id` | Integer, default `0`; zero is stored as SQL NULL and read back as zero in the core projection. The handler does not fetch the source event to verify its meaning. |
| `importance` | Integer, default `50`; the store also converts an explicit zero to `50`. Other values are not clamped to a declared range. |
| `memory_id` | Handler-only optional ID. Reusing an ID replaces the core fields on that row, retaining creation time and updating its modification time. |
| `status` | Handler-only optional string, default `active`. Arbitrary nonempty values are stored; summary filtering specifically excludes `stale`. |

Updating through `memory.record` requires the complete desired core record. Omitted optional fields reset to their defaults; it is not a partial patch. Existing extra fields managed by other surfaces are not changed by this core upsert. The response contains `entry` with the saved core record.

## Query behavior

`memory.query` accepts a required `query` plus optional exact `scope`, exact `kind`, and `limit` (default 10). The current scorer lowercases title/body/scope/kind and counts occurrences of the entire query string; it does not tokenize into semantic concepts, use FTS/embeddings, or boost recency. Empty query assigns importance as the score. Results sort by score, then importance, then ID. The query path does not currently exclude stale entries automatically, so inspect each result's status.

Scope/kind matching is case-sensitive. Query text is lowercased but not trimmed; whitespace is part of the searched substring. Scores count non-overlapping occurrences across the four fields. Nonempty queries omit entries with score zero. Score and importance sort descending, then ID ascending. A positive limit truncates; zero or negative limits return all matches. The handler currently treats an omitted/non-string query as empty even though the registry advertises it as required. The response is `{"entries": [...]}`; the query path constructs an empty array when nothing matches.

```json
{"query":"pagination","scope":"project","kind":"decision","limit":10}
```

`memory.summarize` creates a Markdown list from non-stale entries ordered by importance and ID, optionally filtered by scope/task ID/proposal ID. These filters compare the supplied values to an entry's `scope`; they are not joins to task/proposal tables.

All supplied filters must match simultaneously. For example, different `task_id` and `proposal_id` strings cannot both equal the same entry scope and will produce no entries. The summary excludes only exact status `stale`, so an entry with another custom status remains eligible. There is no summary limit. It returns `entries`, `entry_count`, and `summary_md`, a deterministic Markdown list using kind, title, and body. It does not call a model, synthesize new knowledge, or persist the summary. The displayed heading prefers proposal ID, then task ID, then scope; omitted filters label it `project` while searching all scopes.

## Record and stale lifecycle

`memory.record` requires nonempty scope, kind, title, and body. A `source_event_id` can link it to an event; importance defaults to 50. `memory.mark_stale` requires ID and reason, changes status to stale, and appends the reason to the stored Markdown body. It does not delete the row or create a separate immutable revision as part of this MCP handler.

`memory.mark_stale` first loads the existing row and errors for an unknown ID. It appends `Stale reason: <reason>` after the body (or uses `Marked stale: <reason>` for an empty/whitespace body) and returns the updated `entry`. Repeated calls append repeated reasons; this operation is not deduplicated. Query results still include these records unless callers inspect status; summaries omit them. Recording the same ID again with status `active` can restore eligibility, but provide a deliberate updated body and provenance.

## Agent practice

Agents should query prior decisions/constraints before proposing work, record only reusable durable facts with clear scope and provenance, and mark contradicted entries stale with a reason. Memory is supporting context, not the authority for current task assignment or proposal status; read the current table and task records as well. Avoid storing credentials, private session IDs, or sensitive personal data.

See [MCP Tools](MCP_TOOLS.md) for exact inputs and [Database](DATABASE.md) for persistence/schema notes.
## HTTP Revision and Provenance Views

### HTTP Revision Creation

Revision creation requires a nonblank actor header through the generic `authorized` helper, not the human-only helper used by several governance endpoints. Common bearer policy still applies, but this handler does not independently reject agent kind or require an idempotency key. Input is `body_md`, optional `title`/`reason`, and positive integer `expected_revision`. Blank body returns `invalid_revision`; an expected revision below 1 returns `invalid_expected_revision`. `If-Match` is not used by this operation.

Inside one SQL transaction, the handler reads the memory's current revision/title using the workspace-or-null boundary, compares expected revision, inserts the next revision under ID `<memory-id>-r<number>`, then updates current body/title/revision/time. Mismatches and insert/update/commit errors return 409 `memory_revision_conflict`; transaction-begin failure returns 500 `memory_revision_failed`. The read/check is transactional but the final update does not independently contain an expected-revision predicate; database transaction/concurrency behavior remains relevant.

Body is text-redacted before both history and current-row persistence. Any nonempty title replaces the existing title without trimming or write-time text redaction; empty title means retain the old title. Supplied reason is ignored. Historical provenance is always literal `{"reason":"revision"}`, not the submitted reason or a full copy of original provenance. History does not snapshot title, tags, confidence, reliability, importance, source references, pinned/status flags, or all other current metadata. Archived or global memories are not independently blocked from revision here.

After commit, audit and main-event writes occur separately with ignored errors, followed by a reload whose error is also ignored. HTTP 200 therefore does not prove an audit/event exists or that the response projection was successfully reloaded. Revision creation does not trigger resynthesis, validate source evidence, update reliability scores, or produce a historical content diff.

### HTTP Search and Detail Selection

Memory list selects rows with exact workspace ID **or null workspace ID**, so legacy/global unbound memories can appear in every workspace. It always excludes status `archived`; requesting `status=archived` adds a contradictory filter rather than enabling archive browsing. Optional `scope`, `kind`, and `status` values are trimmed exact matches, without enum validation. `q` is trimmed SQL `LIKE` search over title/body, not tags, source references, provenance, or semantic embeddings. `%` and `_` remain wildcard characters. Search runs against stored content before response redaction.

Default limit is 50, accepted range 1 through 200. Rows are ordered oldest-first by creation-time text then ID. The cursor resumes strictly after that tuple, unlike newest-first event/audit pagination. It is not bound to workspace/filter selection or a stable snapshot. The SQL query fetches limit plus one, so page size bounds this query's returned rows; it does not normalize mixed timestamp formats. Query/scan failures use `memory_list_failed`; final iteration errors are not separately checked.

Detail uses exact memory ID and the same workspace-or-null boundary, but does not apply the list's archived exclusion. A known archived memory can therefore be read directly. Lookup failures map to `memory_not_found`, including storage errors. These selectors are unrelated to local MCP memory query scoring and do not establish reliability or source validity.

The HTTP revision, diff, graph, and source-chain handlers first resolve the workspace and verify the requested memory through the workspace memory reader. Lookup failures return `memory_not_found`. These views expose persisted metadata; they do not establish that memory content is correct or that referenced sources are accessible, authentic, or still present.

Revision listing returns all `memory_revisions` rows for the memory ID, ordered by numeric revision, in an `items` array. It is not paginated and has no configured row limit. Each record includes revision ID, memory ID, revision number, body Markdown, provenance object, creator, and creation time. Body receives limited text redaction; provenance receives recursive JSON redaction. Blank, malformed, or non-object provenance becomes an empty object because decoding errors are ignored. Creator is returned without separate text redaction. Query/scan failures use `memory_revision_list_failed`; final iteration errors are not separately checked.

**The current revision-diff endpoint does not compute a diff.** It echoes query strings `from` and `to` as revision labels (defaults `1` and `latest`), always returns `changed: true`, and returns the current memory body's text-redacted Markdown as `current_body_md`. It does not parse revision numbers, verify either revision exists, load historical bodies, compare content, produce patch hunks, or distinguish unchanged revisions. Even identical labels or arbitrary invalid labels can receive the same response. Treat this as a current-body placeholder, not evidence of historical changes.

The graph view returns direct `memory_aliases` rows where this ID is either alias or canonical ID, ordered by creation time. Fields are alias ID, canonical ID, text-redacted reason/creator, and creation time. It does not recursively traverse aliases, detect cycles, resolve a canonical chain, verify target memory visibility, or include provenance edges. Alias and canonical IDs are not independently text-redacted. Query/scan failures use `memory_graph_failed`.

The source-chain view returns direct `memory_provenance_edges` rows for this memory ID, ordered by creation time, with source type, text-redacted source ID, relation, and creation time. Despite its name, it does not recursively follow sources, load source content, validate relation types, or check whether source records belong to the workspace. A missing referenced source remains a label in the response rather than a lookup error. Query/scan failures use `memory_source_chain_failed`.

Graph and source-chain lists are unpaginated and do not separately check final row-iteration errors. Their ordering has no secondary tie-breaker for equal timestamps. These endpoints neither repair broken provenance nor alter memory revisions. Preserve and inspect the original authoritative sources before using provenance labels to justify a governance decision.
## HTTP Canonical Merge

The merge action takes a trimmed `target_id` query parameter identifying a different memory. It creates a **new** workspace-bound canonical memory; it does not overwrite the target or deduplicate content semantically. Source and target lookup both permit exact workspace ID or null workspace ID. No status restriction prevents archived or already-merged inputs. Null confidence/reliability cannot scan into this helper's nonnullable float fields and can be reported as a source/target lookup failure rather than merged as unknown values.

Canonical ID is timestamp-derived with prefix `M-merge-`; status is `active` and revision is 1. The new record takes scope, kind, title, tags, and importance from the target only. Body is target body, two newlines, then source body, with limited text redaction. Confidence and reliability are simple arithmetic averages, not evidence-weighted scores or validated probability updates. Source tags/title/importance are not combined. Original source-event/session/proposal references, pinned state, and full historical revisions are not copied by this insert.

Provenance records `merged_from` IDs and `source` containing the target's original provenance **JSON string**, not a parsed object or union of both provenance trees. No new provenance-edge rows or initial revision-history row are inserted. Existing source/target rows remain in storage with status `merged`; aliases point both old IDs to the new canonical ID. The regular list excludes only archived rows, so merged originals can still appear alongside the canonical record. Detail does not automatically follow aliases.

Canonical creation, two alias inserts, and status updates share a SQL transaction. Existing alias constraints can reject repeated merges; there is no cycle detection, recursive alias rewrite, or replacement of previously referenced canonical IDs. Begin failures use HTTP 500 `memory_merge_failed`; write/commit failures use HTTP 409 `memory_merge_conflict`. Lookup/scan failures use `memory_not_found` or `memory_target_not_found`.

After commit, separate audit and main-event writes are attempted with ignored errors. Canonical reload failure can return 404 even though the merge already committed. No resynthesis, external model invocation, source verification, automatic reference migration, or rollback command is triggered. Inspect aliases and actual source content before treating the canonical record as a verified synthesis.
## HTTP Memory Lifecycle Actions

The shared memory-action handler requires a nonblank actor header, not a human-only role, and does not independently require an idempotency key or expected revision. Common authentication still applies. Exact actions `pin`, `unpin`, `archive`, and `restore` read status/pinned within a transaction and update those fields plus update time. Pin/unpin leave status unchanged; archive sets `archived`; restore sets `active` regardless of prior status, including `merged`. No permitted-prior-state matrix prevents repeated actions. These changes do not increment content revision or create history rows.

Selection uses workspace ID or null workspace ID, so mutations can affect global legacy records visible in other workspaces. Archive does not delete content, revisions, aliases, or provenance edges. Restore does not remove alias relationships. Pin is a stored flag; the HTTP list does not use it to reorder results or guarantee retention. Transaction-begin errors use `memory_action_failed`; write/commit errors use `memory_update_conflict`; unknown actions return `invalid_memory_action` after the initial memory read.

`resynthesize` only sets `resynthesis_status: "requested"` and update time. It does not invoke a model, enqueue a verified background job, generate a body, change revision, or transition automatically to completed. Affected-row counts are not checked before audit/event attempts; a missing record can still produce an attempted request history and then return `memory_not_found` on reload. SQL errors use `memory_resynthesis_conflict`. The requested flag is not proof that any worker will process it.

The retained `merge-legacy` action is distinct from canonical `merge`: it validates a different target ID, marks the source `merged`, and uses SQLite `json_set` to add `merged_into` to its provenance. It does not combine bodies, create a canonical memory, insert aliases, copy metadata, or verify source existence before updating. Invalid stored provenance can cause `memory_merge_conflict`. This path's source row remains directly readable and can still appear in ordinary lists.

All these actions attempt separate audit and main-event writes after state changes, ignoring their errors. Reload can fail after commit, returning `memory_not_found` despite a persisted mutation. State, audit, event, and returned projection are not one atomic operation. No action here is an automatic reliability reassessment, content validation, or source cleanup mechanism.
## HTTP Memory Creation and Response Fields

Creation requires a nonblank actor header through generic authorization and a resolvable workspace, not an independent human-only check or idempotency-key requirement. JSON requires nonblank title and body Markdown, but retains surrounding whitespace. Scope/kind are accepted as supplied without the local MCP service's defaults or enum checks. Generated ID is `M-<UnixNano>`, status is `active`, and revision is 1.

Optional confidence/reliability default to 0.5 when omitted or null; supplied numbers are not restricted to zero through one. Importance is stored as supplied, including its zero default. Tags and provenance are serialized directly; they are not validated against a taxonomy or verified source model. Source event ID zero becomes SQL null, while empty session/proposal IDs become null. Nonempty source references are not looked up or checked for workspace ownership. Body receives limited text redaction before persistence, but title, tags, provenance, and source IDs are not equivalently redacted on this write path.

The insert is followed by independent audit/event attempts with ignored errors, then an ignored-error reload. HTTP 201 can therefore follow a successful insert without complete history or a populated response projection. Insert errors use 409 `memory_create_conflict`; malformed/missing content uses `invalid_memory`. Creation does not make an initial `memory_revisions` history row, provenance edges, embedding, reliability assessment, or synthesized summary.

List/detail responses decode tags and provenance directly, defaulting nil values to empty array/object. Decode errors are ignored and may leave partial decoded values; provenance is **not recursively redacted** in this ordinary memory projection, unlike revision-history provenance. Only title and body receive text redaction. Source identifiers and tags are not separately scrubbed. Pinned is derived from a nonzero stored integer; null confidence/reliability are retained as unknown rather than automatically recomputed. Treat all metadata as untrusted content and keep secrets out of memory records rather than relying on response filtering.
