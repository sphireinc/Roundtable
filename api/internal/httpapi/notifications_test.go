package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"roundtable/internal/db"
)

func TestNotificationsFilterCountsAndAcknowledge(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertWorkspace(t.Context(), db.Workspace{ID: "ws-notifications", DisplayName: "Notifications", RootPath: t.TempDir(), Status: "active", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO notifications(id,workspace_id,recipient_id,severity,category,title,body,entity_type,entity_id,actionable) VALUES('N-1','ws-notifications','human','warning','approval_required','Approve proposal','Review needed','proposal','P-1',1),('N-2','ws-notifications','human','info','repository_degraded','Repo warning','Read only','workspace','ws-notifications',0)`); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Store: store})
	base := "/api/v1/workspaces/ws-notifications/notifications"
	req := httptest.NewRequest(http.MethodGet, base+"?unread=true&actionable=true&category=approval_required", nil)
	req.Header.Set("X-Actor-ID", "human")
	out := httptest.NewRecorder()
	server.Handler().ServeHTTP(out, req)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte("N-1")) || bytes.Contains(out.Body.Bytes(), []byte("N-2")) {
		t.Fatalf("list=%d %s", out.Code, out.Body.String())
	}
	counts := httptest.NewRequest(http.MethodGet, base+"/counts", nil)
	counts.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, counts)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"unread":2`)) || !bytes.Contains(out.Body.Bytes(), []byte(`"actionable":1`)) {
		t.Fatalf("counts=%d %s", out.Code, out.Body.String())
	}
	ack := httptest.NewRequest(http.MethodPost, base+"/N-1/ack", nil)
	ack.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, ack)
	if out.Code != 200 || !bytes.Contains(out.Body.Bytes(), []byte(`"read_at":`)) {
		t.Fatalf("ack=%d %s", out.Code, out.Body.String())
	}
	if _, err := sqlDB.Exec(`UPDATE notifications SET resolved_at=CURRENT_TIMESTAMP WHERE id='N-1'`); err != nil {
		t.Fatal(err)
	}
	counts = httptest.NewRequest(http.MethodGet, base+"/counts", nil)
	counts.Header.Set("X-Actor-ID", "human")
	out = httptest.NewRecorder()
	server.Handler().ServeHTTP(out, counts)
	if !bytes.Contains(out.Body.Bytes(), []byte(`"actionable":0`)) {
		t.Fatalf("resolved counts=%d %s", out.Code, out.Body.String())
	}
}
