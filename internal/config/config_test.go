package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMCPHTTPDefaultsAndConfigParsing(t *testing.T) {
	cfg := Default()
	if cfg.MCP.HTTPAddress != "127.0.0.1:7117" {
		t.Fatalf("default HTTP address = %q", cfg.MCP.HTTPAddress)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
		t.Fatal(err)
	}
	contents := "mcp:\n  http_address: 0.0.0.0:8443\n  tls_cert_file: cert.pem\n  tls_key_file: key.pem\n"
	if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MCP.HTTPAddress != "0.0.0.0:8443" || loaded.MCP.TLSCertFile != "cert.pem" || loaded.MCP.TLSKeyFile != "key.pem" {
		t.Fatalf("MCP HTTP settings did not parse: %+v", loaded.MCP)
	}
}

func TestLoadRejectsNegativeAgentPoolCounts(t *testing.T) {
	for _, field := range []string{"implementers", "reviewers"} {
		t.Run(field, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
				t.Fatal(err)
			}
			contents := "agents:\n  " + field + ":\n    count: -1\n"
			if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil {
				t.Fatal("Load accepted a negative agent pool count")
			}
		})
	}
}

func TestLoadRejectsUnavailableRoleAdapters(t *testing.T) {
	for _, configValue := range []string{"missing", "empty"} {
		t.Run(configValue, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
				t.Fatal(err)
			}
			adapter := "missing"
			adapters := ""
			if configValue == "empty" {
				adapter = "custom"
				adapters = "adapters:\n  custom:\n    command: \"\"\n"
			}
			contents := "agents:\n  chair:\n    adapter: " + adapter + "\n" + adapters
			if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root); err == nil {
				t.Fatal("Load accepted a role adapter without an executable command")
			}
		})
	}
}

func TestLoadAcceptsZeroPoolsAndConfiguredCustomRoleAdapter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
		t.Fatal(err)
	}
	contents := "agents:\n  implementers:\n    count: 0\n  chair:\n    adapter: custom\nadapters:\n  custom:\n    image: registry.example/agent@sha256:abcdef\n    command: custom-agent\n"
	if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load rejected a valid zero-sized pool or custom adapter: %v", err)
	}
	if loaded.Adapters["custom"].Image != "registry.example/agent@sha256:abcdef" {
		t.Fatalf("adapter image was not parsed: %+v", loaded.Adapters["custom"])
	}
}

func TestAdapterProviderHostAndCredentialAllowListsParse(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
		t.Fatal(err)
	}
	contents := "adapters:\n  codex:\n    provider_hosts: api.openai.com, auth.openai.com\n    credential_env: OPENAI_API_KEY\n"
	if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	adapter := loaded.Adapters["codex"]
	if len(adapter.ProviderHosts) != 2 || adapter.ProviderHosts[0] != "api.openai.com" || adapter.ProviderHosts[1] != "auth.openai.com" {
		t.Fatalf("provider host allowlist did not parse: %+v", adapter.ProviderHosts)
	}
	if len(adapter.CredentialEnv) != 1 || adapter.CredentialEnv[0] != "OPENAI_API_KEY" {
		t.Fatalf("credential allowlist did not parse: %+v", adapter.CredentialEnv)
	}
}

func TestConfigRejectsWildcardProviderHostsAndRuntimeCredentialVariables(t *testing.T) {
	for _, contents := range []string{
		"adapters:\n  codex:\n    provider_hosts: '*.openai.com'\n",
		"adapters:\n  codex:\n    credential_env: DOCKER_HOST\n",
	} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); err == nil {
			t.Fatalf("config accepted unsafe provider egress or credential policy: %q", contents)
		}
	}
}
