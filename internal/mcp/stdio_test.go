package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
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
