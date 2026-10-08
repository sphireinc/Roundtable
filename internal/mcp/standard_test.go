package mcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStandardToolSchemasResolve(t *testing.T) {
	registry := DefaultRegistry()
	byName := make(map[string]Tool, len(registry.Tools()))
	for _, tool := range registry.Tools() {
		byName[tool.Name] = tool
	}
	for _, name := range registry.StandardToolNames() {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("standard tool %q is absent from registry", name)
		}
		if _, err := resolveToolSchema(tool.InputSchema); err != nil {
			t.Errorf("resolve schema for %q: %v", name, err)
		}
	}
}

func TestStandardHTTPListsOnlyImplementedToolsAndCallsRuntime(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	handler := NewStreamableHTTPHandler(NewStandardServer(runtime), HTTPOptions{Address: "127.0.0.1:7117"})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox does not permit loopback listeners: %v", err)
	}
	httpServer := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	httpServer.Start()
	defer httpServer.Close()

	ctx := context.Background()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "roundtable-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	foundState, foundUnimplemented := false, false
	for _, tool := range tools.Tools {
		foundState = foundState || tool.Name == "table.get_state"
		foundUnimplemented = foundUnimplemented || tool.Name == "repo.dependency_context"
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
		}
	}
	if !foundState || foundUnimplemented {
		t.Fatalf("tools include implemented table.get_state=%v, unimplemented repo.dependency_context=%v", foundState, foundUnimplemented)
	}
	if len(tools.Tools) != len(DefaultRegistry().StandardToolNames()) {
		t.Fatalf("server returned %d tools, registry supports %d", len(tools.Tools), len(DefaultRegistry().StandardToolNames()))
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "table.get_state", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call table.get_state: %v", err)
	}
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("unexpected tool result: %+v", result)
	}
	invalid, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "agent.turn_request", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("invalid argument call returned protocol error: %v", err)
	}
	if !invalid.IsError {
		t.Fatalf("missing required argument was accepted: %+v", invalid)
	}
}

func TestStdioBridgeForwardsHTTPMCPProtocolWithoutExtraStdout(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox does not permit loopback listeners: %v", err)
	}
	address := listener.Addr().String()
	httpServer := &httptest.Server{Listener: listener, Config: &http.Server{Handler: NewStreamableHTTPHandler(NewStandardServer(runtime), HTTPOptions{Address: address})}}
	httpServer.Start()
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	var stderr io.Writer = io.Discard
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- RunStdioBridge(ctx, stdinReader, stdoutWriter, stderr, httpServer.URL+"/mcp", HTTPAuth{})
	}()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "stdio-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.IOTransport{Reader: stdoutReader, Writer: writerCloser{stdinWriter}}, nil)
	if err != nil {
		t.Fatalf("connect SDK client over stdio: %v", err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools through stdio bridge: %v", err)
	}
	found := false
	for _, tool := range tools.Tools {
		found = found || tool.Name == "table.get_state"
	}
	if !found {
		t.Fatal("stdio bridge did not forward the coordinator tool list")
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "table.get_state", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("stdio call result=%+v err=%v", result, err)
	}
	_ = session.Close()
	cancel()
	_ = stdinWriter.Close()
	_ = stdoutReader.Close()
	select {
	case <-bridgeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("stdio bridge did not exit after cancellation")
	}
}
