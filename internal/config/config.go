package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"roundtable/internal/egress"
)

const DefaultConfigPath = ".roundtable/config.yaml"

type Config struct {
	Version     int
	ProjectName string
	Storage     StorageConfig
	MCP         MCPConfig
	Agents      AgentsConfig
	Adapters    map[string]AdapterConfig
}

type StorageConfig struct {
	SQLitePath string
	WAL        bool
}

type MCPConfig struct {
	Transport   string
	SocketPath  string
	HTTPAddress string
	TLSCertFile string
	TLSKeyFile  string
}

type AgentsConfig struct {
	Chair        SingleAgentConfig
	Architect    SingleAgentConfig
	Implementers MultiAgentConfig
	Reviewers    MultiAgentConfig
	Tester       SingleAgentConfig
	Security     SingleAgentConfig
	MemoryOracle SingleAgentConfig
}

type SingleAgentConfig struct {
	Role    string
	Adapter string
	Model   string
}

type MultiAgentConfig struct {
	Count   int
	Adapter string
	Model   string
}

type AdapterConfig struct {
	Image                     string
	Command                   string
	SupportsResume            bool
	ResumePattern             string
	SupportsMCP               bool
	SupportsReadOnlyWorkspace bool
	CapturesSessionID         bool
	ProviderHosts             []string
	CredentialEnv             []string
}

var credentialEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,126}_API_KEY$`)

func Default() Config {
	return Config{
		Version:     1,
		ProjectName: "Roundtable",
		Storage: StorageConfig{
			SQLitePath: ".roundtable/roundtable.db",
			WAL:        true,
		},
		MCP: MCPConfig{
			Transport:   "unix",
			SocketPath:  ".roundtable/mcp/roundtable.sock",
			HTTPAddress: "127.0.0.1:7117",
		},
		Agents: AgentsConfig{
			Chair:        SingleAgentConfig{Role: "Chair", Adapter: "claude", Model: "default"},
			Architect:    SingleAgentConfig{Role: "Architect", Adapter: "claude", Model: "default"},
			Implementers: MultiAgentConfig{Count: 2, Adapter: "codex", Model: "default"},
			Reviewers:    MultiAgentConfig{Count: 1, Adapter: "gemini", Model: "default"},
			Tester:       SingleAgentConfig{Role: "Tester", Adapter: "codex", Model: "default"},
			Security:     SingleAgentConfig{Role: "Security", Adapter: "claude", Model: "default"},
			MemoryOracle: SingleAgentConfig{Role: "MemoryOracle", Adapter: "claude", Model: "default"},
		},
		Adapters: map[string]AdapterConfig{
			"codex": {
				Command:                   "codex",
				SupportsResume:            true,
				ResumePattern:             "codex resume {{external_session_id}}",
				SupportsMCP:               true,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         true,
				ProviderHosts:             []string{"api.openai.com"},
				CredentialEnv:             []string{"OPENAI_API_KEY"},
			},
			"claude": {
				Command:                   "claude",
				SupportsResume:            true,
				ResumePattern:             "claude --resume {{external_session_id}}",
				SupportsMCP:               true,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         true,
				ProviderHosts:             []string{"api.anthropic.com"},
				CredentialEnv:             []string{"ANTHROPIC_API_KEY"},
			},
			"gemini": {
				Command:                   "gemini",
				SupportsResume:            true,
				ResumePattern:             "gemini resume {{external_session_id}}",
				SupportsMCP:               true,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         true,
				ProviderHosts:             []string{"generativelanguage.googleapis.com"},
				CredentialEnv:             []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"},
			},
			"opencode": {
				Command:                   "opencode",
				SupportsResume:            false,
				ResumePattern:             "",
				SupportsMCP:               true,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         false,
			},
			"generic": {
				Command:                   "sh",
				SupportsResume:            false,
				ResumePattern:             "",
				SupportsMCP:               false,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         false,
			},
		},
	}
}

func Load(root string) (Config, error) {
	path := filepath.Join(root, DefaultConfigPath)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := parseYAMLInto(strings.Split(string(data), "\n"), &cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	if cfg.Agents.Implementers.Count < 0 {
		return errors.New("agents.implementers.count must be nonnegative")
	}
	if cfg.Agents.Reviewers.Count < 0 {
		return errors.New("agents.reviewers.count must be nonnegative")
	}
	roles := []struct {
		name    string
		adapter string
	}{
		{name: "agents.chair", adapter: cfg.Agents.Chair.Adapter},
		{name: "agents.architect", adapter: cfg.Agents.Architect.Adapter},
		{name: "agents.implementers", adapter: cfg.Agents.Implementers.Adapter},
		{name: "agents.reviewers", adapter: cfg.Agents.Reviewers.Adapter},
		{name: "agents.tester", adapter: cfg.Agents.Tester.Adapter},
		{name: "agents.security", adapter: cfg.Agents.Security.Adapter},
		{name: "agents.memory_oracle", adapter: cfg.Agents.MemoryOracle.Adapter},
	}
	for _, role := range roles {
		adapter, ok := cfg.Adapters[role.adapter]
		if !ok {
			return fmt.Errorf("%s.adapter references unknown adapter %q", role.name, role.adapter)
		}
		if strings.TrimSpace(adapter.Command) == "" {
			return fmt.Errorf("adapters.%s.command must not be empty (referenced by %s)", role.adapter, role.name)
		}
	}
	for name, adapter := range cfg.Adapters {
		seenHosts := map[string]bool{}
		for _, host := range adapter.ProviderHosts {
			if err := egress.ValidateHostname(host); err != nil {
				return fmt.Errorf("adapters.%s.provider_hosts contains an invalid exact hostname: %w", name, err)
			}
			host = strings.ToLower(strings.TrimSuffix(host, "."))
			if seenHosts[host] {
				return fmt.Errorf("adapters.%s.provider_hosts contains duplicate hostname %q", name, host)
			}
			seenHosts[host] = true
		}
		seenCredentials := map[string]bool{}
		if len(adapter.CredentialEnv) > 8 {
			return fmt.Errorf("adapters.%s.credential_env may contain at most 8 explicitly allowlisted API key variables", name)
		}
		for _, variable := range adapter.CredentialEnv {
			if !credentialEnvName.MatchString(variable) || strings.HasPrefix(variable, "ROUNDTABLE_") || seenCredentials[variable] {
				return fmt.Errorf("adapters.%s.credential_env contains an invalid or duplicate API key variable %q", name, variable)
			}
			seenCredentials[variable] = true
		}
	}
	return nil
}

func parseYAMLInto(lines []string, cfg *Config) error {
	var section []string

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if trimmed := strings.TrimSpace(line); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := indentation(line)
		if indent%2 != 0 {
			return fmt.Errorf("unsupported config indentation: %q", line)
		}
		level := indent / 2
		key, value, hasValue, err := splitKV(strings.TrimSpace(line))
		if err != nil {
			return err
		}

		if level < len(section) {
			section = section[:level]
		}
		if level == len(section) {
			section = append(section, key)
		} else if level+1 == len(section) {
			section[level] = key
		} else {
			return fmt.Errorf("invalid config nesting near %q", line)
		}

		if !hasValue {
			continue
		}

		switch strings.Join(section, ".") {
		case "version":
			cfg.Version, err = strconv.Atoi(value)
		case "project_name":
			cfg.ProjectName = value
		case "storage.sqlite_path":
			cfg.Storage.SQLitePath = value
		case "storage.wal":
			cfg.Storage.WAL, err = strconv.ParseBool(value)
		case "mcp.transport":
			cfg.MCP.Transport = value
		case "mcp.socket_path":
			cfg.MCP.SocketPath = value
		case "mcp.http_address":
			cfg.MCP.HTTPAddress = value
		case "mcp.tls_cert_file":
			cfg.MCP.TLSCertFile = value
		case "mcp.tls_key_file":
			cfg.MCP.TLSKeyFile = value
		case "agents.chair.role":
			cfg.Agents.Chair.Role = value
		case "agents.chair.adapter":
			cfg.Agents.Chair.Adapter = value
		case "agents.chair.model":
			cfg.Agents.Chair.Model = value
		case "agents.architect.role":
			cfg.Agents.Architect.Role = value
		case "agents.architect.adapter":
			cfg.Agents.Architect.Adapter = value
		case "agents.architect.model":
			cfg.Agents.Architect.Model = value
		case "agents.implementers.count":
			cfg.Agents.Implementers.Count, err = strconv.Atoi(value)
		case "agents.implementers.adapter":
			cfg.Agents.Implementers.Adapter = value
		case "agents.implementers.model":
			cfg.Agents.Implementers.Model = value
		case "agents.reviewers.count":
			cfg.Agents.Reviewers.Count, err = strconv.Atoi(value)
		case "agents.reviewers.adapter":
			cfg.Agents.Reviewers.Adapter = value
		case "agents.reviewers.model":
			cfg.Agents.Reviewers.Model = value
		case "agents.tester.role":
			cfg.Agents.Tester.Role = value
		case "agents.tester.adapter":
			cfg.Agents.Tester.Adapter = value
		case "agents.tester.model":
			cfg.Agents.Tester.Model = value
		case "agents.security.role":
			cfg.Agents.Security.Role = value
		case "agents.security.adapter":
			cfg.Agents.Security.Adapter = value
		case "agents.security.model":
			cfg.Agents.Security.Model = value
		case "agents.memory_oracle.role":
			cfg.Agents.MemoryOracle.Role = value
		case "agents.memory_oracle.adapter":
			cfg.Agents.MemoryOracle.Adapter = value
		case "agents.memory_oracle.model":
			cfg.Agents.MemoryOracle.Model = value
		default:
			if strings.HasPrefix(strings.Join(section, "."), "adapters.") {
				if err = setAdapterValue(cfg, section, value); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("unsupported config key: %s", strings.Join(section, "."))
		}
		if err != nil {
			return fmt.Errorf("invalid value for %s: %w", strings.Join(section, "."), err)
		}
	}
	return nil
}

func setAdapterValue(cfg *Config, section []string, value string) error {
	if len(section) != 3 {
		return fmt.Errorf("unsupported adapter nesting: %s", strings.Join(section, "."))
	}
	name := section[1]
	adapter := cfg.Adapters[name]
	switch section[2] {
	case "image":
		adapter.Image = value
	case "command":
		adapter.Command = value
	case "supports_resume":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		adapter.SupportsResume = v
	case "resume_pattern":
		adapter.ResumePattern = value
	case "supports_mcp":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		adapter.SupportsMCP = v
	case "supports_readonly_workspace":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		adapter.SupportsReadOnlyWorkspace = v
	case "captures_session_id":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		adapter.CapturesSessionID = v
	case "provider_hosts":
		adapter.ProviderHosts = commaSeparated(value)
	case "credential_env":
		adapter.CredentialEnv = commaSeparated(value)
	default:
		return fmt.Errorf("unsupported adapter key: %s", section[2])
	}
	cfg.Adapters[name] = adapter
	return nil
}

func commaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

func splitKV(line string) (key string, value string, hasValue bool, err error) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false, errors.New("invalid config line")
	}
	key = strings.TrimSpace(parts[0])
	value = strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", false, errors.New("missing config key")
	}
	if value == "" {
		return key, "", false, nil
	}
	value = strings.Trim(value, `"`)
	return key, value, true, nil
}

func indentation(line string) int {
	n := 0
	for _, ch := range line {
		if ch != ' ' {
			break
		}
		n++
	}
	return n
}
