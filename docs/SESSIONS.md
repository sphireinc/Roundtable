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

The resume service assembles a Markdown briefing from the session's run/agent, current assigned task if any, active claims, pending proposals by the agent, recent decisions, and a required-next-action summary. The generated briefing is context for a resumed agent, not an automatic invocation of `codex resume`, `claude --resume`, or another command. Adapter resume patterns are capability/configuration metadata; process execution is not wired into the orchestrator.

Read [Agent Adapters](ADAPTERS.md) for capability fields and [Configuration](CONFIGURATION.md) for resume command templates. Treat external session IDs and resume commands as sensitive local operational data; do not publish them in logs or checked-in metadata.
