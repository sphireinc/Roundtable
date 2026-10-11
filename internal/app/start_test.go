package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"roundtable/internal/agentauth"
	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/processlock"
	"roundtable/internal/sandbox"
)

func TestStartRemainsHealthyAndIdleWithoutCreatingRun(t *testing.T) {
	root := t.TempDir()
	var output strings.Builder
	if err := Run(context.Background(), []string{"init", "--root", root}, &output, &output); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox does not permit loopback listeners: %v", err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	socketDir, err := os.MkdirTemp("/tmp", "rts-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	configPath := filepath.Join(root, ".roundtable", "config.yaml")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configText := strings.Replace(string(configData), "http_address: 127.0.0.1:7117", "http_address: "+address, 1)
	configText = strings.Replace(configText, "socket_path: .roundtable/mcp/roundtable.sock", "socket_path: "+filepath.Join(socketDir, "rt.sock"), 1)
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout lockedBuffer
	done := make(chan error, 1)
	go func() { done <- Run(ctx, []string{"start", "--root", root}, &stdout, &stdout) }()
	healthURL := "http://" + address + "/healthz"
	deadline := time.Now().Add(5 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("start exited before ready: %v; logs: %s", err, stdout.String())
		default:
		}
		response, err := http.Get(healthURL)
		if err == nil {
			_ = json.NewDecoder(response.Body).Decode(&struct {
				Status *string `json:"status"`
			}{Status: &status})
			_ = response.Body.Close()
			if status == "healthy-idle" {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != "healthy-idle" {
		t.Fatalf("health status = %q; logs: %s", status, stdout.String())
	}
	sqlDB, err := db.Open(filepath.Join(root, ".roundtable", "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	runs, err := db.NewStore(sqlDB, nil).ListRuns(context.Background())
	_ = sqlDB.Close()
	if err != nil || len(runs) != 0 {
		t.Fatalf("start created a run: runs=%+v err=%v", runs, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("start did not shut down after cancellation")
	}
}

func TestCanonicalProjectRootResolvesRelativeAndSymlinkPaths(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "project-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	workingDirectory, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDirectory, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{link, relative} {
		got, err := canonicalProjectRoot(input)
		if err != nil {
			t.Fatalf("canonicalize project root %q: %v", input, err)
		}
		want, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("canonical project root = %q, want %q", got, want)
		}
	}
}

func TestStartRejectsExistingProjectOwner(t *testing.T) {
	for _, args := range [][]string{{"start"}, {"run"}, {"mcp", "serve"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			root := t.TempDir()
			lock, err := processlock.Acquire(root)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			command := append(append([]string{}, args...), "--root", root)
			err = Run(context.Background(), command, &strings.Builder{}, &strings.Builder{})
			if !errors.Is(err, processlock.ErrAlreadyLocked) {
				t.Fatalf("command error = %v, want duplicate-owner error", err)
			}
		})
	}
}

func TestStartupReconcilesOnlyPersistedContainerExecutions(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, ".roundtable", "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	managed, err := sandbox.ManagedExecutionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	sessionRoot := filepath.Join(managed, "S-startup")
	for _, dir := range []string{"source", "workspace", "temp"} {
		if err := os.MkdirAll(filepath.Join(sessionRoot, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{ID: "S-startup", AgentID: "A-1", RunID: "R-1", Adapter: "codex", WorkingDirectory: filepath.Join(sessionRoot, "workspace"), Status: "running", MetadataJSON: `{"mode":"container"}`}); err != nil {
		t.Fatal(err)
	}
	fake := &startupFakeRuntime{}
	detected := 0
	reconciled, err := reconcileContainerExecutions(context.Background(), root, store, agentauth.New(nil), config.Default(), func(string) (sandbox.Runtime, error) {
		detected++
		return fake, nil
	})
	if err != nil || reconciled != 1 || detected != 1 || fake.stopped == "" {
		t.Fatalf("startup recovery count=%d detect=%d stopped=%q err=%v", reconciled, detected, fake.stopped, err)
	}
	session, err := store.GetAgentSession(context.Background(), "S-startup")
	if err != nil || session.Status != "interrupted" {
		t.Fatalf("startup did not persist interrupted state: status=%q err=%v", session.Status, err)
	}
}

func TestStartupDoesNotDetectRuntimeForLegacySessions(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertAgentSession(context.Background(), db.AgentSession{ID: "legacy", AgentID: "A-1", RunID: "R-1", Adapter: "codex", WorkingDirectory: root, Status: "running", MetadataJSON: `{"mode":"legacy"}`}); err != nil {
		t.Fatal(err)
	}
	detected := 0
	reconciled, err := reconcileContainerExecutions(context.Background(), root, store, agentauth.New(nil), config.Default(), func(string) (sandbox.Runtime, error) {
		detected++
		return nil, errors.New("must not be called")
	})
	if err != nil || reconciled != 0 || detected != 0 {
		t.Fatalf("legacy session triggered container recovery: count=%d detect=%d err=%v", reconciled, detected, err)
	}
}

func TestStartHTTPServeErrorStillDrainsCoordinator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	httpDone := make(chan error, 1)
	httpDone <- errors.New("injected serve failure")
	coordinatorDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		coordinatorDone <- nil
	}()
	var output lockedBuffer
	server := &http.Server{}
	err := stopCoordinator(ctx, cancel, httpDone, coordinatorDone, server, nil, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "injected serve failure") {
		t.Fatalf("expected serving error after shutdown, got %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("coordinator context was not cancelled")
	}
	if !strings.Contains(output.String(), "coordinator stopped") {
		t.Fatalf("shutdown completion was not logged: %s", output.String())
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

type startupFakeRuntime struct{ stopped string }

func (*startupFakeRuntime) Name() string { return "test-runtime" }
func (*startupFakeRuntime) Run(context.Context, sandbox.Spec) (sandbox.ExecutionResult, error) {
	return sandbox.ExecutionResult{}, errors.New("unexpected execution")
}
func (r *startupFakeRuntime) Stop(_ context.Context, name string) error { r.stopped = name; return nil }
func (*startupFakeRuntime) RemoveExecutionVolumes(string) error         { return nil }

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
