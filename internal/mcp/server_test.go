package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestServerRefusesNonSocketPathWithoutRemovingIt(t *testing.T) {
	root := newRuntimeRoot(t)
	socketPath := shortSocketPath(t)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(socketPath, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(root, socketPath, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err == nil {
		t.Fatal("Start succeeded over a regular file")
	}
	data, err := os.ReadFile(socketPath)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("non-socket path changed: data=%q err=%v", data, err)
	}
}

func TestServerRefusesLiveSocketWithoutUnlinking(t *testing.T) {
	root := newRuntimeRoot(t)
	socketPath := shortSocketPath(t)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if socketUnavailable(err) {
			t.Skipf("sandbox does not permit Unix sockets: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	server := NewServer(root, socketPath, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err == nil {
		t.Fatal("Start succeeded over a live socket")
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("live socket was disrupted: %v", err)
	}
	_ = conn.Close()
}

func TestServerReplacesOnlyStaleSocket(t *testing.T) {
	root := newRuntimeRoot(t)
	socketPath := shortSocketPath(t)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	stale, err := net.Listen("unix", socketPath)
	if err != nil {
		if socketUnavailable(err) {
			t.Skipf("sandbox does not permit Unix sockets: %v", err)
		}
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	server := NewServer(root, socketPath, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := server.Start(ctx); err != nil {
		if socketUnavailable(err) {
			t.Skipf("sandbox does not permit Unix sockets: %v", err)
		}
		t.Fatalf("Start with stale socket: %v", err)
	}
	_ = server.Close()
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed server left socket path behind: %v", err)
	}
}

func TestServerCloseDrainsClientAndLeavesInjectedRuntimeOpen(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	socketPath := shortSocketPath(t)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithRuntime(runtime, socketPath, nil)
	if err := server.Start(context.Background()); err != nil {
		if socketUnavailable(err) {
			t.Skipf("sandbox does not permit Unix sockets: %v", err)
		}
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not drain an idle connection")
	}
	if _, err := runtime.Call(context.Background(), "table.get_state", map[string]any{}); err != nil {
		t.Fatalf("server closed its caller-owned runtime: %v", err)
	}
}

func TestServerCloseAllowsInFlightToolCallToFinish(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	lockConn, err := runtime.Store().DB().Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	socketPath := shortSocketPath(t)
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	server := NewServerWithRuntime(runtime, socketPath, nil)
	if err := server.Start(context.Background()); err != nil {
		if socketUnavailable(err) {
			t.Skipf("sandbox does not permit Unix sockets: %v", err)
		}
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"tool":"task.create","args":{"title":"drained","body_md":"finish before shutdown"}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for runtime.Store().DB().Stats().InUse < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if inUse := runtime.Store().DB().Stats().InUse; inUse < 2 {
		t.Fatalf("tool call did not reach SQLite while write lock was held; connections in use = %d", inUse)
	}
	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	time.AfterFunc(100*time.Millisecond, func() {
		_, _ = lockConn.ExecContext(context.Background(), "ROLLBACK")
	})
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response from in-flight call: %v", err)
	}
	var response CallResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("decode in-flight response: %v", err)
	}
	if !response.OK {
		t.Fatalf("in-flight tool call failed during shutdown: %+v", response)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server close did not finish after in-flight call drained")
	}
}

func socketUnavailable(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EOPNOTSUPP)
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "rts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "rt.sock")
}
