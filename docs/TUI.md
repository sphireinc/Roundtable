# Terminal UI and Watch View

`internal/tui` is a Bubble Tea view started by interactive `roundtable run`. It is a snapshot dashboard, not an interactive command terminal or full-featured table inspector.

## Layout and displayed data

The view draws four bordered panes: Live Table (run status/goal, task and claim counts, enabled agent count, pending proposals, requested approvals, latest transactions), Watch Feed (provided activity lines or socket placeholder), Command/Human Terminal (static example commands), and Inspector (latest proposal, transaction, approval summary). The last two panes are illustrative text; commands are not parsed and the inspector does not track keyboard-selected entities.

Pane widths/heights derive from terminal size and have a minimum width/height. At narrow sizes content is clipped by rune count rather than adaptively reflowed, so the current view is not guaranteed to be usable at all terminal sizes.

## Refresh and keys

The run command supplies a callback that reloads a database snapshot and recent event feed. The TUI schedules refreshes every two seconds by default. Successful refreshes replace the displayed snapshot/feed; refresh errors are currently discarded and the last successful view remains visible without a dedicated stale/error indicator. This is periodic polling, not an event subscription.

Press `q` or `Ctrl+C` to quit the UI. The process context also controls shutdown. No other keys, mouse navigation, command submission, approval, proposal editing, or transaction rollback are implemented in this TUI.

## Related monitoring surfaces

`roundtable table` prints a compact summary; `roundtable watch` displays feed snapshots and optionally polls with `--follow`. The browser administration UI is a separate application; its current scope is in [Web UI](UI.md).
