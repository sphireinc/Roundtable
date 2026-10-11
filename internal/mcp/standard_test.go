package mcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"roundtable/internal/agentauth"
	"roundtable/internal/db"
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

func TestAgentStandardServerExposesOnlyCapabilityTools(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	ctx := context.Background()
	snapshotRoot := testAgentSnapshotRoot(t, root, "S-1")
	if err := os.WriteFile(filepath.Join(snapshotRoot, "README.md"), []byte("assigned source\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := runtime.store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Assigned", Status: "in_progress", AssignedAgentID: "A-1", ReadablePaths: []string{"README.md"}}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}); err != nil {
		t.Fatal(err)
	}
	authority := agentauth.New(nil)
	runtime.SetAgentAuthority(authority)
	identity := agentauth.Identity{SessionID: "S-1", AgentID: "A-1", TaskID: "T-1", RunID: "RUN-1", Role: "implementer", BaseRevision: "rev-1", BaseStateHash: "tree-1", SnapshotRoot: snapshotRoot}
	token := issueRuntimeTestCapability(t, runtime, authority, identity)
	if err := runtime.store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "A-1", TaskID: "T-1", ClaimType: "read", Status: "active", ExecutionSessionID: "S-1"}); err != nil {
		t.Fatal(err)
	}
	server, err := NewAgentStandardServer(runtime, token)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewStreamableHTTPHandler(server, HTTPOptions{Address: "127.0.0.1:7117"})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox does not permit loopback listeners: %v", err)
	}
	httpServer := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler}}
	httpServer.Start()
	defer httpServer.Close()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "agent-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: httpServer.URL + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	page, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	foundRead, foundProposal, foundApply, foundAdmin := false, false, false, false
	for _, tool := range page.Tools {
		foundRead = foundRead || tool.Name == "repo.read_file"
		foundProposal = foundProposal || tool.Name == "proposal.create"
		foundApply = foundApply || tool.Name == "patch.apply"
		foundAdmin = foundAdmin || tool.Name == "task.create"
	}
	if !foundRead || !foundProposal || foundApply || foundAdmin {
		t.Fatalf("unexpected implementer MCP tools: read=%v proposal=%v apply=%v admin=%v", foundRead, foundProposal, foundApply, foundAdmin)
	}
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "repo.read_file", Arguments: map[string]any{"path": "README.md"}})
	if err != nil || result.IsError {
		t.Fatalf("authorized read failed: result=%+v err=%v", result, err)
	}
	if err := authority.Revoke("S-1"); err != nil {
		t.Fatal(err)
	}
	result, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "repo.read_file", Arguments: map[string]any{"path": "README.md"}})
	if err != nil || !result.IsError {
		t.Fatalf("revoked token remained usable: result=%+v err=%v", result, err)
	}
}

func TestScopedAgentHTTPHandlerBindsAndRechecksCapability(t *testing.T) {
	root := newRuntimeRoot(t)
	runtime, cleanup, err := OpenRuntime(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	authority := agentauth.New(nil)
	runtime.SetAgentAuthority(authority)
	token, err := authority.Issue(agentauth.Identity{SessionID: "S-http-agent", AgentID: "A-1", TaskID: "T-1", RunID: "RUN-1", Role: "implementer", BaseRevision: "rev-1", BaseStateHash: "tree-1"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("sandbox does not permit loopback listeners: %v", err)
	}
	address := listener.Addr().String()
	httpServer := &http.Server{Handler: NewRoundtableHTTPHandler(runtime, HTTPOptions{Address: address})}
	go func() { _ = httpServer.Serve(listener) }()
	t.Cleanup(func() { _ = httpServer.Close() })
	for _, authorization := range []string{"", "Bearer invalid-capability"} {
		request, err := http.NewRequest(http.MethodPost, "http://"+address+"/agent/mcp", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("agent HTTP authorization %q returned %d, want 401", authorization, response.StatusCode)
		}
	}

	ctx := context.Background()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "agent-http-test", Version: "test"}, nil)
	transport := func(bearer string) *sdkmcp.StreamableClientTransport {
		httpClient := &http.Client{Transport: bearerRoundTripper{token: bearer, next: http.DefaultTransport}}
		return &sdkmcp.StreamableClientTransport{Endpoint: "http://" + address + "/agent/mcp", HTTPClient: httpClient, DisableStandaloneSSE: true}
	}
	session, err := client.Connect(ctx, transport(token), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	foundProposal, foundAdmin, foundApply := false, false, false
	for _, tool := range tools.Tools {
		foundProposal = foundProposal || tool.Name == "proposal.create"
		foundAdmin = foundAdmin || tool.Name == "task.create"
		foundApply = foundApply || tool.Name == "patch.apply"
	}
	if !foundProposal || foundAdmin || foundApply {
		t.Fatalf("scoped implementer tools: proposal=%v admin=%v direct-apply=%v", foundProposal, foundAdmin, foundApply)
	}
	adminSession, err := client.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: "http://" + address + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer adminSession.Close()
	adminTools, err := adminSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	adminSurfaceContainsTaskCreate := false
	for _, tool := range adminTools.Tools {
		adminSurfaceContainsTaskCreate = adminSurfaceContainsTaskCreate || tool.Name == "task.create"
	}
	if !adminSurfaceContainsTaskCreate {
		t.Fatal("coordinator MCP surface unexpectedly lost its admin tool while adding scoped agent endpoint")
	}
	if err := authority.Revoke("S-http-agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.ListTools(ctx, nil); err == nil {
		t.Fatal("revoked bearer capability remained accepted by agent HTTP endpoint")
	}
}

type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(clone)
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
