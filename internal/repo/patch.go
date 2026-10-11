package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
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
	oldHeaderPath, newHeaderPath := "", ""
	seenOldHeader, seenNewHeader := false, false
	inHunk := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
			parts := strings.Fields(line)
			if len(parts) != 4 || strings.ContainsAny(parts[2]+parts[3], "\"'") {
				return PatchInfo{}, fmt.Errorf("invalid diff header: %s", line)
			}
			if currentIndex >= 0 && seenOldHeader != seenNewHeader {
				return PatchInfo{}, fmt.Errorf("incomplete apply paths for previous diff header")
			}
			oldPath := normalizeDiffPath(parts[2], "a/")
			newPath := normalizeDiffPath(parts[3], "b/")
			for _, path := range []string{oldPath, newPath} {
				if path != "" {
					if err := validateRepositoryPath(path); err != nil {
						return PatchInfo{}, fmt.Errorf("unsafe patch path %q: %w", path, err)
					}
				}
			}
			currentPath = changePath(oldPath, newPath)
			fileChanges = append(fileChanges, FileChange{
				OldPath: oldPath,
				NewPath: newPath,
			})
			currentIndex = len(fileChanges) - 1
			oldHeaderPath, newHeaderPath = oldPath, newPath
			seenOldHeader, seenNewHeader = false, false
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
		case !inHunk && strings.HasPrefix(line, "new file mode "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].IsNew = true
			}
		case !inHunk && strings.HasPrefix(line, "deleted file mode "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].IsDelete = true
			}
		case !inHunk && strings.HasPrefix(line, "rename from "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].OldPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(line, "rename from ")))
				if err := validateRepositoryPath(fileChanges[currentIndex].OldPath); err != nil {
					return PatchInfo{}, fmt.Errorf("unsafe patch rename source: %w", err)
				}
				fileChanges[currentIndex].IsRename = true
				currentPath = changePath(fileChanges[currentIndex].OldPath, fileChanges[currentIndex].NewPath)
				if path := fileChanges[currentIndex].OldPath; path != "" {
					if _, ok := seen[path]; !ok {
						seen[path] = struct{}{}
						files = append(files, path)
					}
				}
			}
		case !inHunk && strings.HasPrefix(line, "--- "):
			if currentIndex < 0 || seenOldHeader {
				return PatchInfo{}, fmt.Errorf("unexpected or duplicate old apply path")
			}
			value := strings.TrimPrefix(line, "--- ")
			if strings.ContainsAny(value, "\t\"") {
				return PatchInfo{}, fmt.Errorf("unsupported quoted or timestamped apply path %q", value)
			}
			path := normalizeDiffPath(value, "a/")
			expected := oldHeaderPath
			if fileChanges[currentIndex].IsNew {
				expected = ""
			}
			if path != expected {
				return PatchInfo{}, fmt.Errorf("old apply path %q does not match diff path %q", path, oldHeaderPath)
			}
			seenOldHeader = true
		case !inHunk && strings.HasPrefix(line, "+++ "):
			if currentIndex < 0 || !seenOldHeader || seenNewHeader {
				return PatchInfo{}, fmt.Errorf("unexpected or duplicate new apply path")
			}
			value := strings.TrimPrefix(line, "+++ ")
			if strings.ContainsAny(value, "\t\"") {
				return PatchInfo{}, fmt.Errorf("unsupported quoted or timestamped apply path %q", value)
			}
			path := normalizeDiffPath(value, "b/")
			expected := newHeaderPath
			if fileChanges[currentIndex].IsDelete {
				expected = ""
			}
			if path != expected {
				return PatchInfo{}, fmt.Errorf("new apply path %q does not match diff path %q", path, newHeaderPath)
			}
			seenNewHeader = true
		case !inHunk && strings.HasPrefix(line, "rename to "):
			if currentIndex >= 0 {
				fileChanges[currentIndex].NewPath = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(line, "rename to ")))
				if err := validateRepositoryPath(fileChanges[currentIndex].NewPath); err != nil {
					return PatchInfo{}, fmt.Errorf("unsafe patch rename destination: %w", err)
				}
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
			inHunk = true
		}
	}
	if seenOldHeader != seenNewHeader {
		return PatchInfo{}, fmt.Errorf("incomplete apply paths")
	}
	sort.Strings(files)
	if len(files) == 0 {
		return PatchInfo{}, fmt.Errorf("patch contains no diff headers")
	}
	return PatchInfo{Files: files, Changes: changes, FileChanges: fileChanges}, nil
}

func validateRepositoryPath(value string) error {
	if value == "" || strings.ContainsAny(value, "\\\x00:") || strings.HasPrefix(value, "/") {
		return fmt.Errorf("path must be a nonempty repository-relative slash path")
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != value {
		return fmt.Errorf("path traversal or noncanonical path")
	}
	first := strings.SplitN(clean, "/", 2)[0]
	if first == ".git" || first == ".roundtable" {
		return fmt.Errorf("protected Roundtable/Git metadata paths cannot be patched")
	}
	return nil
}

// ValidatePatchPaths rejects targets whose existing path components are
// symlinks, preventing git apply from following an in-repository link outside
// the authoritative tree.
func ValidatePatchPaths(root string, files []string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	for _, rel := range files {
		if err := validateRepositoryPath(rel); err != nil {
			return err
		}
		candidate := filepath.Join(canonicalRoot, filepath.FromSlash(rel))
		if !withinRoot(canonicalRoot, candidate) {
			return fmt.Errorf("patch path %q escapes repository root", rel)
		}
		current := canonicalRoot
		parts := strings.Split(filepath.FromSlash(rel), string(filepath.Separator))
		for index, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return fmt.Errorf("inspect patch path %q: %w", rel, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("patch path %q traverses a symlink", rel)
			}
			if index < len(parts)-1 && !info.IsDir() {
				return fmt.Errorf("patch path %q has a non-directory parent", rel)
			}
			if index == len(parts)-1 && info.IsDir() {
				return fmt.Errorf("patch path %q targets a directory", rel)
			}
		}
	}
	return nil
}

func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// GitHeadRevision returns the immutable commit id anchoring a snapshot. It is
// empty only when root is not a Git work tree or has no commit yet.
func GitHeadRevision(root string) (string, error) {
	head, isRepo, err := gitHead(root)
	if err != nil {
		return "", err
	}
	if !isRepo {
		return "", nil
	}
	return head, nil
}

func GitTreeHash(root string) (string, error) {
	head, isRepo, err := gitHead(root)
	if err != nil {
		return "", err
	}
	if !isRepo || head == "" {
		return "", nil
	}
	cmd := exec.Command("git", "rev-parse", "--verify", head+"^{tree}")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse tree: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func GitWorkingTreeClean(root string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=normal")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if strings.Contains(message, "not a git repository") {
			return true, nil
		}
		return false, fmt.Errorf("git status: %w", err)
	}
	return len(strings.TrimSpace(string(output))) == 0, nil
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
