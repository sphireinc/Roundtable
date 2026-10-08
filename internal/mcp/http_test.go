package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPOptionsRequireTLSAndTokenForRemoteBind(t *testing.T) {
	if err := (HTTPOptions{Address: "0.0.0.0:7117"}).Validate(); err == nil {
		t.Fatal("remote listener accepted without TLS and token")
	}
	if err := (HTTPOptions{Address: "0.0.0.0:7117", Token: "secret", TLSCertFile: "cert", TLSKeyFile: "key"}).Validate(); err != nil {
		t.Fatalf("valid authenticated TLS listener rejected: %v", err)
	}
	if err := (HTTPOptions{Address: "127.0.0.1:7117"}).Validate(); err != nil {
		t.Fatalf("loopback listener rejected: %v", err)
	}
}

func TestHTTPMiddlewareRejectsHostOriginAndMissingBearer(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	loopback := protectMCPHTTP(next, HTTPOptions{Address: "127.0.0.1:7117"})
	badHost := httptest.NewRequest(http.MethodPost, "http://example.invalid/mcp", nil)
	badHost.Host = "example.invalid:7117"
	response := httptest.NewRecorder()
	loopback.ServeHTTP(response, badHost)
	if response.Code != http.StatusForbidden {
		t.Fatalf("Host status = %d, want 403", response.Code)
	}
	badOrigin := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7117/mcp", nil)
	badOrigin.Host = "127.0.0.1:7117"
	badOrigin.Header.Set("Origin", "http://attacker.invalid")
	response = httptest.NewRecorder()
	loopback.ServeHTTP(response, badOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("Origin status = %d, want 403", response.Code)
	}
	wrongScheme := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7117/mcp", nil)
	wrongScheme.Host = "127.0.0.1:7117"
	wrongScheme.Header.Set("Origin", "https://127.0.0.1:7117")
	response = httptest.NewRecorder()
	loopback.ServeHTTP(response, wrongScheme)
	if response.Code != http.StatusForbidden {
		t.Fatalf("scheme-mismatched Origin status = %d, want 403", response.Code)
	}
	remote := protectMCPHTTP(next, HTTPOptions{Address: "192.0.2.10:7117", Token: "expected", TLSCertFile: "cert", TLSKeyFile: "key"})
	request := httptest.NewRequest(http.MethodPost, "https://192.0.2.10:7117/mcp", nil)
	request.Host = "192.0.2.10:7117"
	response = httptest.NewRecorder()
	remote.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", response.Code)
	}
	request.Header.Set("Authorization", "Bearer expected")
	response = httptest.NewRecorder()
	remote.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authenticated remote status = %d, want 204", response.Code)
	}
}
