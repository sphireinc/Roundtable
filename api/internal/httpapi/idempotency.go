package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync"
)

type idempotencyRecord struct {
	fingerprint string
	status      int
	headers     http.Header
	body        []byte
}

type idempotencyStore struct {
	mu      sync.Mutex
	records map[string]idempotencyRecord
}

type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *captureWriter) Header() http.Header { return w.header }
func (w *captureWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *captureWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func withIdempotency(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodPut && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			WriteProblem(w, r, http.StatusBadRequest, "invalid_request_body", "Request body could not be read", err.Error())
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		fingerprint := requestFingerprint(r, body)
		cacheKey := r.Method + " " + r.URL.RequestURI() + "\x00" + r.Header.Get("X-Actor-ID") + "\x00" + key
		s.idempotency.mu.Lock()
		record, found := s.idempotency.records[cacheKey]
		s.idempotency.mu.Unlock()
		if found {
			if record.fingerprint != fingerprint {
				WriteProblem(w, r, http.StatusConflict, "idempotency_key_reuse", "Idempotency key was reused", "The key is already bound to a different request payload")
				return
			}
			for name, values := range record.headers {
				if name == "X-Request-Id" {
					continue
				}
				for _, value := range values {
					w.Header().Add(name, value)
				}
			}
			w.WriteHeader(record.status)
			_, _ = w.Write(record.body)
			return
		}
		capture := &captureWriter{header: make(http.Header)}
		next.ServeHTTP(capture, r)
		for name, values := range capture.header {
			if name == "X-Request-Id" {
				continue
			}
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		status := capture.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write(capture.body.Bytes())
		// Empty-body lifecycle transitions are intentionally re-evaluated on
		// retry so a second attempt receives the current typed state conflict;
		// body-bearing create/update requests are replay-safe.
		if len(body) > 0 && status >= 200 && status < 500 {
			s.idempotency.mu.Lock()
			s.idempotency.records[cacheKey] = idempotencyRecord{fingerprint: fingerprint, status: status, headers: cloneHeaders(capture.header), body: append([]byte(nil), capture.body.Bytes()...)}
			s.idempotency.mu.Unlock()
		}
	})
}

func requestFingerprint(r *http.Request, body []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(r.Method + "\n" + r.URL.RequestURI() + "\n"))
	_, _ = hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneHeaders(source http.Header) http.Header {
	cloned := make(http.Header, len(source))
	for name, values := range source {
		cloned[name] = append([]string(nil), values...)
	}
	return cloned
}
