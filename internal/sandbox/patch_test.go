package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roundtable/internal/repo"
)

func TestCollectPatchIncludesWorkspaceEditsAndExcludesWorkspaceGitMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	workspace := filepath.Join(root, "workspace")
	temp := filepath.Join(root, "temp")
	for _, dir := range []string{source, workspace, temp} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "edit.txt"), []byte("before\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "edit.txt"), []byte("after\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "new.txt"), []byte("created\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git", "config"), []byte("untrusted metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch, err := CollectPatch(SnapshotPaths{ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"diff --git a/edit.txt b/edit.txt", "-before", "+after", "diff --git a/new.txt b/new.txt", "+created"} {
		if !strings.Contains(patch, want) {
			t.Errorf("collected patch missing %q:\n%s", want, patch)
		}
	}
	if strings.Contains(patch, ".git/config") {
		t.Fatalf("workspace Git metadata leaked into patch:\n%s", patch)
	}
	applyRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(applyRoot, "edit.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApplyPatch(applyRoot, patch); err != nil {
		t.Fatalf("collected patch did not apply: %v\n%s", err, patch)
	}
	if content, err := os.ReadFile(filepath.Join(applyRoot, "edit.txt")); err != nil || string(content) != "after\n" {
		t.Fatalf("updated file after patch = %q, %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(applyRoot, "new.txt")); err != nil || string(content) != "created\n" {
		t.Fatalf("created file after patch = %q, %v", content, err)
	}
}

func TestCollectPatchRejectsSymlinksAndProtectedOrAmbiguousPaths(t *testing.T) {
	for name, setup := range map[string]func(string, string){
		"symlink": func(_, workspace string) {
			_ = os.Symlink(t.TempDir(), filepath.Join(workspace, "link"))
		},
		"metadata": func(_, workspace string) {
			_ = os.MkdirAll(filepath.Join(workspace, ".roundtable"), 0o755)
			_ = os.WriteFile(filepath.Join(workspace, ".roundtable", "x"), []byte("x"), 0o644)
		},
		"spaces": func(_, workspace string) {
			_ = os.WriteFile(filepath.Join(workspace, "file name.txt"), []byte("x"), 0o644)
		},
		"binary": func(_, workspace string) {
			_ = os.WriteFile(filepath.Join(workspace, "image.bin"), []byte{0, 1, 2}, 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			source, workspace, temp := filepath.Join(root, "source"), filepath.Join(root, "workspace"), filepath.Join(root, "temp")
			for _, dir := range []string{source, workspace, temp} {
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			setup(source, workspace)
			if _, err := CollectPatch(SnapshotPaths{ExecutionRoot: root, Source: source, Workspace: workspace, Temp: temp}); err == nil {
				t.Fatal("unsafe workspace content produced a patch")
			}
		})
	}
}
