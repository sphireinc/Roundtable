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

## HTTP session observability

The API exposes six GET subresources under `/api/v1/workspaces/{id}/sessions/{session_id}/`. Each first resolves the workspace and session through the HTTP workspace membership checks. Session lookup failure returns `session_not_found`; that boundary does not mean every subsequently selected related record is session- or workspace-scoped.

| Suffix | Source and selection |
| --- | --- |
| `logs` | Dedicated agent-session events for this session ID. Each item has `id`, `event_type`, optional `payload`, and `created_at`. Payload is a redacted JSON **string**, not an embedded JSON object. These are recorded session events, not captured process stdout/stderr. |
| `tool-calls` | General events for the session's run, including other agents/sessions in that run. Selects event types starting with exact `tool.` or raw payloads containing case-insensitive text `"tool"`. This is a heuristic projection, not a complete session-specific MCP invocation trace. |
| `claims` | All local claims whose agent ID matches the session's agent, regardless of claim status, expiry, run, workspace, or originating session. Only rationale receives text redaction here. |
| `proposals` | All local proposals whose agent ID matches, regardless of status, run, workspace, or session. Returns ID, task ID, redacted title/summary, status, risk, and creation time; omits patch path/content. |
| `metrics` | Numeric values supplied in this session's metadata `usage` object; not measured provider usage or a live process probe. |
| `environment` | API process Go version, OS, architecture, and stored session adapter. Not the agent subprocess environment or executable/toolchain fingerprint. |

The first four endpoints load their candidate records before applying shared in-memory offset pagination. Default limit is 50; accepted limits are 1 through 200. Cursor format follows the offset family described in [API Pagination](../api/docs/pagination.md), not event-ID replay or audit-ID cursors. Pagination does not bound the initial database load or provide stable snapshot isolation. Source query errors use `session_logs_failed`, `tool_calls_failed`, `claims_failed`, or `proposals_failed` respectively.

### Tool-call projection

Tool-call items contain ID, tool, optional action/status, and creation time. The handler decodes payload strings `tool`, `action`, and `status`; malformed JSON is tolerated. Missing/empty tool falls back to the event type with a leading `tool.` removed. A non-tool event selected only by payload text can therefore report its entire event type as the fallback tool. No arguments, result payload, duration, caller session, authorization verdict, or explicit success/failure validation is supplied. Absence from this projection does not prove a tool was never called.

### Metrics and environment limits

Metrics return `{session_id, usage, source: "adapter_metadata"}`. Missing/malformed metadata or a non-object usage value produces an empty usage object. Only numeric values are accepted; strings, booleans, arrays, nested objects, and null are omitted. Keys containing case-insensitive `secret`, `password`, `api_key`, or `authorization` are rejected. Unlike general JSON payload redaction, keys containing `token` are allowed here, so numeric token counts can be returned. There is no schema, unit conversion, cost calculation, freshness timestamp, aggregation, or provider verification. Caller-supplied numeric metadata remains caller-supplied attribution.

Environment returns `{session_id, fingerprint}` with `go_version`, `os`, `arch`, and `adapter`. It does not enumerate environment variables, working-directory contents, model identity, host name, provider credentials, installed CLI versions, sandbox settings, or session command. An API container's fingerprint can differ from the host or agent environment.

### Shared redaction semantics

The observability JSON redactor recursively replaces values whose object keys contain `token`, `password`, `secret`, `api_key`, `authorization`, or `chain_of_thought`, case-insensitively. This can hide benign fields such as token counts. Other string values pass through text redaction, which looks for the first occurrence of each of `token=`, `password=`, `secret=`, `api_key=`, and `authorization=` and replaces text until a space, ampersand, comma, newline, or end of string. Repeated occurrences of the same marker, alternate separators, credentials without these markers, and arbitrary sensitive prose are not comprehensively detected. Invalid JSON falls back to this limited text treatment. Do not use successful redaction as permission to publish untrusted session content or internal reasoning.
## HTTP Lifecycle State Controls

Creation requires human authorization, a nonblank trimmed idempotency key, and JSON fields `agent_id`, `run_id`, and `adapter`, all trimmed before the required-value check. Optional provider, model, and external session ID are retained without normalization. The agent must exist, but need not be enabled; adapter is not checked against that agent's adapter or the configured registry. Run existence, run/workspace association, provider availability, session concurrency, and external-session validity are not verified by this handler.

The API assigns a timestamp-derived session ID, status `starting`, current last-seen/start timestamps, and workspace root as working directory. Metadata records workspace ID and the workspace's **default branch**, not an inspected current branch. This can later cause a resume branch conflict even if the actual branch never changed since creation. External resume command is synthesized as `<adapter> resume {{external_session_id}}`, not copied from adapter configuration. No subprocess is launched, MCP socket assigned, initial briefing generated, or transition to active scheduled automatically.

Session upsert, post-write reload, and history writes are separate. Reload errors are ignored, so HTTP 201 can contain an empty projection after successful persistence. Creation errors use `invalid_session`, `agent_not_found`, or `session_create_conflict`. List loads all local sessions, filters by metadata workspace membership, and applies offset pagination; it does not offer status/agent filters or expire stale sessions. Detail uses the same metadata boundary and reports `session_not_found` for lookup or membership failure. Response includes identifiers, adapter/provider/model, optional external ID, synthesized resume template, status, and timestamps, but omits stored working directory, MCP socket, process ID, and metadata.

HTTP session membership uses exact `workspace_id` in the session's decoded metadata, not its run's workspace column. Invalid metadata or a different value makes the session unavailable to these handlers. This differs from the event snapshot's session marker selection by run membership.

Heartbeat, pause, and stop operate on stored state. Active states are exact `starting`, `active`, `running`, `paused`, and `resuming`. Heartbeat accepts any of those as optional `status` query value and refreshes last-seen time; it rejects terminal source states. No human-only check is applied to heartbeat beyond common authentication and workspace/session lookup; caller-to-session ownership is not checked here. Pause/stop require human authorization. Repeated pause on an already-paused row is rejected, while stop sets status `stopped` and an end timestamp. These shared transition handlers do not independently require an idempotency key.

Resume requires human authorization, a nonblank idempotency key, and exact stored source state `paused` or `stopped`. It checks a saved branch when present, but ignores repository-inspection failure rather than proving branch compatibility. It also requires an existing enabled agent. Success sets `resuming` and refreshes last-seen time, without invoking an adapter resume command or clearing prior end metadata. The returned resume-command template is mechanically `<adapter> resume <external_session_id>`, not the configured adapter-specific pattern or a safely executable shell command.

Termination requires human authorization and a nonblank idempotency key. It counts all active claims for the session's agent globally, without workspace/session/expiry filtering. When any exist, a nonblank `impact_details` body string is required; this is acknowledgment text, not an automated impact analysis or claim-release instruction. Body decoding errors are ignored. Only active source states may terminate. Success writes `terminated`, end time, and matching last-seen time; it does not kill a process, release/suspend claims, cancel tasks, or resolve proposals. Impact details are not persisted by this handler.

Lifecycle writes use session upsert after a separate state read, without a compare-and-swap expected-state condition. Concurrent transitions can overwrite one another. Dedicated session-event and general-event writes follow separately with errors ignored, so a successful response does not prove both histories were recorded. No lifecycle action here provides operating-system process control or guarantees the scheduler observes the intended state. Re-read authoritative records before resuming governed work.
