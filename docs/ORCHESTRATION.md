# Orchestration

The Go coordinator is implemented in `internal/orchestrator`. It maintains the shared run state and performs idempotent scheduling work; it does not launch external agent processes. Agent work enters through the MCP tool boundary and is represented by persisted tasks, proposals, votes, decisions, claims, and events.

## Run lifecycle

`roundtable run` loads `.roundtable/config.yaml`, opens/migrates SQLite, synchronizes configured agents and adapter capabilities, creates or resumes a run, writes generated MCP assets, and starts the orchestrator. Interactive mode starts the MCP Unix socket server and TUI alongside the loop. Headless mode runs without those services and defaults to one orchestration iteration.

The loop performs one idempotent tick per iteration:

1. Assign eligible open tasks to available configured agents.
2. Request review from eligible reviewers for proposals that need it.
3. Schedule the next queued agent turn request in FIFO order.
4. Inspect run state to decide whether to keep polling, complete, or report blocked.

The loop retries non-cancellation errors with bounded backoff. Each retry is recorded as an orchestration event. Reaching the consecutive error threshold marks the run failed. Context cancellation stops promptly; it is not treated as an ordinary retry.

## Completion and waiting

An outstanding agent turn request keeps the run active. A run with no tasks and no proposals remains active as `awaiting_initial_work`; this permits work to arrive later through MCP. When tasks and proposals are all terminal and no blocked tasks remain, the run completes. A fully blocked task state is reported as blocked. Pending, in-review, and revision-requested proposals prevent convergence.

This is state-based convergence, not proof that an external agent executed code or that a test suite passed. Agent process execution/session capture and automated test orchestration are distinct capabilities and must not be inferred from run-loop behavior.

## Loop options

| Option | Default | Behavior |
| --- | --- | --- |
| `--interval` | `1s` | Wait between successful nonterminal iterations. |
| `--retry-backoff` | `250ms` | Wait after an iteration error before retrying. |
| `--max-consecutive-errors` | `5` | Fail and persist run failure after this many consecutive errors; a successful iteration resets the count. |
| `--max-iterations` | `0` | Zero means no iteration limit for interactive execution. A positive number bounds iterations. |
| `--headless` | `false` | Avoid starting the MCP server/TUI. If no positive iteration limit is given, execute one cycle. |

See [CLI Reference](CLI.md) for run identifiers, goals, and resume flags. Turn-request MCP events are documented in [MCP Tools](MCP_TOOLS.md).
