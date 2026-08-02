package claims

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"roundtable/internal/db"
	"roundtable/internal/events"
)

func TestCreateClaimRecordsResourceAndEvent(t *testing.T) {
	service, store := newTestService(t)
	ctx := context.Background()

	claim, err := service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "README.md",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("create claim failed: %v", err)
	}
	if claim.ResourceID != "file:README.md" || claim.Status != "active" {
		t.Fatalf("unexpected claim: %+v", claim)
	}

	resource, err := store.GetResource(ctx, "file:README.md")
	if err != nil {
		t.Fatalf("get resource failed: %v", err)
	}
	if resource.Path != "README.md" || resource.Type != "file" {
		t.Fatalf("unexpected resource: %+v", resource)
	}

	events, err := store.ListEvents(ctx, "RUN-1")
	if err != nil {
		t.Fatalf("list events failed: %v", err)
	}
	if len(events) != 1 || events[0].Type != "claim.created" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestDirectoryFileConflictDetected(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	_, err := service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "directory",
		ResourcePath: "internal",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("seed claim failed: %v", err)
	}

	_, err = service.Create(ctx, CreateRequest{
		ID:           "C-2",
		RunID:        "RUN-1",
		AgentID:      "A-2",
		TaskID:       "T-2",
		ResourceType: "file",
		ResourcePath: "internal/app/app.go",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err == nil || !errorsIs(err, ErrClaimConflict) {
		t.Fatalf("expected claim conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "internal") {
		t.Fatalf("expected overlap detail in error, got %v", err)
	}
}

func TestReadClaimsDoNotConflictWithWriteClaims(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	_, err := service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "README.md",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("seed claim failed: %v", err)
	}

	_, err = service.Create(ctx, CreateRequest{
		ID:           "C-2",
		RunID:        "RUN-1",
		AgentID:      "A-2",
		TaskID:       "T-2",
		ResourceType: "file",
		ResourcePath: "README.md",
		ClaimType:    "read",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("expected read claim to coexist, got %v", err)
	}
}

func TestSymbolClaimConflictsWithFileClaim(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	_, err := service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "internal/app/app.go",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("seed file claim failed: %v", err)
	}

	_, err = service.Create(ctx, CreateRequest{
		ID:           "C-2",
		RunID:        "RUN-1",
		AgentID:      "A-2",
		TaskID:       "T-2",
		ResourceType: "symbol",
		ResourcePath: "internal/app/app.go",
		SymbolName:   "runTable",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err == nil || !errorsIs(err, ErrClaimConflict) {
		t.Fatalf("expected symbol/file conflict, got %v", err)
	}
}

func TestLifecycleTransitionsAndExpiry(t *testing.T) {
	service, store := newTestService(t)
	ctx := context.Background()

	claim, err := service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "README.md",
		ClaimType:    "write",
		TTL:          1 * time.Second,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("create claim failed: %v", err)
	}

	released, err := service.Release(ctx, "RUN-1", claim.ID, "A-1", "done")
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if released.Status != "released" {
		t.Fatalf("expected released claim, got %+v", released)
	}

	claim2, err := service.Create(ctx, CreateRequest{
		ID:           "C-2",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "PROJECT.ROUNDTABLE.md",
		ClaimType:    "write",
		TTL:          1 * time.Second,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}

	expired, err := service.ExpireDueClaims(ctx, "RUN-1", time.Now().UTC().Add(2*time.Second))
	if err != nil {
		t.Fatalf("expire claims failed: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != claim2.ID || expired[0].Status != "expired" {
		t.Fatalf("unexpected expired claims: %+v", expired)
	}

	events, err := store.ListEvents(ctx, "RUN-1")
	if err != nil {
		t.Fatalf("list events failed: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
}

func TestListFiltersClaims(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	_, _ = service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "README.md",
		ClaimType:    "write",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	_, _ = service.Create(ctx, CreateRequest{
		ID:           "C-2",
		RunID:        "RUN-1",
		AgentID:      "A-2",
		TaskID:       "T-2",
		ResourceType: "file",
		ResourcePath: "PROJECT.ROUNDTABLE.md",
		ClaimType:    "read",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})

	claims, err := service.List(ctx, StatusFilter{AgentID: "A-2"})
	if err != nil {
		t.Fatalf("list claims failed: %v", err)
	}
	if len(claims) != 1 || claims[0].ID != "C-2" {
		t.Fatalf("unexpected filtered claims: %+v", claims)
	}
}

func TestReconcileStaleClaimsSuspendsDriftedClaim(t *testing.T) {
	service, store := newTestService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write seed file failed: %v", err)
	}

	claim, err := service.Create(ctx, CreateRequest{
		ID:           "C-1",
		RunID:        "RUN-1",
		AgentID:      "A-1",
		TaskID:       "T-1",
		ResourceType: "file",
		ResourcePath: "README.md",
		ClaimType:    "write",
		BaseHash:     "base-hash",
		TTL:          10 * time.Minute,
		Renewable:    true,
	})
	if err != nil {
		t.Fatalf("create claim failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("after\n"), 0o644); err != nil {
		t.Fatalf("mutate file failed: %v", err)
	}

	suspended, err := service.ReconcileStaleClaims(ctx, ReconcileRequest{
		Root:    root,
		RunID:   "RUN-1",
		AgentID: "A-1",
		ActorID: "A-1",
	})
	if err != nil {
		t.Fatalf("reconcile failed: %v", err)
	}
	if len(suspended) != 1 || suspended[0].ID != claim.ID || suspended[0].Status != "suspended" {
		t.Fatalf("unexpected suspended claims: %+v", suspended)
	}

	stored, err := store.GetClaim(ctx, claim.ID)
	if err != nil {
		t.Fatalf("get claim failed: %v", err)
	}
	if stored.Status != "suspended" {
		t.Fatalf("expected suspended claim, got %+v", stored)
	}

	resource, err := store.GetResource(ctx, "file:README.md")
	if err != nil {
		t.Fatalf("get resource failed: %v", err)
	}
	if resource.CurrentHash == "" {
		t.Fatalf("expected current hash to be recorded, got %+v", resource)
	}
}

func newTestService(t *testing.T) (*Service, *db.Store) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := db.NewStore(sqlDB, events.NewBus[db.Event]())
	return NewService(store), store
}

func errorsIs(err, target error) bool {
	return err != nil && target != nil && strings.Contains(err.Error(), target.Error())
}
