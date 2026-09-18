# Database maintenance API

The admin-only maintenance routes use fixed SQLite operations and never accept
SQL text. `POST /api/v1/maintenance/backup` uses `VACUUM INTO` and returns a
server-relative artifact reference beneath the configured runtime directory.
`integrity-check` runs `PRAGMA integrity_check`; `checkpoint` runs a WAL
checkpoint; and `retention` removes only expired notification rows.

Audit, event outbox, configuration revision, transaction phase, memory
revision, and policy revision tables are immutable governance history and are
never removed by retention cleanup. All mutating maintenance requests require
a human role and `Idempotency-Key`; each operation records an audit event with
the request ID.
