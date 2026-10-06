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
