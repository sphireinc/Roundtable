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
