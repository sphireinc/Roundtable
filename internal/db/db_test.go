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
	if version != 2 {
		t.Fatalf("migration version = %d, want 2", version)
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
