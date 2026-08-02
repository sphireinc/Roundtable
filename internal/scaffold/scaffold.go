package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
)

type FileSpec struct {
	Path    string
	Content string
}

func EnsureLayout(root string, force bool, files []FileSpec) error {
	dirs := []string{
		".roundtable",
		".roundtable/mcp",
		".roundtable/sessions",
		".roundtable/sessions/agents",
		".roundtable/sessions/runs",
		".roundtable/memory",
		".roundtable/patches",
		".roundtable/logs",
		".roundtable/runs",
		"TASKS.ROUNDTABLE",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	for _, file := range files {
		if err := writeFile(root, file, force); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(root string, file FileSpec, force bool) error {
	path := filepath.Join(root, file.Path)
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing file: %s", file.Path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent dir for %s: %w", file.Path, err)
	}
	if err := os.WriteFile(path, []byte(file.Content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", file.Path, err)
	}
	return nil
}
