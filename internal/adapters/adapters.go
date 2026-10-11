package adapters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/sandbox"
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

const agentMCPContainerSocket = "/run/roundtable-agent-mcp.sock"
const agentEgressContainerSocket = "/run/roundtable-egress/g"

var validExternalSessionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type StartAgentRequest struct {
	SessionID            string
	AgentID              string
	RunID                string
	TaskID               string
	ProposalID           string
	Role                 string
	Prompt               string
	WorkingDirectory     string
	MCPSocket            string
	Provider             string
	Model                string
	ExternalSessionID    string
	AgentCapabilityToken string
	AgentMCPURL          string
	BaseRevision         string
	BaseStateHash        string
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

func (a *CLIAdapter) Execute(ctx context.Context, runtime sandbox.Runtime, req StartAgentRequest, spec sandbox.Spec) (LaunchSession, sandbox.ExecutionResult, error) {
	if runtime == nil {
		return LaunchSession{}, sandbox.ExecutionResult{}, errors.New("container runtime is required; host execution fallback is disabled")
	}
	if strings.TrimSpace(a.cfg.Image) == "" {
		return LaunchSession{}, sandbox.ExecutionResult{}, fmt.Errorf("adapters.%s.image must identify a trusted agent image before execution", a.name)
	}
	if !a.cfg.SupportsMCP || len(req.AgentCapabilityToken) < 40 || req.BaseRevision == "" || req.BaseStateHash == "" {
		return LaunchSession{}, sandbox.ExecutionResult{}, errors.New("agent execution requires MCP support and a session capability bound to a pinned repository revision")
	}
	if !filepath.IsAbs(spec.AgentEgressSocketPath) || filepath.Clean(spec.AgentEgressSocketPath) != spec.AgentEgressSocketPath {
		return LaunchSession{}, sandbox.ExecutionResult{}, errors.New("agent execution requires a session-scoped provider egress socket")
	}
	mcpURL, err := url.Parse(req.AgentMCPURL)
	if err != nil || (mcpURL.Scheme != "https" && mcpURL.Scheme != "http") || mcpURL.Host == "" || mcpURL.User != nil || mcpURL.RawQuery != "" || mcpURL.Fragment != "" {
		return LaunchSession{}, sandbox.ExecutionResult{}, errors.New("agent execution requires a valid authenticated MCP endpoint URL")
	}
	launch, err := a.Start(ctx, req)
	if err != nil {
		return LaunchSession{}, sandbox.ExecutionResult{}, err
	}
	configEnv, configArgs, err := installAgentMCPConfig(a.name, spec.Temp)
	if err != nil {
		return LaunchSession{}, sandbox.ExecutionResult{}, err
	}
	spec.BootstrapFiles, err = loadAgentMCPConfigFiles(a.name, spec.Temp)
	if err != nil {
		return LaunchSession{}, sandbox.ExecutionResult{}, err
	}
	providerCommand := append(launch.Command, configArgs...)
	launch.Command = append([]string{"roundtable", "agent", "exec", "--egress-socket", agentEgressContainerSocket, "--"}, providerCommand...)
	if spec.Timeout < 0 {
		return LaunchSession{}, sandbox.ExecutionResult{}, errors.New("agent execution timeout must be positive")
	}
	if spec.Timeout == 0 {
		spec.Timeout = 30 * time.Minute
	}
	spec.Image = a.cfg.Image
	spec.Command = launch.Command
	if spec.Env == nil {
		spec.Env = make(map[string]string)
	}
	for name, value := range map[string]string{
		"ROUNDTABLE_AGENT_CAPABILITY": req.AgentCapabilityToken,
		"ROUNDTABLE_AGENT_MCP_URL":    req.AgentMCPURL,
		"ROUNDTABLE_AGENT_MCP_SOCKET": agentMCPContainerSocket,
	} {
		if existing, ok := spec.Env[name]; ok && existing != value {
			return LaunchSession{}, sandbox.ExecutionResult{}, fmt.Errorf("refusing to override scoped agent environment variable %s", name)
		}
		spec.Env[name] = value
		spec.AllowedEnv = appendUnique(spec.AllowedEnv, name)
	}
	for name, value := range configEnv {
		if existing, ok := spec.Env[name]; ok && existing != value {
			return LaunchSession{}, sandbox.ExecutionResult{}, fmt.Errorf("refusing to override isolated provider config variable %s", name)
		}
		spec.Env[name] = value
		spec.AllowedEnv = appendUnique(spec.AllowedEnv, name)
	}
	for _, name := range a.cfg.CredentialEnv {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			if existing, exists := spec.Env[name]; exists && existing != value {
				return LaunchSession{}, sandbox.ExecutionResult{}, fmt.Errorf("refusing to override explicitly assigned provider credential %s", name)
			}
			spec.Env[name] = value
			spec.AllowedEnv = appendUnique(spec.AllowedEnv, name)
		}
	}
	if spec.Name == "" {
		digest := sha256.Sum256([]byte(req.SessionID))
		spec.Name = "rt-agent-" + hex.EncodeToString(digest[:8])
	}
	result, runErr := runtime.Run(ctx, spec)
	for _, name := range a.cfg.CredentialEnv {
		secret := spec.Env[name]
		if secret == "" {
			continue
		}
		result.Stdout = strings.ReplaceAll(result.Stdout, secret, "[REDACTED_PROVIDER_CREDENTIAL]")
		result.Stderr = strings.ReplaceAll(result.Stderr, secret, "[REDACTED_PROVIDER_CREDENTIAL]")
		if runErr != nil {
			runErr = errors.New(strings.ReplaceAll(runErr.Error(), secret, "[REDACTED_PROVIDER_CREDENTIAL]"))
		}
	}
	if a.cfg.CapturesSessionID && !result.OutputTruncated {
		if externalID := parseExternalSessionID(a.name, result.Stdout); externalID != "" {
			launch.Session.ExternalSessionID = externalID
			launch.Session.ExternalResumeCommand = a.resumeCommandWhenSupported(externalID)
		}
	}
	metadata := map[string]any{}
	_ = json.Unmarshal([]byte(launch.Session.MetadataJSON), &metadata)
	metadata["container_runtime"] = runtime.Name()
	metadata["container_name"] = spec.Name
	metadata["container_image"] = spec.Image
	metadata["base_revision"] = req.BaseRevision
	metadata["base_state_hash"] = req.BaseStateHash
	metadata["execution_started_at"] = result.StartedAt.Format(time.RFC3339Nano)
	metadata["execution_ended_at"] = result.EndedAt.Format(time.RFC3339Nano)
	metadata["exit_code"] = result.ExitCode
	metadata["output_truncated"] = result.OutputTruncated
	launch.Session.MetadataJSON = mustJSON(metadata)
	if runErr != nil {
		launch.Session.Status = "failed"
		metadata["failure"] = runErr.Error()
		launch.Session.MetadataJSON = mustJSON(metadata)
		return launch, result, runErr
	}
	launch.Session.Status = "completed"
	return launch, result, nil
}

func loadAgentMCPConfigFiles(adapter, temp string) (map[string][]byte, error) {
	paths := map[string]string{
		"codex":    "codex-home/config.toml",
		"claude":   "roundtable-claude-mcp.json",
		"gemini":   "roundtable-gemini-settings.json",
		"opencode": "roundtable-opencode.json",
	}
	relative, ok := paths[adapter]
	if !ok {
		return nil, fmt.Errorf("adapter %q has no supported session-scoped MCP config", adapter)
	}
	root, err := os.OpenRoot(temp)
	if err != nil {
		return nil, fmt.Errorf("open session temp for MCP config bootstrap: %w", err)
	}
	defer root.Close()
	var file *os.File
	if adapter == "codex" {
		home, err := root.OpenRoot("codex-home")
		if err != nil {
			return nil, err
		}
		defer home.Close()
		file, err = home.Open("config.toml")
	} else {
		file, err = root.Open(relative)
	}
	if err != nil {
		return nil, fmt.Errorf("open generated %s MCP config: %w", adapter, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("generated agent MCP config is not a bounded regular file")
	}
	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{relative: contents}, nil
}

func installAgentMCPConfig(adapter, temp string) (map[string]string, []string, error) {
	if !filepath.IsAbs(temp) || filepath.Clean(temp) != temp {
		return nil, nil, errors.New("agent MCP config requires an absolute clean execution temp directory")
	}
	server := map[string]any{
		"command": "roundtable",
		"args":    []string{"mcp", "stdio", "--unix-socket", agentMCPContainerSocket},
	}
	configEnv := map[string]string{}
	configPath := ""
	var body []byte
	switch adapter {
	case "codex":
		root, err := os.OpenRoot(temp)
		if err != nil {
			return nil, nil, fmt.Errorf("open agent temp for Codex config: %w", err)
		}
		defer root.Close()
		if err := root.Mkdir("codex-home", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, nil, fmt.Errorf("create isolated Codex home: %w", err)
		}
		home, err := root.OpenRoot("codex-home")
		if err != nil {
			return nil, nil, err
		}
		defer home.Close()
		info, err := root.Lstat("codex-home")
		opened, statErr := home.Stat(".")
		if err != nil || statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, opened) {
			return nil, nil, errors.New("isolated Codex home changed during creation")
		}
		body = []byte("[mcp_servers.roundtable]\ncommand = \"roundtable\"\nargs = [\"mcp\", \"stdio\", \"--unix-socket\", \"" + agentMCPContainerSocket + "\"]\n")
		if err := writeAgentMCPConfig(home, "config.toml", body); err != nil {
			return nil, nil, err
		}
		configEnv["CODEX_HOME"] = "/tmp/codex-home"
	case "claude":
		body, _ = json.Marshal(map[string]any{"mcpServers": map[string]any{"roundtable": server}})
		configPath = "roundtable-claude-mcp.json"
	case "gemini":
		body, _ = json.Marshal(map[string]any{"mcpServers": map[string]any{"roundtable": server}})
		configPath = "roundtable-gemini-settings.json"
		configEnv["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] = "/tmp/" + configPath
	case "opencode":
		body, _ = json.Marshal(map[string]any{"mcp": map[string]any{"roundtable": map[string]any{
			"type": "local", "command": []string{"roundtable", "mcp", "stdio", "--unix-socket", agentMCPContainerSocket},
		}}})
		configPath = "roundtable-opencode.json"
		configEnv["OPENCODE_CONFIG"] = "/tmp/" + configPath
	default:
		return nil, nil, fmt.Errorf("adapter %q has no supported session-scoped MCP config format", adapter)
	}
	if configPath != "" {
		root, err := os.OpenRoot(temp)
		if err != nil {
			return nil, nil, fmt.Errorf("open agent temp for MCP config: %w", err)
		}
		defer root.Close()
		if err := writeAgentMCPConfig(root, configPath, body); err != nil {
			return nil, nil, err
		}
	}
	args := []string(nil)
	if adapter == "claude" {
		args = []string{"--mcp-config", "/tmp/" + configPath}
	}
	return configEnv, args, nil
}

func writeAgentMCPConfig(root *os.Root, name string, body []byte) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		return fmt.Errorf("create isolated agent MCP config %q: %w", name, err)
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = root.Remove(name)
		return fmt.Errorf("write isolated agent MCP config %q: %w", name, err)
	}
	return nil
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

type Registry struct {
	adapters map[string]Adapter
	config   config.Config
}

func NewRegistry(cfg config.Config) *Registry {
	adapters := make(map[string]Adapter, len(cfg.Adapters))
	for name, adapterCfg := range cfg.Adapters {
		adapters[name] = NewCLIAdapter(name, adapterCfg)
	}
	return &Registry{adapters: adapters, config: cfg}
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
		ExternalResumeCommand: a.resumeCommandWhenSupported(req.ExternalSessionID),
		WorkingDirectory:      req.WorkingDirectory,
		MCPSocket:             req.MCPSocket,
		Status:                "starting",
		LastSeenAt:            time.Now().UTC().Format(time.RFC3339),
		MetadataJSON:          mustJSON(map[string]any{"mode": "start", "role": req.Role, "task_id": req.TaskID, "base_revision": req.BaseRevision, "base_state_hash": req.BaseStateHash}),
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
	if !validExternalSessionID.MatchString(req.ExternalSessionID) {
		return LaunchSession{}, errors.New("invalid external_session_id for resume")
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
		ExternalResumeCommand: a.resumeCommandWhenSupported(req.ExternalSessionID),
		WorkingDirectory:      req.WorkingDirectory,
		MCPSocket:             req.MCPSocket,
		Status:                "resuming",
		LastSeenAt:            time.Now().UTC().Format(time.RFC3339),
		MetadataJSON:          mustJSON(map[string]any{"mode": "resume", "role": req.Role, "task_id": req.TaskID, "briefing_md": req.ResumeBriefingMD}),
	}
	args, err := a.resumeArgs(req.ExternalSessionID, req.ResumeBriefingMD)
	if err != nil {
		return LaunchSession{}, err
	}
	return LaunchSession{
		Session: session,
		Command: args,
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
	switch a.name {
	case "codex":
		args = append(args, "--ask-for-approval", "never", "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "workspace-write")
		if a.cfg.CapturesSessionID {
			args = append(args, "--json")
		}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		if req.Prompt != "" {
			args = append(args, req.Prompt)
		}
	case "claude":
		args = append(args, "-p")
		if a.cfg.CapturesSessionID {
			args = append(args, "--output-format", "json")
		}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		if req.Prompt != "" {
			args = append(args, req.Prompt)
		}
	case "gemini":
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		if req.Prompt != "" {
			args = append(args, "-p", req.Prompt)
		}
		if a.cfg.CapturesSessionID {
			args = append(args, "--output-format", "json")
		}
	case "opencode":
		args = append(args, "run")
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		if req.Prompt != "" {
			args = append(args, req.Prompt)
		}
	default:
		if a.name != "generic" {
			if req.Model != "" {
				args = append(args, "--model", req.Model)
			}
			if req.Prompt != "" {
				args = append(args, "--prompt", req.Prompt)
			}
		}
	}
	return args
}

func (a *CLIAdapter) resumeArgs(externalSessionID, briefing string) ([]string, error) {
	if !validExternalSessionID.MatchString(externalSessionID) {
		return nil, errors.New("invalid external_session_id for resume")
	}
	args := strings.Fields(a.cfg.ResumePattern)
	placeholderCount := 0
	for index, arg := range args {
		if arg == "{{external_session_id}}" {
			args[index] = externalSessionID
			placeholderCount++
		}
	}
	if placeholderCount != 1 || len(args) == 0 {
		return nil, fmt.Errorf("adapter %s resume_pattern must contain exactly one standalone {{external_session_id}} argument", a.name)
	}
	if strings.TrimSpace(briefing) != "" {
		args = append(args, strings.TrimSpace(briefing))
	}
	return args, nil
}

func (a *CLIAdapter) resumeCommand(externalSessionID string) string {
	if !validExternalSessionID.MatchString(externalSessionID) || a.cfg.ResumePattern == "" {
		return ""
	}
	args := strings.Fields(a.cfg.ResumePattern)
	placeholderCount := 0
	for index, arg := range args {
		if arg == "{{external_session_id}}" {
			args[index] = externalSessionID
			placeholderCount++
		}
	}
	if placeholderCount != 1 {
		return ""
	}
	return strings.Join(args, " ")
}

func (a *CLIAdapter) resumeCommandWhenSupported(externalSessionID string) string {
	if !a.cfg.SupportsResume {
		return ""
	}
	return a.resumeCommand(externalSessionID)
}

func parseExternalSessionID(provider, output string) string {
	valid := func(candidate string) string {
		if validExternalSessionID.MatchString(candidate) {
			return candidate
		}
		return ""
	}
	if provider == "codex" {
		for _, line := range strings.Split(output, "\n") {
			var event struct {
				Type     string `json:"type"`
				ThreadID string `json:"thread_id"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
				return valid(event.ThreadID)
			}
		}
		return ""
	}
	if provider != "claude" && provider != "gemini" {
		return ""
	}
	var outputEnvelope struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(output)), &outputEnvelope) != nil {
		return ""
	}
	if provider == "claude" && outputEnvelope.Type != "result" {
		return ""
	}
	return valid(outputEnvelope.SessionID)
}

func mustJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}
