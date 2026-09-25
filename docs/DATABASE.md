# Database and Persistence

The Go runtime and standalone API use SQLite via `internal/db`. `db.Open` creates the parent directory, opens the SQLite driver, applies connection pragmas, and runs idempotent schema-creation/column-addition code. The default Go path is `.roundtable/roundtable.db`; the API can select a separate path with `-database`. Point both processes at the same file only when they are intended to share one workspace state.

The schema-migration table records versions, but the current migration implementation is not a conventional runner that applies one isolated, transactional script per version. It ensures base tables and control-plane tables exist, adds missing columns, creates indexes, and records versions. Review `internal/db/db.go` as the source of truth before relying on a migration rollback or version gate.

The schema includes audit/event/outbox and revision-named tables, but table names alone do not prove all runtime writes use append-only semantics or that every state transition and outbox insert share one transaction. The Go patch application path spans filesystem mutation and later DB writes; see [Transactions](TRANSACTIONS.md) for its actual failure boundaries.

Large patch artifacts, test logs, and generated MCP files are stored under `.roundtable/` with paths or metadata persisted in SQLite. Protect that directory and do not remove SQLite WAL/SHM sidecars while writers are active.

## Required pragmas

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
```

## Initial schema

The following DDL documents the original/base table declarations. Later initialization steps add fields to some of these tables; it is not a complete dump of the final schema for a newly initialized database. See [Final and Control-Plane Schema](DATABASE_SCHEMA.md) for every added column and the complete control-plane table definitions, keys, and indexes.

```sql
CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  goal TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ended_at TEXT,
  metadata_json TEXT
);

CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  type TEXT NOT NULL,
  actor_id TEXT,
  task_id TEXT,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE agents (
  id TEXT PRIMARY KEY,
  role TEXT NOT NULL,
  name TEXT NOT NULL,
  adapter TEXT NOT NULL,
  command TEXT,
  status TEXT NOT NULL DEFAULT 'idle',
  is_enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE agent_sessions (
  id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  adapter TEXT NOT NULL,
  provider TEXT,
  model TEXT,
  external_session_id TEXT,
  external_resume_command TEXT,
  working_directory TEXT NOT NULL,
  mcp_socket TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at TEXT,
  ended_at TEXT,
  metadata_json TEXT
);

CREATE TABLE agent_session_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE adapter_capabilities (
  adapter TEXT PRIMARY KEY,
  supports_resume INTEGER NOT NULL,
  supports_mcp INTEGER NOT NULL,
  supports_readonly_workspace INTEGER NOT NULL,
  supports_session_capture INTEGER NOT NULL,
  metadata_json TEXT
);

CREATE TABLE run_snapshots (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  snapshot_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  body_md TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open',
  priority INTEGER NOT NULL DEFAULT 100,
  risk TEXT NOT NULL DEFAULT 'normal',
  assigned_agent_id TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE resources (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  path TEXT,
  symbol TEXT,
  language TEXT,
  start_line INTEGER,
  end_line INTEGER,
  current_hash TEXT,
  metadata_json TEXT
);

CREATE TABLE claims (
  id TEXT PRIMARY KEY,
  resource_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  claim_type TEXT NOT NULL,
  base_hash TEXT,
  status TEXT NOT NULL DEFAULT 'active',
  expires_at TEXT NOT NULL,
  heartbeat_at TEXT,
  renewable INTEGER NOT NULL DEFAULT 1,
  resume_policy TEXT NOT NULL DEFAULT 'hold',
  rationale_md TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE proposals (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  title TEXT NOT NULL,
  summary_md TEXT NOT NULL,
  patch_path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  risk TEXT NOT NULL DEFAULT 'normal',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE proposal_resources (
  proposal_id TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  PRIMARY KEY (proposal_id, resource_id)
);

CREATE TABLE votes (
  id TEXT PRIMARY KEY,
  proposal_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  vote TEXT NOT NULL,
  confidence REAL,
  reason_md TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE decisions (
  id TEXT PRIMARY KEY,
  proposal_id TEXT,
  task_id TEXT,
  decision TEXT NOT NULL,
  rationale_md TEXT NOT NULL,
  decided_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transactions (
  id TEXT PRIMARY KEY,
  proposal_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  before_git_hash TEXT NOT NULL,
  after_git_hash TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  applied_by TEXT NOT NULL DEFAULT 'orchestrator',
  applied_at TEXT,
  rollback_patch_path TEXT,
  metadata_json TEXT
);

CREATE TABLE test_runs (
  id TEXT PRIMARY KEY,
  proposal_id TEXT,
  task_id TEXT,
  command TEXT NOT NULL,
  status TEXT NOT NULL,
  summary_md TEXT,
  log_path TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE memory_entries (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  body_md TEXT NOT NULL,
  source_event_id INTEGER,
  importance INTEGER NOT NULL DEFAULT 50,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

## Complete table inventory

| Table | Purpose |
| --- | --- |
| `schema_migrations` | Records initialized schema versions. |
| `runs` | Run goal, status, lifecycle timestamps, metadata, and workspace association. |
| `events` | Run-scoped event stream with actor/task, JSON payload, and ordered integer ID. |
| `agents` | Configured/registered agent identity, role, adapter, command, enabled/status fields. |
| `agent_sessions` | External session metadata, run/agent association, heartbeat/status timestamps, workspace association. |
| `agent_session_events` | Session registration, heartbeat, and end history. |
| `adapter_capabilities` | Persisted adapter support flags and metadata JSON. |
| `run_snapshots` | Serialized state snapshots keyed to a run. |
| `tasks` | Work title/body, status, priority, risk, assignment, timestamps. |
| `resources` | Resource IDs/types and path/symbol/language/span/hash/metadata. |
| `claims` | Resource leases, owner/task, mode, base hash, expiration, resume policy, status, rationale. |
| `proposals` | Proposal content, patch path, risk/status, run/workspace/deliberation-related fields. |
| `proposal_resources` | Resource IDs associated with each proposal. |
| `votes` | Proposal/agent vote, confidence, rationale, session, and policy-version/weight metadata. |
| `decisions` | Proposal/task decision and rationale/actor. |
| `transactions` | Patch apply record, hashes/status/applier/time, rollback artifact path, workspace/metadata. |
| `human_approvals` | Requested human decision, subject/reason/status, requester/decider and override metadata. |
| `security_reviews` | Proposal/resource security reviewer, status, summary, and timestamps. |
| `test_runs` | Test command, proposal/task association, status, summary, log path, creation time. |
| `memory_entries` | Durable memory body and scope/kind/importance/status plus workspace, provenance, confidence, pinning, and revision fields. |
| `memory_aliases` | Alias-to-canonical memory mapping and reason/actor. |
| `memory_provenance_edges` | Links memory records to event/session/proposal or other source records. |
| `workspaces` | Workspace registry: repository root, name/status, identity/default branch/open time, and revision. |
| `deliberations` | Workspace-scoped deliberation topic/status/creator/timestamps and metadata/moderator. |
| `deliberation_participants` | Agent participation role/status and join time. |
| `deliberation_rounds` | Deliberation round records and state. |
| `deliberation_conflicts` | Recorded disagreement/conflict details within a deliberation. |
| `deliberation_links` | Links between deliberation and associated project entities. |
| `deliberation_messages` | Persisted deliberation transcript messages. |
| `proposal_files` | Per-file patch/diff metadata for proposals. |
| `claim_contentions` | Requested resource/mode/path, challenged claim, reason, and resolution state. |
| `consensus_snapshots` | Stored consensus projection/state for proposals or deliberations. |
| `policies` | Policy catalog entry, scope/selector/severity/enforcement and human-approval metadata. |
| `policy_revisions` | Versioned policy bodies and revision status. |
| `policy_evaluations` | Persisted policy evaluation inputs/results. |
| `transaction_phases` | Phase-level transaction inputs/outputs, status, failure/recovery metadata and logs. |
| `memory_revisions` | Memory revision history records. |
| `notifications` | Workspace notifications, read/actionable/resolution state and timestamps. |
| `configuration_revisions` | Versioned configuration snapshots/revisions. |
| `audit_events` | API audit records. |
| `event_outbox` | API event publication/outbox records. |

This catalog describes schema intent and the data represented by the tables. It does not assert that every table is currently populated by the Go runtime, that all table writes are append-only, or that every API endpoint has a corresponding complete user interface. API-side control-plane tables are partly broader than the local Go CLI feature set.

## IDs

Recommended prefixes:

- `RUN-` runs
- `A-` agents
- `S-` sessions
- `T-` tasks
- `R-` resources
- `C-` claims
- `P-` proposals
- `V-` votes
- `D-` decisions
- `TX-` transactions
- `TR-` test runs
- `M-` memory entries
