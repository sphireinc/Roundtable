package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type PatchInfo struct {
	Files       []string
	Changes     map[string][]LineRange
	FileChanges []FileChange
}

type LineRange struct {
	StartLine int
	EndLine   int
}

type FileChange struct {
	OldPath  string
	NewPath  string
	IsNew    bool
	IsDelete bool
	IsRename bool
}

type RollbackEntry struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Content string `json:"content"`
}

func ParseTouchedFiles(patch string) (PatchInfo, error) {
	lines := strings.Split(patch, "\n")
	seen := map[string]struct{}{}
	var files []string
	changes := map[string][]LineRange{}
	fileChanges := []FileChange{}
	currentPath := ""
	currentIndex := -1
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			parts := strings.Fields(line)
			if len(parts) < 4 {
				return PatchInfo{}, fmt.Errorf("invalid diff header: %s", line)
			}
			oldPath := normalizeDiffPath(parts[2], "a/")
			newPath := normalizeDiffPath(parts[3], "b/")
			currentPath = changePath(oldPath, newPath)
			fileChanges = append(fileChanges, FileChange{
				OldPath: oldPath,
				NewPath: newPath,
			})
			currentIndex = len(fileChanges) - 1
			for _, path := range []string{oldPath, newPath} {
				if path == "" {
					continue
				}
				if _, ok := seen[path]; ok {
					continue
				}
				seen[path] = struct{}{}
				files = append(files, path)
			}
		case strings.HasPrefix(line, "new file mode "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].IsNew = true
			}
		case strings.HasPrefix(line, "deleted file mode "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].IsDelete = true
			}
		case strings.HasPrefix(line, "rename from "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].OldPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(line, "rename from ")))
				fileChanges[currentIndex].IsRename = true
				currentPath = changePath(fileChanges[currentIndex].OldPath, fileChanges[currentIndex].NewPath)
				if path := fileChanges[currentIndex].OldPath; path != "" {
					if _, ok := seen[path]; !ok {
						seen[path] = struct{}{}
						files = append(files, path)
					}
				}
			}
		case strings.HasPrefix(line, "rename to "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].NewPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(line, "rename to ")))
				fileChanges[currentIndex].IsRename = true
				currentPath = changePath(fileChanges[currentIndex].OldPath, fileChanges[currentIndex].NewPath)
				if path := fileChanges[currentIndex].NewPath; path != "" {
					if _, ok := seen[path]; !ok {
						seen[path] = struct{}{}
						files = append(files, path)
					}
				}
			}
		case strings.HasPrefix(line, "@@ "):
			if currentPath == "" {
				return PatchInfo{}, fmt.Errorf("hunk encountered before diff header")
			}
			lineRange, err := parseHunkRange(line)
			if err != nil {
				return PatchInfo{}, err
			}
			changes[currentPath] = append(changes[currentPath], lineRange)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return PatchInfo{}, fmt.Errorf("patch contains no diff headers")
	}
	return PatchInfo{Files: files, Changes: changes, FileChanges: fileChanges}, nil
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@`)

func parseHunkRange(line string) (LineRange, error) {
	matches := hunkHeaderRe.FindStringSubmatch(line)
	if len(matches) != 5 {
		return LineRange{}, fmt.Errorf("invalid hunk header: %s", line)
	}
	oldStart, _ := strconv.Atoi(matches[1])
	oldCount := 1
	if matches[2] != "" {
		oldCount, _ = strconv.Atoi(matches[2])
	}
	newStart, _ := strconv.Atoi(matches[3])
	if oldCount == 0 {
		start := oldStart
		if start <= 0 {
			start = newStart
		}
		if start <= 0 {
			start = 1
		}
		return LineRange{StartLine: start, EndLine: start}, nil
	}
	end := oldStart + oldCount - 1
	if oldStart <= 0 {
		oldStart = 1
	}
	if end < oldStart {
		end = oldStart
	}
	return LineRange{StartLine: oldStart, EndLine: end}, nil
}

func WorkspaceHash(root string) (string, error) {
	hasher := sha256.New()
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() && (name == ".roundtable" || name == ".git") {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}
		_, _ = hasher.Write([]byte(rel))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(data)
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func RepoStateHash(root string) (string, error) {
	head, ok, err := gitHead(root)
	if err != nil {
		return "", err
	}
	workspaceHash, err := WorkspaceHash(root)
	if err != nil {
		return "", err
	}
	if !ok {
		return workspaceHash, nil
	}
	if head == "" {
		head = "no-head"
	}
	return "git:" + head + ":" + workspaceHash, nil
}

func FileHash(root, rel string) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func LineRangeHash(root, rel string, startLine, endLine int) (string, error) {
	if startLine <= 0 || endLine < startLine {
		return "", nil
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if startLine > len(lines) {
		return "", nil
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	segment := strings.Join(lines[startLine-1:endLine], "\n")
	sum := sha256.Sum256([]byte(segment))
	return hex.EncodeToString(sum[:]), nil
}

func DirectoryHash(root, rel string) (string, error) {
	dir := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", rel)
	}
	hasher := sha256.New()
	var paths []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, relPath := range paths {
		data, err := os.ReadFile(filepath.Join(root, relPath))
		if err != nil {
			return "", err
		}
		_, _ = hasher.Write([]byte(relPath))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(data)
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func ApplyPatch(root, patch string) error {
	cmd := exec.Command("patch", "-p1")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(patch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("patch apply failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func CopyWorkspace(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if info.IsDir() && (name == ".roundtable" || name == ".git") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func CreateRollbackArtifact(root string, files []string) ([]RollbackEntry, error) {
	entries := make([]RollbackEntry, 0, len(files))
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				entries = append(entries, RollbackEntry{Path: rel, Existed: false})
				continue
			}
			return nil, err
		}
		entries = append(entries, RollbackEntry{Path: rel, Existed: true, Content: string(data)})
	}
	return entries, nil
}

func WriteRollbackArtifact(path string, entries []RollbackEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func gitHead(root string) (string, bool, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if strings.Contains(message, "not a git repository") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("git rev-parse --show-toplevel: %w: %s", err, message)
	}

	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if strings.Contains(message, "unknown revision") || strings.Contains(message, "ambiguous argument 'HEAD'") || strings.Contains(message, "Needed a single revision") {
			return "", true, nil
		}
		return "", true, fmt.Errorf("git rev-parse HEAD: %w: %s", err, message)
	}
	return strings.TrimSpace(string(output)), true, nil
}

func normalizeDiffPath(value, prefix string) string {
	path := strings.TrimPrefix(value, prefix)
	path = filepath.ToSlash(path)
	if path == "/dev/null" {
		return ""
	}
	return path
}

func changePath(oldPath, newPath string) string {
	if oldPath != "" {
		return oldPath
	}
	return newPath
}
