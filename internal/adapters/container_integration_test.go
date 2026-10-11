package adapters

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"roundtable/internal/egress"
	"roundtable/internal/sandbox"
)

func TestAgentSandboxExecutesInDockerImage(t *testing.T) {
	repositoryRoot, err := os.MkdirTemp(".", ".roundtable-adapter-repository-")
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err = filepath.Abs(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repositoryRoot) })
	executionBase, err := sandbox.ManagedExecutionRoot(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sandbox.NewCLIRuntime("docker", repositoryRoot)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = exec.CommandContext(ctx, "docker", "info").Run()
		cancel()
	}
	if err != nil {
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("Docker adapter integration test is required but Docker is unavailable: %v", err)
		}
		t.Skipf("Docker adapter integration test skipped; set ROUNDTABLE_REQUIRE_CONTAINER_TESTS=1 to require it: %v", err)
	}
	root, err := os.MkdirTemp(executionBase, "session-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	source, workspace, temp := filepath.Join(root, "source"), filepath.Join(root, "workspace"), filepath.Join(root, "temp")
	for _, directory := range []string{source, workspace, temp} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	execution, err := runtime.Run(context.Background(), sandbox.Spec{
		Name: "rt-agent-integration", Image: "alpine:3", Command: []string{"id"},
		ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp,
	})
	if err != nil {
		t.Fatalf("execute adapter in Docker: %v; stdout=%q stderr=%q", err, execution.Stdout, execution.Stderr)
	}
	if execution.ExitCode != 0 || !strings.Contains(execution.Stdout, "uid=1000") {
		t.Fatalf("unexpected container execution: result=%+v", execution)
	}
}

func TestDockerAgentEgressRejectsUnlistedProviderHost(t *testing.T) {
	runtime, repositoryRoot, executionBase := adapterIntegrationRuntime(t)
	image := os.Getenv("ROUNDTABLE_TEST_AGENT_IMAGE")
	if image == "" {
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatal("ROUNDTABLE_TEST_AGENT_IMAGE must name an image containing roundtable and wget when strict container tests are required")
		}
		t.Skip("set ROUNDTABLE_TEST_AGENT_IMAGE to an image containing roundtable and wget for provider egress integration coverage")
	}
	root, source, workspace, temp := adapterIntegrationDirs(t, executionBase)
	broker, err := egress.ListenSessionIn(context.Background(), repositoryRoot, "S-egress-deny", egress.Policy{AllowedHosts: []string{"api.example.com"}}, nil)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") && os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") != "1" {
			t.Skipf("host sandbox does not permit the provider gateway Unix socket bind: %v", err)
		}
		t.Fatalf("start isolated provider egress broker: %v", err)
	}
	defer func() { _ = broker.Close() }()
	command := []string{
		"roundtable", "agent", "exec", "--egress-socket", "/run/roundtable-egress/g", "--",
		"sh", "-c", `response=$(wget -S -O /dev/null -T 5 https://blocked.example 2>&1 || true); printf '%s' "$response" | grep -q '403 Forbidden'`,
	}
	result, err := runtime.Run(context.Background(), sandbox.Spec{
		Name: "rt-egress-deny", Image: image, ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp,
		AgentEgressSocketPath: broker.SocketPath(), Command: command, Timeout: 20 * time.Second,
	})
	if strings.Contains(result.Stderr, "lstat: lstat /run/roundtable-egress/g: operation not supported") {
		message := "the Docker daemon's host-filesystem bridge does not support Unix-socket bind mounts; run this integration on a Linux-native Docker/Podman filesystem"
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("strict provider-egress integration is required but unavailable: %s", message)
		}
		t.Skip(message)
	}
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("container did not prove unlisted egress was denied by the broker: err=%v result=%+v", err, result)
	}
}

func adapterIntegrationRuntime(t *testing.T) (*sandbox.CLIRuntime, string, string) {
	t.Helper()
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	executionBase, err := sandbox.ManagedExecutionRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sandbox.NewCLIRuntime("docker", repository)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = exec.CommandContext(ctx, "docker", "info").Run()
		cancel()
	}
	if err != nil {
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("strict provider egress integration requires Docker: %v", err)
		}
		t.Skipf("Docker provider egress integration skipped: %v", err)
	}
	return runtime, repository, executionBase
}

func adapterIntegrationDirs(t *testing.T, base string) (string, string, string, string) {
	t.Helper()
	root, err := os.MkdirTemp(base, "session-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	source, workspace, temp := filepath.Join(root, "source"), filepath.Join(root, "workspace"), filepath.Join(root, "temp")
	for _, dir := range []string{source, workspace, temp} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, source, workspace, temp
}
