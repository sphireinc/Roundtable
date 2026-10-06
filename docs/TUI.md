# Terminal UI and Watch View

`internal/tui` is a Bubble Tea view started by interactive `roundtable run`. It is a snapshot dashboard, not an interactive command terminal or full-featured table inspector.

## Layout and displayed data

The view draws four bordered panes: Live Table (run status/goal, task and claim counts, enabled agent count, pending proposals, requested approvals, latest transactions), Watch Feed (provided activity lines or socket placeholder), Command/Human Terminal (static example commands), and Inspector (latest proposal, transaction, approval summary). The last two panes are illustrative text; commands are not parsed and the inspector does not track keyboard-selected entities.

Pane widths/heights derive from terminal size and have a minimum width/height. At narrow sizes content is clipped by rune count rather than adaptively reflowed, so the current view is not guaranteed to be usable at all terminal sizes.

## Refresh and keys

The run command supplies a callback that reloads a database snapshot and recent event feed. The TUI schedules refreshes every two seconds by default. Successful refreshes replace the displayed snapshot/feed; refresh errors are currently discarded and the last successful view remains visible without a dedicated stale/error indicator. This is periodic polling, not an event subscription.

Press `q` or `Ctrl+C` to quit the UI. The process context also controls shutdown. No other keys, mouse navigation, command submission, approval, proposal editing, or transaction rollback are implemented in this TUI.

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

The live table shows up to two transactions ordered by descending `AppliedAt`. The inspector independently chooses the newest-created proposal, latest-applied transaction, and latest-updated human approval. It does not restrict these choices to pending or successful records. Equal timestamp ordering is unspecified. A header for one run does not imply every pane contains only that run's records.

The run command supplies the latest six events for its run, displayed in chronological order as event type, actor (or `system`), and task ID (or `-`). It appends the socket path and quit hint. These are plain text lines, not event payloads, timestamps, clickable records, or streamed command output. The `MCP server listening` fallback is placeholder text, not an independently measured socket-health check.

### Refresh and rendering limitations

The refresh interval is an internal `Options` field; this TUI does not expose an interactive interval control. A nonpositive interval falls back to two seconds. A new timer is scheduled after the previous refresh response, so callback execution time adds to the effective polling period. With no refresh callback, the initial snapshot stays static. The callback receives `context.Background()`, not the program's cancellation context; quitting the UI does not propagate cancellation through that callback context.

Each pane clips excess body rows and pads or truncates each line without wrapping, scrolling, or an ellipsis. Text width is measured in Unicode runes, not terminal display columns, so wide glyphs and combining sequences can misalign borders. Minimum pane width is 20; minimum nominal pane height is six. Those minima can make the layout exceed a small terminal's dimensions. The example approval/rejection commands in the command pane are illustrative, not a supported input syntax or confirmation that those actions are available in this view.

### Watch CLI polling contract

```sh
roundtable watch --root . --run RUN-ID --limit 10
roundtable watch --root . --run RUN-ID --limit 50 --follow --interval 2s
```

`--root` defaults to `.`, `--run` to no explicit run filter, `--limit` to 10, `--follow` to false, and `--interval` to two seconds. The interval uses Go duration syntax (`250ms`, `2s`, `1m`); zero or negative durations fall back to two seconds. The CLI opens the local runtime/database and invokes `table.watch` directly; it does not require a separate socket client connection.

Every iteration prints the `WATCH` summary and the current blocked-task, pending-proposal, required-approval, and transaction lines. Those projection lines repeat even if unchanged. Event lines contain ID, type, actor ID, and task ID, without payload details. In follow mode only, event IDs at or below the highest previously printed ID are suppressed. The CLI does **not** send `after_event_id` to the runtime: each poll fetches a recent limited window. If more events arrive between polls than fit in that window, intermediate events can be missed. Increasing `--limit` reduces that risk but does not make delivery cursor-based. Use MCP `table.watch` with `after_event_id` and its returned cursor when reliable incremental event delivery is required; see [MCP Tools](MCP_TOOLS.md).

Without `--follow`, the command returns after one snapshot. With it, polling continues until a runtime error or context cancellation; cancellation returns the context error rather than being treated as a successful one-shot completion. This monitoring command does not acknowledge, mutate, approve, or execute any displayed record.

`roundtable table` prints a compact summary; `roundtable watch` displays feed snapshots and optionally polls with `--follow`. The browser administration UI is a separate application; its current scope is in [Web UI](UI.md).
