package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"roundtable/internal/db"
)

type settingSectionResponse struct {
	Values          map[string]any `json:"values"`
	Source          string         `json:"source"`
	Version         int            `json:"version"`
	RestartRequired bool           `json:"restart_required"`
}

var defaultSettings = map[string]map[string]any{
	"general":       {"locale": "en-US", "timezone": "UTC"},
	"governance":    {"approval_required": true, "quorum": 1, "require_security_review": true},
	"adapters":      {"default": "codex", "allow_shell": false},
	"mcp":           {"transport": "unix", "readonly_repository": true},
	"storage":       {"wal": true, "retention_days": 30},
	"notifications": {"enabled": true, "default_recipient": "human"},
}

func (s *Server) settingsSchemaAPI(w http.ResponseWriter, r *http.Request) {
	if _, err := s.workspace(r); err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	schema := map[string]any{}
	for section, values := range defaultSettings {
		fields := map[string]any{}
		for key, value := range values {
			fields[key] = map[string]any{"default": value, "secret": isSettingSecret(key), "type": settingType(value)}
		}
		schema[section] = map[string]any{"fields": fields, "restart_required": section == "mcp" || section == "storage"}
	}
	writeJSON(w, 200, map[string]any{"sections": schema})
}

func (s *Server) effectiveSettingsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	sections, version, err := s.loadEffectiveSettings(r, workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "settings_read_failed", "Unable to read settings", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"workspace_id": workspace.ID, "version": version, "sections": sections})
}

func (s *Server) settingsPreflightAPI(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input struct {
		Section string         `json:"section"`
		Values  map[string]any `json:"values"`
	}
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_settings", "Invalid settings preflight", err.Error())
		return
	}
	if err := validateSettings(input.Section, input.Values); err != nil {
		WriteProblem(w, r, 400, "invalid_settings", "Invalid settings", err.Error())
		return
	}
	_, version, _ := s.loadEffectiveSettings(r, workspace.ID)
	restart := input.Section == "mcp" || input.Section == "storage"
	writeJSON(w, 200, map[string]any{"allowed": true, "version": version, "confirmation_token": settingsConfirmationToken(workspace.ID, input.Section, version), "impact": map[string]any{"section": input.Section, "affected_components": []string{input.Section}, "restart_required": restart}})
}

func (s *Server) updateSettingsAPI(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	section := r.PathValue("section")
	var request map[string]any
	if err := decodeJSON(r, &request); err != nil {
		WriteProblem(w, r, 400, "invalid_settings", "Invalid settings", err.Error())
		return
	}
	values := request
	if nested, ok := request["values"].(map[string]any); ok {
		values = nested
	}
	expected := -1
	if raw := r.Header.Get("If-Match"); raw != "" {
		expected, _ = strconv.Atoi(strings.Trim(raw, "\""))
	}
	if raw, ok := request["expected_version"].(float64); ok {
		expected = int(raw)
	}
	if expected < 0 {
		WriteProblem(w, r, 400, "invalid_settings_version", "Expected version required", "Set If-Match or expected_version")
		return
	}
	if err := validateSettings(section, values); err != nil {
		WriteProblem(w, r, 400, "invalid_settings", "Invalid settings", err.Error())
		return
	}
	_, current, _ := s.loadEffectiveSettings(r, workspace.ID)
	if current != expected {
		WriteProblem(w, r, 409, "settings_version_conflict", "Settings changed", fmt.Sprintf("expected version %d, current version %d", expected, current))
		return
	}
	if section == "governance" && r.Header.Get("X-Confirmation-Token") != settingsConfirmationToken(workspace.ID, section, current) {
		WriteProblem(w, r, 409, "settings_confirmation_required", "Confirmation required", "Run settings preflight and provide X-Confirmation-Token")
		return
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "settings_update_failed", "Unable to update settings", err.Error())
		return
	}
	defer tx.Rollback()
	version := current + 1
	payload, _ := json.Marshal(map[string]any{"section": section, "values": values})
	id := fmt.Sprintf("CFG-%s-%d", workspace.ID, version)
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO configuration_revisions(id,workspace_id,version,values_json,changed_by) VALUES(?,?,?,?,?)`, id, workspace.ID, version, string(payload), r.Header.Get("X-Actor-ID")); err != nil {
		WriteProblem(w, r, 409, "settings_update_conflict", "Unable to save settings", err.Error())
		return
	}
	audit, _ := json.Marshal(map[string]any{"section": section, "version": version})
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events(workspace_id,actor_id,action,entity_type,entity_id,request_id,payload_json) VALUES(?,?,?,'workspace',?,?,?)`, workspace.ID, r.Header.Get("X-Actor-ID"), "settings.updated", workspace.ID, requestID(r.Context()), string(audit)); err != nil {
		WriteProblem(w, r, 409, "settings_update_conflict", "Unable to audit settings", err.Error())
		return
	}
	if err = tx.Commit(); err != nil {
		WriteProblem(w, r, 409, "settings_update_conflict", "Unable to commit settings", err.Error())
		return
	}
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: workspace.ID, Type: "settings.updated", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: string(audit)})
	sections, version, _ := s.loadEffectiveSettings(r, workspace.ID)
	writeJSON(w, 200, map[string]any{"workspace_id": workspace.ID, "version": version, "sections": sections})
}

func (s *Server) loadEffectiveSettings(r *http.Request, workspaceID string) (map[string]settingSectionResponse, int, error) {
	sections := map[string]settingSectionResponse{}
	for name, values := range defaultSettings {
		sections[name] = settingSectionResponse{Values: sanitizeSettings(values), Source: "default", RestartRequired: name == "mcp" || name == "storage"}
	}
	var version int
	var raw string
	err := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COALESCE(MAX(version),0),COALESCE((SELECT values_json FROM configuration_revisions WHERE workspace_id=? ORDER BY version DESC LIMIT 1),'') FROM configuration_revisions WHERE workspace_id=?`, workspaceID, workspaceID).Scan(&version, &raw)
	if err != nil {
		return nil, 0, err
	}
	if raw != "" {
		var changed map[string]any
		if json.Unmarshal([]byte(raw), &changed) == nil {
			section, _ := changed["section"].(string)
			if values, ok := changed["values"].(map[string]any); ok {
				current := sections[section]
				for key, value := range values {
					current.Values[key] = sanitizeSetting(key, value)
				}
				current.Source = "workspace"
				current.Version = version
				sections[section] = current
			}
		}
	}
	return sections, version, nil
}
func validateSettings(section string, values map[string]any) error {
	if _, ok := defaultSettings[section]; !ok {
		return fmt.Errorf("unknown settings section %q", section)
	}
	if section == "governance" {
		if quorum, ok := values["quorum"].(float64); ok && (quorum < 1 || quorum > 100 || quorum != float64(int(quorum))) {
			return fmt.Errorf("quorum must be an integer from 1 to 100")
		}
	}
	return nil
}
func settingsConfirmationToken(workspace, section string, version int) string {
	sum := sha256.Sum256([]byte(workspace + "|" + section + "|" + strconv.Itoa(version)))
	return hex.EncodeToString(sum[:])
}
func isSettingSecret(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "secret") || strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "api_key")
}
func settingType(value any) string {
	switch value.(type) {
	case bool:
		return "boolean"
	case float64, int:
		return "number"
	default:
		return "string"
	}
}
func sanitizeSettings(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		out[key] = sanitizeSetting(key, value)
	}
	return out
}
func sanitizeSetting(key string, value any) any {
	if isSettingSecret(key) {
		return map[string]any{"configured": value != nil && value != ""}
	}
	return value
}
