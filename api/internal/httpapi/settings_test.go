package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"roundtable/internal/db"
	"testing"
)

func TestSettingsEffectivePreflightConcurrencyAndSecrets(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-settings", DisplayName: "Settings", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	base := "/api/v1/workspaces/ws-settings/settings"
	get := httptest.NewRequest(http.MethodGet, base, nil)
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, get)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"source":"default"`)) {
		t.Fatalf("effective=%d %s", out.Code, out.Body.String())
	}
	pre := httptest.NewRequest(http.MethodPost, base+"/preflight", bytes.NewBufferString(`{"section":"governance","values":{"quorum":2}}`))
	pre.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, pre)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte("confirmation_token")) {
		t.Fatalf("preflight=%d %s", out.Code, out.Body.String())
	}
	token := settingsConfirmationToken("ws-settings", "governance", 0)
	update := httptest.NewRequest(http.MethodPatch, base+"/governance", bytes.NewBufferString(`{"quorum":2,"secret_token":"hidden"}`))
	update.Header.Set("X-Actor-ID", "human")
	update.Header.Set("If-Match", "0")
	update.Header.Set("X-Confirmation-Token", token)
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, update)
	if out.Code != 200 {
		t.Fatalf("update=%d %s", out.Code, out.Body.String())
	}
	if bytes.Contains(out.Body.Bytes(), []byte("hidden")) {
		t.Fatal("secret leaked")
	}
	stale := httptest.NewRequest(http.MethodPatch, base+"/governance", bytes.NewBufferString(`{"quorum":3}`))
	stale.Header.Set("X-Actor-ID", "human")
	stale.Header.Set("If-Match", "0")
	stale.Header.Set("X-Confirmation-Token", token)
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, stale)
	if out.Code != 409 {
		t.Fatalf("stale=%d %s", out.Code, out.Body.String())
	}
}
