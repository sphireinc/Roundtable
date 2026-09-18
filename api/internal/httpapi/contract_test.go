package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContractSmokeRoutesReturnDeclaredContentTypesAndRequestIDs(t *testing.T) {
	server := NewServer(Config{})
	for _, route := range []struct {
		path        string
		contentType string
	}{
		{path: "/api/v1/health", contentType: "application/json"},
		{path: "/api/v1/status", contentType: "application/json"},
		{path: "/api/v1/security/capabilities", contentType: "application/json"},
		{path: "/metrics", contentType: "text/plain"},
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, route.path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", route.path, response.Code, response.Body.String())
		}
		if response.Header().Get("X-Request-ID") == "" || !strings.HasPrefix(response.Header().Get("Content-Type"), route.contentType) {
			t.Fatalf("%s headers = %v", route.path, response.Header())
		}
	}
}
