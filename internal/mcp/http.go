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

func protectMCPHTTP(next http.Handler, options HTTPOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := options.Validate(); err != nil {
			http.Error(w, "MCP HTTP listener is not securely configured", http.StatusServiceUnavailable)
			return
		}
		requestHost := hostName(r.Host)
		if requestHost == "" || (!isLoopbackHost(requestHost) && isLoopbackAddress(options.Address)) {
			http.Error(w, "invalid Host header", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			requestScheme := "http"
			if r.TLS != nil {
				requestScheme = "https"
			}
			if err != nil || !strings.EqualFold(parsed.Scheme, requestScheme) || !strings.EqualFold(parsed.Host, r.Host) || (!isLoopbackHost(parsed.Hostname()) && isLoopbackAddress(options.Address)) {
				http.Error(w, "invalid Origin header", http.StatusForbidden)
				return
			}
		}
		if !isLoopbackAddress(options.Address) {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(options.Token)) != 1 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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
