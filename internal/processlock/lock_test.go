package processlock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireDuplicateAndRelease(t *testing.T) {
	root := t.TempDir()
	first, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(root); !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("second Acquire error = %v, want ErrAlreadyLocked", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(root)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	_ = second.Close()
}

func TestCanonicalRootAliasesShareLock(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	lock, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := Acquire(alias); !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("Acquire(alias) error = %v, want ErrAlreadyLocked", err)
	}
}

func TestCaseAliasesShareLockOnCaseInsensitiveFilesystem(t *testing.T) {
	root := t.TempDir()
	alias := strings.ToUpper(root)
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(rootInfo, aliasInfo) {
		t.Skip("filesystem does not resolve case aliases to the same directory")
	}
	first, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := Acquire(alias); !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("Acquire(case alias) error = %v, want ErrAlreadyLocked", err)
	}
}

func TestAcquireRejectsSymlinkLockFile(t *testing.T) {
	root := t.TempDir()
	lock, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".roundtable", "owner.lock")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Acquire(root); err == nil {
		t.Fatal("Acquire accepted a symlink lock path")
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "preserve" {
		t.Fatalf("external lock target changed: data=%q err=%v", contents, err)
	}
}
