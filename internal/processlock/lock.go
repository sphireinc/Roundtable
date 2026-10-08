package processlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrAlreadyLocked = errors.New("roundtable project is already owned by another process")

type Lock struct {
	file *os.File
	mu   sync.Mutex
}

func Acquire(root string) (*Lock, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("canonicalize project root: %w", err)
	}
	canonical = filepath.Clean(canonical)
	stateDir := filepath.Join(canonical, ".roundtable")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create runtime state directory: %w", err)
	}
	path := filepath.Join(stateDir, "owner.lock")
	if existing, err := os.Lstat(path); err == nil && !existing.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing non-regular project lock path %q", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect project lock path: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open project lock: %w", err)
	}
	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened project lock: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("recheck project lock path: %w", err)
		}
		return nil, fmt.Errorf("project lock path changed during open")
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure project lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, errLockBusy) {
			return nil, ErrAlreadyLocked
		}
		return nil, fmt.Errorf("lock project: %w", err)
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l == nil || l.file == nil {
		return nil
	}
	err := unlockFile(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
