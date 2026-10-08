# Orchestration

The Go coordinator is implemented in `internal/orchestrator`. It maintains the shared run state and performs idempotent scheduling work; it does not launch external agent processes. Agent work enters through the MCP tool boundary and is represented by persisted tasks, proposals, votes, decisions, claims, and events.

`roundtable start` is the continuously running project owner. It stays alive while idle, ticks each active run independently, and exposes coordinator health through `/healthz`; it does not create a run. The default interval is one second. Per-run tick failures are retried with capped backoff and mark service health degraded without preventing other runs from ticking. The official MCP HTTP endpoint and stdio bridge both dispatch through the same runtime. Foreground operation does not install a boot service or restart after host reboot.

## Run lifecycle

`roundtable run` loads `.roundtable/config.yaml`, opens/migrates SQLite, synchronizes configured agents and adapter capabilities, creates or resumes one run, writes generated MCP assets, and starts the finite-per-run orchestrator. Interactive mode also starts the legacy Unix socket and TUI. Headless mode runs without those services and defaults to one orchestration iteration. Use `roundtable start` when the coordinator must remain alive across runs and idle periods; it has no TUI and creates no run.

The loop performs one idempotent tick per iteration:

1. Assign unassigned tasks with exact status `open` to configured implementer IDs.
2. Request review from eligible reviewers for proposals that need it.
3. Schedule the next queued agent turn request in FIFO order.
4. Inspect run state to decide whether to keep polling, complete, or report blocked.

The loop retries non-cancellation errors with a fixed configured delay. Each retry is recorded as an orchestration event. Reaching the consecutive error threshold attempts to mark the run failed. Context cancellation is not treated as an ordinary retry; responsiveness still depends on the current operation honoring its context.

## Completion and waiting

An outstanding agent turn request keeps the run active. A run with no tasks and no proposals remains active as `awaiting_initial_work`; this permits work to arrive later through MCP. Completion and blocking use the exact status classification below. Pending, in-review, and revision-requested proposals prevent convergence, while other proposal statuses do not, even if they are unfamiliar strings.

This is state-based convergence, not proof that an external agent executed code or that a test suite passed. Agent process execution/session capture and automated test orchestration are distinct capabilities and must not be inferred from run-loop behavior.

## Loop options

| Option | Default | Behavior |
| --- | --- | --- |
| `--interval` | `1s` | Wait between successful nonterminal iterations. |
| `--retry-backoff` | `250ms` | Wait after an iteration error before retrying. |
| `--max-consecutive-errors` | `5` | Fail and persist run failure after this many consecutive errors; a successful iteration resets the count. |
| `--max-iterations` | `0` | Zero means no iteration limit for interactive execution. A positive number bounds iterations. |
| `--headless` | `false` | Avoid starting the MCP server/TUI. Exactly zero iteration limit is changed to one cycle; negative limits remain unbounded. |

## Assignment and synchronization details

Agent synchronization upserts configured IDs with status `idle` and enabled flag true. Existing rows for those IDs are reset to the configured role/name/adapter/command, not preserved as manually disabled agents. Reducing a pool count does not delete surplus historical agent rows. No provider liveness, session heartbeat, current workload, or claim occupancy check selects an available implementer.

Each tick reads all local tasks in database order, skips assigned tasks and any status other than exact `open`, and assigns remaining tasks round-robin to `implementer-1` through the configured count. The round-robin counter restarts at zero each tick; it is not a persisted fairness or capacity scheduler. Assignment changes status to `in_progress` and appends `task.assigned` under the tick's run ID. With zero implementers, assignments are skipped. Pending proposals are also database-wide; requesting their review and recording `proposal.review_requested` does not launch reviewers, cast votes, or apply patches.

These operations are sequential rather than one atomic tick. If assignment or review succeeds before a later failure, the earlier changes remain. In particular, a state update followed by an event error can leave the state advanced without its intended event. Retry logic does not restore a before-tick snapshot. Run only one coordinator for a shared state unless concurrency has been separately reviewed; this service is not a distributed leader-election or database-wide scheduling lock.

## Exact convergence classification

Task and proposal reads are database-wide, while turn-event reads are filtered by the supplied run ID. Old tasks/proposals can therefore keep a new run active or influence its completion. There is no independent test-result, approval, claim-expiry, active-session, or provider-process check in convergence.

| Record | Classification |
| --- | --- |
| Task `open`, `in_progress`, `pending`, `review` | Active work |
| Task `blocked` | Blocked work |
| Task `completed`, `done`, `closed`, `cancelled`, `canceled`, `failed` | Terminal; not active or blocked |
| Any other task status | Active work |
| Proposal `pending`, `in_review`, `revision_requested` | Active work |
| Any other proposal status | Not active for this calculation |

Outstanding turns take precedence and return `active` / `pending_agent_turn`. An empty task-and-proposal database returns `active` / `awaiting_initial_work`. Otherwise, zero active tasks/proposals and zero blocked tasks returns `completed` / `all_tasks_and_proposals_terminal`; zero active tasks/proposals with blocked tasks returns `blocked` / `all_remaining_tasks_blocked`. Other states return `active` / `work_remains`. A failed task can thus coexist with a completed run; completion is not an acceptance-test verdict.

## Timing, limits, and failure persistence

The first iteration starts immediately. Successful nonterminal iterations wait on a ticker created at loop startup, rather than sleeping a full interval after each tick. Long operations can consume the waiting period. Nonpositive interval, retry delay, and consecutive-error threshold normalize to their defaults. Retry delay is constant, without exponential growth or jitter. Both successful and failed cycles count toward a positive maximum iteration limit.

Below the error threshold, `orchestrator.retry` stores iteration, consecutive attempt, maximum attempts, error text, and the next delay. Failure to append that retry event stops the loop immediately. At the threshold, the loop attempts status `failed` with an end timestamp and an `orchestrator.failed` event. Completed/blocked outcomes similarly update the run and then append their event. These writes are not atomic: event failure may follow an already-persisted terminal status, and status persistence can itself fail. Error text is stored as event payload data and can contain operational details.

Cancellation and iteration-limit exits do not mark the run completed, blocked, or failed. A positive limit produces `ErrIterationLimit` with reason `iteration_limit`. The headless CLI deliberately treats that sentinel as a normal bounded run and then persists a snapshot; interactive mode returns it as an error. Headless snapshots are written after the loop returns, whereas interactive startup writes an initial snapshot before starting the loop. Snapshot persistence failure can still make a headless invocation fail after its cycles have changed state.

Scheduled turns have no timeout, preemption, or automatic stale-owner recovery in this scheduler. A scheduled request without completion prevents scheduling another request, even before its start acknowledgment. Malformed lifecycle JSON can fail a tick and enter retry handling. FIFO scheduling is based on durable request event IDs, not urgency scoring, and does not bypass the normal claim/review gates.

See [CLI Reference](CLI.md) for run identifiers, goals, and resume flags. Turn-request MCP events are documented in [MCP Tools](MCP_TOOLS.md).

## HTTP Run-Control Surface

The API exposes `GET /api/v1/workspaces/{id}/runs/status` and `POST /api/v1/workspaces/{id}/runs/{action}` in `api/internal/httpapi/runs.go`. These are database projections/transitions, not RPCs to the separate Go coordinator. They do not launch, pause, cancel, drain, or kill provider processes; they do not cancel the coordinator's context. The coordinator loop does not consult a persisted `paused-intake` or `stopped` flag before each tick. Do not treat API pause/stop as an enforced stop to shared-state activity.

### Status Selection and Counts

Status selects the workspace-associated run with newest `started_at`; equal timestamp order is unspecified. Legacy runs without that workspace association are excluded. Any lookup error, not just absence, produces HTTP 200 with state `stopped` and a current response timestamp. For a found run, `updated_at` actually contains its `started_at`, not the time of the latest transition. Goal text is passed through the API redaction helper.

| Status Field | Current Query Scope |
| --- | --- |
| `active_agent_count` | Sessions for the selected run with exact `active` or `running` status; not a heartbeat/process-health check. |
| `active_deliberations` | All database deliberations with `active` or `running` status, without a workspace filter. |
| `open_proposals` | Workspace proposals with `pending` or `in_review` status; revision-requested proposals are omitted. |
| `pending_approvals` | Requested approvals joined to proposals in the workspace; standalone/task-only approvals are omitted. |
| `blockers` | Rejected workspace proposals, not a comprehensive policy/task/claim blocker count. |

Count-query errors are ignored and can appear as zero. These fields are separate reads, not one transactionally consistent snapshot.

### Action State Machine

Every action requires the shared human-authorization check, nonempty `Idempotency-Key`, and a resolvable workspace. Unknown action returns `invalid_run_action`. Allowed prior states and targets are exact strings:

| Action | Allowed Prior States | Target |
| --- | --- | --- |
| `start` | `stopped`, `starting` | `active` |
| `pause` | `active` | `paused-intake` |
| `resume` | `paused-intake`, `degraded` | `active` |
| `stop` | `active`, `paused-intake`, `draining` | `stopped` |
| `force-stop` | `active`, `paused-intake`, `draining`, `degraded` | `stopped` |

The body declares `run_id` and `goal`, but `run_id` is not used for selection. The handler ignores body-decoding errors and can proceed using empty or partially decoded values. It always selects the latest run for the workspace. `start` creates a new `run-<Unix nanoseconds>` row with status `starting` when lookup fails or the newest run is `stopped`; it does not reactivate that stopped row. That insert precedes the transition transaction and can remain after a later failure. Starting from `completed`, `blocked`, or `failed` is not an allowed transition in this API state machine, unlike CLI resume behavior.

Disallowed states return `409 illegal_run_transition`. The transition transaction conditionally updates by ID/prior status and inserts an audit event. It does not check affected-row count, so a concurrent status change can produce an audit/success response even when the conditional update matched nothing. No `If-Match` revision or run-specific compare token is accepted here. Failed begin uses `run_transition_failed`; later update/audit/commit failure also uses that code, with a conflict response. The success body contains run ID and target state.

Transitions do not update `ended_at`, append the main `events` stream, create a run snapshot, or reconcile outstanding turns/claims. `force-stop` has the same target as stop and is not an OS process-kill mechanism. Empty-body actions are not retained by the general idempotency cache and are reevaluated on retries; a repeated empty-body start can encounter a different latest state. Inspect the run and actual coordinator/process state separately after any ambiguous response. See [API Error and Retry Semantics](../api/docs/error-semantics.md).
