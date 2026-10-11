package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func AgentMCPSocketPaths(repositoryRoot, sessionID string) (string, string, error) {
	if !safeName.MatchString(sessionID) {
		return "", "", errors.New("agent MCP socket requires a safe session identifier")
	}
	repository, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", "", err
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository for agent MCP socket: %w", err)
	}
	digest := sha256.Sum256([]byte(repository + "\x00" + sessionID))
	directory := filepath.Join(repository, ".roundtable", "mcp", "a-"+hex.EncodeToString(digest[:8]))
	socket := filepath.Join(directory, "m")
	return directory, socket, nil
}

func PrepareAgentMCPSocketDir(repositoryRoot, sessionID string) (string, string, error) {
	directory, socket, err := AgentMCPSocketPaths(repositoryRoot, sessionID)
	if err != nil {
		return "", "", err
	}
	if len(socket) >= 100 {
		return "", "", errors.New("agent MCP Unix socket path exceeds the platform limit; shorten the repository path")
	}
	repository, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return "", "", err
	}
	if err := ensureAgentMCPSocketRoot(repository); err != nil {
		return "", "", err
	}
	if err := os.Mkdir(directory, 0o711); err != nil {
		return "", "", fmt.Errorf("create per-session agent MCP socket directory: %w", err)
	}
	return directory, socket, nil
}

func CleanupAgentMCPSocketDir(repositoryRoot, sessionID string) error {
	directory, socket, err := AgentMCPSocketPaths(repositoryRoot, sessionID)
	if err != nil {
		return err
	}
	if len(socket) >= 100 {
		return nil
	}
	repository, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository for agent MCP cleanup: %w", err)
	}
	root, err := os.OpenRoot(repository)
	if err != nil {
		return fmt.Errorf("open repository for agent MCP cleanup: %w", err)
	}
	defer root.Close()
	stateInfo, err := root.Lstat(".roundtable")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || stateInfo.Mode()&os.ModeSymlink != 0 || !stateInfo.IsDir() {
		return errors.New("refusing cleanup through an unsafe project state directory")
	}
	state, err := root.OpenRoot(".roundtable")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open project state for agent MCP cleanup: %w", err)
	}
	defer state.Close()
	mcpRoot, err := state.OpenRoot("mcp")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open project MCP directory for agent cleanup: %w", err)
	}
	defer mcpRoot.Close()
	mcpInfo, err := state.Lstat("mcp")
	if err != nil || mcpInfo.Mode()&os.ModeSymlink != 0 || !mcpInfo.IsDir() || mcpInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("refusing cleanup in an unsafe project MCP directory")
	}
	dirName := filepath.Base(directory)
	dirRoot, err := mcpRoot.OpenRoot(dirName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open assigned agent MCP socket directory: %w", err)
	}
	defer dirRoot.Close()
	dirInfo, err := mcpRoot.Lstat(dirName)
	openedInfo, openedErr := dirRoot.Stat(".")
	if err != nil || openedErr != nil || dirInfo.Mode()&os.ModeSymlink != 0 || !dirInfo.IsDir() || dirInfo.Mode().Perm() != 0o711 || !os.SameFile(dirInfo, openedInfo) {
		return errors.New("agent MCP socket directory changed or became unsafe during cleanup")
	}
	entries, err := fs.ReadDir(dirRoot.FS(), ".")
	if err != nil || len(entries) > 1 || (len(entries) == 1 && entries[0].Name() != filepath.Base(socket)) {
		return errors.New("refusing cleanup of an unexpected agent MCP socket directory")
	}
	socketInfo, err := dirRoot.Lstat(filepath.Base(socket))
	if err == nil {
		if socketInfo.Mode()&os.ModeSocket == 0 {
			return errors.New("refusing cleanup because agent MCP socket path is not a socket")
		}
		if err := dirRoot.Remove(filepath.Base(socket)); err != nil {
			return fmt.Errorf("remove agent MCP socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect agent MCP socket before cleanup: %w", err)
	}
	if err := mcpRoot.Remove(dirName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove agent MCP socket directory: %w", err)
	}
	return nil
}

func ensureAgentMCPSocketRoot(repositoryRoot string) error {
	repository, err := os.OpenRoot(repositoryRoot)
	if err != nil {
		return fmt.Errorf("open repository for agent MCP socket: %w", err)
	}
	defer repository.Close()
	stateInfo, err := repository.Lstat(".roundtable")
	if err == nil && (stateInfo.Mode()&os.ModeSymlink != 0 || !stateInfo.IsDir()) {
		return errors.New("project state directory must be a real directory for agent MCP sockets")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect project state for agent MCP socket: %w", err)
	}
	if err := repository.Mkdir(".roundtable", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	state, err := repository.OpenRoot(".roundtable")
	if err != nil {
		return fmt.Errorf("open project state for agent MCP socket: %w", err)
	}
	defer state.Close()
	if err := state.Mkdir("mcp", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	mcpRoot, err := state.OpenRoot("mcp")
	if err != nil {
		return fmt.Errorf("open project MCP directory for agent socket: %w", err)
	}
	defer mcpRoot.Close()
	info, err := state.Lstat("mcp")
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("project MCP socket directory must be a real directory")
	}
	return mcpRoot.Chmod(".", 0o700)
}

func validateAgentMCPSocket(path, executionRoot, sessionID string) error {
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "m" || strings.ContainsAny(path, ",\x00\r\n") {
		return errors.New("agent MCP socket must be an absolute clean assigned socket path")
	}
	parent := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return errors.New("agent MCP socket parent cannot be a symlink")
	}
	root, err := canonicalDirectory(executionRoot)
	if err != nil {
		return errors.New("agent MCP execution directory is invalid")
	}
	repository := filepath.Clean(filepath.Join(root, "..", "..", "..", ".."))
	canonicalRepository, err := filepath.EvalSymlinks(repository)
	_, expectedSocket, expectedErr := AgentMCPSocketPaths(repository, sessionID)
	if sessionID == "" || expectedErr != nil || expectedSocket != path {
		return errors.New("agent MCP socket does not belong to the assigned execution session")
	}
	directoryName := filepath.Base(parent)
	if err != nil || canonicalRepository != repository || len(directoryName) != 18 || !strings.HasPrefix(directoryName, "a-") || filepath.Dir(filepath.Dir(parent)) != filepath.Join(repository, ".roundtable") || filepath.Base(filepath.Dir(parent)) != "mcp" {
		return errors.New("agent MCP socket must be under the project MCP directory")
	}
	stateInfo, err := os.Lstat(filepath.Join(repository, ".roundtable"))
	if err != nil || stateInfo.Mode()&os.ModeSymlink != 0 || !stateInfo.IsDir() {
		return errors.New("agent MCP project state directory must be a real directory")
	}
	mcpInfo, err := os.Lstat(filepath.Dir(parent))
	if err != nil || mcpInfo.Mode()&os.ModeSymlink != 0 || !mcpInfo.IsDir() || mcpInfo.Mode().Perm()&0o077 != 0 {
		return errors.New("agent MCP project directory must remain private")
	}
	for _, char := range strings.TrimPrefix(directoryName, "a-") {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return errors.New("agent MCP socket directory must contain its session digest")
		}
	}
	info, err := os.Lstat(parent)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o711 {
		return errors.New("agent MCP socket directory must be private and session-scoped")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "m" {
		return errors.New("agent MCP socket directory must contain only its assigned socket")
	}
	socketInfo, err := os.Lstat(path)
	if err != nil || socketInfo.Mode()&os.ModeSocket == 0 {
		return errors.New("agent MCP socket path must reference an existing Unix socket")
	}
	return nil
}

func validateAgentEgressSocket(path, executionRoot string) error {
	if path == "" {
		return nil
	}
	name := filepath.Base(filepath.Dir(path))
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "g" || !strings.HasPrefix(name, "e-") || len(name) != 2+24 || strings.ContainsAny(path, ",\x00\r\n") {
		return errors.New("agent egress socket must be a clean assigned gateway socket path")
	}
	for _, char := range strings.TrimPrefix(name, "e-") {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return errors.New("agent egress socket filename must contain a session digest")
		}
	}
	parent := filepath.Dir(path)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return errors.New("agent egress socket parent cannot be a symlink")
	}
	execution, err := canonicalDirectory(executionRoot)
	if err != nil {
		return errors.New("agent egress execution session root is invalid")
	}
	repositoryRoot := filepath.Clean(filepath.Join(execution, "..", "..", "..", ".."))
	canonicalRepository, err := filepath.EvalSymlinks(repositoryRoot)
	if err != nil || canonicalRepository != repositoryRoot || filepath.Dir(filepath.Dir(parent)) != filepath.Join(repositoryRoot, ".roundtable") || filepath.Base(filepath.Dir(parent)) != "mcp" {
		return errors.New("agent egress socket must be under the project MCP directory")
	}
	info, err := os.Lstat(parent)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o711 {
		return errors.New("agent egress mount directory must be private and session-scoped")
	}
	socket, err := os.Lstat(path)
	if err != nil || socket.Mode()&os.ModeSocket == 0 {
		return errors.New("agent egress socket path must reference an existing Unix socket")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "g" {
		return errors.New("agent egress mount directory must contain only its assigned socket")
	}
	return nil
}
