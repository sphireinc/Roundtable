package sandbox

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"roundtable/internal/repo"
)

const (
	maxChangedFileBytes = 16 << 20
	maxPatchBytes       = 32 << 20
)

// CollectPatch compares the stopped workspace with its pinned source snapshot.
// It does not inspect or trust Git metadata created inside the workspace.
func CollectPatch(paths SnapshotPaths) (string, error) {
	root, err := canonicalDirectory(paths.ExecutionRoot)
	if err != nil {
		return "", fmt.Errorf("execution root: %w", err)
	}
	canonical := make([]string, 3)
	for i, candidate := range []string{paths.Source, paths.Workspace, paths.Temp} {
		canonical[i], err = canonicalDirectory(candidate)
		if err != nil || !within(root, canonical[i]) {
			return "", errors.New("patch collection paths must remain inside the session execution root")
		}
	}
	if pathsOverlap(canonical[0], canonical[1]) || pathsOverlap(canonical[0], canonical[2]) || pathsOverlap(canonical[1], canonical[2]) {
		return "", errors.New("source, workspace, and temp paths must not overlap")
	}
	sourceRoot, err := os.OpenRoot(canonical[0])
	if err != nil {
		return "", err
	}
	defer sourceRoot.Close()
	workspaceRoot, err := os.OpenRoot(canonical[1])
	if err != nil {
		return "", err
	}
	defer workspaceRoot.Close()
	sourceFiles, err := listRegularFiles(sourceRoot, true)
	if err != nil {
		return "", fmt.Errorf("inspect pinned source snapshot: %w", err)
	}
	workspaceFiles, err := listRegularFiles(workspaceRoot, true)
	if err != nil {
		return "", fmt.Errorf("inspect agent workspace: %w", err)
	}
	union := make(map[string]struct{}, len(sourceFiles)+len(workspaceFiles))
	for name := range sourceFiles {
		union[name] = struct{}{}
	}
	for name := range workspaceFiles {
		union[name] = struct{}{}
	}
	names := make([]string, 0, len(union))
	for name := range union {
		names = append(names, name)
	}
	sort.Strings(names)
	stage, err := os.MkdirTemp("", "roundtable-patch-collection-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	var patch strings.Builder
	for _, name := range names {
		oldInfo, oldExists := sourceFiles[name]
		newInfo, newExists := workspaceFiles[name]
		if oldExists && newExists {
			equal, err := regularFilesEqual(sourceRoot, name, oldInfo, workspaceRoot, name, newInfo)
			if err != nil {
				return "", err
			}
			if equal {
				continue
			}
		}
		if newExists {
			newExecutable := newInfo.Mode()&0o111 != 0
			oldExecutable := false
			if oldExists {
				oldExecutable = oldInfo.Mode()&0o111 != 0
			}
			if newExecutable != oldExecutable {
				return "", fmt.Errorf("changed file %q changes executable permissions that unified patch application cannot preserve", name)
			}
			if !oldExists && newInfo.Mode().Perm() != 0o644 {
				return "", fmt.Errorf("new file %q must have standard 0644 permissions for safe patch application", name)
			}
		}
		if strings.ContainsAny(name, " \t\r\n\\:") {
			return "", fmt.Errorf("changed workspace path %q cannot be represented safely in a unified patch", name)
		}
		oldLabel, newLabel := "a/"+name, "b/"+name
		oldArg, newArg := "/dev/null", "/dev/null"
		if oldExists {
			oldArg, err = stageRegularFile(sourceRoot, name, oldInfo, stage)
			if err != nil {
				return "", err
			}
		}
		if newExists {
			newArg, err = stageRegularFile(workspaceRoot, name, newInfo, stage)
			if err != nil {
				return "", err
			}
		}
		if !oldExists {
			oldLabel = "/dev/null"
		}
		if !newExists {
			newLabel = "/dev/null"
		}
		command := exec.Command("diff", "-u", "--label", oldLabel, "--label", newLabel, oldArg, newArg)
		output, err := command.CombinedOutput()
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return "", fmt.Errorf("generate patch for %q: %w: %s", name, err, strings.TrimSpace(string(output)))
			}
		}
		if strings.HasPrefix(string(output), "Binary files ") {
			return "", fmt.Errorf("changed file %q is binary; binary patch artifacts are not accepted", name)
		}
		patch.WriteString("diff --git a/")
		patch.WriteString(name)
		patch.WriteString(" b/")
		patch.WriteString(name)
		patch.WriteByte('\n')
		if !oldExists {
			patch.WriteString("new file mode 100644\n")
		}
		if !newExists {
			patch.WriteString("deleted file mode 100644\n")
		}
		patch.Write(output)
		if patch.Len() > maxPatchBytes {
			return "", errors.New("collected patch exceeds the 32 MiB limit")
		}
	}
	result := patch.String()
	if result == "" {
		return "", nil
	}
	info, err := repo.ParseTouchedFiles(result)
	if err != nil {
		return "", fmt.Errorf("validate collected patch: %w", err)
	}
	if err := repo.ValidatePatchPaths(canonical[0], info.Files); err != nil {
		return "", fmt.Errorf("validate collected patch targets: %w", err)
	}
	return result, nil
}

func listRegularFiles(root *os.Root, ignoreRootGit bool) (map[string]fs.FileInfo, error) {
	files := make(map[string]fs.FileInfo)
	err := fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			return nil
		}
		if ignoreRootGit && rel == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("workspace contains symlink %q", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if rel != ".git" && strings.HasSuffix(filepath.ToSlash(rel), "/.git") {
				return fmt.Errorf("nested Git metadata is not allowed at %q", filepath.ToSlash(rel))
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported special workspace entry %q", rel)
		}
		files[rel] = info
		return nil
	})
	return files, err
}

func regularFilesEqual(leftRoot *os.Root, left string, leftInfo fs.FileInfo, rightRoot *os.Root, right string, rightInfo fs.FileInfo) (bool, error) {
	leftFile, err := openRegularChecked(leftRoot, left, leftInfo)
	if err != nil {
		return false, err
	}
	defer leftFile.Close()
	rightFile, err := openRegularChecked(rightRoot, right, rightInfo)
	if err != nil {
		return false, err
	}
	defer rightFile.Close()
	leftHash, rightHash := sha256.New(), sha256.New()
	if _, err := io.Copy(leftHash, leftFile); err != nil {
		return false, err
	}
	if _, err := io.Copy(rightHash, rightFile); err != nil {
		return false, err
	}
	return bytes.Equal(leftHash.Sum(nil), rightHash.Sum(nil)), nil
}

func stageRegularFile(root *os.Root, relative string, expected fs.FileInfo, destination string) (string, error) {
	file, err := openRegularChecked(root, relative, expected)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() < 0 || info.Size() > maxChangedFileBytes {
		return "", fmt.Errorf("changed file %q exceeds the 16 MiB patch collection limit", relative)
	}
	staged, err := os.CreateTemp(destination, "file-")
	if err != nil {
		return "", err
	}
	name := staged.Name()
	_, copyErr := io.Copy(staged, file)
	closeErr := staged.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func openRegularChecked(root *os.Root, relative string, expected fs.FileInfo) (*os.File, error) {
	before, err := root.Lstat(relative)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || !os.SameFile(expected, before) {
		return nil, fmt.Errorf("workspace file %q changed or is not a regular file", relative)
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("workspace file %q changed while opening", relative)
	}
	return file, nil
}
