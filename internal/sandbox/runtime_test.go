package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeCancellationDuringContainerStartupStillRemovesContainer(t *testing.T) {
	repository := t.TempDir()
	executionRoot, err := ManagedExecutionRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	root, source, workspace, temp := testDirs(t, executionRoot)
	stateFile := filepath.Join(t.TempDir(), "runtime.log")
	executable := filepath.Join(t.TempDir(), "docker")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  volume) [ "$2" = create ] && { for last do :; done; printf '%%s' "$last"; exit 0; }; exit 0 ;;
  run) printf 'run\\n' >> '%s'; printf 'fake-container-id'; exec sleep 30 ;;
  kill) printf 'kill\\n' >> '%s'; exit 0 ;;
  rm) printf 'rm\\n' >> '%s'; exit 0 ;;
  inspect) exit 1 ;;
  *) exit 0 ;;
esac
`, stateFile, stateFile, stateFile)
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := &CLIRuntime{name: "docker", executable: executable, executionRoot: executionRoot, lookup: exec.LookPath, command: exec.CommandContext}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runtime.Run(ctx, Spec{Name: "rt-start-cancel", Image: "alpine:3", ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp, Command: []string{"sleep", "30"}})
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(stateFile)
		if strings.Contains(string(data), "run") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(stateFile)
	if !strings.Contains(string(data), "run") {
		cancel()
		t.Fatal("fake runtime never entered container startup")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("startup cancellation result = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup cancellation did not return")
	}
	data, _ = os.ReadFile(stateFile)
	if !strings.Contains(string(data), "kill") || !strings.Contains(string(data), "rm") {
		t.Fatalf("container cleanup was not attempted after canceled startup: %q", data)
	}
}

func TestPodmanStopTreatsMissingContainersAsAlreadyStopped(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "podman")
	script := "#!/bin/sh\n[ \"$1\" = rm ] && [ \"$2\" = --force ] && [ \"$3\" = --ignore ] && [ \"$4\" = test-container ]\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := &CLIRuntime{name: "podman", executable: executable}
	if err := runtime.Stop(context.Background(), "test-container"); err != nil {
		t.Fatalf("Podman did not idempotently stop an absent container: %v", err)
	}
}
