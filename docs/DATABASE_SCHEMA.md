# Final and Control-Plane Schema

This reference complements [Database and Persistence](DATABASE.md), which shows the original local-runtime DDL. The source of truth is `internal/db/db.go`: initialization creates the base tables, applies control-plane DDL, adds compatibility columns when absent, creates indexes, and inserts migration markers. It does not rebuild tables to match a versioned schema. The columns below describe the final result for a newly initialized database, including fields added after the original `CREATE TABLE` statements.

All identifiers and JSON values are stored as SQLite `TEXT` unless a different affinity is shown. Boolean values use integer `0`/`1`; times are text timestamps. `NOT NULL`, defaults, primary keys, unique constraints, and foreign keys are called out where present. SQLite foreign-key enforcement is enabled by `db.Open`; tables without a declared `REFERENCES` clause are not thereby referentially constrained.

## Local Runtime Tables: Migration-Added Columns

The base declarations for runs, sessions, claims, proposals, votes, transactions, and memories appear in [Database and Persistence](DATABASE.md). These fields are appended to those declarations by initialization:

| Table | Added field | Type and default | Meaning |
| --- | --- | --- | --- |
| `runs` | `workspace_id` | `TEXT`, nullable | Optional workspace association. |
| `agent_sessions` | `workspace_id` | `TEXT`, nullable | Workspace scoping for session listings and status indexes. |
| `claims` | `workspace_id` | `TEXT`, nullable | Workspace association for claim records. |
| `proposals` | `workspace_id` | `TEXT`, nullable | Workspace association. |
| `proposals` | `deliberation_id` | `TEXT`, nullable | Related deliberation, if any. |
| `proposals` | `proposer_session_id` | `TEXT`, nullable | Session that submitted the proposal. |
| `proposals` | `base_revision` | `TEXT`, nullable | Repository/entity revision the proposal is based on. |
| `proposals` | `vote_state` | `TEXT NOT NULL DEFAULT 'pending'` | Vote-gate projection. |
| `proposals` | `policy_state` | `TEXT NOT NULL DEFAULT 'pending'` | Policy-gate projection. |
| `proposals` | `approval_state` | `TEXT NOT NULL DEFAULT 'pending'` | Human-approval projection. |
| `proposals` | `transaction_state` | `TEXT NOT NULL DEFAULT 'not_started'` | Transaction progress projection. |
| `proposals` | `updated_at` | `TEXT`, nullable | Last-update timestamp used by API ordering. |
| `votes` | `session_id` | `TEXT`, nullable | Session associated with the vote. |
| `votes` | `policy_weight` | `INTEGER NOT NULL DEFAULT 1` | Weight captured for the vote. |
| `votes` | `policy_version` | `TEXT`, nullable | Policy version used when calculating weight. |
| `memory_entries` | `workspace_id` | `TEXT`, nullable | Workspace association. |
| `memory_entries` | `tags_json` | `TEXT NOT NULL DEFAULT '[]'` | JSON array of tags. |
| `memory_entries` | `provenance_json` | `TEXT NOT NULL DEFAULT '{}'` | JSON provenance summary. |
| `memory_entries` | `source_session_id` | `TEXT`, nullable | Source agent session. |
| `memory_entries` | `source_proposal_id` | `TEXT`, nullable | Source proposal. |
| `memory_entries` | `confidence` | `REAL NOT NULL DEFAULT 0.5` | Confidence score. |
| `memory_entries` | `reliability` | `REAL NOT NULL DEFAULT 0.5` | Reliability score. |
| `memory_entries` | `pinned` | `INTEGER NOT NULL DEFAULT 0` | Whether the entry is protected from ordinary stale handling. |
| `memory_entries` | `revision` | `INTEGER NOT NULL DEFAULT 1` | Current memory revision number. |
| `memory_entries` | `archived_at` | `TEXT`, nullable | Archive timestamp. |
| `memory_entries` | `resynthesis_status` | `TEXT NOT NULL DEFAULT 'none'` | Memory resynthesis state. |
| `transaction_phases` | `inputs_json` | `TEXT NOT NULL DEFAULT '{}'` | Phase input snapshot. |
| `transaction_phases` | `outputs_json` | `TEXT NOT NULL DEFAULT '{}'` | Phase output snapshot. |
| `transaction_phases` | `log_ref` | `TEXT`, nullable | Optional log artifact reference. |
| `transaction_phases` | `failure_code` | `TEXT`, nullable | Structured phase failure code. |
| `transaction_phases` | `recovery_state` | `TEXT NOT NULL DEFAULT 'recoverable'` | Recovery classification. |
| `claim_contentions` | `requested_resource_id` | `TEXT`, nullable | Requested resource in the contention workflow. |
| `claim_contentions` | `requested_agent_id` | `TEXT`, nullable | Agent requesting arbitration. |
| `claim_contentions` | `requested_task_id` | `TEXT`, nullable | Requester's task. |
| `claim_contentions` | `requested_mode` | `TEXT`, nullable | Requested claim mode. |
| `claim_contentions` | `requested_path` | `TEXT`, nullable | Requested repository path. |
| `claim_contentions` | `resolution` | `TEXT`, nullable | Resolution outcome/details. |
| `policies` | `scope` | `TEXT NOT NULL DEFAULT 'workspace'` | Policy scope. |
| `policies` | `selector_json` | `TEXT NOT NULL DEFAULT '{}'` | JSON subject selector. |
| `policies` | `severity` | `TEXT NOT NULL DEFAULT 'normal'` | Policy severity classification. |
| `policies` | `enforcement_mode` | `TEXT NOT NULL DEFAULT 'advisory'` | Advisory/enforcing behavior label. |
| `policies` | `human_approval_required` | `INTEGER NOT NULL DEFAULT 0` | Whether matching operations require human approval. |
| `policies` | `metadata_json` | `TEXT NOT NULL DEFAULT '{}'` | Policy metadata. |
| `policy_revisions` | `status` | `TEXT NOT NULL DEFAULT 'draft'` | Revision lifecycle status. |
| `notifications` | `actionable` | `INTEGER NOT NULL DEFAULT 1` | Whether the item requires attention. |
| `notifications` | `resolved_at` | `TEXT`, nullable | Resolution timestamp. |
| `notifications` | `resolution_note` | `TEXT`, nullable | Resolution explanation. |
| `workspaces` | `display_name` | `TEXT`, nullable | User-facing workspace name. |
| `workspaces` | `root_alias` | `TEXT`, nullable | Display-safe repository root alias. |
| `workspaces` | `canonical_repository_identity` | `TEXT`, nullable | Normalized repository identity used for duplicate detection. |
| `workspaces` | `default_branch` | `TEXT`, nullable | Workspace default branch. |
| `workspaces` | `last_opened_at` | `TEXT`, nullable | Last-open timestamp. |
| `workspaces` | `revision` | `INTEGER NOT NULL DEFAULT 1` | Optimistic-concurrency revision. |

Initialization also creates the local-runtime indexes listed under [Indexes](#indexes-and-migration-markers) below.

## Workspace and Deliberation Tables

These API control-plane tables share the same SQLite database and are created by the core initializer. Deliberation rows are workspace-owned; transcript order is enforced by a unique sequence per deliberation.

| Table | Columns and constraints |
| --- | --- |
| `workspaces` | `id TEXT PRIMARY KEY`; `name TEXT NOT NULL`; `root_path TEXT NOT NULL UNIQUE`; `status TEXT NOT NULL DEFAULT 'active'`; `created_at`, `updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; nullable `display_name`, `root_alias`, `canonical_repository_identity`, `default_branch`, `last_opened_at`; `revision INTEGER NOT NULL DEFAULT 1`. |
| `deliberations` | `id TEXT PRIMARY KEY`; `workspace_id TEXT NOT NULL REFERENCES workspaces(id)`; `title TEXT NOT NULL`; `status TEXT NOT NULL DEFAULT 'open'`; `created_by TEXT NOT NULL`; nullable `started_at`, `ended_at`; `created_at`, `updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; nullable `metadata_json`, `moderator_id`. |
| `deliberation_participants` | `deliberation_id TEXT NOT NULL REFERENCES deliberations(id)`; `agent_id TEXT NOT NULL`; `role TEXT NOT NULL DEFAULT 'participant'`; `status TEXT NOT NULL DEFAULT 'active'`; `joined_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; primary key `(deliberation_id, agent_id)`. |
| `deliberation_rounds` | `id TEXT PRIMARY KEY`; `deliberation_id TEXT NOT NULL REFERENCES deliberations(id)`; `number INTEGER NOT NULL`; `status TEXT NOT NULL DEFAULT 'open'`; `started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; nullable `ended_at`; unique `(deliberation_id, number)`. |
| `deliberation_conflicts` | `id TEXT PRIMARY KEY`; `deliberation_id TEXT NOT NULL REFERENCES deliberations(id)`; `summary TEXT NOT NULL`; `status TEXT NOT NULL DEFAULT 'open'`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. |
| `deliberation_links` | `deliberation_id TEXT NOT NULL REFERENCES deliberations(id)`; `entity_type`, `entity_id TEXT NOT NULL`; primary key `(deliberation_id, entity_type, entity_id)`. Linked entity IDs are polymorphic and are not foreign-keyed. |
| `deliberation_messages` | `id TEXT PRIMARY KEY`; `deliberation_id TEXT NOT NULL REFERENCES deliberations(id)`; nullable `agent_id`; `actor`, `message_type`, `body TEXT NOT NULL`; `sequence INTEGER NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; unique `(deliberation_id, sequence)`. |

## Governance and Transaction Tables

| Table | Columns and constraints |
| --- | --- |
| `proposal_files` | `id TEXT PRIMARY KEY`; `proposal_id TEXT NOT NULL REFERENCES proposals(id)`; `path`, `blob_path`, `content_hash`, `operation TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; unique `(proposal_id, path)`. |
| `claim_contentions` | `id TEXT PRIMARY KEY`; `resource_id TEXT NOT NULL REFERENCES resources(id)`; `claimant_id TEXT NOT NULL REFERENCES claims(id)`; `challenged_claim_id TEXT NOT NULL REFERENCES claims(id)`; `status TEXT NOT NULL DEFAULT 'open'`; `reason TEXT NOT NULL`; nullable `resolved_by`, `resolved_at`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; plus nullable requested-resource/agent/task/mode/path and `resolution` fields listed above. |
| `consensus_snapshots` | `id TEXT PRIMARY KEY`; `proposal_id TEXT NOT NULL REFERENCES proposals(id)`; `status TEXT NOT NULL`; approval/rejection/abstain counts `INTEGER NOT NULL DEFAULT 0`; nullable `policy_version`; `snapshot_json TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. |
| `policies` | `id TEXT PRIMARY KEY`; nullable `workspace_id TEXT REFERENCES workspaces(id)`; `name TEXT NOT NULL`; `status TEXT NOT NULL DEFAULT 'active'`; nullable `current_revision_id`; `created_at`, `updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; plus scope, selector, severity, enforcement, approval, and metadata fields listed above. |
| `policy_revisions` | `id TEXT PRIMARY KEY`; `policy_id TEXT NOT NULL REFERENCES policies(id)`; `version INTEGER NOT NULL`; `definition_json TEXT NOT NULL`; `created_by TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; unique `(policy_id, version)`; `status TEXT NOT NULL DEFAULT 'draft'`. |
| `policy_evaluations` | `id TEXT PRIMARY KEY`; `policy_id TEXT NOT NULL REFERENCES policies(id)`; `policy_revision_id TEXT NOT NULL REFERENCES policy_revisions(id)`; nullable `proposal_id TEXT REFERENCES proposals(id)`; `subject_type`, `subject_id`, `result`, `reasons_json`, `evaluated_by TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. |
| `transaction_phases` | `id TEXT PRIMARY KEY`; `transaction_id TEXT NOT NULL REFERENCES transactions(id)`; `phase TEXT NOT NULL`; `status TEXT NOT NULL DEFAULT 'pending'`; `actor TEXT NOT NULL`; nullable `reason`, `request_id`, `started_at`, `ended_at`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; unique `(transaction_id, phase)`; plus inputs, outputs, log reference, failure code, and recovery state listed above. |

## Memory, Attention, Audit, and Event Tables

| Table | Columns and constraints |
| --- | --- |
| `memory_aliases` | `alias_id TEXT PRIMARY KEY`; `canonical_id TEXT NOT NULL`; nullable `reason`; `created_by TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. `canonical_id` is not declared as a foreign key. |
| `memory_provenance_edges` | `id INTEGER PRIMARY KEY AUTOINCREMENT`; `memory_id`, `source_type`, `source_id`, `relation TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. |
| `memory_revisions` | `id TEXT PRIMARY KEY`; `memory_id TEXT NOT NULL REFERENCES memory_entries(id)`; `revision INTEGER NOT NULL`; `body_md`, `provenance_json`, `created_by TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; unique `(memory_id, revision)`. |
| `notifications` | `id TEXT PRIMARY KEY`; nullable `workspace_id TEXT REFERENCES workspaces(id)`; `recipient_id TEXT NOT NULL`; `severity TEXT NOT NULL DEFAULT 'info'`; `category`, `title`, `body TEXT NOT NULL`; nullable `entity_type`, `entity_id`, `read_at`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; plus `actionable INTEGER NOT NULL DEFAULT 1`, nullable `resolved_at`, `resolution_note`. |
| `configuration_revisions` | `id TEXT PRIMARY KEY`; nullable `workspace_id TEXT REFERENCES workspaces(id)`; `version INTEGER NOT NULL`; `values_json`, `changed_by TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`; unique `(workspace_id, version)`. SQLite permits multiple `NULL` values in this unique key. |
| `audit_events` | `id INTEGER PRIMARY KEY AUTOINCREMENT`; nullable `workspace_id TEXT REFERENCES workspaces(id)`; `actor_id`, `action`, `entity_type`, `entity_id TEXT NOT NULL`; nullable `request_id`, `reason`; `payload_json TEXT NOT NULL`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. |
| `event_outbox` | `id INTEGER PRIMARY KEY AUTOINCREMENT`; nullable `workspace_id TEXT REFERENCES workspaces(id)`; `event_id TEXT NOT NULL UNIQUE`; `event_type`, `entity_type`, `entity_id`, `payload_json TEXT NOT NULL`; nullable `published_at`; `created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP`. |

## Indexes and Migration Markers

All indexes use `IF NOT EXISTS`; descending order is shown explicitly where present.

| Index | Table and indexed columns |
| --- | --- |
| `idx_memory_alias_canonical` | `memory_aliases(canonical_id)` |
| `idx_memory_provenance_memory` | `memory_provenance_edges(memory_id, source_type, source_id)` |
| `idx_memory_workspace_status` | `memory_entries(workspace_id, status, updated_at DESC)` |
| `idx_memory_workspace_scope` | `memory_entries(workspace_id, scope, kind, updated_at DESC)` |
| `idx_memory_workspace_pinned` | `memory_entries(workspace_id, pinned, importance DESC)` |
| `idx_notifications_workspace_actionable` | `notifications(workspace_id, actionable, read_at, created_at DESC)` |
| `idx_workspace_sessions_status` | `agent_sessions(workspace_id, status)` |
| `idx_workspace_claims_status` | `claims(workspace_id, status)` |
| `idx_workspace_proposals_status` | `proposals(workspace_id, status)` |
| `idx_workspace_transactions_status` | `transactions(workspace_id, status)` |
| `idx_deliberations_workspace_status` | `deliberations(workspace_id, status, updated_at DESC)` |
| `idx_deliberation_messages_timeline` | `deliberation_messages(deliberation_id, sequence)` |
| `idx_deliberation_participants_agent` | `deliberation_participants(agent_id, status)` |
| `idx_deliberation_rounds_timeline` | `deliberation_rounds(deliberation_id, number)` |
| `idx_deliberation_conflicts_status` | `deliberation_conflicts(deliberation_id, status)` |
| `idx_proposal_files_proposal` | `proposal_files(proposal_id, path)` |
| `idx_contentions_resource_status` | `claim_contentions(resource_id, status, created_at DESC)` |
| `idx_consensus_proposal_created` | `consensus_snapshots(proposal_id, created_at DESC)` |
| `idx_policy_revisions_policy_version` | `policy_revisions(policy_id, version DESC)` |
| `idx_policy_evaluations_subject` | `policy_evaluations(subject_type, subject_id, created_at DESC)` |
| `idx_transaction_phases_timeline` | `transaction_phases(transaction_id, created_at)` |
| `idx_memory_revisions_timeline` | `memory_revisions(memory_id, revision DESC)` |
| `idx_notifications_recipient_unread` | `notifications(recipient_id, read_at, created_at DESC)` |
| `idx_audit_events_entity` | `audit_events(entity_type, entity_id, created_at DESC)` |
| `idx_audit_events_workspace` | `audit_events(workspace_id, created_at DESC)` |
| `idx_event_outbox_pending` | `event_outbox(published_at, created_at)` |

`schema_migrations` is `version INTEGER PRIMARY KEY`. Initialization inserts markers `1` (base/local schema), `2` (control-plane DDL), `3` (workspace identity additions), and `4` (workspace hardening). These markers are descriptive checkpoints; initialization still runs idempotent table/index/column checks rather than selecting and transactionally replaying one migration per version. They are not proof that a database can be downgraded or that a migration is atomic.

## Integrity and Operational Limits

- Foreign keys exist only where the declarations above explicitly say `REFERENCES`; several association tables deliberately use polymorphic IDs or lack FKs.
- Most lifecycle values are unconstrained `TEXT`, not SQLite enums or `CHECK` constraints. Runtime/API validators enforce allowed states where implemented.
- JSON-named columns are text and are not protected by SQLite JSON validity constraints.
- Event, audit, and revision table names do not prove append-only behavior; inspect the writer and service path before relying on immutability.
- The local CLI, standalone API, and UI do not all populate or expose every table. See [Architecture](ARCHITECTURE.md) and [HTTP API](../api/README.md) for component boundaries.
- Keep the SQLite database with its WAL/SHM sidecars while processes are active. Back up through the supported maintenance flow rather than copying an open database without its journal state.
