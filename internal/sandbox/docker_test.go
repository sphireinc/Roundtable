package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunArgsEnforceContainerIsolationAndOnlyScopedMounts(t *testing.T) {
	root, source, workspace, temp := testDirs(t, t.TempDir())
	spec := Spec{
		Name: "rt-agent-s1", Image: "roundtable-agent:test", ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp,
		Command: []string{"agent-cli", "--task", "fix issue"}, Env: map[string]string{"API_KEY": "scoped"}, AllowedEnv: []string{"API_KEY"},
	}
	args, err := BuildRunArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, required := range []string{"--network none", "--read-only", "--cap-drop ALL", "no-new-privileges:true", "--pids-limit", "--memory", "--cpus", "1000:1000", "--env API_KEY", "type=volume,src=rtw-", "type=volume,src=rtt-"} {
		if !strings.Contains(joined, required) {
			t.Errorf("container arguments missing %q: %v", required, args)
		}
	}
	execArgs, err := BuildExecArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(execArgs, "agent-cli") || !slices.Contains(execArgs, "fix issue") || slices.Contains(execArgs, "sh") {
		t.Fatalf("agent command arguments were not preserved without shell parsing: %v", execArgs)
	}
	if strings.Contains(joined, "scoped") {
		t.Fatal("credential value leaked into runtime command arguments")
	}
	if strings.Contains(joined, ".docker.sock") || strings.Contains(joined, ".ssh") || strings.Contains(joined, "--privileged") || strings.Contains(joined, "--network host") {
		t.Fatalf("unsafe host capability present in arguments: %v", args)
	}
	if strings.Contains(joined, "src="+workspace) || strings.Contains(joined, "src="+temp) {
		t.Fatalf("container writable storage unexpectedly bind-mounts host workspace or temp: %v", args)
	}
}

func TestRunArgsAgentSocket(t *testing.T) {
	socketPath := "/private/test/agent.sock"
	args := appendAgentMCPSocketMount([]string{"run"}, socketPath)
	joined := strings.Join(args, " ")
	wantMount := "src=" + socketPath + ",dst=/run/roundtable-agent-mcp.sock"
	if !strings.Contains(joined, wantMount) {
		t.Fatalf("container arguments missing scoped MCP socket mount %q: %v", wantMount, args)
	}
	if strings.Contains(joined, "src=/private/test,") {
		t.Fatalf("container received the socket parent directory rather than the socket: %v", args)
	}
}

func TestRunArgsEgressSocketMountExposesOnlyTheAssignedSocket(t *testing.T) {
	args := appendAgentEgressSocketMount([]string{"run"}, "/project/.roundtable/mcp/e-0123456789abcdef01234567/g")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "src=/project/.roundtable/mcp/e-0123456789abcdef01234567,dst=/run/roundtable-egress,readonly") {
		t.Fatalf("container arguments missing the assigned read-only egress directory bind: %v", args)
	}
	if strings.Contains(joined, "src=/project/.roundtable/mcp,") {
		t.Fatalf("container received the shared MCP directory rather than its session directory: %v", args)
	}
}

func TestOutputCaptureIsBoundedAndReportsTruncation(t *testing.T) {
	capture := &boundedCapture{limit: 5}
	if n, err := capture.Write([]byte("abcdefgh")); err != nil || n != 8 {
		t.Fatalf("write result n=%d err=%v", n, err)
	}
	if capture.String() != "abcde" || !capture.truncated {
		t.Fatalf("unexpected bounded output: %q truncated=%v", capture.String(), capture.truncated)
	}
}

func TestDockerEnforcesFilesystemIdentityAndNetworkIsolation(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	root, source, workspace, temp := testDirs(t, executionBase)
	if err := os.WriteFile(filepath.Join(source, "authoritative.txt"), []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostSecret := filepath.Join(root, "host-secret")
	if err := os.WriteFile(hostSecret, []byte("host-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Name: "rt-it-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")), Image: "alpine:3",
		ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp,
		Command: []string{"sh", "-c", `test "$(id -u)" = 1000 && test "$(cat /tmp/roundtable/config.json)" = '{"mcp":"scoped"}' && test "$OPENAI_API_KEY" = "test-scoped-value" && test -z "${AWS_SECRET_ACCESS_KEY:-}" && test -z "${SSH_AUTH_SOCK:-}" && test ! -r "$1" && touch /workspace/writable && ! touch /container-root-escape 2>/dev/null && ! touch /roundtable/source/escape 2>/dev/null && ! wget -T 2 -q -O /dev/null https://example.com >/dev/null 2>&1 && printf agent-stdout && printf agent-stderr >&2`, "sh", hostSecret},
		Env:     map[string]string{"OPENAI_API_KEY": "test-scoped-value"}, AllowedEnv: []string{"OPENAI_API_KEY"},
		BootstrapFiles: map[string][]byte{"roundtable/config.json": []byte(`{"mcp":"scoped"}`)},
		Timeout:        20 * time.Second,
	}
	result, err := runtime.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("sandbox integration command failed: %v; stdout=%q stderr=%q", err, result.Stdout, result.Stderr)
	}
	if result.ExitCode != 0 || result.StartedAt.IsZero() || result.EndedAt.Before(result.StartedAt) {
		t.Fatalf("incomplete process metadata: %+v", result)
	}
	if result.Stdout != "agent-stdout" || result.Stderr != "agent-stderr" || result.OutputTruncated {
		t.Fatalf("unexpected captured output status: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "writable")); err != nil {
		t.Fatalf("agent could not write disposable workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "escape")); !os.IsNotExist(err) {
		t.Fatalf("agent wrote read-only source snapshot: stat error=%v", err)
	}
	data, err := os.ReadFile(filepath.Join(source, "authoritative.txt"))
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("authoritative source changed: data=%q err=%v", data, err)
	}
}

func TestRunArgsRejectUnsafeBootstrapFilePaths(t *testing.T) {
	root, source, workspace, temp := testDirs(t, t.TempDir())
	for _, name := range []string{"../escape", "/etc/passwd", "nested/../../escape"} {
		spec := Spec{Name: "rt-agent-seed", Image: "alpine:3", ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp, Command: []string{"true"}, BootstrapFiles: map[string][]byte{name: []byte("data")}}
		if _, err := BuildRunArgs(spec); err == nil {
			t.Errorf("accepted unsafe bootstrap file path %q", name)
		}
	}
}

func TestDockerOnlyReceivesOperatorAssignedSnapshotPaths(t *testing.T) {
	repository := newGitRepo(t)
	for name, content := range map[string]string{
		"assigned/visible.txt":  "visible",
		"assigned/.env":         "secret=hidden",
		"unassigned/secret.txt": "not authorized",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repository, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repository, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		commitFile(t, repository, name)
	}
	runtime, err := NewCLIRuntime("docker", repository)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = exec.CommandContext(ctx, "docker", "info").Run()
		cancel()
	}
	if err != nil {
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("container path-scope integration is required but Docker is unavailable: %v", err)
		}
		t.Skipf("Docker path-scope integration skipped; set ROUNDTABLE_REQUIRE_CONTAINER_TESTS=1 to require it: %v", err)
	}
	paths, err := CreateScopedSnapshot(context.Background(), repository, "S-docker-scope", []string{"assigned"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CleanupExecution(repository, paths) })
	result, err := runtime.Run(context.Background(), Spec{
		Name: "rt-scoped-snapshot", Image: "alpine:3", ExecutionRoot: paths.ExecutionRoot,
		Source: paths.Source, Workspace: paths.Workspace, Temp: paths.Temp,
		Command: []string{"sh", "-c", `test "$(id -u)" = 1000 && test -r /roundtable/source/assigned/visible.txt && test ! -w /roundtable/source/assigned/visible.txt && test ! -e /roundtable/source/assigned/.env && test ! -e /roundtable/source/unassigned/secret.txt && test -w /workspace/assigned/visible.txt && printf changed > /workspace/assigned/visible.txt && printf new > /workspace/assigned/new.txt && test ! -e /workspace/unassigned/secret.txt`},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("container could access files outside assigned scope: err=%v result=%+v", err, result)
	}
	if data, err := os.ReadFile(filepath.Join(paths.Workspace, "assigned", "visible.txt")); err != nil || string(data) != "changed" {
		t.Fatalf("container UID 1000 could not edit an existing workspace file: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(paths.Workspace, "assigned", "new.txt")); err != nil || string(data) != "new" {
		t.Fatalf("container UID 1000 could not create a workspace file: data=%q err=%v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(paths.Source, "assigned", "visible.txt")); err != nil || string(data) != "visible" {
		t.Fatalf("container changed the read-only source snapshot: data=%q err=%v", data, err)
	}
}

func TestDockerBoundsWorkspaceAndTempStorage(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	root, source, workspace, temp := testDirs(t, executionBase)
	result, err := runtime.Run(context.Background(), Spec{
		Name: "rt-agent-storage-limit", Image: "alpine:3", ExecutionRoot: root,
		Source: source, Workspace: workspace, Temp: temp,
		WorkspaceLimitBytes: 1 << 20, TempLimitBytes: 1 << 20,
		Command: []string{"sh", "-c", `dd if=/dev/zero of=/workspace/overflow bs=1048576 count=2 2>/dev/null; workspace_status=$?; dd if=/dev/zero of=/tmp/overflow bs=1048576 count=2 2>/dev/null; temp_status=$?; test "$workspace_status" -ne 0 && test "$temp_status" -ne 0`},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("bounded tmpfs-backed storage did not reject excess data: err=%v result=%+v", err, result)
	}
	for _, path := range []string{filepath.Join(workspace, "overflow"), filepath.Join(temp, "overflow")} {
		if info, err := os.Stat(path); err == nil && info.Size() >= 2<<20 {
			t.Fatalf("agent exceeded the configured 1 MiB storage budget at %s: %d bytes", path, info.Size())
		}
	}
}

func TestDockerTerminatesAgentChildrenBeforeWorkspaceCollection(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	root, source, workspace, temp := testDirs(t, executionBase)
	originalCommand := runtime.command
	runtime.command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == runtime.executable && len(args) > 0 && args[0] == "cp" {
			time.Sleep(1500 * time.Millisecond)
		}
		return originalCommand(ctx, name, args...)
	}
	result, err := runtime.Run(context.Background(), Spec{
		Name: "rt-it-stop-before-copy", Image: "alpine:3", ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp,
		Command: []string{"sh", "-c", `nohup sh -c 'sleep 0.5; printf late > /workspace/late-write' </dev/null >/dev/null 2>&1 & printf finished > /workspace/finished`},
		Timeout: 15 * time.Second,
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("agent command failed: err=%v result=%+v", err, result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "finished")); err != nil {
		t.Fatalf("completed agent output was not collected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "late-write")); !os.IsNotExist(err) {
		t.Fatalf("container child modified its workspace during collection: stat error=%v", err)
	}
}

func TestDockerMountsOnlyTheAssignedAgentMCPSocket(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	repository := filepath.Dir(filepath.Dir(filepath.Dir(executionBase)))
	const sessionID = "S-docker-mcp-mount"
	root := filepath.Join(executionBase, sessionID)
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	source, workspace, temp := filepath.Join(root, "source"), filepath.Join(root, "workspace"), filepath.Join(root, "temp")
	for _, directory := range []string{source, workspace, temp} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_, socketPath, err := PrepareAgentMCPSocketDir(repository, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CleanupAgentMCPSocketDir(repository, sessionID) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("strict container integration requires a host MCP Unix listener: %v", err)
		}
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EAFNOSUPPORT) {
			t.Skipf("host does not permit the MCP Unix listener required by the runtime integration: %v", err)
		}
		t.Fatalf("create host MCP Unix listener: %v", err)
	}
	defer listener.Close()
	if err := os.Chmod(socketPath, 0o666); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Run(context.Background(), Spec{
		Name: "rt-agent-mcp-mount", Image: "alpine:3",
		ExecutionRoot: root, AgentSessionID: sessionID, Source: source, Workspace: workspace, Temp: temp,
		AgentMCPSocketPath: socketPath,
		Command:            []string{"sh", "-c", `test "$(id -u)" = 1000 && test -S /run/roundtable-agent-mcp.sock && test ! -e /run/roundtable-admin-mcp.sock`},
		Timeout:            20 * time.Second,
	})
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "cannot bind-mount") || strings.Contains(strings.ToLower(result.Stderr), "operation not supported")) {
		message := fmt.Sprintf("container host-filesystem bridge cannot mount Unix sockets: %v", err)
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("strict container integration is required but unavailable: %s", message)
		}
		t.Skip(message)
	}
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("container could not access its assigned MCP socket without an admin endpoint: err=%v result=%+v", err, result)
	}
}

func TestDockerCancellationTerminatesContainer(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	root, source, workspace, temp := testDirs(t, executionBase)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	name := "rt-cancel-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	go func() {
		_, err := runtime.Run(ctx, Spec{Name: name, Image: "alpine:3", ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp, Command: []string{"sleep", "30"}})
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("cancellation result = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("container execution did not stop after cancellation")
	}
	assertContainerRemoved(t, runtime, name)
}

func TestDockerTimeoutTerminatesContainer(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	root, source, workspace, temp := testDirs(t, executionBase)
	started := time.Now()
	name := "rt-timeout-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	result, err := runtime.Run(context.Background(), Spec{
		Name: name, Image: "alpine:3",
		ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp,
		Command: []string{"sleep", "30"}, Timeout: 500 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("timeout result = %v", err)
	}
	if time.Since(started) > 10*time.Second {
		t.Fatalf("container timeout took too long: %s", time.Since(started))
	}
	if !result.TimedOut {
		t.Fatalf("timeout metadata not captured: %+v", result)
	}
	assertContainerRemoved(t, runtime, name)
}

func TestDockerStopIsIdempotentForAlreadyRemovedContainer(t *testing.T) {
	runtime, _ := integrationRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Stop(ctx, "rt-already-removed-container"); err != nil {
		t.Fatalf("stopping an already-removed container should succeed: %v", err)
	}
}

func assertContainerRemoved(t *testing.T, runtime *CLIRuntime, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, runtime.executable, "inspect", name)
	command.Env = filteredRuntimeEnv(nil)
	if err := command.Run(); err == nil {
		t.Fatalf("container %q survived execution termination", name)
	}
}

func TestDockerConcurrentWorkspacesRemainIsolated(t *testing.T) {
	runtime, executionBase := integrationRuntime(t)
	type worker struct {
		root, source, workspace, temp, value string
	}
	workers := []worker{}
	for _, value := range []string{"worker-one", "worker-two"} {
		root, source, workspace, temp := testDirs(t, executionBase)
		workers = append(workers, worker{root, source, workspace, temp, value})
	}
	results := make(chan error, len(workers))
	for _, worker := range workers {
		worker := worker
		go func() {
			_, err := runtime.Run(context.Background(), Spec{
				Name: "rt-concurrent-" + worker.value, Image: "alpine:3",
				ExecutionRoot: worker.root, Source: worker.source, Workspace: worker.workspace, Temp: worker.temp,
				Command: []string{"sh", "-c", `printf %s "$1" > shared && sleep 0.5 && test "$(cat shared)" = "$1"`, "sh", worker.value},
				Timeout: 10 * time.Second,
			})
			results <- err
		}()
	}
	for range workers {
		if err := <-results; err != nil {
			t.Errorf("isolated worker failed: %v", err)
		}
	}
}

func integrationRuntime(t *testing.T) (*CLIRuntime, string) {
	t.Helper()
	repositoryBase, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err := os.MkdirTemp(repositoryBase, ".rt-")
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot, err = filepath.Abs(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	executionBase, err := ManagedExecutionRoot(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repositoryRoot) })
	runtime, err := NewCLIRuntime("docker", repositoryRoot)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = exec.CommandContext(ctx, runtime.executable, "info").Run()
		cancel()
	}
	if err != nil {
		if os.Getenv("ROUNDTABLE_REQUIRE_CONTAINER_TESTS") == "1" {
			t.Fatalf("container integration tests are required but Docker is unavailable: %v", err)
		}
		t.Skipf("Docker integration tests skipped; set ROUNDTABLE_REQUIRE_CONTAINER_TESTS=1 to require them: %v", err)
	}
	return runtime, executionBase
}

func TestBuildRunArgsRejectsUnsafeInputs(t *testing.T) {
	root, source, workspace, temp := testDirs(t, t.TempDir())
	base := Spec{Name: "s1", Image: "agent:test", ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp, Command: []string{"agent"}}
	cases := map[string]func(*Spec, string){
		"empty image":             func(s *Spec, _ string) { s.Image = "" },
		"workspace traversal":     func(s *Spec, _ string) { s.Workspace = filepath.Join(s.Workspace, "..", "host") },
		"source equals workspace": func(s *Spec, _ string) { s.Source = s.Workspace },
		"unsafe environment name": func(s *Spec, _ string) {
			s.Env = map[string]string{"LD_PRELOAD": "x"}
			s.AllowedEnv = []string{"LD_PRELOAD"}
		},
		"unapproved environment name": func(s *Spec, _ string) { s.Env = map[string]string{"API_KEY": "x"} },
		"empty command":               func(s *Spec, _ string) { s.Command = nil },
		"unsafe name":                 func(s *Spec, _ string) { s.Name = "../host" },
		"symlink escape":              func(s *Spec, link string) { s.Temp = link },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec := base
			link := filepath.Join(spec.ExecutionRoot, "linked-"+strings.ReplaceAll(name, "/", "-"))
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Fatal(err)
			}
			mutate(&spec, link)
			if _, err := BuildRunArgs(spec); err == nil {
				t.Fatal("unsafe spec accepted")
			}
		})
	}
}

func TestRuntimeRejectsMountsOutsideConfiguredExecutionRoot(t *testing.T) {
	managedRoot := t.TempDir()
	outsideRoot, source, workspace, temp := testDirs(t, t.TempDir())
	runtime := &CLIRuntime{name: "docker", executable: "docker", executionRoot: managedRoot}
	_, err := runtime.Run(context.Background(), Spec{
		Name: "rt-outside-root", Image: "alpine:3", ExecutionRoot: outsideRoot,
		Source: source, Workspace: workspace, Temp: temp, Command: []string{"true"},
	})
	if err == nil || !strings.Contains(err.Error(), "configured Roundtable-managed root") {
		t.Fatalf("runtime accepted caller-selected mount root: %v", err)
	}
}

func testDirs(t *testing.T, executionBase string) (string, string, string, string) {
	t.Helper()
	root, err := os.MkdirTemp(executionBase, "session-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	source, workspace, temp := filepath.Join(root, "source"), filepath.Join(root, "workspace"), filepath.Join(root, "temp")
	for _, path := range []string{source, workspace, temp} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root, source, workspace, temp
}
