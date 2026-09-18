package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestAgentRepositorySurfaceRejectsPathsOutsideRoot(t *testing.T) {
	root := newRuntimeRoot(t)
	rt, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := rt.Call(context.Background(), "repo.read_file", map[string]any{"path": "../outside.txt"}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected outside-root read rejection, got %v", err)
	}
	if _, err := rt.Call(context.Background(), "repo.search", map[string]any{"query": "hello", "path": "/tmp"}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected absolute outside-root search rejection, got %v", err)
	}
}

func TestAgentCommandSurfaceRejectsWritePrimitivesAndShellControl(t *testing.T) {
	for _, command := range []string{"rm -rf .", "go test ./...; touch hacked", "echo ok", "cat > hacked"} {
		if err := validateAgentCommand(command); err == nil {
			t.Fatalf("command %q was accepted", command)
		}
	}
	if err := validateAgentCommand("printf 'PASS\\n'"); err != nil {
		t.Fatalf("safe test command rejected: %v", err)
	}
}
