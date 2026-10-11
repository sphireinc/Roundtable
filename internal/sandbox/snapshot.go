package sandbox

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"roundtable/internal/security"
)

const (
	maxSnapshotFiles = 100_000
	maxSnapshotBytes = int64(2 << 30)
)

type SnapshotPaths struct {
	ExecutionRoot string
	Source        string
	Workspace     string
	Temp          string
	Revision      string
	BaseStateHash string
}

// CreateSnapshot materializes a clean committed tree without its .git metadata
// into a disposable session directory. Symlinks and special files fail closed.
func CreateSnapshot(ctx context.Context, repositoryRoot, sessionID string) (SnapshotPaths, error) {
	return createPinnedSnapshot(ctx, repositoryRoot, sessionID, nil, false)
}

// CreateScopedSnapshot exposes only explicitly authorized paths to the agent
// container; an empty or sensitive path scope is rejected.
func CreateScopedSnapshot(ctx context.Context, repositoryRoot, sessionID string, readablePaths []string) (SnapshotPaths, error) {
	if len(readablePaths) == 0 {
		return SnapshotPaths{}, errors.New("agent execution requires operator-assigned readable_paths; refusing a full-repository workspace")
	}
	for _, value := range readablePaths {
		if len(value) > 1024 || !security.ValidAgentPath(value) {
			return SnapshotPaths{}, fmt.Errorf("agent execution path %q is not permitted", value)
		}
	}
	return createPinnedSnapshot(ctx, repositoryRoot, sessionID, readablePaths, true)
}

func createPinnedSnapshot(ctx context.Context, repositoryRoot, sessionID string, readablePaths []string, scoped bool) (SnapshotPaths, error) {
	if !safeName.MatchString(sessionID) {
		return SnapshotPaths{}, errors.New("session id must be a simple directory identifier")
	}
	canonicalRepo, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return SnapshotPaths{}, fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := gitOutput(ctx, canonicalRepo, "rev-parse", "--verify", "--end-of-options", "HEAD^{commit}")
	if err != nil || resolved == "" {
		return SnapshotPaths{}, fmt.Errorf("resolve snapshot commit HEAD: %w", err)
	}
	status, err := gitOutput(ctx, canonicalRepo, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return SnapshotPaths{}, fmt.Errorf("inspect repository before snapshot: %w", err)
	}
	if status != "" {
		return SnapshotPaths{}, errors.New("agent execution requires a clean authoritative working tree; commit or preserve local changes before creating a revision-pinned snapshot")
	}
	treeHash, err := gitOutput(ctx, canonicalRepo, "rev-parse", "--verify", resolved+"^{tree}")
	if err != nil || treeHash == "" {
		return SnapshotPaths{}, fmt.Errorf("resolve snapshot tree hash: %w", err)
	}
	canonicalExecutionRoot, err := ManagedExecutionRoot(canonicalRepo)
	if err != nil {
		return SnapshotPaths{}, fmt.Errorf("prepare managed execution root: %w", err)
	}
	if filepath.Clean(canonicalExecutionRoot) != canonicalExecutionRoot {
		return SnapshotPaths{}, errors.New("execution root path is not canonical")
	}
	return createSnapshotAt(ctx, canonicalRepo, canonicalExecutionRoot, sessionID, resolved, treeHash, readablePaths, scoped)
}

func createSnapshotAt(ctx context.Context, repositoryRoot, executionRoot, sessionID, revision, treeHash string, readablePaths []string, scoped bool) (SnapshotPaths, error) {
	if !safeName.MatchString(sessionID) {
		return SnapshotPaths{}, errors.New("session id must be a simple directory identifier")
	}
	sessionRoot := filepath.Join(executionRoot, sessionID)
	if _, err := os.Lstat(sessionRoot); err == nil {
		return SnapshotPaths{}, errors.New("execution session directory already exists")
	} else if !os.IsNotExist(err) {
		return SnapshotPaths{}, fmt.Errorf("inspect execution session directory: %w", err)
	}
	if err := os.Mkdir(sessionRoot, 0o700); err != nil {
		return SnapshotPaths{}, fmt.Errorf("create execution session directory: %w", err)
	}
	created := true
	defer func() {
		if created {
			_ = os.RemoveAll(sessionRoot)
		}
	}()
	paths := SnapshotPaths{
		ExecutionRoot: sessionRoot,
		Source:        filepath.Join(sessionRoot, "source"),
		Workspace:     filepath.Join(sessionRoot, "workspace"),
		Temp:          filepath.Join(sessionRoot, "temp"),
		Revision:      revision,
		BaseStateHash: treeHash,
	}
	if err := os.Mkdir(paths.Source, 0o700); err != nil {
		return SnapshotPaths{}, err
	}
	if err := os.Mkdir(paths.Workspace, 0o777); err != nil {
		return SnapshotPaths{}, err
	}
	if err := os.Mkdir(paths.Temp, 0o777); err != nil {
		return SnapshotPaths{}, err
	}
	if err := os.Chmod(paths.Temp, 0o777); err != nil {
		return SnapshotPaths{}, fmt.Errorf("make agent temp mount accessible to the container user: %w", err)
	}
	if err := unpackGitArchive(ctx, repositoryRoot, revision, paths.Source, readablePaths, scoped); err != nil {
		return SnapshotPaths{}, err
	}
	if scoped {
		for _, value := range readablePaths {
			parent := filepath.Dir(filepath.Join(paths.Source, filepath.FromSlash(value)))
			if err := os.MkdirAll(parent, 0o700); err != nil {
				return SnapshotPaths{}, fmt.Errorf("prepare assigned new-file path: %w", err)
			}
		}
	}
	if err := copySnapshotWorkspace(paths.Source, paths.Workspace); err != nil {
		return SnapshotPaths{}, fmt.Errorf("populate writable agent workspace: %w", err)
	}
	if err := makeWorkspaceWritable(paths.Workspace); err != nil {
		return SnapshotPaths{}, fmt.Errorf("make disposable workspace writable by the container user: %w", err)
	}
	if err := makeSnapshotReadonly(paths.Source); err != nil {
		return SnapshotPaths{}, fmt.Errorf("make source snapshot read-only: %w", err)
	}
	created = false
	return paths, nil
}

func makeSnapshotReadonly(root string) error {
	return filepath.Walk(root, func(filename string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("snapshot contains unsupported entry %q", filename)
		}
		if info.IsDir() {
			return os.Chmod(filename, 0o555)
		}
		if info.Mode().IsRegular() {
			mode := os.FileMode(0o444)
			if info.Mode()&0o111 != 0 {
				mode |= 0o111
			}
			return os.Chmod(filename, mode)
		}
		return nil
	})
}

func makeWorkspaceWritable(root string) error {
	return filepath.Walk(root, func(filename string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("workspace contains unsupported entry %q", filename)
		}
		mode := os.FileMode(0o666)
		if info.IsDir() || info.Mode()&0o111 != 0 {
			mode = 0o777
		}
		return os.Chmod(filename, mode)
	})
}

func copySnapshotWorkspace(source, workspace string) error {
	return filepath.Walk(source, func(filename string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, filename)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(workspace, rel)
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("snapshot contains unsupported workspace entry %q", rel)
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(filename)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inputCloseErr := in.Close()
		closeErr := out.Close()
		return errors.Join(copyErr, inputCloseErr, closeErr)
	})
}

func CleanupExecution(repositoryRoot string, paths SnapshotPaths) error {
	root, err := filepath.Abs(paths.ExecutionRoot)
	if err != nil || !safeName.MatchString(filepath.Base(root)) {
		return errors.New("refusing cleanup of an invalid execution path")
	}
	managed, err := ManagedExecutionRoot(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve managed execution root before cleanup: %w", err)
	}
	if filepath.Dir(root) != managed || paths.Source != filepath.Join(root, "source") || paths.Workspace != filepath.Join(root, "workspace") || paths.Temp != filepath.Join(root, "temp") {
		return errors.New("refusing cleanup outside the repository-managed session directory")
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("resolve execution path before cleanup: %w", err)
	}
	if canonicalRoot != root {
		return errors.New("refusing cleanup when execution paths do not match the session root")
	}
	if err := removeExecutionContents(root); err != nil {
		return err
	}
	return os.RemoveAll(root)
}

func removeExecutionContents(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return fmt.Errorf("open managed session for cleanup: %w", err)
	}
	defer root.Close()
	if err := makeExecutionDirectoriesWritable(root, "."); err != nil {
		return err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return fmt.Errorf("list managed session for cleanup: %w", err)
	}
	for _, entry := range entries {
		if err := root.RemoveAll(entry.Name()); err != nil {
			return fmt.Errorf("remove managed session entry %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func makeExecutionDirectoriesWritable(root *os.Root, directory string) error {
	info, err := root.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil
	}
	if err := root.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("make managed session directory writable: %w", err)
	}
	entries, err := fs.ReadDir(root.FS(), directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			if err := makeExecutionDirectoriesWritable(root, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func unpackGitArchive(ctx context.Context, root, revision, destination string, readablePaths []string, scoped bool) error {
	command := exec.CommandContext(ctx, "git", "archive", "--format=tar", revision)
	command.Dir = root
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &boundedCapture{limit: 64 << 10}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start pinned Git archive: %w", err)
	}
	reader := tar.NewReader(stdout)
	var total int64
	count := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("read pinned Git archive: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		count++
		if count > maxSnapshotFiles {
			_ = command.Process.Kill()
			_ = command.Wait()
			return errors.New("Git snapshot exceeds 100,000 entries")
		}
		archiveName := header.Name
		if header.Typeflag == tar.TypeDir {
			archiveName = strings.TrimSuffix(archiveName, "/")
		}
		rel := filepath.FromSlash(archiveName)
		clean := path.Clean(archiveName)
		if clean == "." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.Contains(archiveName, "\\") || clean != archiveName {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			total += header.Size
			if header.Size < 0 || total > maxSnapshotBytes {
				_ = command.Process.Kill()
				_ = command.Wait()
				return errors.New("Git snapshot exceeds 2 GiB")
			}
		}
		if scoped && (!archiveEntryInScope(archiveName, header.Typeflag == tar.TypeDir, readablePaths) || security.SensitiveAgentPath(archiveName)) {
			continue
		}
		target := filepath.Join(destination, rel)
		if !within(destination, target) {
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("archive path %q escapes snapshot", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				_ = command.Process.Kill()
				_ = command.Wait()
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				_ = command.Process.Kill()
				_ = command.Wait()
				return err
			}
			mode := os.FileMode(0o600)
			if header.Mode&0o111 != 0 {
				mode = 0o700
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				_ = command.Process.Kill()
				_ = command.Wait()
				return err
			}
			written, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil || written != header.Size {
				_ = command.Process.Kill()
				_ = command.Wait()
				err := errors.Join(copyErr, closeErr)
				if err != nil {
					return err
				}
				return fmt.Errorf("archive entry size mismatch for %q", header.Name)
			}
		default:
			_ = command.Process.Kill()
			_ = command.Wait()
			return fmt.Errorf("Git snapshot contains unsupported symlink or special entry %q", header.Name)
		}
	}
	if err := command.Wait(); err != nil {
		return fmt.Errorf("create pinned Git archive: %w: %s", err, stderr.String())
	}
	return nil
}

func archiveEntryInScope(name string, directory bool, scopes []string) bool {
	for _, scope := range scopes {
		if name == scope || strings.HasPrefix(name, strings.TrimSuffix(scope, "/")+"/") {
			return true
		}
		if directory && strings.HasPrefix(scope, strings.TrimSuffix(name, "/")+"/") {
			return true
		}
	}
	return false
}

func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}
