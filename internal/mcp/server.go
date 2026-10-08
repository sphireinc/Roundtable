package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	root           string
	socketPath     string
	listener       net.Listener
	socketInfo     os.FileInfo
	registry       *Registry
	runtime        *Runtime
	runtimeCleanup func()
	wg             sync.WaitGroup
	connectionsMu  sync.Mutex
	connections    map[net.Conn]bool
	closing        bool
	cancel         context.CancelFunc
	closeOnce      sync.Once
	closeErr       error
}

func NewServer(root, socketPath string, registry *Registry) *Server {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Server{root: root, socketPath: socketPath, registry: registry}
}

// NewServerWithRuntime serves the supplied runtime without taking ownership of it.
func NewServerWithRuntime(runtime *Runtime, socketPath string, registry *Registry) *Server {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Server{socketPath: socketPath, registry: registry, runtime: runtime}
}

func (s *Server) Start(ctx context.Context) error {
	if s.runtime == nil {
		runtime, cleanup, err := OpenRuntime(s.root)
		if err != nil {
			return err
		}
		s.runtime = runtime
		s.runtimeCleanup = cleanup
	}
	cleanupOnError := func(err error) error {
		if s.runtimeCleanup != nil {
			s.runtimeCleanup()
			s.runtimeCleanup = nil
			s.runtime = nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o755); err != nil {
		return cleanupOnError(fmt.Errorf("create mcp socket dir: %w", err))
	}
	if err := removeStaleSocket(s.socketPath); err != nil {
		return cleanupOnError(err)
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return cleanupOnError(fmt.Errorf("listen on mcp socket: %w", err))
	}
	socketInfo, err := os.Lstat(s.socketPath)
	if err != nil || socketInfo.Mode()&os.ModeSocket == 0 {
		_ = ln.Close()
		if err != nil {
			return cleanupOnError(fmt.Errorf("inspect bound MCP socket: %w", err))
		}
		return cleanupOnError(fmt.Errorf("bound MCP path is not a socket: %q", s.socketPath))
	}
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		if current, statErr := os.Lstat(s.socketPath); statErr == nil && os.SameFile(socketInfo, current) {
			_ = os.Remove(s.socketPath)
		}
		return cleanupOnError(fmt.Errorf("secure mcp socket: %w", err))
	}
	s.listener = ln
	s.socketInfo = socketInfo
	runCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.connections = make(map[net.Conn]bool)
	s.closing = false
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				select {
				case <-runCtx.Done():
					return
				default:
					continue
				}
			}
			s.connectionsMu.Lock()
			if s.closing {
				s.connectionsMu.Unlock()
				_ = conn.Close()
				continue
			}
			s.connections[conn] = false
			s.wg.Add(1)
			s.connectionsMu.Unlock()
			go func() {
				defer s.wg.Done()
				defer func() {
					s.connectionsMu.Lock()
					delete(s.connections, conn)
					s.connectionsMu.Unlock()
				}()
				s.handleConn(runCtx, conn)
			}()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-runCtx.Done():
		}
	}()
	return nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing mcp socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket MCP path %q", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("MCP socket is already active at %q", path)
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) {
		return fmt.Errorf("cannot confirm MCP socket is stale at %q: %w", path, dialErr)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("recheck stale MCP socket: %w", err)
	}
	if !os.SameFile(info, current) {
		return fmt.Errorf("MCP socket path changed during recovery: %q", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale MCP socket: %w", err)
	}
	return nil
}

func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		if s.listener != nil {
			s.closeErr = s.listener.Close()
		}
		s.connectionsMu.Lock()
		s.closing = true
		for conn, inFlight := range s.connections {
			if !inFlight {
				_ = conn.Close()
			}
		}
		s.connectionsMu.Unlock()
		drained := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(drained)
		}()
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			if s.cancel != nil {
				s.cancel()
			}
			s.connectionsMu.Lock()
			for conn := range s.connections {
				_ = conn.Close()
			}
			s.connectionsMu.Unlock()
			if s.closeErr == nil {
				s.closeErr = errors.New("MCP socket handlers did not drain before timeout")
			}
		}
		if s.cancel != nil {
			s.cancel()
		}
		if s.runtimeCleanup != nil {
			s.runtimeCleanup()
			s.runtimeCleanup = nil
		}
		if s.socketInfo != nil {
			if current, err := os.Lstat(s.socketPath); err == nil && os.SameFile(s.socketInfo, current) {
				_ = os.Remove(s.socketPath)
			}
		}
	})
	return s.closeErr
}

func (s *Server) Registry() *Registry {
	return s.registry
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	line = trimSpace(line)
	if len(line) == 0 {
		return
	}
	req, err := ParseCallRequest(line)
	if err != nil {
		data, _ := EncodeResponse(nil, err)
		_, _ = conn.Write(append(data, '\n'))
		return
	}
	s.connectionsMu.Lock()
	if s.closing {
		s.connectionsMu.Unlock()
		return
	}
	s.connections[conn] = true
	s.connectionsMu.Unlock()
	result, err := s.runtime.Call(ctx, req.Tool, req.Args)
	data, _ := EncodeResponse(result, err)
	_, _ = conn.Write(append(data, '\n'))
}

func trimSpace(data []byte) []byte {
	start := 0
	end := len(data)
	for start < end && (data[start] == ' ' || data[start] == '\n' || data[start] == '\r' || data[start] == '\t') {
		start++
	}
	for end > start && (data[end-1] == ' ' || data[end-1] == '\n' || data[end-1] == '\r' || data[end-1] == '\t') {
		end--
	}
	return data[start:end]
}
