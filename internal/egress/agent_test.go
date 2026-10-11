package egress

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAgentRequiresMountedUnixBrokerSocket(t *testing.T) {
	if err := RunAgent(context.Background(), "", []string{"agent"}, nil, nil); err == nil {
		t.Fatal("agent wrapper accepted an empty broker socket")
	}
	file := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RunAgent(context.Background(), file, []string{"agent"}, nil, nil); err == nil || !strings.Contains(err.Error(), "Unix socket") {
		t.Fatalf("agent wrapper did not reject a non-socket broker path: %v", err)
	}
}

func TestProxyEnvironmentReplacesInheritedProxySettings(t *testing.T) {
	env := proxyEnvironment([]string{"PATH=/usr/bin", "HTTP_PROXY=http://evil", "http_proxy=http://evil-lower", "NO_PROXY=*", "OPENAI_API_KEY=key"}, "http://127.0.0.1:8123")
	joined := strings.Join(env, "\n")
	for _, want := range []string{"HTTP_PROXY=http://127.0.0.1:8123", "HTTPS_PROXY=http://127.0.0.1:8123", "ALL_PROXY=http://127.0.0.1:8123", "NO_PROXY=localhost,127.0.0.1,::1", "OPENAI_API_KEY=key"} {
		if !strings.Contains(joined, want) {
			t.Errorf("proxy environment missing %q: %v", want, env)
		}
	}
	if strings.Contains(joined, "evil") || strings.Contains(joined, "NO_PROXY=*") {
		t.Fatalf("inherited proxy bypass or endpoint survived: %v", env)
	}
}
