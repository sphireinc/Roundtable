package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSnapshotPinsCleanRevisionAndCreatesSeparateWorkspaces(t *testing.T) {
	repository := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(repository, "source.txt"), []byte("pinned source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repository, "source.txt")
	commitFile(t, repository, filepath.Join("src", "main.go"))
	paths, err := CreateSnapshot(context.Background(), repository, "SESSION-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := CleanupExecution(repository, paths); err != nil {
			t.Errorf("clean snapshot execution: %v", err)
		}
	})
	if paths.Revision == "" || paths.BaseStateHash == "" || paths.Source == paths.Workspace || paths.Workspace == paths.Temp {
		t.Fatalf("invalid pinned execution paths: %+v", paths)
	}
	if _, err := os.Stat(filepath.Join(paths.Source, ".git")); !os.IsNotExist(err) {
		t.Fatalf("snapshot exposed Git metadata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.Workspace, "agent-edit.txt"), []byte("proposal only"), 0o600); err != nil {
		t.Fatalf("disposable workspace is not writable: %v", err)
	}
	workspaceSource := filepath.Join(paths.Workspace, "src", "main.go")
	workspaceData, err := os.ReadFile(workspaceSource)
	if err != nil || string(workspaceData) != "package main\n" {
		t.Fatalf("workspace was not provisioned from the pinned snapshot: content=%q err=%v", workspaceData, err)
	}
	if err := os.WriteFile(workspaceSource, []byte("package main\n// agent edit\n"), 0o644); err != nil {
		t.Fatalf("workspace source copy is not writable: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(paths.Source, "source.txt"))
	if err != nil || string(data) != "pinned source" {
		t.Fatalf("incorrect source snapshot: %q err=%v", data, err)
	}
}

func TestSnapshotMountPermissionsWorkForNonOwnerContainerUID(t *testing.T) {
	repository := newGitRepo(t)
	if err := os.Mkdir(filepath.Join(repository, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repository, filepath.Join("src", "main.go"))
	paths, err := CreateSnapshot(context.Background(), repository, "S-mount-perms")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CleanupExecution(repository, paths) })
	for path, want := range map[string]os.FileMode{
		paths.Source:                                     0o555,
		filepath.Join(paths.Source, "src"):               0o555,
		paths.Workspace:                                  0o777,
		filepath.Join(paths.Workspace, "src"):            0o777,
		filepath.Join(paths.Workspace, "src", "main.go"): 0o666,
		paths.Temp: 0o777,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat agent mount path %s: %v", path, err)
			continue
		}
		if info.Mode().Perm() != want {
			t.Errorf("agent mount permissions for %s = %04o, want %04o", path, info.Mode().Perm(), want)
		}
	}
}

func TestCreateScopedSnapshotExcludesUnassignedRepositoryFiles(t *testing.T) {
	repository := newGitRepo(t)
	for name, content := range map[string]string{
		"assigned/visible.go":         "package visible\n",
		"assigned/.env":               "secret=never-mounted\n",
		"assigned/subdir/credentials": "never-mounted\n",
		"unassigned/secret.txt":       "not for this task\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repository, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repository, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		commitFile(t, repository, name)
	}
	paths, err := CreateScopedSnapshot(context.Background(), repository, "S-scoped", []string{"assigned", "assigned/new.go"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = CleanupExecution(repository, paths) })
	for _, root := range []string{paths.Source, paths.Workspace} {
		if _, err := os.Stat(filepath.Join(root, "assigned", "visible.go")); err != nil {
			t.Fatalf("assigned file missing from %s: %v", root, err)
		}
		if _, err := os.Stat(filepath.Join(root, "unassigned", "secret.txt")); !os.IsNotExist(err) {
			t.Fatalf("unassigned file is exposed from %s: %v", root, err)
		}
		for _, hidden := range []string{filepath.Join("assigned", ".env"), filepath.Join("assigned", "subdir", "credentials")} {
			if _, err := os.Stat(filepath.Join(root, hidden)); !os.IsNotExist(err) {
				t.Fatalf("sensitive descendant is exposed from %s: %s (%v)", root, hidden, err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(paths.Workspace, "assigned", "new.go"), []byte("package visible\n"), 0o644); err != nil {
		t.Fatalf("exact authorized new-file path was not provisioned: %v", err)
	}
}

func TestCreateScopedSnapshotRejectsEmptyAndSensitiveScopes(t *testing.T) {
	repository := newGitRepo(t)
	for _, scope := range [][]string{nil, {"../outside"}, {".env"}, {".ssh"}} {
		if _, err := CreateScopedSnapshot(context.Background(), repository, "S-invalid-scope", scope); err == nil {
			t.Fatalf("unsafe snapshot scope was accepted: %#v", scope)
		}
	}
}

func TestCleanupExecutionRefusesCallerSelectedDirectories(t *testing.T) {
	repository := newGitRepo(t)
	outside := t.TempDir()
	source := filepath.Join(outside, "source")
	workspace := filepath.Join(outside, "workspace")
	temp := filepath.Join(outside, "temp")
	for _, dir := range []string{source, workspace, temp} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := filepath.Join(outside, "preserve.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := SnapshotPaths{ExecutionRoot: outside, Source: source, Workspace: workspace, Temp: temp}
	if err := CleanupExecution(repository, paths); err == nil {
		t.Fatal("cleanup accepted a caller-selected path outside the managed execution root")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("outside data changed: content=%q err=%v", data, err)
	}
}

func TestCleanupExecutionHandlesRestrictedDirsWithoutFollowingSymlinks(t *testing.T) {
	repository := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(repository, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repository, "base.txt")
	paths, err := CreateSnapshot(context.Background(), repository, "S-cleanup-safe")
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(paths.Workspace, "linked")); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(paths.Workspace, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.Workspace, 0); err != nil {
		t.Fatal(err)
	}
	if err := CleanupExecution(repository, paths); err != nil {
		t.Fatalf("cleanup could not remove restricted workspace safely: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "preserve" {
		t.Fatalf("cleanup followed workspace symlink: content=%q err=%v", data, err)
	}
}

func TestCreateSnapshotRejectsDirtyOrSymlinkedRepositoryState(t *testing.T) {
	repository := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(repository, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repository, "base.txt")
	if err := os.WriteFile(filepath.Join(repository, "dirty.txt"), []byte("uncommitted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSnapshot(context.Background(), repository, "S-dirty"); err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("dirty working tree was accepted: %v", err)
	}
	if err := os.Remove(filepath.Join(repository, "dirty.txt")); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repository, "link")); err != nil {
		t.Fatal(err)
	}
	commitFile(t, repository, "link")
	if _, err := CreateSnapshot(context.Background(), repository, "S-link"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked repository snapshot was accepted: %v", err)
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(".", ".snapshot-test-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "roundtable-test@example.invalid"}, {"config", "user.name", "Roundtable Test"}, {"config", "commit.gpgsign", "false"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	return root
}

func commitFile(t *testing.T, root, file string) {
	t.Helper()
	for _, args := range [][]string{{"add", "--", file}, {"commit", "-qm", "fixture"}} {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
}
