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
	contents := "agents:\n  implementers:\n    count: 0\n  chair:\n    adapter: custom\nadapters:\n  custom:\n    command: custom-agent\n"
	if err := os.WriteFile(filepath.Join(root, DefaultConfigPath), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err != nil {
		t.Fatalf("Load rejected a valid zero-sized pool or custom adapter: %v", err)
	}
}
