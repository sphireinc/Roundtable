package db

import (
	"context"
	"database/sql"
	"fmt"

	"roundtable/internal/events"
)

type Store struct {
	db       *sql.DB
	eventBus *events.Bus[Event]
}

func NewStore(db *sql.DB, eventBus *events.Bus[Event]) *Store {
	if eventBus == nil {
		eventBus = events.NewBus[Event]()
	}
	return &Store{db: db, eventBus: eventBus}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Events() *events.Bus[Event] {
	return s.eventBus
}

func (s *Store) UpsertWorkspace(ctx context.Context, workspace Workspace) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workspaces (
			id, name, root_path, status, display_name, root_alias,
			canonical_repository_identity, default_branch, last_opened_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			root_path = excluded.root_path,
			status = excluded.status,
			display_name = excluded.display_name,
			root_alias = excluded.root_alias,
			canonical_repository_identity = excluded.canonical_repository_identity,
			default_branch = excluded.default_branch,
			last_opened_at = excluded.last_opened_at,
			updated_at = CURRENT_TIMESTAMP
	`, workspace.ID, workspace.DisplayName, workspace.RootPath, defaultIfEmpty(workspace.Status, "active"), workspace.DisplayName, nullIfEmpty(workspace.RootAlias), nullIfEmpty(workspace.CanonicalRepositoryIdentity), nullIfEmpty(workspace.DefaultBranch), nullIfEmpty(workspace.LastOpenedAt))
	return wrapErr("upsert workspace", err)
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (Workspace, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(display_name, name), COALESCE(root_alias, ''),
		       COALESCE(canonical_repository_identity, ''), status,
		       COALESCE(default_branch, ''), root_path, created_at,
		       COALESCE(last_opened_at, ''), updated_at
		FROM workspaces WHERE id = ?
	`, id)
	var workspace Workspace
	if err := row.Scan(&workspace.ID, &workspace.DisplayName, &workspace.RootAlias, &workspace.CanonicalRepositoryIdentity, &workspace.Status, &workspace.DefaultBranch, &workspace.RootPath, &workspace.CreatedAt, &workspace.LastOpenedAt, &workspace.UpdatedAt); err != nil {
		return Workspace{}, fmt.Errorf("get workspace %s: %w", id, err)
	}
	return workspace, nil
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(display_name, name), COALESCE(root_alias, ''),
		       COALESCE(canonical_repository_identity, ''), status,
		       COALESCE(default_branch, ''), root_path, created_at,
		       COALESCE(last_opened_at, ''), updated_at
		FROM workspaces ORDER BY display_name, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var workspace Workspace
		if err := rows.Scan(&workspace.ID, &workspace.DisplayName, &workspace.RootAlias, &workspace.CanonicalRepositoryIdentity, &workspace.Status, &workspace.DefaultBranch, &workspace.RootPath, &workspace.CreatedAt, &workspace.LastOpenedAt, &workspace.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan workspace: %w", err)
		}
		out = append(out, workspace)
	}
	return out, rows.Err()
}

func (s *Store) DetachWorkspace(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE workspaces SET status = 'detached', updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	if err != nil {
		return wrapErr("detach workspace", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("detach workspace %s: %w", id, sql.ErrNoRows)
	}
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, event Event) (Event, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO events (run_id, type, actor_id, task_id, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, event.RunID, event.Type, nullIfEmpty(event.ActorID), nullIfEmpty(event.TaskID), event.PayloadJSON)
	if err != nil {
		return Event{}, fmt.Errorf("insert event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Event{}, fmt.Errorf("event last insert id: %w", err)
	}
	saved, err := s.GetEvent(ctx, id)
	if err != nil {
		return Event{}, err
	}
	s.eventBus.Publish(saved)
	return saved, nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, run_id, type, COALESCE(actor_id, ''), COALESCE(task_id, ''), payload_json, created_at
		FROM events
		WHERE id = ?
	`, id)
	var event Event
	if err := row.Scan(&event.ID, &event.RunID, &event.Type, &event.ActorID, &event.TaskID, &event.PayloadJSON, &event.CreatedAt); err != nil {
		return Event{}, fmt.Errorf("get event %d: %w", id, err)
	}
	return event, nil
}

func (s *Store) ListEvents(ctx context.Context, runID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, type, COALESCE(actor_id, ''), COALESCE(task_id, ''), payload_json, created_at
		FROM events
		WHERE (? = '' OR run_id = ?)
		ORDER BY id
	`, runID, runID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.RunID, &event.Type, &event.ActorID, &event.TaskID, &event.PayloadJSON, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func (s *Store) UpsertRun(ctx context.Context, run Run) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (id, goal, status, ended_at, metadata_json)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			goal = excluded.goal,
			status = excluded.status,
			ended_at = excluded.ended_at,
			metadata_json = excluded.metadata_json
	`, run.ID, nullIfEmpty(run.Goal), defaultIfEmpty(run.Status, "active"), nullIfEmpty(run.EndedAt), nullIfEmpty(run.MetadataJSON))
	return wrapErr("upsert run", err)
}

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(goal, ''), status, started_at, COALESCE(ended_at, ''), COALESCE(metadata_json, '')
		FROM runs WHERE id = ?
	`, id)
	var run Run
	if err := row.Scan(&run.ID, &run.Goal, &run.Status, &run.StartedAt, &run.EndedAt, &run.MetadataJSON); err != nil {
		return Run{}, fmt.Errorf("get run %s: %w", id, err)
	}
	return run, nil
}

func (s *Store) ListRuns(ctx context.Context) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(goal, ''), status, started_at, COALESCE(ended_at, ''), COALESCE(metadata_json, '')
		FROM runs ORDER BY started_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var run Run
		if err := rows.Scan(&run.ID, &run.Goal, &run.Status, &run.StartedAt, &run.EndedAt, &run.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAgent(ctx context.Context, agent Agent) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agents (id, role, name, adapter, command, status, is_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			role = excluded.role,
			name = excluded.name,
			adapter = excluded.adapter,
			command = excluded.command,
			status = excluded.status,
			is_enabled = excluded.is_enabled
	`, agent.ID, agent.Role, agent.Name, agent.Adapter, nullIfEmpty(agent.Command), defaultIfEmpty(agent.Status, "idle"), boolToInt(agent.IsEnabled))
	return wrapErr("upsert agent", err)
}

func (s *Store) GetAgent(ctx context.Context, id string) (Agent, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, role, name, adapter, COALESCE(command, ''), status, is_enabled, created_at
		FROM agents WHERE id = ?
	`, id)
	var agent Agent
	var enabled int
	if err := row.Scan(&agent.ID, &agent.Role, &agent.Name, &agent.Adapter, &agent.Command, &agent.Status, &enabled, &agent.CreatedAt); err != nil {
		return Agent{}, fmt.Errorf("get agent %s: %w", id, err)
	}
	agent.IsEnabled = enabled != 0
	return agent, nil
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, name, adapter, COALESCE(command, ''), status, is_enabled, created_at
		FROM agents ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	var out []Agent
	for rows.Next() {
		var agent Agent
		var enabled int
		if err := rows.Scan(&agent.ID, &agent.Role, &agent.Name, &agent.Adapter, &agent.Command, &agent.Status, &enabled, &agent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		agent.IsEnabled = enabled != 0
		out = append(out, agent)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAgentSession(ctx context.Context, session AgentSession) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_sessions (
			id, agent_id, run_id, adapter, provider, model, external_session_id, external_resume_command,
			working_directory, mcp_socket, status, last_seen_at, ended_at, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id = excluded.agent_id,
			run_id = excluded.run_id,
			adapter = excluded.adapter,
			provider = excluded.provider,
			model = excluded.model,
			external_session_id = excluded.external_session_id,
			external_resume_command = excluded.external_resume_command,
			working_directory = excluded.working_directory,
			mcp_socket = excluded.mcp_socket,
			status = excluded.status,
			last_seen_at = excluded.last_seen_at,
			ended_at = excluded.ended_at,
			metadata_json = excluded.metadata_json
	`, session.ID, session.AgentID, session.RunID, session.Adapter, nullIfEmpty(session.Provider), nullIfEmpty(session.Model), nullIfEmpty(session.ExternalSessionID), nullIfEmpty(session.ExternalResumeCommand), session.WorkingDirectory, nullIfEmpty(session.MCPSocket), defaultIfEmpty(session.Status, "active"), nullIfEmpty(session.LastSeenAt), nullIfEmpty(session.EndedAt), nullIfEmpty(session.MetadataJSON))
	return wrapErr("upsert agent session", err)
}

func (s *Store) GetAgentSession(ctx context.Context, id string) (AgentSession, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, run_id, adapter, COALESCE(provider, ''), COALESCE(model, ''),
		       COALESCE(external_session_id, ''), COALESCE(external_resume_command, ''),
		       working_directory, COALESCE(mcp_socket, ''), status, started_at,
		       COALESCE(last_seen_at, ''), COALESCE(ended_at, ''), COALESCE(metadata_json, '')
		FROM agent_sessions WHERE id = ?
	`, id)
	var v AgentSession
	if err := row.Scan(&v.ID, &v.AgentID, &v.RunID, &v.Adapter, &v.Provider, &v.Model, &v.ExternalSessionID, &v.ExternalResumeCommand, &v.WorkingDirectory, &v.MCPSocket, &v.Status, &v.StartedAt, &v.LastSeenAt, &v.EndedAt, &v.MetadataJSON); err != nil {
		return AgentSession{}, fmt.Errorf("get agent session %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListAgentSessions(ctx context.Context) ([]AgentSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, run_id, adapter, COALESCE(provider, ''), COALESCE(model, ''),
		       COALESCE(external_session_id, ''), COALESCE(external_resume_command, ''),
		       working_directory, COALESCE(mcp_socket, ''), status, started_at,
		       COALESCE(last_seen_at, ''), COALESCE(ended_at, ''), COALESCE(metadata_json, '')
		FROM agent_sessions ORDER BY started_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", err)
	}
	defer rows.Close()
	var out []AgentSession
	for rows.Next() {
		var v AgentSession
		if err := rows.Scan(&v.ID, &v.AgentID, &v.RunID, &v.Adapter, &v.Provider, &v.Model, &v.ExternalSessionID, &v.ExternalResumeCommand, &v.WorkingDirectory, &v.MCPSocket, &v.Status, &v.StartedAt, &v.LastSeenAt, &v.EndedAt, &v.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan agent session: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) AppendAgentSessionEvent(ctx context.Context, event AgentSessionEvent) (AgentSessionEvent, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_session_events (session_id, event_type, payload_json)
		VALUES (?, ?, ?)
	`, event.SessionID, event.EventType, event.PayloadJSON)
	if err != nil {
		return AgentSessionEvent{}, fmt.Errorf("insert agent session event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AgentSessionEvent{}, fmt.Errorf("agent session event last insert id: %w", err)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, event_type, payload_json, created_at
		FROM agent_session_events WHERE id = ?
	`, id)
	var saved AgentSessionEvent
	if err := row.Scan(&saved.ID, &saved.SessionID, &saved.EventType, &saved.PayloadJSON, &saved.CreatedAt); err != nil {
		return AgentSessionEvent{}, fmt.Errorf("get agent session event %d: %w", id, err)
	}
	return saved, nil
}

func (s *Store) ListAgentSessionEvents(ctx context.Context, sessionID string) ([]AgentSessionEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, event_type, payload_json, created_at
		FROM agent_session_events WHERE session_id = ?
		ORDER BY id
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list agent session events: %w", err)
	}
	defer rows.Close()
	var out []AgentSessionEvent
	for rows.Next() {
		var v AgentSessionEvent
		if err := rows.Scan(&v.ID, &v.SessionID, &v.EventType, &v.PayloadJSON, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan agent session event: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAdapterCapability(ctx context.Context, cap AdapterCapability) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO adapter_capabilities (adapter, supports_resume, supports_mcp, supports_readonly_workspace, supports_session_capture, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(adapter) DO UPDATE SET
			supports_resume = excluded.supports_resume,
			supports_mcp = excluded.supports_mcp,
			supports_readonly_workspace = excluded.supports_readonly_workspace,
			supports_session_capture = excluded.supports_session_capture,
			metadata_json = excluded.metadata_json
	`, cap.Adapter, boolToInt(cap.SupportsResume), boolToInt(cap.SupportsMCP), boolToInt(cap.SupportsReadOnlyWorkspace), boolToInt(cap.SupportsSessionCapture), nullIfEmpty(cap.MetadataJSON))
	return wrapErr("upsert adapter capability", err)
}

func (s *Store) GetAdapterCapability(ctx context.Context, adapter string) (AdapterCapability, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT adapter, supports_resume, supports_mcp, supports_readonly_workspace, supports_session_capture, COALESCE(metadata_json, '')
		FROM adapter_capabilities WHERE adapter = ?
	`, adapter)
	var v AdapterCapability
	var sr, sm, sro, ssc int
	if err := row.Scan(&v.Adapter, &sr, &sm, &sro, &ssc, &v.MetadataJSON); err != nil {
		return AdapterCapability{}, fmt.Errorf("get adapter capability %s: %w", adapter, err)
	}
	v.SupportsResume = sr != 0
	v.SupportsMCP = sm != 0
	v.SupportsReadOnlyWorkspace = sro != 0
	v.SupportsSessionCapture = ssc != 0
	return v, nil
}

func (s *Store) ListAdapterCapabilities(ctx context.Context) ([]AdapterCapability, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT adapter, supports_resume, supports_mcp, supports_readonly_workspace, supports_session_capture, COALESCE(metadata_json, '')
		FROM adapter_capabilities ORDER BY adapter
	`)
	if err != nil {
		return nil, fmt.Errorf("list adapter capabilities: %w", err)
	}
	defer rows.Close()
	var out []AdapterCapability
	for rows.Next() {
		var v AdapterCapability
		var sr, sm, sro, ssc int
		if err := rows.Scan(&v.Adapter, &sr, &sm, &sro, &ssc, &v.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan adapter capability: %w", err)
		}
		v.SupportsResume = sr != 0
		v.SupportsMCP = sm != 0
		v.SupportsReadOnlyWorkspace = sro != 0
		v.SupportsSessionCapture = ssc != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) InsertRunSnapshot(ctx context.Context, snapshot RunSnapshot) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO run_snapshots (id, run_id, snapshot_json)
		VALUES (?, ?, ?)
	`, snapshot.ID, snapshot.RunID, snapshot.SnapshotJSON)
	return wrapErr("insert run snapshot", err)
}

func (s *Store) ListRunSnapshots(ctx context.Context, runID string) ([]RunSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, run_id, snapshot_json, created_at
		FROM run_snapshots WHERE run_id = ?
		ORDER BY created_at, id
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list run snapshots: %w", err)
	}
	defer rows.Close()
	var out []RunSnapshot
	for rows.Next() {
		var v RunSnapshot
		if err := rows.Scan(&v.ID, &v.RunID, &v.SnapshotJSON, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan run snapshot: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertTask(ctx context.Context, task Task) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (id, title, body_md, status, priority, risk, assigned_agent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title = excluded.title,
			body_md = excluded.body_md,
			status = excluded.status,
			priority = excluded.priority,
			risk = excluded.risk,
			assigned_agent_id = excluded.assigned_agent_id,
			updated_at = CURRENT_TIMESTAMP
	`, task.ID, task.Title, task.BodyMD, defaultIfEmpty(task.Status, "open"), defaultIfZero(task.Priority, 100), defaultIfEmpty(task.Risk, "normal"), nullIfEmpty(task.AssignedAgentID))
	return wrapErr("upsert task", err)
}

func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, body_md, status, priority, risk, COALESCE(assigned_agent_id, ''), created_at, updated_at
		FROM tasks WHERE id = ?
	`, id)
	var v Task
	if err := row.Scan(&v.ID, &v.Title, &v.BodyMD, &v.Status, &v.Priority, &v.Risk, &v.AssignedAgentID, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return Task{}, fmt.Errorf("get task %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, body_md, status, priority, risk, COALESCE(assigned_agent_id, ''), created_at, updated_at
		FROM tasks ORDER BY priority, created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var v Task
		if err := rows.Scan(&v.ID, &v.Title, &v.BodyMD, &v.Status, &v.Priority, &v.Risk, &v.AssignedAgentID, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertResource(ctx context.Context, resource Resource) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO resources (id, type, path, symbol, language, start_line, end_line, current_hash, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			path = excluded.path,
			symbol = excluded.symbol,
			language = excluded.language,
			start_line = excluded.start_line,
			end_line = excluded.end_line,
			current_hash = excluded.current_hash,
			metadata_json = excluded.metadata_json
	`, resource.ID, resource.Type, nullIfEmpty(resource.Path), nullIfEmpty(resource.Symbol), nullIfEmpty(resource.Language), zeroToNull(resource.StartLine), zeroToNull(resource.EndLine), nullIfEmpty(resource.CurrentHash), nullIfEmpty(resource.MetadataJSON))
	return wrapErr("upsert resource", err)
}

func (s *Store) GetResource(ctx context.Context, id string) (Resource, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, COALESCE(path, ''), COALESCE(symbol, ''), COALESCE(language, ''),
		       COALESCE(start_line, 0), COALESCE(end_line, 0), COALESCE(current_hash, ''), COALESCE(metadata_json, '')
		FROM resources WHERE id = ?
	`, id)
	var v Resource
	if err := row.Scan(&v.ID, &v.Type, &v.Path, &v.Symbol, &v.Language, &v.StartLine, &v.EndLine, &v.CurrentHash, &v.MetadataJSON); err != nil {
		return Resource{}, fmt.Errorf("get resource %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListResources(ctx context.Context) ([]Resource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, COALESCE(path, ''), COALESCE(symbol, ''), COALESCE(language, ''),
		       COALESCE(start_line, 0), COALESCE(end_line, 0), COALESCE(current_hash, ''), COALESCE(metadata_json, '')
		FROM resources ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	defer rows.Close()
	var out []Resource
	for rows.Next() {
		var v Resource
		if err := rows.Scan(&v.ID, &v.Type, &v.Path, &v.Symbol, &v.Language, &v.StartLine, &v.EndLine, &v.CurrentHash, &v.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan resource: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertClaim(ctx context.Context, claim Claim) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO claims (id, resource_id, agent_id, task_id, claim_type, base_hash, status, expires_at, heartbeat_at, renewable, resume_policy, rationale_md)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			resource_id = excluded.resource_id,
			agent_id = excluded.agent_id,
			task_id = excluded.task_id,
			claim_type = excluded.claim_type,
			base_hash = excluded.base_hash,
			status = excluded.status,
			expires_at = excluded.expires_at,
			heartbeat_at = excluded.heartbeat_at,
			renewable = excluded.renewable,
			resume_policy = excluded.resume_policy,
			rationale_md = excluded.rationale_md
	`, claim.ID, claim.ResourceID, claim.AgentID, claim.TaskID, claim.ClaimType, nullIfEmpty(claim.BaseHash), defaultIfEmpty(claim.Status, "active"), claim.ExpiresAt, nullIfEmpty(claim.HeartbeatAt), boolToInt(claim.Renewable), defaultIfEmpty(claim.ResumePolicy, "hold"), nullIfEmpty(claim.RationaleMD))
	return wrapErr("upsert claim", err)
}

func (s *Store) GetClaim(ctx context.Context, id string) (Claim, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, resource_id, agent_id, task_id, claim_type, COALESCE(base_hash, ''), status, expires_at,
		       COALESCE(heartbeat_at, ''), renewable, resume_policy, COALESCE(rationale_md, ''), created_at
		FROM claims WHERE id = ?
	`, id)
	var v Claim
	var renewable int
	if err := row.Scan(&v.ID, &v.ResourceID, &v.AgentID, &v.TaskID, &v.ClaimType, &v.BaseHash, &v.Status, &v.ExpiresAt, &v.HeartbeatAt, &renewable, &v.ResumePolicy, &v.RationaleMD, &v.CreatedAt); err != nil {
		return Claim{}, fmt.Errorf("get claim %s: %w", id, err)
	}
	v.Renewable = renewable != 0
	return v, nil
}

func (s *Store) ListClaims(ctx context.Context) ([]Claim, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, resource_id, agent_id, task_id, claim_type, COALESCE(base_hash, ''), status, expires_at,
		       COALESCE(heartbeat_at, ''), renewable, resume_policy, COALESCE(rationale_md, ''), created_at
		FROM claims ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list claims: %w", err)
	}
	defer rows.Close()
	var out []Claim
	for rows.Next() {
		var v Claim
		var renewable int
		if err := rows.Scan(&v.ID, &v.ResourceID, &v.AgentID, &v.TaskID, &v.ClaimType, &v.BaseHash, &v.Status, &v.ExpiresAt, &v.HeartbeatAt, &renewable, &v.ResumePolicy, &v.RationaleMD, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan claim: %w", err)
		}
		v.Renewable = renewable != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertProposal(ctx context.Context, proposal Proposal) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO proposals (id, task_id, agent_id, title, summary_md, patch_path, status, risk)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task_id = excluded.task_id,
			agent_id = excluded.agent_id,
			title = excluded.title,
			summary_md = excluded.summary_md,
			patch_path = excluded.patch_path,
			status = excluded.status,
			risk = excluded.risk
	`, proposal.ID, proposal.TaskID, proposal.AgentID, proposal.Title, proposal.SummaryMD, proposal.PatchPath, defaultIfEmpty(proposal.Status, "pending"), defaultIfEmpty(proposal.Risk, "normal"))
	return wrapErr("upsert proposal", err)
}

func (s *Store) GetProposal(ctx context.Context, id string) (Proposal, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, agent_id, title, summary_md, patch_path, status, risk, created_at
		FROM proposals WHERE id = ?
	`, id)
	var v Proposal
	if err := row.Scan(&v.ID, &v.TaskID, &v.AgentID, &v.Title, &v.SummaryMD, &v.PatchPath, &v.Status, &v.Risk, &v.CreatedAt); err != nil {
		return Proposal{}, fmt.Errorf("get proposal %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListProposals(ctx context.Context) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, agent_id, title, summary_md, patch_path, status, risk, created_at
		FROM proposals ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var v Proposal
		if err := rows.Scan(&v.ID, &v.TaskID, &v.AgentID, &v.Title, &v.SummaryMD, &v.PatchPath, &v.Status, &v.Risk, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan proposal: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) ReplaceProposalResources(ctx context.Context, proposalID string, resourceIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace proposal resources: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM proposal_resources WHERE proposal_id = ?`, proposalID); err != nil {
		return fmt.Errorf("delete proposal resources: %w", err)
	}
	for _, resourceID := range resourceIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO proposal_resources (proposal_id, resource_id) VALUES (?, ?)`, proposalID, resourceID); err != nil {
			return fmt.Errorf("insert proposal resource: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListProposalResources(ctx context.Context, proposalID string) ([]ProposalResource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT proposal_id, resource_id
		FROM proposal_resources WHERE proposal_id = ?
		ORDER BY resource_id
	`, proposalID)
	if err != nil {
		return nil, fmt.Errorf("list proposal resources: %w", err)
	}
	defer rows.Close()
	var out []ProposalResource
	for rows.Next() {
		var v ProposalResource
		if err := rows.Scan(&v.ProposalID, &v.ResourceID); err != nil {
			return nil, fmt.Errorf("scan proposal resource: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertVote(ctx context.Context, vote Vote) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO votes (id, proposal_id, agent_id, vote, confidence, reason_md)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			proposal_id = excluded.proposal_id,
			agent_id = excluded.agent_id,
			vote = excluded.vote,
			confidence = excluded.confidence,
			reason_md = excluded.reason_md
	`, vote.ID, vote.ProposalID, vote.AgentID, vote.Vote, zeroFloatToNull(vote.Confidence), nullIfEmpty(vote.ReasonMD))
	return wrapErr("upsert vote", err)
}

func (s *Store) GetVote(ctx context.Context, id string) (Vote, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, proposal_id, agent_id, vote, COALESCE(confidence, 0), COALESCE(reason_md, ''), created_at
		FROM votes WHERE id = ?
	`, id)
	var v Vote
	if err := row.Scan(&v.ID, &v.ProposalID, &v.AgentID, &v.Vote, &v.Confidence, &v.ReasonMD, &v.CreatedAt); err != nil {
		return Vote{}, fmt.Errorf("get vote %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListVotes(ctx context.Context, proposalID string) ([]Vote, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, proposal_id, agent_id, vote, COALESCE(confidence, 0), COALESCE(reason_md, ''), created_at
		FROM votes WHERE (? = '' OR proposal_id = ?)
		ORDER BY created_at, id
	`, proposalID, proposalID)
	if err != nil {
		return nil, fmt.Errorf("list votes: %w", err)
	}
	defer rows.Close()
	var out []Vote
	for rows.Next() {
		var v Vote
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.AgentID, &v.Vote, &v.Confidence, &v.ReasonMD, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan vote: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertDecision(ctx context.Context, decision Decision) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO decisions (id, proposal_id, task_id, decision, rationale_md, decided_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			proposal_id = excluded.proposal_id,
			task_id = excluded.task_id,
			decision = excluded.decision,
			rationale_md = excluded.rationale_md,
			decided_by = excluded.decided_by
	`, decision.ID, nullIfEmpty(decision.ProposalID), nullIfEmpty(decision.TaskID), decision.Decision, decision.RationaleMD, decision.DecidedBy)
	return wrapErr("upsert decision", err)
}

func (s *Store) GetDecision(ctx context.Context, id string) (Decision, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(task_id, ''), decision, rationale_md, decided_by, created_at
		FROM decisions WHERE id = ?
	`, id)
	var v Decision
	if err := row.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Decision, &v.RationaleMD, &v.DecidedBy, &v.CreatedAt); err != nil {
		return Decision{}, fmt.Errorf("get decision %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListDecisions(ctx context.Context) ([]Decision, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(task_id, ''), decision, rationale_md, decided_by, created_at
		FROM decisions ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list decisions: %w", err)
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var v Decision
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Decision, &v.RationaleMD, &v.DecidedBy, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan decision: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertTransaction(ctx context.Context, transaction Transaction) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO transactions (id, proposal_id, run_id, before_git_hash, after_git_hash, status, applied_by, applied_at, rollback_patch_path, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			proposal_id = excluded.proposal_id,
			run_id = excluded.run_id,
			before_git_hash = excluded.before_git_hash,
			after_git_hash = excluded.after_git_hash,
			status = excluded.status,
			applied_by = excluded.applied_by,
			applied_at = excluded.applied_at,
			rollback_patch_path = excluded.rollback_patch_path,
			metadata_json = excluded.metadata_json
	`, transaction.ID, transaction.ProposalID, transaction.RunID, transaction.BeforeGitHash, nullIfEmpty(transaction.AfterGitHash), defaultIfEmpty(transaction.Status, "pending"), defaultIfEmpty(transaction.AppliedBy, "orchestrator"), nullIfEmpty(transaction.AppliedAt), nullIfEmpty(transaction.RollbackPatchPath), nullIfEmpty(transaction.MetadataJSON))
	return wrapErr("upsert transaction", err)
}

func (s *Store) GetTransaction(ctx context.Context, id string) (Transaction, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, proposal_id, run_id, before_git_hash, COALESCE(after_git_hash, ''), status, applied_by,
		       COALESCE(applied_at, ''), COALESCE(rollback_patch_path, ''), COALESCE(metadata_json, '')
		FROM transactions WHERE id = ?
	`, id)
	var v Transaction
	if err := row.Scan(&v.ID, &v.ProposalID, &v.RunID, &v.BeforeGitHash, &v.AfterGitHash, &v.Status, &v.AppliedBy, &v.AppliedAt, &v.RollbackPatchPath, &v.MetadataJSON); err != nil {
		return Transaction{}, fmt.Errorf("get transaction %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListTransactions(ctx context.Context) ([]Transaction, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, proposal_id, run_id, before_git_hash, COALESCE(after_git_hash, ''), status, applied_by,
		       COALESCE(applied_at, ''), COALESCE(rollback_patch_path, ''), COALESCE(metadata_json, '')
		FROM transactions ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer rows.Close()
	var out []Transaction
	for rows.Next() {
		var v Transaction
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.RunID, &v.BeforeGitHash, &v.AfterGitHash, &v.Status, &v.AppliedBy, &v.AppliedAt, &v.RollbackPatchPath, &v.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan transaction: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertHumanApproval(ctx context.Context, approval HumanApproval) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO human_approvals (
			id, proposal_id, task_id, subject, reason_md, status, requested_by,
			decision_md, decided_by, override_policy, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			proposal_id = excluded.proposal_id,
			task_id = excluded.task_id,
			subject = excluded.subject,
			reason_md = excluded.reason_md,
			status = excluded.status,
			requested_by = excluded.requested_by,
			decision_md = excluded.decision_md,
			decided_by = excluded.decided_by,
			override_policy = excluded.override_policy,
			updated_at = CURRENT_TIMESTAMP
	`, approval.ID, nullIfEmpty(approval.ProposalID), nullIfEmpty(approval.TaskID), approval.Subject, approval.ReasonMD, defaultIfEmpty(approval.Status, "requested"), defaultIfEmpty(approval.RequestedBy, "orchestrator"), nullIfEmpty(approval.DecisionMD), nullIfEmpty(approval.DecidedBy), boolToInt(approval.OverridePolicy))
	return wrapErr("upsert human approval", err)
}

func (s *Store) GetHumanApproval(ctx context.Context, id string) (HumanApproval, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(task_id, ''), subject, reason_md, status,
		       requested_by, COALESCE(decision_md, ''), COALESCE(decided_by, ''), override_policy,
		       created_at, updated_at
		FROM human_approvals WHERE id = ?
	`, id)
	var v HumanApproval
	var override int
	if err := row.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Subject, &v.ReasonMD, &v.Status, &v.RequestedBy, &v.DecisionMD, &v.DecidedBy, &override, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return HumanApproval{}, fmt.Errorf("get human approval %s: %w", id, err)
	}
	v.OverridePolicy = override != 0
	return v, nil
}

func (s *Store) ListHumanApprovals(ctx context.Context, proposalID string) ([]HumanApproval, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(task_id, ''), subject, reason_md, status,
		       requested_by, COALESCE(decision_md, ''), COALESCE(decided_by, ''), override_policy,
		       created_at, updated_at
		FROM human_approvals
		WHERE (? = '' OR proposal_id = ?)
		ORDER BY created_at, id
	`, proposalID, proposalID)
	if err != nil {
		return nil, fmt.Errorf("list human approvals: %w", err)
	}
	defer rows.Close()
	var out []HumanApproval
	for rows.Next() {
		var v HumanApproval
		var override int
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Subject, &v.ReasonMD, &v.Status, &v.RequestedBy, &v.DecisionMD, &v.DecidedBy, &override, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan human approval: %w", err)
		}
		v.OverridePolicy = override != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertSecurityReview(ctx context.Context, review SecurityReview) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO security_reviews (
			id, proposal_id, resource_id, task_id, reviewer_id, status, summary_md, findings_json, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			proposal_id = excluded.proposal_id,
			resource_id = excluded.resource_id,
			task_id = excluded.task_id,
			reviewer_id = excluded.reviewer_id,
			status = excluded.status,
			summary_md = excluded.summary_md,
			findings_json = excluded.findings_json,
			updated_at = CURRENT_TIMESTAMP
	`, review.ID, nullIfEmpty(review.ProposalID), nullIfEmpty(review.ResourceID), nullIfEmpty(review.TaskID), defaultIfEmpty(review.ReviewerID, "security"), defaultIfEmpty(review.Status, "approved"), review.SummaryMD, nullIfEmpty(review.FindingsJSON))
	return wrapErr("upsert security review", err)
}

func (s *Store) GetSecurityReview(ctx context.Context, id string) (SecurityReview, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(resource_id, ''), COALESCE(task_id, ''),
		       reviewer_id, status, summary_md, COALESCE(findings_json, ''), created_at, updated_at
		FROM security_reviews WHERE id = ?
	`, id)
	var v SecurityReview
	if err := row.Scan(&v.ID, &v.ProposalID, &v.ResourceID, &v.TaskID, &v.ReviewerID, &v.Status, &v.SummaryMD, &v.FindingsJSON, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return SecurityReview{}, fmt.Errorf("get security review %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListSecurityReviews(ctx context.Context, proposalID string) ([]SecurityReview, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(resource_id, ''), COALESCE(task_id, ''),
		       reviewer_id, status, summary_md, COALESCE(findings_json, ''), created_at, updated_at
		FROM security_reviews
		WHERE (? = '' OR proposal_id = ?)
		ORDER BY created_at, id
	`, proposalID, proposalID)
	if err != nil {
		return nil, fmt.Errorf("list security reviews: %w", err)
	}
	defer rows.Close()
	var out []SecurityReview
	for rows.Next() {
		var v SecurityReview
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.ResourceID, &v.TaskID, &v.ReviewerID, &v.Status, &v.SummaryMD, &v.FindingsJSON, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan security review: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertTestRun(ctx context.Context, testRun TestRun) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO test_runs (id, proposal_id, task_id, command, status, summary_md, log_path)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			proposal_id = excluded.proposal_id,
			task_id = excluded.task_id,
			command = excluded.command,
			status = excluded.status,
			summary_md = excluded.summary_md,
			log_path = excluded.log_path
	`, testRun.ID, nullIfEmpty(testRun.ProposalID), nullIfEmpty(testRun.TaskID), testRun.Command, testRun.Status, nullIfEmpty(testRun.SummaryMD), nullIfEmpty(testRun.LogPath))
	return wrapErr("upsert test run", err)
}

func (s *Store) GetTestRun(ctx context.Context, id string) (TestRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(task_id, ''), command, status, COALESCE(summary_md, ''), COALESCE(log_path, ''), created_at
		FROM test_runs WHERE id = ?
	`, id)
	var v TestRun
	if err := row.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Command, &v.Status, &v.SummaryMD, &v.LogPath, &v.CreatedAt); err != nil {
		return TestRun{}, fmt.Errorf("get test run %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListTestRuns(ctx context.Context) ([]TestRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(proposal_id, ''), COALESCE(task_id, ''), command, status, COALESCE(summary_md, ''), COALESCE(log_path, ''), created_at
		FROM test_runs ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list test runs: %w", err)
	}
	defer rows.Close()
	var out []TestRun
	for rows.Next() {
		var v TestRun
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.TaskID, &v.Command, &v.Status, &v.SummaryMD, &v.LogPath, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan test run: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpsertMemoryEntry(ctx context.Context, entry MemoryEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO memory_entries (id, scope, kind, title, body_md, source_event_id, importance, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			scope = excluded.scope,
			kind = excluded.kind,
			title = excluded.title,
			body_md = excluded.body_md,
			source_event_id = excluded.source_event_id,
			importance = excluded.importance,
			status = excluded.status,
			updated_at = CURRENT_TIMESTAMP
	`, entry.ID, entry.Scope, entry.Kind, entry.Title, entry.BodyMD, zeroInt64ToNull(entry.SourceEventID), defaultIfZero(entry.Importance, 50), defaultIfEmpty(entry.Status, "active"))
	return wrapErr("upsert memory entry", err)
}

func (s *Store) GetMemoryEntry(ctx context.Context, id string) (MemoryEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, scope, kind, title, body_md, COALESCE(source_event_id, 0), importance, status, created_at, updated_at
		FROM memory_entries WHERE id = ?
	`, id)
	var v MemoryEntry
	if err := row.Scan(&v.ID, &v.Scope, &v.Kind, &v.Title, &v.BodyMD, &v.SourceEventID, &v.Importance, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return MemoryEntry{}, fmt.Errorf("get memory entry %s: %w", id, err)
	}
	return v, nil
}

func (s *Store) ListMemoryEntries(ctx context.Context) ([]MemoryEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, scope, kind, title, body_md, COALESCE(source_event_id, 0), importance, status, created_at, updated_at
		FROM memory_entries ORDER BY importance DESC, created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list memory entries: %w", err)
	}
	defer rows.Close()
	var out []MemoryEntry
	for rows.Next() {
		var v MemoryEntry
		if err := rows.Scan(&v.ID, &v.Scope, &v.Kind, &v.Title, &v.BodyMD, &v.SourceEventID, &v.Importance, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan memory entry: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func wrapErr(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func defaultIfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultIfZero(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func zeroToNull(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func zeroFloatToNull(value float64) any {
	if value == 0 {
		return nil
	}
	return value
}

func zeroInt64ToNull(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}
