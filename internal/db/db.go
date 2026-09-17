package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func applyPragmas(db *sql.DB) error {
	for _, stmt := range []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("apply pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY
		);`,
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			goal TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			ended_at TEXT,
			metadata_json TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			type TEXT NOT NULL,
			actor_id TEXT,
			task_id TEXT,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			role TEXT NOT NULL,
			name TEXT NOT NULL,
			adapter TEXT NOT NULL,
			command TEXT,
			status TEXT NOT NULL DEFAULT 'idle',
			is_enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS agent_sessions (
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
		);`,
		`CREATE TABLE IF NOT EXISTS agent_session_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS adapter_capabilities (
			adapter TEXT PRIMARY KEY,
			supports_resume INTEGER NOT NULL,
			supports_mcp INTEGER NOT NULL,
			supports_readonly_workspace INTEGER NOT NULL,
			supports_session_capture INTEGER NOT NULL,
			metadata_json TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS run_snapshots (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			snapshot_json TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			body_md TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open',
			priority INTEGER NOT NULL DEFAULT 100,
			risk TEXT NOT NULL DEFAULT 'normal',
			assigned_agent_id TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS resources (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			path TEXT,
			symbol TEXT,
			language TEXT,
			start_line INTEGER,
			end_line INTEGER,
			current_hash TEXT,
			metadata_json TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS claims (
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
		);`,
		`CREATE TABLE IF NOT EXISTS proposals (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			title TEXT NOT NULL,
			summary_md TEXT NOT NULL,
			patch_path TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			risk TEXT NOT NULL DEFAULT 'normal',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS proposal_resources (
			proposal_id TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			PRIMARY KEY (proposal_id, resource_id)
		);`,
		`CREATE TABLE IF NOT EXISTS votes (
			id TEXT PRIMARY KEY,
			proposal_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			vote TEXT NOT NULL,
			confidence REAL,
			reason_md TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS decisions (
			id TEXT PRIMARY KEY,
			proposal_id TEXT,
			task_id TEXT,
			decision TEXT NOT NULL,
			rationale_md TEXT NOT NULL,
			decided_by TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS transactions (
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
		);`,
		`CREATE TABLE IF NOT EXISTS human_approvals (
			id TEXT PRIMARY KEY,
			proposal_id TEXT,
			task_id TEXT,
			subject TEXT NOT NULL,
			reason_md TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'requested',
			requested_by TEXT NOT NULL DEFAULT 'orchestrator',
			decision_md TEXT,
			decided_by TEXT,
			override_policy INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS security_reviews (
			id TEXT PRIMARY KEY,
			proposal_id TEXT,
			resource_id TEXT,
			task_id TEXT,
			reviewer_id TEXT NOT NULL DEFAULT 'security',
			status TEXT NOT NULL DEFAULT 'approved',
			summary_md TEXT NOT NULL,
			findings_json TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS test_runs (
			id TEXT PRIMARY KEY,
			proposal_id TEXT,
			task_id TEXT,
			command TEXT NOT NULL,
			status TEXT NOT NULL,
			summary_md TEXT,
			log_path TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS memory_entries (
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
		);`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);`,
		apiControlPlaneMigration,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate statement failed: %w", err)
		}
	}
	if err := ensureWorkspaceColumns(db); err != nil {
		return err
	}
	if err := ensureWorkspaceHardening(db); err != nil {
		return err
	}
	return nil
}

func ensureWorkspaceHardening(db *sql.DB) error {
	if err := addColumnIfMissing(db, "workspaces", "revision INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	for _, table := range []string{"agent_sessions", "claims", "proposals", "transactions"} {
		if err := addColumnIfMissing(db, table, "workspace_id TEXT"); err != nil {
			return err
		}
	}
	for _, column := range []string{"metadata_json TEXT", "moderator_id TEXT"} {
		if err := addColumnIfMissing(db, "deliberations", column); err != nil {
			return err
		}
	}
	for _, column := range []string{"deliberation_id TEXT", "proposer_session_id TEXT", "base_revision TEXT", "vote_state TEXT NOT NULL DEFAULT 'pending'", "policy_state TEXT NOT NULL DEFAULT 'pending'", "approval_state TEXT NOT NULL DEFAULT 'pending'", "transaction_state TEXT NOT NULL DEFAULT 'not_started'", "updated_at TEXT"} {
		if err := addColumnIfMissing(db, "proposals", column); err != nil {
			return err
		}
	}
	for _, column := range []string{"requested_resource_id TEXT", "requested_agent_id TEXT", "requested_task_id TEXT", "requested_mode TEXT", "requested_path TEXT", "resolution TEXT"} {
		if err := addColumnIfMissing(db, "claim_contentions", column); err != nil {
			return err
		}
	}
	for _, column := range []string{"session_id TEXT", "policy_weight INTEGER NOT NULL DEFAULT 1", "policy_version TEXT"} {
		if err := addColumnIfMissing(db, "votes", column); err != nil {
			return err
		}
	}
	for _, column := range []string{"scope TEXT NOT NULL DEFAULT 'workspace'", "selector_json TEXT NOT NULL DEFAULT '{}'", "severity TEXT NOT NULL DEFAULT 'normal'", "enforcement_mode TEXT NOT NULL DEFAULT 'advisory'", "human_approval_required INTEGER NOT NULL DEFAULT 0", "metadata_json TEXT NOT NULL DEFAULT '{}'"} {
		if err := addColumnIfMissing(db, "policies", column); err != nil { return err }
	}
	if err := addColumnIfMissing(db, "policy_revisions", "status TEXT NOT NULL DEFAULT 'draft'"); err != nil { return err }
	for _, index := range []string{
		"CREATE INDEX IF NOT EXISTS idx_workspace_sessions_status ON agent_sessions(workspace_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_workspace_claims_status ON claims(workspace_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_workspace_proposals_status ON proposals(workspace_id, status)",
		"CREATE INDEX IF NOT EXISTS idx_workspace_transactions_status ON transactions(workspace_id, status)",
	} {
		if _, err := db.Exec(index); err != nil {
			return fmt.Errorf("create workspace impact index: %w", err)
		}
	}
	if _, err := db.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES (4)"); err != nil {
		return fmt.Errorf("record workspace hardening migration: %w", err)
	}
	return nil
}

func addColumnIfMissing(db *sql.DB, table, definition string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	name := definition[:strings.IndexByte(definition, ' ')]
	for rows.Next() {
		var cid, notNull, pk int
		var columnName, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan %s schema: %w", table, err)
		}
		if columnName == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s schema: %w", table, err)
	}
	if _, err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + definition); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, name, err)
	}
	return nil
}

func ensureWorkspaceColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(workspaces)`)
	if err != nil {
		return fmt.Errorf("inspect workspaces schema: %w", err)
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan workspaces schema: %w", err)
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read workspaces schema: %w", err)
	}
	for _, column := range []string{
		"display_name TEXT",
		"root_alias TEXT",
		"canonical_repository_identity TEXT",
		"default_branch TEXT",
		"last_opened_at TEXT",
	} {
		name := column[:strings.IndexByte(column, ' ')]
		if existing[name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE workspaces ADD COLUMN " + column); err != nil {
			return fmt.Errorf("add workspaces.%s: %w", name, err)
		}
	}
	if _, err := db.Exec("INSERT OR IGNORE INTO schema_migrations(version) VALUES (3)"); err != nil {
		return fmt.Errorf("record workspace migration: %w", err)
	}
	return nil
}

const apiControlPlaneMigration = `
CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS deliberations (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id),
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    created_by TEXT NOT NULL,
    started_at TEXT,
    ended_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS deliberation_participants (
    deliberation_id TEXT NOT NULL REFERENCES deliberations(id),
    agent_id TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'participant',
    status TEXT NOT NULL DEFAULT 'active',
    joined_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (deliberation_id, agent_id)
);
CREATE TABLE IF NOT EXISTS deliberation_rounds (
    id TEXT PRIMARY KEY,
    deliberation_id TEXT NOT NULL REFERENCES deliberations(id),
    number INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    started_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ended_at TEXT,
    UNIQUE(deliberation_id, number)
);
CREATE TABLE IF NOT EXISTS deliberation_conflicts (
    id TEXT PRIMARY KEY,
    deliberation_id TEXT NOT NULL REFERENCES deliberations(id),
    summary TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS deliberation_links (
    deliberation_id TEXT NOT NULL REFERENCES deliberations(id),
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (deliberation_id, entity_type, entity_id)
);
CREATE TABLE IF NOT EXISTS deliberation_messages (
    id TEXT PRIMARY KEY,
    deliberation_id TEXT NOT NULL REFERENCES deliberations(id),
    agent_id TEXT,
    actor TEXT NOT NULL,
    message_type TEXT NOT NULL,
    body TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(deliberation_id, sequence)
);
CREATE TABLE IF NOT EXISTS proposal_files (
    id TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL REFERENCES proposals(id),
    path TEXT NOT NULL,
    blob_path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    operation TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(proposal_id, path)
);
CREATE TABLE IF NOT EXISTS claim_contentions (
    id TEXT PRIMARY KEY,
    resource_id TEXT NOT NULL REFERENCES resources(id),
    claimant_id TEXT NOT NULL REFERENCES claims(id),
    challenged_claim_id TEXT NOT NULL REFERENCES claims(id),
    status TEXT NOT NULL DEFAULT 'open',
    reason TEXT NOT NULL,
    resolved_by TEXT,
    resolved_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS consensus_snapshots (
    id TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL REFERENCES proposals(id),
    status TEXT NOT NULL,
    approval_count INTEGER NOT NULL DEFAULT 0,
    rejection_count INTEGER NOT NULL DEFAULT 0,
    abstain_count INTEGER NOT NULL DEFAULT 0,
    policy_version TEXT,
    snapshot_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS policies (
    id TEXT PRIMARY KEY,
    workspace_id TEXT REFERENCES workspaces(id),
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    current_revision_id TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS policy_revisions (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id),
    version INTEGER NOT NULL,
    definition_json TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(policy_id, version)
);
CREATE TABLE IF NOT EXISTS policy_evaluations (
    id TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id),
    policy_revision_id TEXT NOT NULL REFERENCES policy_revisions(id),
    proposal_id TEXT REFERENCES proposals(id),
    subject_type TEXT NOT NULL,
    subject_id TEXT NOT NULL,
    result TEXT NOT NULL,
    reasons_json TEXT NOT NULL,
    evaluated_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS transaction_phases (
    id TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transactions(id),
    phase TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    actor TEXT NOT NULL,
    reason TEXT,
    request_id TEXT,
    started_at TEXT,
    ended_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(transaction_id, phase)
);
CREATE TABLE IF NOT EXISTS memory_revisions (
    id TEXT PRIMARY KEY,
    memory_id TEXT NOT NULL REFERENCES memory_entries(id),
    revision INTEGER NOT NULL,
    body_md TEXT NOT NULL,
    provenance_json TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(memory_id, revision)
);
CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    workspace_id TEXT REFERENCES workspaces(id),
    recipient_id TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    category TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    entity_type TEXT,
    entity_id TEXT,
    read_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS configuration_revisions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT REFERENCES workspaces(id),
    version INTEGER NOT NULL,
    values_json TEXT NOT NULL,
    changed_by TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workspace_id, version)
);
CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT REFERENCES workspaces(id),
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    request_id TEXT,
    reason TEXT,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS event_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT REFERENCES workspaces(id),
    event_id TEXT NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    published_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_deliberations_workspace_status ON deliberations(workspace_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_deliberation_messages_timeline ON deliberation_messages(deliberation_id, sequence);
CREATE INDEX IF NOT EXISTS idx_deliberation_participants_agent ON deliberation_participants(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_deliberation_rounds_timeline ON deliberation_rounds(deliberation_id, number);
CREATE INDEX IF NOT EXISTS idx_deliberation_conflicts_status ON deliberation_conflicts(deliberation_id, status);
CREATE INDEX IF NOT EXISTS idx_proposal_files_proposal ON proposal_files(proposal_id, path);
CREATE INDEX IF NOT EXISTS idx_contentions_resource_status ON claim_contentions(resource_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_consensus_proposal_created ON consensus_snapshots(proposal_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_policy_revisions_policy_version ON policy_revisions(policy_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_policy_evaluations_subject ON policy_evaluations(subject_type, subject_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_transaction_phases_timeline ON transaction_phases(transaction_id, created_at);
CREATE INDEX IF NOT EXISTS idx_memory_revisions_timeline ON memory_revisions(memory_id, revision DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_recipient_unread ON notifications(recipient_id, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_entity ON audit_events(entity_type, entity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_workspace ON audit_events(workspace_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_event_outbox_pending ON event_outbox(published_at, created_at);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);`
