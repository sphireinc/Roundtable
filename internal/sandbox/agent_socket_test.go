package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentMCPSocketLivesInItsManagedExecutionSession(t *testing.T) {
	repositoryBase, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repository, err := os.MkdirTemp(repositoryBase, ".m-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repository) })
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "S-agent-mcp"
	directory, socketPath, err := AgentMCPSocketPaths(repository, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(directory) != filepath.Join(repository, ".roundtable", "mcp") || filepath.Base(socketPath) != "m" {
		t.Fatalf("MCP socket must live in its session-scoped, individually mountable project directory: directory=%q socket=%q", directory, socketPath)
	}
	_, preparedPath, err := PrepareAgentMCPSocketDir(repository, sessionID)
	if err != nil || preparedPath != socketPath {
		t.Fatalf("prepare MCP socket: path=%q err=%v", preparedPath, err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("host sandbox does not permit a project-scoped Unix listener: %v", err)
		}
		t.Fatal(err)
	}
	_ = listener.Close()
	if err := CleanupAgentMCPSocketDir(repository, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket path survived cleanup: %v", err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("session-scoped socket directory survived cleanup: %v", err)
	}
}

func TestAgentMCPCleanupIsIdempotentWhenSocketPathExceedsPlatformLimit(t *testing.T) {
	if err := CleanupAgentMCPSocketDir(t.TempDir(), "S-long-socket-path"); err != nil {
		t.Fatalf("reconciliation of an execution that could not create a valid Unix socket must remain safe: %v", err)
	}
}

func TestRunArgsRejectsMCPsocketAssignedToAnotherSession(t *testing.T) {
	repository, err := os.MkdirTemp("/tmp", "rt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repository) })
	managed, err := ManagedExecutionRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "S-authorized"
	executionRoot := filepath.Join(managed, sessionID)
	for _, directory := range []string{executionRoot, filepath.Join(executionRoot, "source"), filepath.Join(executionRoot, "workspace"), filepath.Join(executionRoot, "temp")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, socketPath, err := PrepareAgentMCPSocketDir(repository, "S-other")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CleanupAgentMCPSocketDir(repository, "S-other") })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("host sandbox does not permit project-scoped Unix listeners: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	_, err = BuildRunArgs(Spec{Name: "rt-agent-cross-session", Image: "alpine:3", ExecutionRoot: executionRoot,
		AgentSessionID: sessionID, Source: filepath.Join(executionRoot, "source"), Workspace: filepath.Join(executionRoot, "workspace"), Temp: filepath.Join(executionRoot, "temp"), AgentMCPSocketPath: socketPath})
	if err == nil {
		t.Fatal("container accepted an MCP socket belonging to another execution session")
	}
}
