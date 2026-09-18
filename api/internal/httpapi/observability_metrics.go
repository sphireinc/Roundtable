package httpapi

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

type httpMetrics struct {
	requests      atomic.Int64
	errors        atomic.Int64
	durationNanos atomic.Int64
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func withObservability(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = newRequestID()
		}
		w.Header().Set("X-Correlation-ID", correlationID)
		response := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(response, r)
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started)
		s.metrics.requests.Add(1)
		s.metrics.durationNanos.Add(duration.Nanoseconds())
		if status >= 400 {
			s.metrics.errors.Add(1)
		}
		requestIDValue := response.Header().Get("X-Request-ID")
		s.config.Logger.Info("http_request", "request_id", requestIDValue, "correlation_id", correlationID, "workspace_id", r.Header.Get("X-Workspace-ID"), "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", duration.Milliseconds())
	})
}

func (s *Server) metricsAPI(w http.ResponseWriter, _ *http.Request) {
	requests := s.metrics.requests.Load()
	duration := s.metrics.durationNanos.Load()
	writeMetric := func(name, help string, value int64) {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%d\n", name, help, name, value)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writeMetric("roundtable_http_requests_total", "HTTP requests handled.", requests)
	writeMetric("roundtable_http_errors_total", "HTTP responses with status 400 or greater.", s.metrics.errors.Load())
	writeMetric("roundtable_http_request_duration_nanoseconds_total", "Total HTTP request duration in nanoseconds.", duration)
}
