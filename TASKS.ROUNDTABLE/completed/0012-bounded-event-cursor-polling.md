# 0012: Bounded Event Cursor Polling

## Goal

Keep cursor-based MCP event polling bounded to the requested page instead of loading and filtering the full event history for a run.

## Acceptance criteria

- SQLite has an idempotently-created `(run_id, id)` event index.
- The store can return an ordered, limited event page strictly after a nonnegative global event ID cursor.
- Coordinator and agent `table.watch` cursor modes fetch at most `limit + 1` rows, preserving `has_more_events` and cursor advancement semantics.
- Event visibility and run scoping remain unchanged.
- Tests verify page ordering/size and use of the event index.
- Race tests, build, vet, independent review, and whitespace checks pass.

## Completion evidence

- `TestListEventsAfterUsesBoundedIndexedRunPage` verifies the bounded ordered page and SQLite query plan.
- Existing MCP watch and agent-turn cursor regressions pass.
- `go test -race ./...`, `go build ./...`, `go vet ./...`, and `git diff --check` pass.
- Independent review found no actionable issues.
