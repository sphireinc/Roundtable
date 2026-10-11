# 0015: TUI Live Watch Feed

## Goal

Deliver immediate in-process event updates to the TUI Watch Feed and recover from dropped notifications or writes from another process.

## Acceptance criteria

- Subscribe to the shared store event bus before taking the initial feed snapshot.
- Append only newer events scoped to the active run.
- Avoid stale periodic refreshes replacing newer streamed feed state.
- Retain periodic database refresh as reconciliation because bus notifications are buffered and process-local.
- Unsubscribe and release the waiting reader when the interactive run exits.
- Document event delivery and recovery limits.
- Tests, race detector, build, vet, independent review, and whitespace checks pass.

## Completion evidence

- TUI tests cover event delivery, duplicate and other-run filtering, and stale-refresh ordering; app and TUI tests pass.
- `go test -race ./...`, `go build ./...`, `go vet ./...`, and `git diff --check` pass.
- Independent review found no actionable issues.

## Parent task status

This completes only the Watch Feed live-update criterion from `0005-tui.md`. Live Table consensus/blocker detail and selection-driven Inspector views remain open.
