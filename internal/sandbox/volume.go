package sandbox

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

func executionVolumeName(kind, container string) string {
	digest := sha256.Sum256([]byte(container))
	return "rt" + kind + "-" + hex.EncodeToString(digest[:8])
}

func (r *CLIRuntime) createBoundedVolume(ctx context.Context, name string, size int64) error {
	if !safeName.MatchString(name) {
		return errors.New("agent volume name is invalid")
	}
	options := fmt.Sprintf("size=%d,uid=1000,gid=1000,mode=0770", size)
	command := exec.CommandContext(ctx, r.executable, "volume", "create", "--driver", "local", "--opt", "type=tmpfs", "--opt", "device=tmpfs", "--opt", "o="+options, name)
	command.Env = filteredRuntimeEnv(nil)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s volume create failed: %w (%s)", r.name, err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != name {
		return fmt.Errorf("%s runtime created unexpected agent volume %q", r.name, strings.TrimSpace(string(output)))
	}
	return nil
}

func (r *CLIRuntime) removeVolume(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var lastOutput []byte
	var lastErr error
	for attempt := 0; attempt < 30; attempt++ {
		command := exec.CommandContext(ctx, r.executable, "volume", "rm", name)
		command.Env = filteredRuntimeEnv(nil)
		lastOutput, lastErr = command.CombinedOutput()
		if lastErr == nil || containerNotFound(lastOutput) || strings.Contains(strings.ToLower(string(lastOutput)), "no such volume") {
			return nil
		}
		if !strings.Contains(strings.ToLower(string(lastOutput)), "volume is in use") {
			break
		}
		select {
		case <-ctx.Done():
			break
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("remove bounded agent volume %s: %w (%s)", name, lastErr, strings.TrimSpace(string(lastOutput)))
}

func (r *CLIRuntime) copyWorkspaceFromContainer(ctx context.Context, spec Spec) error {
	copyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := r.command(copyCtx, r.executable, "cp", spec.Name+":/workspace/.", "-")
	command.Env = filteredRuntimeEnv(nil)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr boundedCapture
	stderr.limit = 1 << 20
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start workspace collection: %w", err)
	}
	extractErr := extractWorkspaceArchive(stdout, spec.Workspace)
	if extractErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := command.Wait()
	if extractErr != nil || waitErr != nil {
		return fmt.Errorf("collect agent workspace: %w (docker output: %s)", errors.Join(extractErr, waitErr), stderr.String())
	}
	return nil
}

func extractWorkspaceArchive(source io.Reader, destination string) error {
	root, err := os.OpenRoot(destination)
	if err != nil {
		return fmt.Errorf("open disposable workspace for safe extraction: %w", err)
	}
	defer root.Close()
	if err := clearCollectedWorkspace(root); err != nil {
		return err
	}
	reader := tar.NewReader(io.LimitReader(source, defaultWorkspaceVolumeBytes+(64<<20)))
	seen := make(map[string]bool)
	var total int64
	for count := 0; ; count++ {
		if count >= maxWorkspaceEntries {
			return errors.New("agent workspace archive exceeds the entry limit")
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read agent workspace archive: %w", err)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if name == "." || name == "./" || name == "" {
			if header.Typeflag == tar.TypeDir {
				continue
			}
			return errors.New("agent workspace archive contains an invalid root entry")
		}
		clean := path.Clean(name)
		if clean != name && clean != strings.TrimPrefix(name, "./") || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || strings.ContainsAny(clean, "\\\x00\r\n:") {
			return fmt.Errorf("agent workspace archive contains an unsafe path %q", name)
		}
		if seen[clean] {
			return fmt.Errorf("agent workspace archive repeats path %q", clean)
		}
		seen[clean] = true
		switch header.Typeflag {
		case tar.TypeDir:
			if err := makeWorkspaceArchiveDirectory(root, clean); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || total+header.Size > defaultWorkspaceVolumeBytes {
				return errors.New("agent workspace archive exceeds the configured size limit")
			}
			parent, base, err := openWorkspaceArchiveParent(root, clean)
			if err != nil {
				return err
			}
			file, err := parent.OpenFile(base, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(header.Mode)&0o777)
			if err != nil {
				_ = parent.Close()
				return fmt.Errorf("create collected workspace file %q: %w", clean, err)
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			_ = parent.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return fmt.Errorf("write collected workspace file %q: %w", clean, err)
			}
			total += header.Size
		default:
			return fmt.Errorf("agent workspace archive contains unsupported entry type %d at %q", header.Typeflag, clean)
		}
	}
}

func clearCollectedWorkspace(root *os.Root) error {
	if err := makeCollectedWorkspaceWritable(root, "."); err != nil {
		return err
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := root.RemoveAll(entry.Name()); err != nil {
			return fmt.Errorf("clear previous disposable workspace entry %q: %w", entry.Name(), err)
		}
	}
	return nil
}

func makeCollectedWorkspaceWritable(root *os.Root, directory string) error {
	info, err := root.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil
	}
	if err := root.Chmod(directory, 0o700); err != nil {
		return err
	}
	entries, err := fs.ReadDir(root.FS(), directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if err := makeCollectedWorkspaceWritable(root, path.Join(directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func openWorkspaceArchiveParent(root *os.Root, relative string) (*os.Root, string, error) {
	parts := strings.Split(relative, "/")
	parent, err := root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, component := range parts[:len(parts)-1] {
		if err := makeWorkspaceArchiveDirectory(parent, component); err != nil {
			_ = parent.Close()
			return nil, "", err
		}
		child, err := parent.OpenRoot(component)
		if err != nil {
			_ = parent.Close()
			return nil, "", err
		}
		_ = parent.Close()
		parent = child
	}
	return parent, parts[len(parts)-1], nil
}

func makeWorkspaceArchiveDirectory(root *os.Root, name string) error {
	if err := root.Mkdir(name, 0o777); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create collected workspace directory %q: %w", name, err)
	}
	info, err := root.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("refusing unsafe collected workspace directory %q", name)
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	openedInfo, err := child.Stat(".")
	_ = child.Close()
	if err != nil || !os.SameFile(info, openedInfo) {
		return fmt.Errorf("collected workspace directory %q changed during open", name)
	}
	return nil
}
