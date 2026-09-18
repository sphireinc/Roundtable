package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestIdempotencyReplaysSuccessfulMutationAndRejectsPayloadReuse(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	server := NewServer(Config{Store: db.NewStore(sqlDB, nil), AllowedWorkspaceRoots: []string{root}})
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewBufferString(body))
		req.Header.Set("X-Actor-ID", "human-1")
		req.Header.Set("Idempotency-Key", "workspace-create-1")
		req.Header.Set("X-Request-ID", "req-test")
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		return res
	}
	body := `{"display_name":"Demo","root_alias":"demo","root_path":"` + root + `","default_branch":"main"}`
	first := post(body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create = %d %s", first.Code, first.Body.String())
	}
	second := post(body)
	if second.Code != first.Code || second.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s; first = %d %s", second.Code, second.Body.String(), first.Code, first.Body.String())
	}
	reused := post(`{"display_name":"Other","root_alias":"other","root_path":"` + root + `","default_branch":"main"}`)
	if reused.Code != http.StatusConflict || !bytes.Contains(reused.Body.Bytes(), []byte(`"code":"idempotency_key_reuse"`)) {
		t.Fatalf("payload reuse = %d %s", reused.Code, reused.Body.String())
	}
}
