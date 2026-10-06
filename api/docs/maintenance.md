# Database maintenance API

The human-authorized maintenance routes use fixed SQLite operations and never accept
SQL text. `POST /api/v1/maintenance/backup` uses `VACUUM INTO` and returns a
server-relative artifact reference beneath the configured runtime directory.
`integrity-check` runs `PRAGMA integrity_check`; `checkpoint` runs a WAL
checkpoint; and `retention` deletes notification rows older than a calculated cutoff.

Audit, event outbox, configuration revision, transaction phase, memory
revision, and policy revision tables are reported as protected and are never
deleted by this retention handler. That exclusion is not database-wide
immutability enforcement. All mutating maintenance requests require a human
role and `Idempotency-Key`; each operation attempts an audit insert with the
request ID, but audit failures are ignored.

## Authorization and operation scope

Maintenance uses the shared human-authorization helper, not a unique admin-only privilege tier. Bearer authentication, when configured, is enforced by the common middleware; actor headers alone suffice only in development mode. A nonempty idempotency header is additionally required. See [Error and Retry Semantics](error-semantics.md) for process-local caching and the important difference between empty-body and body-bearing requests.

These operations apply to the configured database as a whole, not one workspace. There is no caller-supplied SQL, checkpoint mode, arbitrary backup destination, table selector, restore command, or maintenance scheduler. Coordinate them with every runtime/API process using that file. A successful response is not proof of a filesystem-artifact backup or a verified restore.

## Status and integrity

`GET /api/v1/maintenance` reads `PRAGMA foreign_keys` and `PRAGMA journal_mode` and reports booleans plus `protected_immutable_tables`. A foreign-key query failure returns an error; a journal-mode query failure is ignored and can appear as `wal: false`. The values reflect the pooled connection used by those reads, not an independently verified invariant across all connections. The protected-table array is descriptive output from a fixed helper.

Status does not require the maintenance-specific human-role/idempotency checks, though common authentication still applies. With no configured store it returns HTTP 503 `database_unavailable`; foreign-key query failure returns HTTP 500 `maintenance_status_failed`. It does not run integrity checking, report backup age/size, inspect disk space, or verify whether retention has ever run.

Integrity checking reads the first result returned by `PRAGMA integrity_check`. Exact `ok` returns success; another result returns `409 database_integrity_failed` with that result in metadata. Query failure returns `integrity_check_failed`. The handler does not repair corruption, run a separate `foreign_key_check`, or prove that referenced patch/log files and Git state match database rows. Preserve evidence and backups before attempting recovery.

Success is HTTP 200 with `status: "ok"`, `result: "ok"`, and current UTC RFC3339Nano `completed_at`; query failure is HTTP 500. The operation does not collect all corruption-report rows or save a dedicated integrity report. Only successful exact-`ok` checks attempt the `database.integrity_checked` audit row.

## WAL checkpoint

Checkpoint executes `PRAGMA wal_checkpoint(TRUNCATE)` and returns `busy`, `log_pages`, and `checkpointed_pages`. A scan/query error returns `409 checkpoint_failed`. A successful scan returns status `completed` even when `busy` is nonzero; inspect the numeric fields before assuming WAL truncation succeeded. This operation is not a command to delete sidecar files and does not stop other connections or writers.

HTTP 200 also includes current UTC RFC3339Nano `completed_at`. The handler attempts `database.checkpointed` audit **after** the checkpoint, which is itself a database write and can create new WAL content. A completed checkpoint response is therefore not evidence that the WAL remains empty when received. Other writers can also resume immediately; never remove WAL/SHM files based on this response alone.

## Backup artifact

Backup creates `<MaintenanceDirectory>/backups`, falling back to `.roundtable/backups` only when the library configuration has no maintenance directory. Standalone startup uses the configured database's parent directory. New directory creation requests mode `0700`; it does not tighten an existing directory's permissions or explicitly set the backup file's mode. The filename is `roundtable-<UTC Unix nanoseconds>.db`, generated server-side.

The handler executes parameterized `VACUUM INTO` and returns HTTP 201 with an artifact such as `backups/roundtable-....db`. That reference is relative to the maintenance directory, not a download URL. Directory creation failure uses `backup_directory_failed`; SQLite execution failure uses `409 backup_failed`. There is no subsequent integrity/restore verification, checksum manifest, automatic encryption, archive of external patches/test logs, or cleanup of a partially created artifact in this handler. Protect backups as sensitive database copies and test restoration separately.

The response has `status: "completed"`, `artifact`, and current UTC RFC3339Nano `completed_at`, not an absolute server path or file contents. Directory failure is HTTP 500. The `database.backup_created` audit is inserted into the source database only after `VACUUM INTO` completes, so that backup does not contain its own creation audit row. Later writes are not copied into the already-created artifact. The response timestamp is completion metadata, not a database snapshot-version identifier. No artifact-serving route is provided by this maintenance handler.

## Retention deletion rules

The body is `{"retention_days": N}` with integer N between 1 and 3650 inclusive. Invalid JSON/unknown typed fields or an out-of-range value yields `400 invalid_retention`. Cutoff is current UTC time minus N calendar days, formatted as RFC3339Nano. The SQL predicate is `created_at < cutoff` on stored timestamp text; it is not a normalized SQLite date comparison. Mixed timestamp formats can therefore affect boundary interpretation. Do not assume sub-day precision or timezone normalization beyond that implementation.

Deletion affects **all** notifications older than that predicate across every workspace and recipient, whether read, unread, actionable, resolved, or unresolved. There is no dry run, archived copy, per-workspace filter, or unresolved-notification exemption. The response reports `deleted_notifications`; it does not shrink the database file, remove backups/logs/patches, or delete events and revisions. Failure uses `409 retention_cleanup_failed`. Back up and inspect the retention scope before invoking a destructive cleanup.

HTTP 200 includes literal `status: "completed"`, submitted `retention_days`, deleted count, the fixed `protected_tables` array, and current UTC RFC3339Nano `completed_at`. A `RowsAffected` retrieval error is ignored, so the reported count is not independently checked. The operation does not read the settings API's `storage.retention_days` value or persist a scheduled retention policy; each request supplies its own cutoff. Backup, checkpoint, and integrity handlers do not decode their request bodies, so body-supplied destinations, modes, and options do not customize those operations.

## Audit and retry boundaries

Successful operations attempt database-global audit rows with empty workspace ID, entity type `database`, entity ID `maintenance`, actor header, action, request ID, and operation payload. Those inserts occur after the operation and are not in one transaction with it. Audit insert errors do not change the successful response. Workspace-filtered audit views may not include these global rows.

Empty-body backup/checkpoint/integrity requests are reevaluated on retry even when the key is unchanged, because the general idempotency cache does not retain empty-body requests. A retry can create another backup or repeat maintenance. Body-bearing retention can replay a cached response within the same server instance, but that cache is not durable or concurrency serialization. Inspect authoritative state and artifact existence after an ambiguous response rather than assuming the key guarantees exactly one operation.
