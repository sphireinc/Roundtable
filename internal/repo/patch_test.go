package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTouchedFiles(t *testing.T) {
	patch := "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n"
	info, err := ParseTouchedFiles(patch)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Files) != 1 || info.Files[0] != "README.md" {
		t.Fatalf("unexpected files: %+v", info)
	}
	ranges := info.Changes["README.md"]
	if len(ranges) != 1 || ranges[0].StartLine != 1 || ranges[0].EndLine != 1 {
		t.Fatalf("unexpected ranges: %+v", ranges)
	}
	if len(info.FileChanges) != 1 || info.FileChanges[0].OldPath != "README.md" || info.FileChanges[0].NewPath != "README.md" {
		t.Fatalf("unexpected file changes: %+v", info.FileChanges)
	}
}

func TestApplyPatchAndWorkspaceHash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := WorkspaceHash(root)
	if err != nil {
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
	if err := ApplyPatch(root, patch); err != nil {
		t.Fatal(err)
	}
	after, err := WorkspaceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("expected workspace hash to change after patch")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestResourceHashes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "sample.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fileHash, err := FileHash(root, "pkg/sample.txt")
	if err != nil || fileHash == "" {
		t.Fatalf("expected file hash, got %q %v", fileHash, err)
	}
	rangeHash, err := LineRangeHash(root, "pkg/sample.txt", 2, 3)
	if err != nil || rangeHash == "" {
		t.Fatalf("expected range hash, got %q %v", rangeHash, err)
	}
	dirHash, err := DirectoryHash(root, "pkg")
	if err != nil || dirHash == "" {
		t.Fatalf("expected directory hash, got %q %v", dirHash, err)
	}
	if fileHash == rangeHash {
		t.Fatalf("expected full-file and line-range hashes to differ")
	}
}

func TestRepoStateHashFallsBackWithoutGit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceHash, err := WorkspaceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	stateHash, err := RepoStateHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if stateHash != workspaceHash {
		t.Fatalf("expected workspace hash fallback, got %q want %q", stateHash, workspaceHash)
	}
}

func TestRepoStateHashUsesGitHeadWhenRepoExists(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "roundtable@example.com")
	runGit(t, root, "config", "user.name", "Roundtable")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "commit", "-m", "initial")

	before, err := RepoStateHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(before, "git:") {
		t.Fatalf("expected git-aware hash, got %q", before)
	}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := RepoStateHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("expected repo state hash to change with worktree mutation")
	}
}

func TestParseTouchedFilesHandlesCreateDeleteAndRename(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/old.txt b/new.txt",
		"similarity index 100%",
		"rename from old.txt",
		"rename to new.txt",
		"diff --git a/create.txt b/create.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/create.txt",
		"@@ -0,0 +1 @@",
		"+created",
		"diff --git a/delete.txt b/delete.txt",
		"deleted file mode 100644",
		"--- a/delete.txt",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-gone",
		"",
	}, "\n")
	info, err := ParseTouchedFiles(patch)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"old.txt", "new.txt", "create.txt", "delete.txt"} {
		if !containsString(info.Files, want) {
			t.Fatalf("expected files to include %s, got %+v", want, info.Files)
		}
	}
	if len(info.FileChanges) != 3 {
		t.Fatalf("expected 3 file changes, got %+v", info.FileChanges)
	}
	if !info.FileChanges[0].IsRename || info.FileChanges[0].OldPath != "old.txt" || info.FileChanges[0].NewPath != "new.txt" {
		t.Fatalf("unexpected rename change: %+v", info.FileChanges[0])
	}
	if !info.FileChanges[1].IsNew || info.FileChanges[1].NewPath != "create.txt" {
		t.Fatalf("unexpected create change: %+v", info.FileChanges[1])
	}
	if !info.FileChanges[2].IsDelete || info.FileChanges[2].OldPath != "delete.txt" {
		t.Fatalf("unexpected delete change: %+v", info.FileChanges[2])
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
