package proposals

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roundtable/internal/db"
)

func TestGeneratedProposalIDsAreDistinctAndSafeForArtifacts(t *testing.T) {
	seen := make(map[string]bool)
	for range 256 {
		id := generatedID("P")
		if !safeProposalID.MatchString(id) {
			t.Fatalf("generated proposal id %q is not safe for its artifact path", id)
		}
		if strings.Contains(id, ".") {
			t.Fatalf("generated proposal id %q contains an unsupported path character", id)
		}
		if seen[id] {
			t.Fatalf("generated proposal id collided within one process: %q", id)
		}
		seen[id] = true
	}
}

func TestProposalCreateRejectsSymlinkedPatchDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Task", Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "A-1", TaskID: "T-1", ClaimType: "write", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	outside := t.TempDir()
	outsideTarget := filepath.Join(outside, "P-escape.diff")
	if err := os.WriteFile(outsideTarget, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".roundtable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".roundtable", "patches")); err != nil {
		t.Fatal(err)
	}

	patch := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n"
	_, err = NewService(root, store, nil).ProposalCreate(ctx, map[string]any{
		"task_id": "T-1", "agent_id": "A-1", "proposal_id": "P-escape",
		"title": "Change README", "summary_md": "Update the README", "patch": patch,
		"base_revision": "revision-1", "base_state_hash": "tree-1",
	})
	if err == nil {
		t.Fatal("proposal creation accepted a symlinked artifact directory")
	}
	data, err := os.ReadFile(outsideTarget)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("outside patch target changed: data=%q err=%v", data, err)
	}
}

func TestProposalCreateDoesNotReplaceExistingProposalWithoutPatchArtifact(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := db.NewStore(sqlDB, nil)
	if err := store.UpsertTask(ctx, db.Task{ID: "T-1", Title: "Task", Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertResource(ctx, db.Resource{ID: "file:README.md", Type: "file", Path: "README.md"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(ctx, db.Claim{ID: "C-1", ResourceID: "file:README.md", AgentID: "A-1", TaskID: "T-1", ClaimType: "write", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	prior := db.Proposal{ID: "P-existing", TaskID: "T-1", AgentID: "A-1", Title: "Original", SummaryMD: "Original summary", PatchPath: ".roundtable/patches/P-existing.diff", Status: "pending"}
	if err := store.UpsertProposal(ctx, prior); err != nil {
		t.Fatal(err)
	}

	patch := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n"
	_, err = NewService(root, store, nil).ProposalCreate(ctx, map[string]any{
		"task_id": "T-1", "agent_id": "A-1", "proposal_id": prior.ID,
		"title": "Replacement", "summary_md": "Replacement summary", "patch": patch,
		"base_revision": "revision-1", "base_state_hash": "tree-1",
	})
	if err == nil {
		t.Fatal("proposal creation replaced an existing proposal ID")
	}
	got, err := store.GetProposal(ctx, prior.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != prior.Title || got.SummaryMD != prior.SummaryMD || got.AgentID != prior.AgentID || got.TaskID != prior.TaskID {
		t.Fatalf("existing proposal was modified: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(prior.PatchPath))); !os.IsNotExist(err) {
		t.Fatalf("rejected duplicate created an orphan replacement artifact: %v", err)
	}
}

func TestReplacingProposalPatchInvalidatesPriorApprovalEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "roundtable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	store := db.NewStore(sqlDB, nil)
	for _, resource := range []db.Resource{{ID: "file:README.md", Type: "file", Path: "README.md"}} {
		if err := store.UpsertResource(ctx, resource); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertTask(ctx, db.Task{ID: "T-replace", Title: "Replace", Status: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertClaim(ctx, db.Claim{ID: "C-replace", ResourceID: "file:README.md", AgentID: "A-implementer", TaskID: "T-replace", ClaimType: "write", Status: "active"}); err != nil {
		t.Fatal(err)
	}
	proposal := db.Proposal{ID: "P-replace", TaskID: "T-replace", AgentID: "A-implementer", Title: "Replace", PatchPath: ".roundtable/patches/P-replace.diff", Status: "pending"}
	if err := store.UpsertProposal(ctx, proposal); err != nil {
		t.Fatal(err)
	}
	_ = store.UpsertVote(ctx, db.Vote{ID: "V-replace", ProposalID: proposal.ID, AgentID: "A-reviewer", Vote: "approve"})
	_ = store.UpsertHumanApproval(ctx, db.HumanApproval{ID: "H-replace", ProposalID: proposal.ID, TaskID: "T-replace", Subject: "Approval", ReasonMD: "Reviewed", Status: "approved", DecidedBy: "operator"})
	_ = store.UpsertSecurityReview(ctx, db.SecurityReview{ID: "SR-replace", ProposalID: proposal.ID, TaskID: "T-replace", ReviewerID: "security", Status: "approved", SummaryMD: "safe"})
	_ = store.UpsertTestRun(ctx, db.TestRun{ID: "TEST-replace", ProposalID: proposal.ID, TaskID: "T-replace", Command: "go test", Status: "passed"})
	patch := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+replacement\n"
	if _, err := NewService(root, store, nil).ProposalAttachPatch(ctx, map[string]any{"proposal_id": proposal.ID, "patch": patch}); err != nil {
		t.Fatal(err)
	}
	for name, list := range map[string]int{
		"votes": countRows(t, sqlDB, "votes", proposal.ID), "human approvals": countRows(t, sqlDB, "human_approvals", proposal.ID),
		"security reviews": countRows(t, sqlDB, "security_reviews", proposal.ID), "test evidence": countRows(t, sqlDB, "test_runs", proposal.ID),
	} {
		if list != 0 {
			t.Errorf("patch replacement retained %d prior %s", list, name)
		}
	}
	saved, err := store.GetProposal(ctx, proposal.ID)
	if err != nil || saved.Status != "pending" {
		t.Fatalf("replacement did not reset proposal to pending: %+v err=%v", saved, err)
	}
}

func countRows(t *testing.T, sqlDB *sql.DB, table, proposalID string) int {
	t.Helper()
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE proposal_id = ?", proposalID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestWriteProposalPatchRejectsParentSymlinksAndExistingArtifacts(t *testing.T) {
	for _, component := range []string{".roundtable", "patches"} {
		t.Run("symlink_"+component, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if component == ".roundtable" {
				if err := os.Symlink(outside, filepath.Join(root, component)); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(root, ".roundtable"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(root, ".roundtable", component)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeProposalPatch(root, ".roundtable/patches/P-escape.diff", []byte("malicious")); err == nil {
				t.Fatal("accepted a symlinked proposal artifact parent")
			}
			if _, err := os.Stat(filepath.Join(outside, "P-escape.diff")); !os.IsNotExist(err) {
				t.Fatalf("outside artifact was created: %v", err)
			}
		})
	}

	t.Run("existing_artifact", func(t *testing.T) {
		root := t.TempDir()
		patchPath := filepath.Join(root, ".roundtable", "patches", "P-existing.diff")
		if err := os.MkdirAll(filepath.Dir(patchPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(patchPath, []byte("original"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeProposalPatch(root, ".roundtable/patches/P-existing.diff", []byte("replacement")); err == nil {
			t.Fatal("overwrote an existing proposal artifact")
		}
		data, err := os.ReadFile(patchPath)
		if err != nil || string(data) != "original" {
			t.Fatalf("existing artifact changed: data=%q err=%v", data, err)
		}
	})

	for _, path := range []string{"other/P-1.diff", ".roundtable/patches/../P-1.diff", ".roundtable/patches/../other/P-1.diff"} {
		t.Run("invalid_path_"+path, func(t *testing.T) {
			root := t.TempDir()
			if err := writeProposalPatch(root, path, []byte("data")); err == nil {
				t.Fatalf("accepted unexpected artifact path %q", path)
			}
		})
	}
}
