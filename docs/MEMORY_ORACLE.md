# Memory Oracle

The Memory Oracle is a first-class agent responsible for persistent team memory.

## Responsibilities

- Maintain durable project memory.
- Summarize decisions.
- Extract reusable constraints, conventions, architecture facts, risks, failed attempts, and human preferences.
- Answer agent queries about prior rationale.
- Prevent repeated debates.
- Mark stale memories when contradicted by newer decisions.
- Speak up when a proposal appears to contradict durable memory.

## Memory categories

- decision
- constraint
- architecture
- convention
- risk
- open-question
- failed-attempt
- human-preference
- dependency
- security-note

## MCP tools

- `memory.query`
- `memory.record`
- `memory.summarize`
- `memory.mark_stale`

## Example memory

```md
# Auth tokens must never be logged

Kind: security-note
Scope: project
Source: D-0008
Importance: 95

The team decided that access tokens and refresh tokens must never be included in logs, test snapshots, table messages, or proposal summaries. Redacted placeholders are required.
```

## Oracle warning example

```text
Oracle: This proposal appears to violate D-0008. It adds token values to debug logs.
```

## Implementation notes

Start with structured memory in SQLite. Add embeddings later only if needed.

Memory query v1 can use:

- full text search
- keyword scoring
- kind/scope filters
- importance boost
- recency boost

Optional v2:

- sqlite-vec or external embedding index
- semantic clustering
- automatic contradiction detection
