# 0014: TUI Human Command Terminal

## Goal

Make the TUI command pane accept and execute bounded Roundtable CLI commands without exposing a host shell or allowing commands to switch the active project root.

## Acceptance criteria

- The Bubble Tea model accepts editable command input and submits on Enter.
- Commands execute asynchronously and display bounded output and errors.
- Tokenization supports quoted arguments without shell expansion or command chaining.
- Only supported Roundtable command families are accepted, and the active project root is pinned before user-supplied flags.
- MCP call cancellation follows the TUI lifecycle.
- Tests cover editing, execution, quoting, disallowed entrypoints/root overrides, root pinning with positional arguments, and output limits.
- TUI documentation describes the command and key behavior.
- Full Go race tests, build, vet, independent review, and whitespace checks pass.

## Completion evidence

- `go test -race ./...`, `go build ./...`, `go vet ./...`, and `git diff --check` pass.
- Independent review found and verified fixes for root-flag bypass and unbounded command-output capture; follow-up review found no actionable findings.

## Parent task status

This completes only the Human Command Terminal criterion from `0005-tui.md`. The legacy TUI task remains open for live event streaming, consensus/blocker detail in the live table, and keyboard selection-driven inspection.
