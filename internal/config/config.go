package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	Command                   string
	SupportsResume            bool
	ResumePattern             string
	SupportsMCP               bool
	SupportsReadOnlyWorkspace bool
	CapturesSessionID         bool
}

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
			},
			"claude": {
				Command:                   "claude",
				SupportsResume:            true,
				ResumePattern:             "claude --resume {{external_session_id}}",
				SupportsMCP:               true,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         true,
			},
			"gemini": {
				Command:                   "gemini",
				SupportsResume:            true,
				ResumePattern:             "gemini resume {{external_session_id}}",
				SupportsMCP:               true,
				SupportsReadOnlyWorkspace: true,
				CapturesSessionID:         true,
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
	return cfg, nil
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
	default:
		return fmt.Errorf("unsupported adapter key: %s", section[2])
	}
	cfg.Adapters[name] = adapter
	return nil
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
