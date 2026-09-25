# Persistent Memory

Roundtable currently provides a SQLite-backed memory-entry store and MCP tools for querying, recording, summarizing, and marking entries stale. “Memory Oracle” is the product concept/role name; the current Go runtime does not launch a dedicated Memory Oracle agent, automatically extract memories, detect contradictions, or use embeddings.

## Entry fields

The record includes ID, scope, kind, title, Markdown body, optional source event ID, importance, status, created/updated timestamps, and additional schema fields for workspace, tags, provenance, source session/proposal, confidence/reliability, pinning, revision/archive/resynthesis. The current MCP `memory.record` tool exposes scope, kind, title, body, source event ID, and importance; other fields are not configurable through that tool. A missing ID is generated and importance defaults to 50.

## Query behavior

`memory.query` accepts a required `query` plus optional exact `scope`, exact `kind`, and `limit` (default 10). The current scorer lowercases title/body/scope/kind and counts occurrences of the entire query string; it does not tokenize into semantic concepts, use FTS/embeddings, or boost recency. Empty query assigns importance as the score. Results sort by score, then importance, then ID. The query path does not currently exclude stale entries automatically, so inspect each result's status.

`memory.summarize` creates a Markdown list from non-stale entries ordered by importance and ID, optionally filtered by scope/task ID/proposal ID. These filters compare the supplied values to an entry's `scope`; they are not joins to task/proposal tables.

## Record and stale lifecycle

`memory.record` requires nonempty scope, kind, title, and body. A `source_event_id` can link it to an event; importance defaults to 50. `memory.mark_stale` requires ID and reason, changes status to stale, and appends the reason to the stored Markdown body. It does not delete the row or create a separate immutable revision as part of this MCP handler.

## Agent practice

Agents should query prior decisions/constraints before proposing work, record only reusable durable facts with clear scope and provenance, and mark contradicted entries stale with a reason. Memory is supporting context, not the authority for current task assignment or proposal status; read the current table and task records as well. Avoid storing credentials, private session IDs, or sensitive personal data.

See [MCP Tools](MCP_TOOLS.md) for exact inputs and [Database](DATABASE.md) for persistence/schema notes.
