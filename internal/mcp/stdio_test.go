package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"roundtable/internal/agentauth"
)

func TestBridgeServerForwardsToolDiscoveryAndCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "upstream", Version: "test"}, nil)
	upstream.AddTool(&sdkmcp.Tool{Name: "echo", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}}}, func(_ context.Context, request *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		var input map[string]any
		if err := json.Unmarshal(request.Params.Arguments, &input); err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: input["value"].(string)}}}, nil
	})
	upstreamServerTransport, upstreamClientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = upstream.Run(ctx, upstreamServerTransport) }()
	upstreamClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "bridge-test", Version: "test"}, nil)
	remoteSession, err := upstreamClient.Connect(ctx, upstreamClientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer remoteSession.Close()
	bridge, err := newBridgeServer(ctx, remoteSession)
	if err != nil {
		t.Fatal(err)
	}
	bridgeServerTransport, bridgeClientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = bridge.Run(ctx, bridgeServerTransport) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "local-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, bridgeClientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" {
		t.Fatalf("forwarded tools = %+v, err=%v", tools, err)
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "echo", Arguments: map[string]any{"value": "forwarded"}})
	if err != nil || result.IsError || len(result.Content) != 1 || result.Content[0].(*sdkmcp.TextContent).Text != "forwarded" {
		t.Fatalf("forwarded call result = %+v, err=%v", result, err)
	}
	cancel()
	time.Sleep(time.Millisecond)
}

func TestStdioBridgeUnavailableCoordinatorKeepsStdoutProtocolClean(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := RunStdioBridge(context.Background(), strings.NewReader(""), &stdout, &stderr, "http://127.0.0.1:1/mcp", HTTPAuth{Token: "do-not-print"})
	if err == nil {
		t.Fatal("expected unavailable coordinator error")
	}
	if stdout.Len() != 0 {
		t.Fatalf("startup failure wrote non-protocol stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "coordinator endpoint unavailable") || strings.Contains(stderr.String(), "do-not-print") {
		t.Fatalf("stderr should be useful and credential-free: %q", stderr.String())
	}
}

func TestStdioBridgeUsesScopedAgentMCPOverUnixSocket(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	authority := agentauth.New(nil)
	runtime.SetAgentAuthority(authority)
	token, err := authority.Issue(agentauth.Identity{
		SessionID: "S-stdio-unix", AgentID: "A-implementer", RunID: "R-1", TaskID: "T-1", Role: "implementer",
		BaseRevision: "revision-1", BaseStateHash: "tree-1",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(t.TempDir(), "agent-mcp.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("sandbox does not permit Unix listeners: %v", err)
	}
	handler, err := NewScopedAgentHTTPHandler(runtime, HTTPOptions{Address: "127.0.0.1:7117"}, token)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	otherToken, err := authority.Issue(agentauth.Identity{
		SessionID: "S-other", AgentID: "A-other", RunID: "R-1", TaskID: "T-2", Role: "reviewer",
		BaseRevision: "revision-1", BaseStateHash: "tree-1",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	unixHTTP := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}}
	wrongTokenRequest, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:7117/agent/mcp", nil)
	wrongTokenRequest.Header.Set("Authorization", "Bearer "+otherToken)
	wrongTokenResponse, err := unixHTTP.Do(wrongTokenRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = wrongTokenResponse.Body.Close()
	if wrongTokenResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("per-session socket accepted another execution capability: %d", wrongTokenResponse.StatusCode)
	}
	adminResponse, err := unixHTTP.Get("http://127.0.0.1:7117/mcp")
	if err != nil {
		t.Fatal(err)
	}
	_ = adminResponse.Body.Close()
	if adminResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("per-session socket exposed coordinator MCP: %d", adminResponse.StatusCode)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	bridgeDone := make(chan error, 1)
	go func() {
		bridgeDone <- RunStdioBridge(ctx, stdinReader, stdoutWriter, io.Discard,
			"http://127.0.0.1:7117/agent/mcp", HTTPAuth{Token: token, UnixSocket: socketPath})
	}()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "stdio-unix-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.IOTransport{Reader: stdoutReader, Writer: writerCloser{stdinWriter}}, nil)
	if err != nil {
		t.Fatalf("connect through Unix MCP bridge: %v", err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "patch.apply" || tool.Name == "task.create" || tool.Name == "vote.cast" {
			t.Fatalf("implementer received privileged tool %q over Unix bridge", tool.Name)
		}
	}
	if _, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "task.list"}); err != nil {
		t.Fatalf("authorized implementer tool failed over Unix bridge: %v", err)
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "patch.apply"})
	if err != nil || !result.IsError {
		t.Fatalf("privileged operation was not rejected: result=%+v err=%v", result, err)
	}
	_ = session.Close()
	cancel()
	_ = stdinWriter.Close()
	_ = stdoutReader.Close()
	select {
	case <-bridgeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Unix MCP stdio bridge did not stop after cancellation")
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("expected socket to remain until supervisor cleanup: %v", err)
	}
}
