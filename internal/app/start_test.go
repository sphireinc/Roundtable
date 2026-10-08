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

	"roundtable/internal/db"
	"roundtable/internal/processlock"
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
	err := stopCoordinator(ctx, cancel, httpDone, coordinatorDone, server, &output, &output)
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
