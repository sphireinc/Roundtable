# MCP Tool Reference

The local agent tool surface is declared in `internal/mcp/registry.go` and dispatched by `internal/mcp/runtime.go`. The registry currently contains **39 advertised tools**; **38 have runtime dispatch handlers**. `repo.dependency_context` is declared but not implemented. Registry membership is not proof of executable support. Use the generated `.roundtable/mcp/tools.schema.json` for machine-readable JSON schemas; regenerate generated MCP assets with `roundtable mcp inspect --write` after registry changes.

The runtime uses a local Unix-domain socket by default. Its registry/schema are MCP-inspired, but the socket is a Roundtable-specific newline-delimited JSON protocol, not a standards-complete MCP/JSON-RPC transport. It does not expose per-tool HTTP auth or a cryptographic agent identity on the socket; `agent_id` and actor fields are tool arguments, not proof of identity. Protect the socket and project account. Do not treat capability flags or a read-only tool name as a complete OS sandbox.

## Local socket protocol

The server accepts one request per Unix-domain connection. Send one JSON object terminated by a newline; the supported fields are `tool` (string) and `args` (object). For example:

```json
{"tool":"task.list","args":{}}
```

The server writes one newline-terminated JSON response and closes the connection. Success uses `{"ok":true,"result":{...}}`; a tool or decoding failure uses `{"ok":false,"error":"..."}`. `result` and `error` are omitted when not applicable. Missing/null `args` is normalized to an empty object; unknown JSON fields are ignored by Go's decoder. There is no JSON-RPC `id`, `method`, initialize handshake, `tools/list` exchange, protocol-version negotiation, or server-push notification framing. Agents discover names and input schemas from generated registry assets, then send Roundtable call envelopes.

Each accepted connection is handled in its own goroutine, but it carries only one call. The server reads through newline or EOF and has no explicit request-line byte limit or per-connection read deadline. Clients should send compact bounded JSON, include the newline, read one response line, and close. Do not build an untrusted network listener around this local implementation.

### Framing, Shutdown, and Embedded Configuration

EOF after nonempty bytes is accepted as a complete request, so a client can finish its write side without a newline. A client that sends neither newline nor EOF can leave a connection goroutine blocked indefinitely. Leading/trailing ASCII space, tab, CR, and LF are removed from the first line; an empty line closes without a response. Only that first line is processed: multiple newline-separated calls on one connection do not create a session or batch. Non-EOF read failures close without a problem envelope. This transport has no cancellation request, progress stream, replay cursor, request ID, or per-call retry key.

Response encoding and socket write errors are ignored by the server. A client receiving no complete response must not infer that the tool did not execute or that persisted/file effects were rolled back. Inspect authoritative state before repeating a mutation. There is no response-byte cap, write deadline, connection-count limit, or bounded worker pool; tool output and open clients can consume process resources. Handler execution receives the server startup context, not a distinct client-disconnection cancellation context.

`NewServer(root, socketPath, registry)` retains the supplied root/socket and defaults a nil registry to `DefaultRegistry()`. It does not canonicalize those paths or apply an allowed-root policy at construction. `Registry()` exposes the metadata registry, but calls dispatch through the runtime opened from root; supplying a custom registry does not add executable handlers or enforce its schemas. Socket directory creation requests mode `0755`, subject to umask, without tightening an existing directory or explicitly setting a restrictive socket mode. Review actual directory/socket ownership and permissions; there is no peer-credential check here.

`Start` opens the runtime before directory cleanup/listening and returns after launching goroutines. Runtime-open, parent-directory, path-removal, or listener errors abort startup; later non-closed accept errors are retried without a backoff or reported health transition. Do not repeatedly start another server at the same socket path: startup removal is not coordinated with an existing listener and can make that listener unreachable by pathname while existing connections remain.

Cancellation closes the listener and invokes runtime cleanup, but does not explicitly close accepted connections, wait for their completion, or interrupt a connection blocked in `ReadBytes`. The internal wait group tracks goroutines but no public wait/drain operation is provided. `Close()` alone closes the listener; it does not cancel the startup context or independently run runtime cleanup. Treat graceful shutdown and completed mutation evidence separately rather than assuming listener closure drains all tool calls.

`roundtable mcp inspect` only prints sorted tool names. Adding `--write` writes three derived files: the Markdown agent manifest, a JSON object containing the `tools` array (`name`, `description`, `input_schema`), and `server.json` metadata (`name`, `transport`, `socket_path`, `manifest_path`, `tools_schema_path`). These are Roundtable-specific integration assets, not a drop-in MCP server configuration for every client.

## Socket startup and trust boundary

`roundtable run` starts this socket alongside the interactive coordinator; `roundtable mcp serve` starts only the tool server, without the TUI or coordinator tick loop. Thus a standalone server accepts state/tool calls but will not itself emit `agent.turn_scheduled`; turn requests advance only while an orchestrator `roundtable run` loop is ticking the same database. Startup opens the local runtime/database, creates the socket parent directory, removes the configured socket path recursively, then binds a Unix listener. Configure a dedicated socket pathname only: a stale socket is replaced, but the implementation does not first verify that an existing path is a socket. The socket has no application-level authentication, and every caller inherits the authority of the Roundtable OS process. Filesystem permissions and the account boundary are therefore the access controls.

The default is `.roundtable/mcp/roundtable.sock`. Keep it inside the project runtime directory, do not forward it to untrusted clients, and avoid paths that name valuable files or directories. Cancellation closes the listener and database; process termination also stops the service. Tool-call errors are returned in the JSON response over the socket, while `roundtable mcp call --socket` converts `ok:false` into a non-zero CLI error.

## Turn scheduling events

An agent that has something time-sensitive to report or do requests the next turn with `agent.turn_request` (`run_id`, `agent_id`, `reason_md`). The request is recorded as a durable event, deduplicated while outstanding, and queued FIFO by request arrival. The Chair schedules only the next request and records `agent.turn_scheduled` with the request event ID.

Poll `table.watch` with the run ID and `after_event_id: 0`, then pass each response's `next_after_event_id` on the next poll until the matching `agent.turn_scheduled` appears. This cursor mode returns bounded events in ascending order with `has_more_events`, so a caller can drain every page; calls without a cursor retain the recent-activity snapshot behavior. After finding the matching schedule event, call `agent.turn_start`; after completing the work call `agent.turn_complete`, allowing the Chair to advance the queue.

Each lifecycle call requires `run_id`, `agent_id`, and `request_event_id`. The schema declares the ID as an integer with minimum 1. The handler converts numeric arguments through its integer helper before checking positivity and ownership; it does not independently reject fractional JSON numbers before conversion. Supply the exact positive integer returned by the request. Duplicate/out-of-order starts or completions are rejected. Requests coordinate MCP turn ownership and do not launch or interrupt external agent processes.

## Tool inventory

“Required” and “optional” below refer to the registry JSON schema except where runtime validation imposes an additional requirement, noted explicitly. An omitted optional property is not necessarily populated with a meaningful default.

### Run state and turns

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `table.get_state` | none | Returns `runs`, `agents`, `tasks`, `claims`, `proposals`, `transactions`, `human_approvals`, `security_reviews`, and `memory` from local state. It does not return votes or decisions in this response; use their dedicated tools/related proposal reads. |
| `table.watch` | none | `run_id`, `limit`; optional `after_event_id` enables cursor polling and returns `next_after_event_id`/`has_more_events`. Without a cursor, returns the recent event/transaction activity snapshot. It is not a blocking stream. |
| `agent.turn_request` | `run_id`, `agent_id`, `reason_md` | Records a durable FIFO request; one outstanding request per agent. |
| `agent.turn_start` | `run_id`, `agent_id`, `request_event_id` | Acknowledge only after the Chair emits a matching `agent.turn_scheduled` event. |
| `agent.turn_complete` | `run_id`, `agent_id`, `request_event_id` | Call after the scheduled work finishes; the next FIFO request is then eligible for scheduling on the next orchestration tick. |

### Tasks

The registry schemas describe advertised fields; the local socket server dispatches parsed arguments directly to handlers without applying JSON Schema validation. Runtime checks, types, and defaults therefore determine the accepted behavior. Several handlers recognize extra ID/status fields not advertised in the registry; their detailed reference pages identify those implementation fields separately. Do not assume schema `required` alone establishes a server-side check or that unknown keys are rejected.

Most string helpers accept only actual JSON strings and default only for missing/empty values, not whitespace-only values. Numeric integer helpers accept JSON numbers and truncate floating-point values; string numbers fall back to defaults. Boolean helpers accept actual booleans, with other types falling back. Turn lifecycle handlers check that the converted request ID is positive; the schema requires an integer, so callers should supply an integer rather than relying on numeric truncation. See each tool's behavior before constructing calls.

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `task.create` | `title`, `body_md` | `priority`, `risk`, `assigned_agent_id`. Stores shared local task state; see [Tasks and Assignments](TASKS.md) for defaults and handler-only fields. |
| `task.get` | `task_id` | Fetch one task. |
| `task.list` | none | Lists all local tasks ordered by priority, creation time, and ID; no run/workspace filter. |
| `task.update_status` | `task_id` | Nonempty `status`/`assigned_agent_id` update those fields; empty values preserve them. See [Tasks and Assignments](TASKS.md). |

### Repository context and symbols

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `repo.read_file` | `path` | Reads through the repository tool boundary; it is not a write API. |
| `repo.search` | `query` | `path` narrows the search. |
| `repo.symbols` | none | Optional `path`, `language`; parser support/limitations are in [Symbol Index](SYMBOLS.md). |
| `repo.dependency_context` | `path` | Advertised only; no runtime handler. Calls fail with `tool not implemented: repo.dependency_context`. |

#### Dependency context availability

There is currently no local dependency-context parser, manifest discovery, lockfile resolution, transitive dependency graph, package-version lookup, or vulnerability scan behind `repo.dependency_context`. Supplying a valid manifest path does not change the result: the runtime reaches its unimplemented-tool fallback before any path inspection. Over the socket, this becomes `{"ok":false,"error":"tool not implemented: repo.dependency_context"}`; `roundtable mcp call` reports the tool error rather than returning dependency data. The name remains present in inspection output and generated schemas.

For current work, read known manifests and lockfiles using `repo.read_file` and inspect relevant imports with `repo.search` or `repo.symbols`. Those tools expose file content or declarations, not a resolved dependency graph. Clearly distinguish direct manifest declarations from lockfile-resolved versions and transitive dependencies in any agent summary. Do not infer installed packages, successful dependency resolution, compatibility, or security status merely from a manifest. External package-manager commands are separate operations with their own trust and execution boundaries; see [Tests and Evidence](TEST_EXECUTION.md).

#### File reads and explicit-path boundary

`repo.read_file` requires a nonempty string `path`. Relative paths resolve against the runtime repository root, not the calling agent's working directory. Absolute paths are accepted when the resolved target remains inside that root. The guard resolves the root's symlinks, attempts to resolve the candidate's symlinks, and rejects a resolved relative path equal to `..` or beginning with `../` (using platform separators). If candidate symlink evaluation fails, it checks the unresolved absolute candidate instead; the subsequent file read may then fail. This is a path check, not an operating-system sandbox or protection against concurrent symlink replacement.

The response is `{path, content}`. Its `path` is the caller's path with slash conversion, not necessarily the canonical path that was read. The entire file is read into memory and returned as a string; there is no line range, byte limit, pagination, binary-file filter, encoding detector, secret redaction, or ignore-file check. Hidden files inside the repository, including local configuration, are not excluded by this handler. Read failures return tool errors rather than an empty content value.

#### Content search

`repo.search` requires a nonempty string `query`. It lowercases both the query and each line and performs literal substring matching; it does not trim the query, interpret regular expressions/globs, rank results, or search across line boundaries. Whitespace-only queries are literal queries. Each matching line produces one result even if the substring occurs multiple times on that line.

With an explicit `path`, the same path guard used by file reads is applied and only that single path is read. A directory path is not recursively expanded: its read fails and is silently skipped, yielding no matches. Without `path`, the handler walks the repository tree and skips directories named `.git` or `.roundtable` at any depth. It does not honor `.gitignore`, exclude dependencies/build outputs, or filter by language, extension, hidden-file status, or binary content. Walk errors abort the operation; individual file-read errors are silently skipped.

**Symlink limitation:** the full-tree walk does not descend through directory symlinks, but collected file paths are read without reapplying the explicit-path guard. A file symlink within the tree can therefore expose content from outside the repository. Do not treat full-tree search as a secure read boundary in an untrusted checkout. Explicit-path checks also have a check/read race and are not a substitute for filesystem isolation.

The response is `{matches: [...]}` with each match containing a slash-normalized repository-relative `path`, one-based `line`, and original line `content`. Empty results may be JSON `null`, because the handler builds a nil slice until a match is found. There is no maximum result count, context-line option, output-size cap, or cancellation check inside the scan. Large dependency trees and files can consume substantial memory and produce large responses. Supply an explicit file path when possible and avoid publishing responses that may contain local secrets.

`repo.symbols` also uses the explicit-path guard, but its returned paths use the resolved indexer input rather than the caller's path. Despite the advertised optional property, the handler requires `path`; see [Symbol Index](SYMBOLS.md) for parser and identifier details.

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
| `vote.cast` | `proposal_id`, `agent_id`, `vote`, `reason_md` | `confidence`, default 0. Requires an existing proposal but stores nonempty vote strings and caller-provided agent IDs without role authentication or an enum check. Policy counts its recognized vote strings; other values do not contribute. |
| `vote.list` | `proposal_id` | List recorded votes. |
| `decision.record` | `decision`, `rationale_md`, `decided_by` | Optional `proposal_id`, `task_id`. Persists a decision record. |
| `patch.validate` | `proposal_id` | Reports claim, diff, resource coverage, temporary-apply, policy, human approval, and security-review results. |
| `patch.apply` | `proposal_id` | Revalidates, checks gates, applies through the service, and records a transaction/rollback artifact. Not an ordinary agent write shortcut. |
| `patch.reject` | `proposal_id`, `reason_md` | Rejects the proposal and records a decision. |

The exact apply flow and non-atomic failure boundaries are described in [Proposals and Transactions](TRANSACTIONS.md).

### Human approval and security

| Tool | Required arguments | Optional arguments / behavior |
| --- | --- | --- |
| `human.request_approval` | `subject`, `reason_md` | `approval_id`, `proposal_id`, `task_id`, `requested_by`, `status`, `decision_md`, `decided_by`, `override_policy`. Creates/replaces an approval record with caller-supplied decision fields. See [Human Approvals](HUMAN_APPROVALS.md) for selection and identity limits. |
| `human.approval_status` | `approval_id` | Returns persisted status. |
| `security.review` | At least one of `proposal_id` or `resource_id` at runtime | Optional `review_id`, `task_id`, `reviewer_id`, `status`, `summary_md`. A proposal with omitted/empty status runs the automatic patch scan; supplying status records a manual review and skips scanning. A resource-only review with omitted status defaults to `approved`. See [Security Reviews](SECURITY_REVIEW.md) for fields, heuristics, selection order, and trust limits. |

### Tests

See [Test Execution and Evidence](TEST_EXECUTION.md) for suggestions, execution status, log persistence, and failure handling.

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
