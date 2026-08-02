# Database Design

SQLite runs in WAL mode.

Large payloads such as full patches, large logs, and raw transcript dumps should be stored on disk under `.roundtable/`, with paths recorded in SQLite.

## Required pragmas

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
```

## Initial schema

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
