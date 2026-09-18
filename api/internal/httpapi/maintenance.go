package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type maintenanceStatus struct {
	ForeignKeys bool     `json:"foreign_keys"`
	WAL         bool     `json:"wal"`
	Protected   []string `json:"protected_immutable_tables"`
}

type retentionInput struct {
	RetentionDays int `json:"retention_days"`
}

func (s *Server) maintenanceStatusAPI(w http.ResponseWriter, r *http.Request) {
	if s.config.Store == nil {
		WriteProblem(w, r, http.StatusServiceUnavailable, "database_unavailable", "Database unavailable", "maintenance status requires the configured database")
		return
	}
	var foreignKeys int
	if err := s.config.Store.DB().QueryRowContext(r.Context(), "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "maintenance_status_failed", "Unable to inspect database", err.Error())
		return
	}
	var journalMode string
	_ = s.config.Store.DB().QueryRowContext(r.Context(), "PRAGMA journal_mode").Scan(&journalMode)
	writeJSON(w, http.StatusOK, maintenanceStatus{ForeignKeys: foreignKeys == 1, WAL: journalMode == "wal", Protected: protectedMaintenanceTables()})
}

func (s *Server) integrityCheckAPI(w http.ResponseWriter, r *http.Request) {
	if !maintenanceAuthorized(w, r) {
		return
	}
	var result string
	if err := s.config.Store.DB().QueryRowContext(r.Context(), "PRAGMA integrity_check").Scan(&result); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "integrity_check_failed", "Integrity check failed", err.Error())
		return
	}
	if result != "ok" {
		WriteProblemDetails(w, r, http.StatusConflict, "database_integrity_failed", "Database integrity check reported a problem", result, map[string]any{"result": result})
		return
	}
	auditMaintenance(r.Context(), s, r, "database.integrity_checked", map[string]any{"result": result})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "result": result, "completed_at": nowRFC3339Nano()})
}

func (s *Server) checkpointAPI(w http.ResponseWriter, r *http.Request) {
	if !maintenanceAuthorized(w, r) {
		return
	}
	var busy, logPages, checkpointed int
	if err := s.config.Store.DB().QueryRowContext(r.Context(), "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logPages, &checkpointed); err != nil {
		WriteProblem(w, r, http.StatusConflict, "checkpoint_failed", "Database checkpoint failed", err.Error())
		return
	}
	auditMaintenance(r.Context(), s, r, "database.checkpointed", map[string]any{"busy": busy, "log_pages": logPages, "checkpointed_pages": checkpointed})
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "busy": busy, "log_pages": logPages, "checkpointed_pages": checkpointed, "completed_at": nowRFC3339Nano()})
}

func (s *Server) backupAPI(w http.ResponseWriter, r *http.Request) {
	if !maintenanceAuthorized(w, r) {
		return
	}
	directory := s.config.MaintenanceDirectory
	if directory == "" {
		directory = ".roundtable"
	}
	directory = filepath.Join(directory, "backups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "backup_directory_failed", "Backup directory unavailable", err.Error())
		return
	}
	name := "roundtable-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10) + ".db"
	path := filepath.Join(directory, name)
	if _, err := s.config.Store.DB().ExecContext(r.Context(), "VACUUM INTO ?", path); err != nil {
		WriteProblem(w, r, http.StatusConflict, "backup_failed", "Database backup failed", err.Error())
		return
	}
	auditMaintenance(r.Context(), s, r, "database.backup_created", map[string]any{"artifact": filepath.ToSlash(filepath.Join("backups", name))})
	writeJSON(w, http.StatusCreated, map[string]any{"status": "completed", "artifact": filepath.ToSlash(filepath.Join("backups", name)), "completed_at": nowRFC3339Nano()})
}

func (s *Server) retentionAPI(w http.ResponseWriter, r *http.Request) {
	if !maintenanceAuthorized(w, r) {
		return
	}
	var input retentionInput
	if err := decodeJSON(r, &input); err != nil || input.RetentionDays < 1 || input.RetentionDays > 3650 {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_retention", "Invalid retention policy", "retention_days must be between 1 and 3650")
		return
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -input.RetentionDays).Format(time.RFC3339Nano)
	result, err := s.config.Store.DB().ExecContext(r.Context(), "DELETE FROM notifications WHERE created_at < ?", cutoff)
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "retention_cleanup_failed", "Retention cleanup failed", err.Error())
		return
	}
	deleted, _ := result.RowsAffected()
	auditMaintenance(r.Context(), s, r, "database.retention_cleaned", map[string]any{"retention_days": input.RetentionDays, "deleted_notifications": deleted})
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "retention_days": input.RetentionDays, "deleted_notifications": deleted, "protected_tables": protectedMaintenanceTables(), "completed_at": nowRFC3339Nano()})
}

func maintenanceAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if !humanAuthorized(r) {
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Human authorization required", "Database maintenance requires a human actor role")
		return false
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteProblem(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency key required", "Database maintenance requires Idempotency-Key")
		return false
	}
	return true
}

func protectedMaintenanceTables() []string {
	return []string{"audit_events", "event_outbox", "configuration_revisions", "transaction_phases", "memory_revisions", "policy_revisions"}
}

func auditMaintenance(ctx context.Context, s *Server, r *http.Request, action string, payload map[string]any) {
	if s.config.Store == nil {
		return
	}
	_, _ = s.config.Store.DB().ExecContext(ctx, `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES('',? ,?,'database','maintenance',?,?)`, r.Header.Get("X-Actor-ID"), action, requestID(r.Context()), mustJSONBytes(payload))
}

func mustJSONBytes(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
