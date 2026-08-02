package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"roundtable/internal/config"
	"roundtable/internal/db"
)

type Adapter interface {
	Name() string
	Capabilities(context.Context) AdapterCapabilities
	Start(context.Context, StartAgentRequest) (LaunchSession, error)
	Resume(context.Context, ResumeAgentRequest) (LaunchSession, error)
	Stop(context.Context, string) error
}

type AdapterCapabilities struct {
	SupportsResume            bool `json:"supports_resume"`
	SupportsMCP               bool `json:"supports_mcp"`
	SupportsReadOnlyWorkspace bool `json:"supports_readonly_workspace"`
	SupportsSessionCapture    bool `json:"supports_session_capture"`
}

type StartAgentRequest struct {
	SessionID         string
	AgentID           string
	RunID             string
	TaskID            string
	Role              string
	Prompt            string
	WorkingDirectory  string
	MCPSocket         string
	Provider          string
	Model             string
	ExternalSessionID string
}

type ResumeAgentRequest struct {
	SessionID         string
	AgentID           string
	RunID             string
	TaskID            string
	Role              string
	WorkingDirectory  string
	MCPSocket         string
	Provider          string
	Model             string
	ExternalSessionID string
	ResumeBriefingMD  string
}

type LaunchSession struct {
	Session db.AgentSession
	Command []string
}

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(cfg config.Config) *Registry {
	adapters := make(map[string]Adapter, len(cfg.Adapters))
	for name, adapterCfg := range cfg.Adapters {
		adapters[name] = NewCLIAdapter(name, adapterCfg)
	}
	return &Registry{adapters: adapters}
}

func (r *Registry) Get(name string) (Adapter, bool) {
	adapter, ok := r.adapters[name]
	return adapter, ok
}

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type CLIAdapter struct {
	name string
	cfg  config.AdapterConfig
}

func NewCLIAdapter(name string, cfg config.AdapterConfig) *CLIAdapter {
	return &CLIAdapter{name: name, cfg: cfg}
}

func (a *CLIAdapter) Name() string {
	return a.name
}

func (a *CLIAdapter) Capabilities(context.Context) AdapterCapabilities {
	return AdapterCapabilities{
		SupportsResume:            a.cfg.SupportsResume,
		SupportsMCP:               a.cfg.SupportsMCP,
		SupportsReadOnlyWorkspace: a.cfg.SupportsReadOnlyWorkspace,
		SupportsSessionCapture:    a.cfg.CapturesSessionID,
	}
}

func (a *CLIAdapter) Start(_ context.Context, req StartAgentRequest) (LaunchSession, error) {
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.WorkingDirectory) == "" {
		return LaunchSession{}, errors.New("session_id, agent_id, run_id, and working_directory are required")
	}
	session := db.AgentSession{
		ID:                    req.SessionID,
		AgentID:               req.AgentID,
		RunID:                 req.RunID,
		Adapter:               a.name,
		Provider:              req.Provider,
		Model:                 req.Model,
		ExternalSessionID:     req.ExternalSessionID,
		ExternalResumeCommand: a.resumeCommand(req.ExternalSessionID),
		WorkingDirectory:      req.WorkingDirectory,
		MCPSocket:             req.MCPSocket,
		Status:                "starting",
		LastSeenAt:            time.Now().UTC().Format(time.RFC3339),
		MetadataJSON:          mustJSON(map[string]any{"mode": "start", "role": req.Role, "task_id": req.TaskID}),
	}
	return LaunchSession{
		Session: session,
		Command: a.startCommand(req),
	}, nil
}

func (a *CLIAdapter) Resume(_ context.Context, req ResumeAgentRequest) (LaunchSession, error) {
	if !a.cfg.SupportsResume {
		return LaunchSession{}, fmt.Errorf("adapter %s does not support resume", a.name)
	}
	if strings.TrimSpace(req.ExternalSessionID) == "" {
		return LaunchSession{}, errors.New("external_session_id is required for resume")
	}
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.WorkingDirectory) == "" {
		return LaunchSession{}, errors.New("session_id, agent_id, run_id, and working_directory are required")
	}
	session := db.AgentSession{
		ID:                    req.SessionID,
		AgentID:               req.AgentID,
		RunID:                 req.RunID,
		Adapter:               a.name,
		Provider:              req.Provider,
		Model:                 req.Model,
		ExternalSessionID:     req.ExternalSessionID,
		ExternalResumeCommand: a.resumeCommand(req.ExternalSessionID),
		WorkingDirectory:      req.WorkingDirectory,
		MCPSocket:             req.MCPSocket,
		Status:                "resuming",
		LastSeenAt:            time.Now().UTC().Format(time.RFC3339),
		MetadataJSON:          mustJSON(map[string]any{"mode": "resume", "role": req.Role, "task_id": req.TaskID, "briefing_md": req.ResumeBriefingMD}),
	}
	return LaunchSession{
		Session: session,
		Command: a.resumeArgs(req.ExternalSessionID),
	}, nil
}

func (a *CLIAdapter) Stop(context.Context, string) error {
	return nil
}

func (a *CLIAdapter) startCommand(req StartAgentRequest) []string {
	command := strings.TrimSpace(a.cfg.Command)
	if command == "" {
		command = a.name
	}
	args := []string{command}
	if req.Model != "" && a.name != "generic" {
		args = append(args, "--model", req.Model)
	}
	if req.Prompt != "" && a.name != "generic" {
		args = append(args, "--prompt", req.Prompt)
	}
	return args
}

func (a *CLIAdapter) resumeArgs(externalSessionID string) []string {
	command := a.resumeCommand(externalSessionID)
	if command == "" {
		return []string{strings.TrimSpace(a.cfg.Command)}
	}
	return strings.Fields(command)
}

func (a *CLIAdapter) resumeCommand(externalSessionID string) string {
	if externalSessionID == "" || a.cfg.ResumePattern == "" {
		return ""
	}
	return strings.ReplaceAll(a.cfg.ResumePattern, "{{external_session_id}}", externalSessionID)
}

func mustJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}
