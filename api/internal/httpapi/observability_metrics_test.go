package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsExposeLowCardinalityRequestCounters(t *testing.T) {
	server := NewServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("X-Correlation-ID", "corr-test")
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Header().Get("X-Correlation-ID") != "corr-test" {
		t.Fatalf("correlation id = %q", res.Header().Get("X-Correlation-ID"))
	}
	metrics := httptest.NewRecorder()
	server.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), "roundtable_http_requests_total") || !strings.Contains(metrics.Body.String(), "roundtable_http_request_duration_nanoseconds_total") {
		t.Fatalf("metrics = %s", metrics.Body.String())
	}
}
