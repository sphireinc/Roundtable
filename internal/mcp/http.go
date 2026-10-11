package mcp

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type HTTPOptions struct {
	Address     string
	Token       string
	AgentToken  string
	TLSCertFile string
	TLSKeyFile  string
	Health      func() string
}

func (o HTTPOptions) Validate() error {
	host, _, err := net.SplitHostPort(o.Address)
	if err != nil {
		return fmt.Errorf("invalid MCP HTTP address: %w", err)
	}
	if (o.TLSCertFile == "") != (o.TLSKeyFile == "") {
		return errors.New("MCP HTTP TLS certificate and key must be configured together")
	}
	if isLoopbackHost(host) {
		return nil
	}
	if o.Token == "" || o.TLSCertFile == "" || o.TLSKeyFile == "" {
		return errors.New("non-loopback MCP HTTP requires TLS certificate/key and bearer token")
	}
	return nil
}

func NewStreamableHTTPHandler(server *sdkmcp.Server, options HTTPOptions) http.Handler {
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, &sdkmcp.StreamableHTTPOptions{
		Stateless:             true,
		CrossOriginProtection: http.NewCrossOriginProtection(),
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", protectMCPHTTP(mcpHandler, options))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := "ready"
		if options.Health != nil {
			status = options.Health()
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
	})
	return mux
}

// NewRoundtableHTTPHandler keeps the coordinator surface at /mcp and adds a
// separate /agent/mcp endpoint whose tool set is derived from each bearer.
func NewRoundtableHTTPHandler(runtime *Runtime, options HTTPOptions) http.Handler {
	admin := NewStreamableHTTPHandler(NewStandardServer(runtime), options)
	agent := sdkmcp.NewStreamableHTTPHandler(func(request *http.Request) *sdkmcp.Server {
		token := bearerToken(request)
		server, err := NewAgentStandardServer(runtime, token)
		if err != nil {
			return sdkmcp.NewServer(&sdkmcp.Implementation{Name: "roundtable-agent", Version: "0.1.0"}, nil)
		}
		return server
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true, CrossOriginProtection: http.NewCrossOriginProtection()})
	mux := http.NewServeMux()
	mux.Handle("/agent/mcp", protectScopedAgentMCPHTTP(agent, runtime, options))
	mux.Handle("/", admin)
	return mux
}

// NewAgentHTTPHandler exposes only the capability-authenticated agent endpoint.
// It is suitable for a per-session Unix socket and never registers admin MCP.
func NewAgentHTTPHandler(runtime *Runtime, options HTTPOptions) http.Handler {
	return newAgentHTTPHandler(runtime, options, nil)
}

// NewScopedAgentHTTPHandler pins a per-execution socket to one capability.
func NewScopedAgentHTTPHandler(runtime *Runtime, options HTTPOptions, token string) (http.Handler, error) {
	if token == "" {
		return nil, errors.New("scoped agent MCP handler requires a capability")
	}
	server, err := NewAgentStandardServer(runtime, token)
	if err != nil {
		return nil, err
	}
	options.AgentToken = token
	return newAgentHTTPHandler(runtime, options, server), nil
}

func newAgentHTTPHandler(runtime *Runtime, options HTTPOptions, pinned *sdkmcp.Server) http.Handler {
	agent := sdkmcp.NewStreamableHTTPHandler(func(request *http.Request) *sdkmcp.Server {
		if pinned != nil {
			return pinned
		}
		token := bearerToken(request)
		server, err := NewAgentStandardServer(runtime, token)
		if err != nil {
			return sdkmcp.NewServer(&sdkmcp.Implementation{Name: "roundtable-agent", Version: "0.1.0"}, nil)
		}
		return server
	}, &sdkmcp.StreamableHTTPOptions{Stateless: true, CrossOriginProtection: http.NewCrossOriginProtection()})
	mux := http.NewServeMux()
	if pinned != nil {
		mux.Handle("/agent/mcp", protectMCPHTTP(agent, options))
	} else {
		mux.Handle("/agent/mcp", protectScopedAgentMCPHTTP(agent, runtime, options))
	}
	return mux
}

func NewAgentStreamableHTTPHandler(server *sdkmcp.Server, address, token string) (http.Handler, error) {
	if token == "" {
		return nil, errors.New("agent MCP endpoint requires a scoped capability token")
	}
	return NewStreamableHTTPHandler(server, HTTPOptions{Address: address, AgentToken: token}), nil
}

func protectMCPHTTP(next http.Handler, options HTTPOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validateMCPHTTPRequest(w, r, options) {
			return
		}
		if options.AgentToken != "" {
			if !bearerMatches(r, options.AgentToken) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if !isLoopbackAddress(options.Address) {
			if !bearerMatches(r, options.Token) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func protectScopedAgentMCPHTTP(next http.Handler, runtime *Runtime, options HTTPOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validateMCPHTTPRequest(w, r, options) {
			return
		}
		token := bearerToken(r)
		if token == "" || runtime == nil || runtime.agentAuth == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, err := runtime.agentAuth.Resolve(token); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validateMCPHTTPRequest(w http.ResponseWriter, r *http.Request, options HTTPOptions) bool {
	if err := options.Validate(); err != nil {
		http.Error(w, "MCP HTTP listener is not securely configured", http.StatusServiceUnavailable)
		return false
	}
	requestHost := hostName(r.Host)
	if requestHost == "" || (!isLoopbackHost(requestHost) && isLoopbackAddress(options.Address)) {
		http.Error(w, "invalid Host header", http.StatusForbidden)
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		requestScheme := "http"
		if r.TLS != nil {
			requestScheme = "https"
		}
		if err != nil || !strings.EqualFold(parsed.Scheme, requestScheme) || !strings.EqualFold(parsed.Host, r.Host) || (!isLoopbackHost(parsed.Hostname()) && isLoopbackAddress(options.Address)) {
			http.Error(w, "invalid Origin header", http.StatusForbidden)
			return false
		}
	}
	return true
}

func bearerMatches(r *http.Request, expected string) bool {
	provided := bearerToken(r)
	return provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(header, "Bearer ")
}

func hostName(hostport string) string {
	if name, _, err := net.SplitHostPort(hostport); err == nil {
		return name
	}
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return strings.Trim(hostport, "[]")
	}
	if strings.Contains(hostport, ":") {
		return ""
	}
	return hostport
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	return err == nil && isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
