# MCP Tool Reference

The local agent tool surface is declared in `internal/mcp/registry.go` and dispatched by `internal/mcp/runtime.go`. The registry currently contains **39 tools**. Use the generated `.roundtable/mcp/tools.schema.json` for machine-readable JSON schemas; regenerate generated MCP assets with `roundtable mcp inspect --write` after registry changes.

The runtime uses a local Unix-domain socket by default. Its registry/schema are MCP-inspired, but the socket is a Roundtable-specific newline-delimited JSON protocol, not a standards-complete MCP/JSON-RPC transport. It does not expose per-tool HTTP auth or a cryptographic agent identity on the socket; `agent_id` and actor fields are tool arguments, not proof of identity. Protect the socket and project account. Do not treat capability flags or a read-only tool name as a complete OS sandbox.

## Local socket protocol

The server accepts one request per Unix-domain connection. Send one JSON object terminated by a newline; the supported fields are `tool` (string) and `args` (object). For example:

```json
{"tool":"task.list","args":{}}
```

The server writes one newline-terminated JSON response and closes the connection. Success uses `{"ok":true,"result":{...}}`; a tool or decoding failure uses `{"ok":false,"error":"..."}`. `result` and `error` are omitted when not applicable. Missing/null `args` is normalized to an empty object; unknown JSON fields are ignored by Go's decoder. There is no JSON-RPC `id`, `method`, initialize handshake, `tools/list` exchange, protocol-version negotiation, or server-push notification framing. Agents discover names and input schemas from generated registry assets, then send Roundtable call envelopes.

Each accepted connection is handled in its own goroutine, but it carries only one call. The server reads through newline or EOF and has no explicit request-line byte limit or per-connection read deadline. Clients should send compact bounded JSON, include the newline, read one response line, and close. Do not build an untrusted network listener around this local implementation.

`roundtable mcp inspect` only prints sorted tool names. Adding `--write` writes three derived files: the Markdown agent manifest, a JSON object containing the `tools` array (`name`, `description`, `input_schema`), and `server.json` metadata (`name`, `transport`, `socket_path`, `manifest_path`, `tools_schema_path`). These are Roundtable-specific integration assets, not a drop-in MCP server configuration for every client.

## Socket startup and trust boundary

`roundtable run` starts this socket alongside the interactive coordinator; `roundtable mcp serve` starts only the tool server, without the TUI or coordinator tick loop. Thus a standalone server accepts state/tool calls but will not itself emit `agent.turn_scheduled`; turn requests advance only while an orchestrator `roundtable run` loop is ticking the same database. Startup opens the local runtime/database, creates the socket parent directory, removes the configured socket path recursively, then binds a Unix listener. Configure a dedicated socket pathname only: a stale socket is replaced, but the implementation does not first verify that an existing path is a socket. The socket has no application-level authentication, and every caller inherits the authority of the Roundtable OS process. Filesystem permissions and the account boundary are therefore the access controls.

The default is `.roundtable/mcp/roundtable.sock`. Keep it inside the project runtime directory, do not forward it to untrusted clients, and avoid paths that name valuable files or directories. Cancellation closes the listener and database; process termination also stops the service. Tool-call errors are returned in the JSON response over the socket, while `roundtable mcp call --socket` converts `ok:false` into a non-zero CLI error.

## Turn scheduling events

An agent that has something time-sensitive to report or do requests the next turn with `agent.turn_request` (`run_id`, `agent_id`, `reason_md`). The request is recorded as a durable event, deduplicated while outstanding, and queued FIFO by request arrival. The Chair schedules only the next request and records `agent.turn_scheduled` with the request event ID. MCP is request/response, not unsolicited push: poll `table.watch` with the run ID and `after_event_id: 0`, then pass each response's `next_after_event_id` on the next poll until the matching `agent.turn_scheduled` appears. This cursor mode returns bounded events in ascending order with `has_more_events`, so a caller can drain every page without missing an event; calls without a cursor retain the recent-activity snapshot behavior. After finding the matching schedule event, call `agent.turn_start`; after completing the work call `agent.turn_complete`, allowing the Chair to advance the queue. Each lifecycle call requires `run_id`, `agent_id`, and positive integer `request_event_id` in both schema and runtime. Duplicate/out-of-order starts or completions are rejected. Requests do not interrupt a currently running external process or create a process; they coordinate MCP turn ownership only.

## Tool inventory

“Required” and “optional” below refer to the registry JSON schema except where runtime validation imposes an additional requirement, noted explicitly. An omitted optional property is not necessarily populated with a meaningful default.

### Run state and turns

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `table.get_state` | none | Returns the current shared snapshot: runs, agents, tasks, claims, proposals, votes, decisions, blockers, approvals, transactions, and memories. |
| `table.watch` | none | `run_id`, `limit`; optional `after_event_id` enables cursor polling and returns `next_after_event_id`/`has_more_events`. Without a cursor, returns the recent event/transaction activity snapshot. It is not a blocking stream. |
| `agent.turn_request` | `run_id`, `agent_id`, `reason_md` | Records a durable FIFO request; one outstanding request per agent. |
| `agent.turn_start` | `run_id`, `agent_id`, `request_event_id` | Acknowledge only after the Chair emits a matching `agent.turn_scheduled` event. |
| `agent.turn_complete` | `run_id`, `agent_id`, `request_event_id` | Call after the scheduled work finishes; the next FIFO request is then eligible for scheduling on the next orchestration tick. |

### Tasks

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `task.create` | `title`, `body_md` | `priority`, `risk`, `assigned_agent_id`. Creates persisted work for the active run. |
| `task.get` | `task_id` | Fetch one task. |
| `task.list` | none | Lists persisted tasks. |
| `task.update_status` | `task_id` | `status`, `assigned_agent_id`. Updates the task's status and/or assignment. |

### Repository context and symbols

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `repo.read_file` | `path` | Reads through the repository tool boundary; it is not a write API. |
| `repo.search` | `query` | `path` narrows the search. |
| `repo.symbols` | none | Optional `path`, `language`; parser support/limitations are in [Symbol Index](SYMBOLS.md). |
| `repo.dependency_context` | `path` | Returns read-only dependency context for a manifest/path. |

### Resources and claims

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `resource.claim` | `resource_type`, `resource_id`, `claim_type`, `task_id` | `ttl_seconds`, `rationale`. Valid claim modes: read/write/review/exclusive. |
| `resource.claim_status` | none | Filter by `resource_id` and/or `agent_id`; inspect active/suspended/expired state. |
| `resource.get` | `resource_id` | Fetch resource metadata. |
| `resource.release` | `claim_id` | Release a previously granted claim. |
| `resource.search` | `query` | Optional `resource_type`. |

Conflict coverage and stale-claim rules are documented in [Claims](CLAIMS.md). Claim IDs/actor fields do not enforce operating-system access controls.

### Proposals, votes, and decisions

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `proposal.create` | `task_id`, `agent_id`, `title`, `summary_md`, `affected_resources`, `risk`, `patch` | `expected_tests`, `rollback_notes`. The patch is stored for validation; affected resources must cover touched files. |
| `proposal.attach_patch` | `proposal_id`, `patch` | Attaches/replaces the patch content. |
| `proposal.get` | `proposal_id` | Fetch one proposal. |
| `proposal.list` | none | Filter with `task_id` and/or `status`. |
| `proposal.request_review` | `proposal_id` | Requests relevant review records/agent work. |
| `vote.cast` | `proposal_id`, `agent_id`, `vote`, `reason_md` | `confidence`. Vote strings and policy requirements are validated/evaluated by runtime policy. |
| `vote.list` | `proposal_id` | List recorded votes. |
| `decision.record` | `decision`, `rationale_md`, `decided_by` | Optional `proposal_id`, `task_id`. Persists a decision record. |
| `patch.validate` | `proposal_id` | Reports claim, diff, resource coverage, temporary-apply, policy, human approval, and security-review results. |
| `patch.apply` | `proposal_id` | Revalidates, checks gates, applies through the service, and records a transaction/rollback artifact. Not an ordinary agent write shortcut. |
| `patch.reject` | `proposal_id`, `reason_md` | Rejects the proposal and records a decision. |

The exact apply flow and non-atomic failure boundaries are described in [Proposals and Transactions](TRANSACTIONS.md).

### Human approval and security

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `human.request_approval` | `subject`, `reason_md` | `approval_id`, `proposal_id`, `task_id`, `requested_by`, `status`, `decision_md`, `decided_by`, `override_policy`. Creates or updates an approval record; it does not cause a human to approve automatically. |
| `human.approval_status` | `approval_id` | Returns persisted status. |
| `security.review` | none | Optional `review_id`, `proposal_id`, `resource_id`, `task_id`, `reviewer_id`, `status`, `summary_md`; invokes/records a security review according to the service. |

### Tests

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `test.suggest` | none | Optional `proposal_id`, `task_id`; suggests test commands. |
| `test.run` | `command` | Optional `proposal_id`, `task_id`; executes `/bin/sh -lc` in the repository root and persists output to `.roundtable/testlogs/`. The current guard checks the executable name against `cargo`, `git`, `go`, `make`, `node`, `npm`, `pnpm`, `printf`, `pytest`, `ruby`, `swift`, `vitest`, and `yarn`, and rejects selected shell-control characters (`; | & > < backtick $ newline carriage-return`). It does not fully inspect executable arguments or sandbox filesystem/network effects; an allowlisted command can still be misused. Only invoke reviewed, known read-only/test commands; this tool is not a security sandbox. |
| `test.get_result` | `test_run_id` | Fetches a stored test run/result. A previous result is not proof the current patch has been tested. |

### Memory

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `memory.query` | `query` | `scope`, `kind`, `limit`. Search durable project knowledge. |
| `memory.record` | `scope`, `kind`, `title`, `body_md` | `source_event_id`, `importance`. Stores a durable memory entry. |
| `memory.summarize` | none | Scope by `task_id`, `proposal_id`, and/or `scope`. |
| `memory.mark_stale` | `memory_id`, `reason_md` | Marks contradicted/outdated memory as stale; does not delete its history. |

## Generated MCP files

`roundtable init`, `roundtable run`, and `roundtable mcp inspect --write` generate:

```text
.roundtable/mcp/AGENT_MCP_MANIFEST.md
.roundtable/mcp/tools.schema.json
.roundtable/mcp/server.json
```

The manifest includes mandatory read-only/proposal rules, startup/resume checklist, and turn scheduling protocol. Generated files are derived artifacts; edit the registry and regenerate them rather than editing the generated copies.

## Agent startup and safe use

Before acting, read `table.get_state`, query task-relevant memory, fetch the assigned task, and inspect relevant claim status. Read repository content through repo tools, claim affected resources, submit a patch proposal, and wait for review/governance results. Explicitly request/start/complete a turn when the coordinator is using the FIFO turn queue. Never treat free-form conversation or local CLI history as authoritative shared state.
