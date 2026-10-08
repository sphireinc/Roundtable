package coordinator

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"roundtable/internal/db"
)

func TestServiceIsHealthyIdleAndTicksAllActiveRuns(t *testing.T) {
	store := coordinatorStore(t)
	ctx := context.Background()
	if err := store.UpsertRun(ctx, db.Run{ID: "A", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(ctx, db.Run{ID: "B", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	var aCalls, bCalls atomic.Int32
	service := NewService(store, func(_ context.Context, id string) error {
		if id == "A" && aCalls.Add(1) == 1 {
			return errors.New("transient")
		}
		if id == "B" {
			bCalls.Add(1)
		}
		return nil
	})
	cycleCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- service.Run(cycleCtx, 10*time.Millisecond) }()
	deadline := time.After(time.Second)
	for bCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("run B was starved by run A failure")
		case <-time.After(time.Millisecond):
		}
	}
	if got := service.Health().Status; got != "degraded" {
		t.Fatalf("health after transient error = %q, want degraded", got)
	}
	recoveryDeadline := time.After(2 * time.Second)
	for aCalls.Load() < 2 {
		select {
		case <-recoveryDeadline:
			t.Fatal("run A did not recover on a later retry")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := service.Health().Status; got != "ready" {
		t.Fatalf("health after recovery = %q, want ready", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if bCalls.Load() == 0 {
		t.Fatal("active run B was never ticked")
	}
}

func TestServiceStaysAliveWithNoRuns(t *testing.T) {
	service := NewService(coordinatorStore(t), func(context.Context, string) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx, 5*time.Millisecond) }()
	time.Sleep(20 * time.Millisecond)
	if got := service.Health().Status; got != "healthy-idle" {
		t.Fatalf("idle health = %q, want healthy-idle", got)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func coordinatorStore(t *testing.T) *db.Store {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db.NewStore(sqlDB, nil)
}
