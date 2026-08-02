package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate statement failed: %w", err)
		}
	}
	return nil
}
