package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type HTTPAuth struct {
	Token      string
	UnixSocket string
}

type writerCloser struct{ io.Writer }

func (writerCloser) Close() error { return nil }

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}

func RunStdioBridge(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, endpoint string, auth HTTPAuth) error {
	if stdin == nil || stdout == nil || stderr == nil {
		return fmt.Errorf("stdio bridge requires stdin, stdout, and stderr")
	}
	if endpoint == "" {
		return fmt.Errorf("MCP HTTP endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return fmt.Errorf("MCP HTTP endpoint must be an http(s) URL")
	}
	if auth.Token != "" && parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("bearer authentication requires HTTPS except for loopback endpoints")
	}
	var transport http.RoundTripper = http.DefaultTransport
	if auth.UnixSocket != "" {
		if auth.Token == "" || parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) || parsed.Path != "/agent/mcp" || parsed.RawPath != "" {
			return fmt.Errorf("Unix MCP transport requires a scoped bearer and the loopback /agent/mcp endpoint")
		}
		if !filepath.IsAbs(auth.UnixSocket) || filepath.Clean(auth.UnixSocket) != auth.UnixSocket {
			return fmt.Errorf("Unix MCP socket path must be absolute and clean")
		}
		info, err := os.Lstat(auth.UnixSocket)
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("Unix MCP socket is unavailable")
		}
		dialer := &net.Dialer{}
		unixTransport := http.DefaultTransport.(*http.Transport).Clone()
		unixTransport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", auth.UnixSocket)
		}
		transport = unixTransport
	}
	httpClient := &http.Client{Transport: bearerTransport{base: transport, token: auth.Token}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	defer httpClient.CloseIdleConnections()
	remote := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "roundtable-stdio-bridge", Version: "0.1.0"}, nil)
	remoteSession, err := remote.Connect(ctx, &sdkmcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "roundtable mcp stdio: coordinator endpoint unavailable")
		return fmt.Errorf("connect to Roundtable coordinator")
	}
	defer remoteSession.Close()
	bridge, err := newBridgeServer(ctx, remoteSession)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "roundtable mcp stdio: unable to discover coordinator tools")
		return fmt.Errorf("discover Roundtable coordinator tools")
	}
	if err := bridge.Run(ctx, &sdkmcp.IOTransport{Reader: io.NopCloser(stdin), Writer: writerCloser{stdout}}); err != nil && ctx.Err() == nil {
		_, _ = fmt.Fprintln(stderr, "roundtable mcp stdio: MCP session stopped")
		return fmt.Errorf("run stdio MCP session")
	}
	return nil
}

func newBridgeServer(ctx context.Context, remote *sdkmcp.ClientSession) (*sdkmcp.Server, error) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "roundtable", Version: "0.1.0"}, nil)
	var cursor string
	for {
		page, err := remote.ListTools(ctx, &sdkmcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, remoteTool := range page.Tools {
			tool := *remoteTool
			server.AddTool(&tool, func(callCtx context.Context, request *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				params := &sdkmcp.CallToolParams{Name: request.Params.Name}
				if len(request.Params.Arguments) > 0 {
					params.Arguments = json.RawMessage(request.Params.Arguments)
				}
				return remote.CallTool(callCtx, params)
			})
		}
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			return nil, fmt.Errorf("coordinator repeated tool-list cursor")
		}
		cursor = page.NextCursor
	}
	return server, nil
}
