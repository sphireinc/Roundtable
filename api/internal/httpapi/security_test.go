package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityCapabilitiesDescribeSeparateBoundaries(t *testing.T) {
	server := NewServer(Config{HumanToken: "human-secret", AgentToken: "agent-secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/capabilities", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated capabilities = %d %s", res.Code, res.Body.String())
	}
	if res.Header().Get("X-Request-ID") == "" || !hasPart(res.Body.String(), `"request_id":"`) {
		t.Fatalf("unauthenticated response missing request id: headers=%v body=%s", res.Header(), res.Body.String())
	}
	req.Header.Set("Authorization", "Bearer human-secret")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK || !hasPart(res.Body.String(), `"authentication":"bearer-token"`) || !hasPart(res.Body.String(), `"agent_boundary"`) {
		t.Fatalf("capabilities = %d %s", res.Code, res.Body.String())
	}
}

func TestSecurityRejectsInvalidBearerAndCrossSiteMutation(t *testing.T) {
	server := NewServer(Config{HumanToken: "human-secret", AgentToken: "agent-secret"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer = %d %s", res.Code, res.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", nil)
	req.Header.Set("Authorization", "Bearer human-secret")
	req.Header.Set("X-Actor-ID", "human-1")
	req.Header.Set("Origin", "https://evil.example")
	res = httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden || !hasPart(res.Body.String(), `"code":"csrf_origin_forbidden"`) {
		t.Fatalf("cross-site mutation = %d %s", res.Code, res.Body.String())
	}
}

func hasPart(value, part string) bool {
	return len(value) >= len(part) && stringIndex(value, part) >= 0
}

func stringIndex(value, part string) int {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
