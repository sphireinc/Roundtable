# Agent Sessions and Resume

Agent sessions are durable records that associate a Roundtable agent and run with adapter/provider/model metadata and optional external CLI session identifiers. Persistence and briefing generation are implemented in `internal/sessions`; external CLI process launch and automatic session capture remain separate adapter work.

## Session record lifecycle

- Register creates or updates a session with agent ID, run ID, adapter, provider, model, external session ID/command, working directory, MCP socket, status, and optional JSON metadata. Missing status defaults to `active`.
- Heartbeat updates `last_seen_at`, and optionally status and external session ID, then appends a session event.
- End records the final status (default `ended`), `ended_at`, and last-seen timestamp, then appends an end event.
- Listing and lookup read persisted session records. Session events are an audit/history stream and do not launch or control the external CLI.

## CLI

```sh
roundtable sessions list --root .
roundtable sessions register --root . --id SESSION --agent AGENT --run RUN --adapter codex --cwd .
roundtable sessions heartbeat --root . --session SESSION
roundtable sessions end --root . --session SESSION
roundtable resume --root . --session SESSION
```

The register command supports `--provider`, `--model`, `--external-session-id`, `--resume-command`, `--cwd`, `--mcp-socket`, and `--status`. Heartbeat supports `--status` and `--external-session-id`; end supports `--status`. The session ID and agent/run identifiers must match persisted records and are not inferred from an installed CLI.

## Resume briefing contents

### Persistence and update semantics

Session registration is an upsert keyed by the caller-supplied session ID, not a merge of only provided fields. Registering an existing ID replaces agent/run associations, adapter/provider/model, external session ID and resume command, working directory, socket, status, and metadata. Omitted optional values clear their previous values. An empty metadata map is stored as no metadata; nonempty metadata must be JSON-serializable or registration fails before persistence. Registration refreshes `last_seen_at` and clears `ended_at`, but preserves the original database-generated `started_at`. Use a new ID when you need a distinct historical session.

Heartbeat reads the existing row and changes status and external session ID only when their supplied strings are nonempty. Empty strings cannot clear these fields through heartbeat. It always refreshes `last_seen_at`; it does not clear `ended_at`, even when changing status back to `active`. Ending a session sets status to the supplied nonempty value or `ended`, sets both `ended_at` and `last_seen_at` to the current UTC time, and preserves the remaining fields. Repeated end calls replace the end timestamp. These operations store status strings rather than enforcing a lifecycle enum or launching/stopping a provider process.

Registration, heartbeat, and end each attempt to append an `agent_session_events` row after saving and reloading the session. Their event types are `registered`, `heartbeat`, and `ended`. Event payloads contain status plus adapter, last-seen timestamp, or end timestamp respectively. Event-append errors are ignored by this service: a successful operation proves the session row was saved, not that its history event exists. Session events are separate from the main table activity stream; they are not automatically `table.watch` events.

Session listing returns all local session rows in ascending `started_at`, then ID order, without a run filter. Service-level lookup by agent prefers exact status `active`; within the same status it prefers descending `last_seen_at`, then `started_at`. Ordering between different non-active statuses is not defined by that comparator. Prefer an explicit session ID when several historical sessions exist.

### Briefing selection rules

The briefing is assembled from current database rows rather than a frozen snapshot captured at session registration. Its constituent reads are separate operations, so concurrent changes can produce a mixed-time view. The session's run ID is displayed but does not constrain the task, claim, proposal, or decision queries:

- Assigned tasks are matched by agent ID. Exact status `in_progress` is preferred. Within the same status, lower numeric priority comes first, followed by older creation time. Ordering between distinct non-`in_progress` statuses is not defined, and completed tasks are not excluded.
- When there is no assigned task, active claims are considered in resource-ID order; the first claim whose task can be read supplies the task. A failed task lookup is skipped.
- Claims must match the agent ID and exact status `active`. They are sorted by resource ID. This selection does not separately check claim expiry timestamps.
- Proposals must match the agent ID and have a status other than exact `accepted` or `rejected`. All other statuses qualify, including unfamiliar status strings. They are sorted newest-created first.
- Decisions are database-wide, not filtered by run, agent, task, or proposal. The newest three are included, ordered by creation time descending and then ID descending.

The required-next-action summary prioritizes the newest pending proposal, then the selected task, then the first active claim, and otherwise instructs the agent to query the table and memory and wait for assignment or claim work. This is a fixed heuristic, not a model-generated recommendation or an authorization to apply a patch.

The rendered Markdown includes agent ID, run ID, an external session ID when present, task ID/title, claim resource IDs, proposal IDs/statuses, decision IDs/rationales, and the next-action text. It does not include full task bodies, review findings, votes, patch contents, test logs, or memory query results. Query those records separately before continuing governed work.

The resume service assembles a Markdown briefing from the session's run/agent, current assigned task if any, active claims, pending proposals by the agent, recent decisions, and a required-next-action summary. The generated briefing is context for a resumed agent, not an automatic invocation of `codex resume`, `claude --resume`, or another command. Adapter resume patterns are capability/configuration metadata; process execution is not wired into the orchestrator.

Read [Agent Adapters](ADAPTERS.md) for capability fields and [Configuration](CONFIGURATION.md) for resume command templates. Treat external session IDs and resume commands as sensitive local operational data; do not publish them in logs or checked-in metadata.
