package egress

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

var safeEgressSessionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

type Session struct {
	path      string
	cancel    context.CancelFunc
	done      chan struct{}
	serveErr  error
	cleanup   func() error
	closeOnce sync.Once
	closeErr  error
}

func ListenSession(ctx context.Context, policy Policy, dial DialContextFunc) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cannot start provider egress session with canceled context: %w", err)
	}
	directory, err := os.MkdirTemp("/tmp", "roundtable-egress-")
	if err != nil {
		return nil, fmt.Errorf("create private provider egress socket directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.Remove(directory)
		return nil, fmt.Errorf("secure provider egress socket directory: %w", err)
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve private provider egress socket directory: %w", err)
	}
	path := filepath.Join(directory, "gateway.sock")
	if len(path) > 100 {
		_ = os.Remove(directory)
		return nil, errors.New("provider egress Unix socket path exceeds the platform limit")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		_ = os.Remove(directory)
		return nil, fmt.Errorf("listen on provider egress Unix socket: %w", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = listener.Close()
		_ = os.Remove(directory)
		return nil, fmt.Errorf("make provider egress socket accessible to the non-root container user: %w", err)
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &Session{path: path, cancel: cancel, done: make(chan struct{}), cleanup: func() error { return CleanupStaleSocket(path) }}
	go func() {
		defer close(session.done)
		session.serveErr = (Gateway{Policy: policy, DialContext: dial}).Serve(sessionCtx, listener)
	}()
	return session, nil
}

// ProjectSocketPaths returns an isolated directory and socket for one session.
func ProjectSocketPaths(repositoryRoot, sessionID string) (string, string, error) {
	if !filepath.IsAbs(repositoryRoot) || filepath.Clean(repositoryRoot) != repositoryRoot || !safeEgressSessionID.MatchString(sessionID) {
		return "", "", errors.New("provider egress socket requires a clean repository root and safe session identity")
	}
	canonical, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil || canonical != repositoryRoot {
		return "", "", errors.New("provider egress repository root must be canonical")
	}
	digest := sha256.Sum256([]byte(sessionID))
	directory := filepath.Join(repositoryRoot, ".roundtable", "mcp", "e-"+hex.EncodeToString(digest[:12]))
	return directory, filepath.Join(directory, "g"), nil
}

// ListenSessionIn places a broker socket under the project's private MCP
// directory, which is normally included in container-runtime shared paths.
func ListenSessionIn(ctx context.Context, repositoryRoot, sessionID string, policy Policy, dial DialContextFunc) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cannot start provider egress session with canceled context: %w", err)
	}
	directory, path, err := ProjectSocketPaths(repositoryRoot, sessionID)
	if err != nil {
		return nil, err
	}
	if err := ensureProjectMCPDirectory(repositoryRoot); err != nil {
		return nil, err
	}
	if len(path) > 100 {
		return nil, errors.New("provider egress Unix socket path exceeds the platform limit")
	}
	if err := os.Mkdir(directory, 0o711); err != nil {
		return nil, fmt.Errorf("create isolated provider egress directory: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		_ = os.Remove(directory)
		return nil, fmt.Errorf("listen on session provider egress socket: %w", err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("make provider egress socket accessible to the non-root container user: %w", err)
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	session := &Session{path: path, cancel: cancel, done: make(chan struct{}), cleanup: func() error { return CleanupProjectSocket(path, repositoryRoot, sessionID) }}
	go func() {
		defer close(session.done)
		session.serveErr = (Gateway{Policy: policy, DialContext: dial}).Serve(sessionCtx, listener)
	}()
	return session, nil
}

func ensureProjectMCPDirectory(repositoryRoot string) error {
	root, err := os.OpenRoot(repositoryRoot)
	if err != nil {
		return fmt.Errorf("open repository for provider egress socket: %w", err)
	}
	defer root.Close()
	if err := root.Mkdir(".roundtable", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	roundtable, err := root.OpenRoot(".roundtable")
	if err != nil {
		return fmt.Errorf("open private project state for provider egress socket: %w", err)
	}
	defer roundtable.Close()
	if err := roundtable.Mkdir("mcp", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	mcp, err := roundtable.OpenRoot("mcp")
	if err != nil {
		return fmt.Errorf("open project MCP directory for provider egress socket: %w", err)
	}
	defer mcp.Close()
	info, err := roundtable.Lstat("mcp")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("project MCP directory must be a real directory")
	}
	if err := mcp.Chmod(".", 0o700); err != nil {
		return fmt.Errorf("secure project MCP directory for provider egress socket: %w", err)
	}
	return nil
}

func (s *Session) SocketPath() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done
		if s.cleanup != nil {
			s.closeErr = errors.Join(s.serveErr, s.cleanup())
		} else {
			s.closeErr = s.serveErr
		}
	})
	return s.closeErr
}

func CleanupProjectSocket(path, repositoryRoot, sessionID string) error {
	expectedDirectory, expected, err := ProjectSocketPaths(repositoryRoot, sessionID)
	if err != nil || path != expected {
		return errors.New("refusing cleanup of provider socket outside its assigned project session")
	}
	root, err := os.OpenRoot(repositoryRoot)
	if err != nil {
		return fmt.Errorf("open repository for provider egress cleanup: %w", err)
	}
	defer root.Close()
	roundtable, err := root.OpenRoot(".roundtable")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open project state for provider egress cleanup: %w", err)
	}
	defer roundtable.Close()
	mcp, err := roundtable.OpenRoot("mcp")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open project MCP directory for provider egress cleanup: %w", err)
	}
	defer mcp.Close()
	dirInfo, err := roundtable.Lstat("mcp")
	if err != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || dirInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("refusing cleanup in an unsafe project MCP directory")
	}
	name := filepath.Base(expectedDirectory)
	directory, err := mcp.OpenRoot(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open assigned provider egress directory: %w", err)
	}
	defer directory.Close()
	dirInfo, err = mcp.Lstat(name)
	openedInfo, openedErr := directory.Stat(".")
	if err != nil || openedErr != nil || !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(dirInfo, openedInfo) {
		return errors.New("refusing cleanup of a replaced provider egress directory")
	}
	socket, err := directory.Lstat("g")
	if errors.Is(err, os.ErrNotExist) {
		return mcp.Remove(name)
	}
	if err != nil || socket.Mode()&os.ModeSocket == 0 {
		return errors.New("refusing cleanup of a replaced provider egress socket")
	}
	if err := directory.Remove("g"); err != nil {
		return err
	}
	return mcp.Remove(name)
}

func CleanupStaleSocket(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "gateway.sock" || strings.ContainsAny(path, ",\x00\r\n") {
		return errors.New("refusing cleanup of invalid provider egress socket path")
	}
	tempRoot, err := filepath.EvalSymlinks("/tmp")
	if err != nil || !within(tempRoot, filepath.Dir(path)) {
		return errors.New("refusing cleanup of provider egress socket outside system temp")
	}
	directory := filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(directory), "roundtable-egress-") {
		return errors.New("refusing cleanup of unassigned provider egress socket directory")
	}
	before, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() || before.Mode().Perm()&0o077 != 0 {
		return errors.New("refusing cleanup of invalid provider egress socket directory")
	}
	parent, err := os.OpenRoot(filepath.Dir(directory))
	if err != nil {
		return fmt.Errorf("open provider egress temp root: %w", err)
	}
	root, err := parent.OpenRoot(filepath.Base(directory))
	if err != nil {
		_ = parent.Close()
		return fmt.Errorf("open private provider egress socket directory: %w", err)
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		_ = parent.Close()
		return errors.New("provider egress socket directory changed before cleanup")
	}
	socket, statErr := root.Lstat("gateway.sock")
	if statErr == nil {
		if socket.Mode()&os.ModeSocket == 0 {
			statErr = errors.New("refusing cleanup of replaced provider egress socket")
		} else {
			statErr = root.Remove("gateway.sock")
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		statErr = nil
	}
	closeRootErr := root.Close()
	removeDirErr := parent.Remove(filepath.Base(directory))
	closeParentErr := parent.Close()
	return errors.Join(statErr, closeRootErr, removeDirErr, closeParentErr)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
