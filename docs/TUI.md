# Terminal UI and Watch View

`internal/tui` is a Bubble Tea view started by interactive `roundtable run`. It provides a snapshot dashboard and a bounded Roundtable command terminal; it is not a full-featured selectable table inspector.

## Layout and displayed data

The view draws four bordered panes: Live Table (run status/goal, task and claim counts, enabled agent count, blocked-task detail, pending proposals, requested approvals, latest proposal ballot tally, latest transaction), Watch Feed (in-process event subscription with periodic database reconciliation), Command/Human Terminal (input, bounded command output, and errors), and Inspector (latest proposal, transaction, approval summary). The inspector does not track keyboard-selected entities.

Pane widths/heights derive from terminal size and have a minimum width/height. At narrow sizes content is clipped by rune count rather than adaptively reflowed, so the current view is not guaranteed to be usable at all terminal sizes.

## Refresh and keys

The run command supplies a callback that reloads a database snapshot and recent event feed. The TUI schedules refreshes every two seconds by default and also subscribes to the in-process event bus for immediate Watch Feed updates. Successful refreshes replace the displayed snapshot/feed when their event cursor is current; refresh errors are discarded and the last successful view remains visible without a dedicated stale/error indicator.

Type a command and press `Enter` to run it. The terminal accepts `table`, `watch`, `claims`, `sessions`, and `mcp call` command families through Roundtable's existing CLI/runtime services. The active project root is forced; callers cannot redirect the command to another root. Input is tokenized with single/double quotes and backslash escapes, but no shell, variable expansion, pipelines, or command chaining is used. `Backspace` edits the input, `Ctrl+U` clears it, and `q` (on an empty prompt) or `Ctrl+C` quits. A command runs asynchronously; the pane keeps a bounded recent output history and displays failures. The terminal does not itself bypass proposal governance or transaction validation: `mcp call` dispatches through the existing MCP handlers.

### Interactive run shutdown

The TUI is not a detachable client layered over an independently managed coordinator. Interactive `roundtable run` starts the MCP listener, orchestration loop, and TUI as one application lifecycle. When the TUI returns (including after `q` or `Ctrl+C`), the application returns from `roundtable run`; deferred cleanup cancels the loop context and closes the MCP server. This does not stop an external agent process because this runtime does not supervise such processes.

Quitting the TUI is not a convergence signal. Cancellation exits the orchestration loop without marking the persisted run completed, blocked, or failed, so its last stored status can remain `active`. Conversely, if the loop itself reaches a terminal outcome first, the app cancels the TUI context and tears down the listener. To inspect or continue after either exit, query the persisted run state and explicitly use the documented CLI resume flow; do not infer terminal state or continued background polling from the last screen. See [Orchestration](ORCHESTRATION.md#timing-limits-and-failure-persistence) for loop cancellation and exit semantics.

## Related monitoring surfaces

### Dashboard selection and counts

The run header selects the configured run ID from the snapshot, or the last run in snapshot order when no ID is configured. If the requested run is absent, its ID remains visible with status `unknown`. The goal comes from the TUI options rather than being reloaded from the selected run row.

Counts use exact status strings across the supplied snapshot, not an additional run filter:

| Label | Included records |
| --- | --- |
| Active tasks | Tasks with `open` or `in_progress` status |
| Blocked tasks | Tasks with `blocked` status |
| Enabled agents | Agents whose `IsEnabled` flag is true, regardless of session liveness |
| Active/suspended claims | Claims with exact `active`/`suspended` status; no independent expiry check |
| Pending proposals | Proposals with `pending` or `in_review` status |
| Requested approvals | Human approvals with `requested` status |

The live table shows one latest transaction and one recent proposal's raw ballot counts (`A` approve/approve-with-notes, `B` blocking revise/reject, `V` veto, `O` other). These counts are not a policy decision or consensus verdict; policy profiles, required roles, tests, security review, and human approval may still prevent a proposal from being approvable. The first blocked task is shown with a count of any additional blocked tasks. The inspector independently chooses the newest-created proposal, latest-applied transaction, and latest-updated human approval. It does not restrict these choices to pending or successful records. Equal timestamp ordering is unspecified. A header for one run does not imply every pane contains only that run's records.

The run command supplies the latest six events for its run, displayed in chronological order as event type, actor (or `system`), and task ID (or `-`). The in-process store event bus appends new events immediately; a two-second database refresh reconciles the display if buffered notifications are dropped. Writes from another process do not publish to this process-local bus and appear on the next refresh. The feed is capped at eight lines and does not include event payloads, timestamps, or clickable records. The `MCP server listening` fallback is placeholder text, not an independently measured socket-health check.

### Refresh and rendering limitations

The refresh interval is an internal `Options` field; this TUI does not expose an interactive interval control. A nonpositive interval falls back to two seconds. A new timer is scheduled after the previous refresh response, so callback execution time adds to the effective polling period. With no refresh callback, the initial snapshot stays static. The callback receives `context.Background()`, not the program's cancellation context; quitting the UI does not propagate cancellation through that callback context.

Each pane clips excess body rows and pads or truncates each line without wrapping, scrolling, or an ellipsis. Text width is measured in Unicode runes, not terminal display columns, so wide glyphs and combining sequences can misalign borders. Minimum pane width is 20; minimum nominal pane height is six. Those minima can make the layout exceed a small terminal's dimensions.

### Watch CLI polling contract

```sh
roundtable watch --root . --run RUN-ID --limit 10
roundtable watch --root . --run RUN-ID --limit 50 --follow --interval 2s
```

`--root` defaults to `.`, `--run` to no explicit run filter, `--limit` to 10, `--follow` to false, and `--interval` to two seconds. The interval uses Go duration syntax (`250ms`, `2s`, `1m`); zero or negative durations fall back to two seconds. The CLI opens the local runtime/database and invokes `table.watch` directly; it does not require a separate socket client connection.

Every iteration prints the `WATCH` summary and the current blocked-task, pending-proposal, required-approval, and transaction lines. Those projection lines repeat even if unchanged. Event lines contain ID, type, actor ID, and task ID, without payload details. In follow mode only, event IDs at or below the highest previously printed ID are suppressed. The CLI does **not** send `after_event_id` to the runtime: each poll fetches a recent limited window. If more events arrive between polls than fit in that window, intermediate events can be missed. Increasing `--limit` reduces that risk but does not make delivery cursor-based. Use MCP `table.watch` with `after_event_id` and its returned cursor when reliable incremental event delivery is required; see [MCP Tools](MCP_TOOLS.md).

Without `--follow`, the command returns after one snapshot. With it, polling continues until a runtime error or context cancellation; cancellation returns the context error rather than being treated as a successful one-shot completion. This monitoring command does not acknowledge, mutate, approve, or execute any displayed record.

`roundtable table` prints a compact summary; `roundtable watch` displays feed snapshots and optionally polls with `--follow`. The browser administration UI is a separate application; its current scope is in [Web UI](UI.md).
