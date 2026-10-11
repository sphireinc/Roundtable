package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenBootstrapsControlPlaneSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "roundtable.db")
	sqlDB, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqlDB.Close()

	var journalMode, foreignKeys, busyTimeout string
	if err := sqlDB.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("journal mode: %v", err)
	}
	if err := sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign keys: %v", err)
	}
	if err := sqlDB.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("busy timeout: %v", err)
	}
	if journalMode != "wal" || foreignKeys != "1" || busyTimeout != "5000" {
		t.Fatalf("unexpected pragmas: journal=%s foreign_keys=%s busy_timeout=%s", journalMode, foreignKeys, busyTimeout)
	}

	for _, table := range []string{"workspaces", "deliberations", "deliberation_messages", "proposal_files", "claim_contentions", "consensus_snapshots", "policies", "policy_revisions", "policy_evaluations", "transaction_phases", "memory_revisions", "notifications", "configuration_revisions", "audit_events", "event_outbox"} {
		var name string
		if err := sqlDB.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}

	var version int
	if err := sqlDB.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("migration version: %v", err)
	}
	if version != 5 {
		t.Fatalf("migration version = %d, want 5", version)
	}
	for _, column := range []string{"revision"} {
		var count int
		if err := sqlDB.QueryRow("SELECT COUNT(*) FROM pragma_table_info('workspaces') WHERE name = ?", column).Scan(&count); err != nil {
			t.Fatalf("workspace column %s: %v", column, err)
		}
		if count != 1 {
			t.Fatalf("workspace column %s missing", column)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqlDB.Close()
	if err := applyPragmas(sqlDB); err != nil {
		t.Fatalf("apply pragmas: %v", err)
	}
	if err := migrate(sqlDB); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := migrate(sqlDB); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}

func TestMigrationAddsTestEvidenceProvenanceToLegacyTestRuns(t *testing.T) {
	sqlDB, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec(`CREATE TABLE test_runs (
		id TEXT PRIMARY KEY, proposal_id TEXT, task_id TEXT, command TEXT NOT NULL,
		status TEXT NOT NULL, summary_md TEXT, log_path TEXT, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO test_runs(id, command, status) VALUES ('TR-legacy', 'go test', 'passed')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(sqlDB); err != nil {
		t.Fatalf("migrate legacy test runs: %v", err)
	}
	var source string
	var outcome, submitter, session sql.NullString
	if err := sqlDB.QueryRow(`SELECT evidence_source, reported_outcome, submitted_by, session_id FROM test_runs WHERE id='TR-legacy'`).Scan(&source, &outcome, &submitter, &session); err != nil {
		t.Fatal(err)
	}
	if source != "host" || outcome.Valid || submitter.Valid || session.Valid {
		t.Fatalf("legacy host test record did not retain safe provenance defaults: source=%q outcome=%+v submitter=%+v session=%+v", source, outcome, submitter, session)
	}
}
