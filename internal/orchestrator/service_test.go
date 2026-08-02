package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roundtable/internal/config"
	"roundtable/internal/db"
)

func TestSyncAgentsSeedsConfiguredRoles(t *testing.T) {
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, nil)
	service, err := NewService(root, config.Default(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncAgents(context.Background()); err != nil {
		t.Fatalf("sync agents failed: %v", err)
	}
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("list agents failed: %v", err)
	}
	if len(agents) < 7 {
		t.Fatalf("expected seeded agents, got %+v", agents)
	}
}

func TestTickAssignsOpenTasksAndRequestsReview(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	if err := os.MkdirAll(filepath.Join(root, ".roundtable", "patches"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "POLICIES.ROUNDTABLE.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.Open(filepath.Join(root, "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	store := db.NewStore(sqlDB, nil)
	service, err := NewService(root, cfg, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SyncAgents(context.Background()); err != nil {
		t.Fatalf("sync agents failed: %v", err)
	}

	ctx := context.Background()
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Implement feature", BodyMD: "body", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-2", Title: "Review patch", BodyMD: "body", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "implementer-1", TaskID: "T-2", ClaimType: "write", Status: "active", ExpiresAt: "2099-01-01T00:00:00Z", Renewable: true, ResumePolicy: "hold"}); err != nil {
		t.Fatal(err)
	}
	patch := strings.Join([]string{
		"diff --git a/README.md b/README.md",
		"--- a/README.md",
		"+++ b/README.md",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n")
	if err := os.WriteFile(PatchPath(root, "P-1"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProposal(ctx, db.Proposal{ID: "P-1", TaskID: "T-2", AgentID: "implementer-1", Title: "Pending patch", SummaryMD: "summary", PatchPath: filepath.ToSlash(filepath.Join(".roundtable/patches", "P-1.diff")), Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceProposalResources(ctx, "P-1", []string{"file:README.md"}); err != nil {
		t.Fatal(err)
	}

	if err := service.Tick(ctx, "RUN-1"); err != nil {
		t.Fatalf("tick failed: %v", err)
	}

	task, err := store.GetTask(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.AssignedAgentID == "" || task.Status != "in_progress" {
		t.Fatalf("expected assigned in-progress task, got %+v", task)
	}
	proposal, err := store.GetProposal(ctx, "P-1")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != "in_review" {
		t.Fatalf("expected in_review proposal, got %+v", proposal)
	}
	events, err := store.ListEvents(ctx, "RUN-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 {
		t.Fatalf("expected orchestration events, got %+v", events)
	}
}
