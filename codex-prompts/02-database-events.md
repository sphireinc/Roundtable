# Prompt 02: Database and Events

Implement the SQLite schema from docs/DATABASE.md.

Requirements:

- migrations
- WAL pragmas
- event append API
- repositories for runs, agents, tasks, resources, claims, proposals, votes, decisions, transactions, test runs, memory, sessions
- pub/sub event bus for TUI/watch

Acceptance:

- migrations run idempotently
- all repositories have unit tests
- event append works and emits to subscribers
