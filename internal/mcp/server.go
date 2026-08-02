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
)

type Server struct {
	root       string
	socketPath string
	listener   net.Listener
	registry   *Registry
	runtime    *Runtime
	wg         sync.WaitGroup
}

func NewServer(root, socketPath string, registry *Registry) *Server {
	if registry == nil {
		registry = DefaultRegistry()
	}
	return &Server{root: root, socketPath: socketPath, registry: registry}
}

func (s *Server) Start(ctx context.Context) error {
	runtime, cleanup, err := OpenRuntime(s.root)
	if err != nil {
		return err
	}
	s.runtime = runtime
	if err := os.MkdirAll(filepath.Dir(s.socketPath), 0o755); err != nil {
		cleanup()
		return fmt.Errorf("create mcp socket dir: %w", err)
	}
	if err := os.RemoveAll(s.socketPath); err != nil {
		cleanup()
		return fmt.Errorf("remove old socket: %w", err)
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		cleanup()
		return fmt.Errorf("listen on mcp socket: %w", err)
	}
	s.listener = ln
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = s.Close()
		cleanup()
	}()
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
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.handleConn(ctx, conn)
			}()
		}
	}()
	return nil
}

func (s *Server) Close() error {
	if s.listener == nil {
		return nil
	}
	err := s.listener.Close()
	s.listener = nil
	return err
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
