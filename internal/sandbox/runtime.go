package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var safeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
var safeImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:@-]{0,254}$`)
var safeEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

const defaultWorkspaceVolumeBytes int64 = 1 << 30
const defaultTempVolumeBytes int64 = 512 << 20
const maxWorkspaceEntries = 100_000

var reservedEnv = map[string]bool{
	"PATH": true, "HOME": true, "LD_PRELOAD": true, "LD_LIBRARY_PATH": true,
	"DOCKER_HOST": true, "DOCKER_CONTEXT": true, "DOCKER_CONFIG": true,
	"CONTAINER_HOST": true, "CONTAINER_CONNECTION": true,
}

type Spec struct {
	Name                  string
	Image                 string
	ExecutionRoot         string
	AgentSessionID        string
	Source                string
	Workspace             string
	Temp                  string
	WorkspaceVolume       string
	TempVolume            string
	WorkspaceLimitBytes   int64
	TempLimitBytes        int64
	AgentMCPSocketPath    string
	AgentEgressSocketPath string
	Command               []string
	Env                   map[string]string
	AllowedEnv            []string
	BootstrapFiles        map[string][]byte
	Timeout               time.Duration
	OutputLimitBytes      int
}

type Runtime interface {
	Name() string
	Run(context.Context, Spec) (ExecutionResult, error)
}

type ExecutionResult struct {
	ExitCode        int
	StartedAt       time.Time
	EndedAt         time.Time
	Stdout          string
	Stderr          string
	OutputTruncated bool
	TimedOut        bool
	Canceled        bool
}

type CLIRuntime struct {
	name          string
	executable    string
	executionRoot string
	lookup        func(string) (string, error)
	command       func(context.Context, string, ...string) *exec.Cmd
}

func Detect(repositoryRoot string) (Runtime, error) {
	root, err := ManagedExecutionRoot(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("managed agent execution root: %w", err)
	}
	for _, name := range []string{"docker", "podman"} {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		runtime := &CLIRuntime{name: name, executable: path, executionRoot: root, lookup: exec.LookPath, command: exec.CommandContext}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		cmd := runtime.command(ctx, path, "info")
		err = cmd.Run()
		cancel()
		if err == nil {
			return runtime, nil
		}
	}
	return nil, errors.New("no usable Docker or Podman runtime; install and start Docker Engine or Podman, then verify `docker info` or `podman info`; Roundtable will not execute agents unsandboxed")
}

func NewCLIRuntime(name, repositoryRoot string) (*CLIRuntime, error) {
	if name != "docker" && name != "podman" {
		return nil, fmt.Errorf("unsupported container runtime %q (supported: docker, podman)", name)
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%s executable is unavailable: %w", name, err)
	}
	root, err := ManagedExecutionRoot(repositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("managed agent execution root: %w", err)
	}
	return &CLIRuntime{name: name, executable: path, executionRoot: root, lookup: exec.LookPath, command: exec.CommandContext}, nil
}

// ManagedExecutionRoot derives the only supported writable execution area from
// the selected authoritative repository. Agent containers receive child binds,
// never a bind of the repository itself.
func ManagedExecutionRoot(repositoryRoot string) (string, error) {
	repository, err := filepath.Abs(repositoryRoot)
	if err != nil || filepath.Clean(repository) != repository {
		return "", errors.New("repository root must be an absolute, clean path")
	}
	repository, err = filepath.EvalSymlinks(repository)
	if err != nil {
		return "", fmt.Errorf("resolve authoritative repository root: %w", err)
	}
	info, err := os.Stat(repository)
	if err != nil || !info.IsDir() {
		return "", errors.New("authoritative repository root must be a directory")
	}
	root := filepath.Join(repository, ".roundtable", "sessions", "agent-executions")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create managed agent execution root: %w", err)
	}
	canonical, err := canonicalDirectory(root)
	if err != nil {
		return "", fmt.Errorf("resolve managed agent execution root: %w", err)
	}
	if !within(repository, canonical) || canonical == repository {
		return "", errors.New("managed agent execution root escaped the authoritative repository")
	}
	return canonical, nil
}

func (r *CLIRuntime) Name() string { return r.name }

// Stop terminates a named Roundtable container and succeeds when it is already absent.
func (r *CLIRuntime) Stop(ctx context.Context, name string) error {
	if r == nil || r.executable == "" || !safeName.MatchString(name) {
		return errors.New("container stop requires a configured runtime and safe container name")
	}
	args := []string{"rm", "--force"}
	if r.name == "podman" {
		args = append(args, "--ignore")
	}
	args = append(args, name)
	cmd := exec.CommandContext(ctx, r.executable, args...)
	cmd.Env = filteredRuntimeEnv(nil)
	if output, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else {
		inspect := exec.CommandContext(ctx, r.executable, "inspect", name)
		inspect.Env = filteredRuntimeEnv(nil)
		inspectOutput, inspectErr := inspect.CombinedOutput()
		if inspectErr != nil && containerNotFound(append(output, inspectOutput...)) {
			return nil
		}
		if inspectErr != nil {
			return fmt.Errorf("stop container %s: %w (%s)", name, err, strings.TrimSpace(string(output)))
		}
		return fmt.Errorf("stop container %s failed: %w", name, err)
	}
}

func containerNotFound(output []byte) bool {
	message := strings.ToLower(string(output))
	return strings.Contains(message, "no such container") || strings.Contains(message, "no such object") || strings.Contains(message, "container not found")
}

func BuildRunArgs(spec Spec) ([]string, error) {
	if !safeName.MatchString(spec.Name) {
		return nil, errors.New("container name must be a simple identifier")
	}
	if !safeImage.MatchString(spec.Image) {
		return nil, errors.New("agent container image must be configured as a single trusted image reference")
	}
	if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return nil, errors.New("agent container command is required")
	}
	root, err := canonicalDirectory(spec.ExecutionRoot)
	if err != nil {
		return nil, fmt.Errorf("execution root: %w", err)
	}
	paths := make([]string, 0, 3)
	for _, entry := range []struct{ label, path string }{{"source", spec.Source}, {"workspace", spec.Workspace}, {"temp", spec.Temp}} {
		canonical, err := canonicalDirectory(entry.path)
		if err != nil {
			return nil, fmt.Errorf("%s directory: %w", entry.label, err)
		}
		if !within(root, canonical) {
			return nil, fmt.Errorf("%s directory resolves outside the assigned execution root", entry.label)
		}
		paths = append(paths, canonical)
	}
	if pathsOverlap(paths[0], paths[1]) || pathsOverlap(paths[0], paths[2]) || pathsOverlap(paths[1], paths[2]) {
		return nil, errors.New("source, workspace, and temp directories must be distinct and non-overlapping")
	}
	allowed := make(map[string]bool, len(spec.AllowedEnv))
	for _, name := range spec.AllowedEnv {
		if !safeEnvName.MatchString(name) || reservedEnv[name] {
			return nil, fmt.Errorf("invalid allowlisted environment variable %q", name)
		}
		allowed[name] = true
	}
	for name := range spec.Env {
		if !safeEnvName.MatchString(name) || !allowed[name] {
			return nil, fmt.Errorf("environment variable %q is not explicitly allowlisted", name)
		}
	}
	for name, value := range spec.Env {
		if strings.ContainsRune(value, '\x00') || len(value) > 64<<10 {
			return nil, fmt.Errorf("environment value for %q is invalid or exceeds 64 KiB", name)
		}
	}
	for _, arg := range spec.Command {
		if strings.ContainsRune(arg, '\x00') {
			return nil, errors.New("agent command argument contains a NUL byte")
		}
	}
	if err := validateAgentMCPSocket(spec.AgentMCPSocketPath, root, spec.AgentSessionID); err != nil {
		return nil, err
	}
	if spec.AgentMCPSocketPath != "" {
		for _, mounted := range paths {
			if pathsOverlap(spec.AgentMCPSocketPath, mounted) {
				return nil, errors.New("agent MCP socket must not overlap source, workspace, or temp mounts")
			}
		}
	}
	if err := validateAgentEgressSocket(spec.AgentEgressSocketPath, root); err != nil {
		return nil, err
	}
	for name, contents := range spec.BootstrapFiles {
		if !safeBootstrapPath(name) || len(contents) > 1<<20 {
			return nil, fmt.Errorf("invalid or oversized agent bootstrap file %q", name)
		}
	}
	if spec.AgentEgressSocketPath != "" {
		for _, mounted := range paths {
			if pathsOverlap(spec.AgentEgressSocketPath, mounted) {
				return nil, errors.New("agent egress socket must not overlap source, workspace, or temp mounts")
			}
		}
		if spec.AgentEgressSocketPath == spec.AgentMCPSocketPath {
			return nil, errors.New("agent MCP and egress sockets must be distinct")
		}
	}

	workspaceVolume, tempVolume := spec.WorkspaceVolume, spec.TempVolume
	if workspaceVolume == "" {
		workspaceVolume = executionVolumeName("w", spec.Name)
	}
	if tempVolume == "" {
		tempVolume = executionVolumeName("t", spec.Name)
	}
	if !safeName.MatchString(workspaceVolume) || !safeName.MatchString(tempVolume) || workspaceVolume == tempVolume {
		return nil, errors.New("agent workspace and temp volumes must have distinct safe names")
	}
	args := []string{
		"run", "--detach", "--name", spec.Name,
		"--user", "1000:1000",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true",
		"--read-only", "--network", "none",
		"--pids-limit", "256", "--memory", "2g", "--cpus", "2",
		"--mount", "type=bind,src=" + paths[0] + ",dst=/roundtable/source,readonly",
		"--mount", "type=volume,src=" + workspaceVolume + ",dst=/workspace",
		"--mount", "type=volume,src=" + tempVolume + ",dst=/tmp",
		"--workdir", "/workspace",
	}
	args = appendAgentMCPSocketMount(args, spec.AgentMCPSocketPath)
	args = appendAgentEgressSocketMount(args, spec.AgentEgressSocketPath)
	for _, name := range sortedKeys(spec.Env) {
		args = append(args, "--env", name)
	}
	args = append(args, spec.Image)
	args = append(args, "sh", "-c", `while :; do sleep 3600; done`)
	return args, nil
}

func BuildExecArgs(spec Spec) ([]string, error) {
	if !safeName.MatchString(spec.Name) || len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return nil, errors.New("agent exec requires a safe container name and command")
	}
	args := []string{"exec", spec.Name}
	return append(args, spec.Command...), nil
}

func appendAgentMCPSocketMount(args []string, socketPath string) []string {
	if socketPath == "" {
		return args
	}
	return append(args, "--mount", "type=bind,src="+socketPath+",dst=/run/roundtable-agent-mcp.sock")
}

func appendAgentEgressSocketMount(args []string, socketPath string) []string {
	if socketPath == "" {
		return args
	}
	return append(args, "--mount", "type=bind,src="+filepath.Dir(socketPath)+",dst=/run/roundtable-egress,readonly")
}

func (r *CLIRuntime) Run(parent context.Context, spec Spec) (result ExecutionResult, runErr error) {
	if r == nil || r.executable == "" {
		return ExecutionResult{}, errors.New("container runtime is not configured; no unsandboxed fallback is available")
	}
	root, err := canonicalDirectory(spec.ExecutionRoot)
	if err != nil || !within(r.executionRoot, root) || root == r.executionRoot {
		return ExecutionResult{}, errors.New("execution paths must belong to the runtime's configured Roundtable-managed root")
	}
	if spec.WorkspaceVolume == "" {
		spec.WorkspaceVolume = executionVolumeName("w", spec.Name)
	}
	if spec.TempVolume == "" {
		spec.TempVolume = executionVolumeName("t", spec.Name)
	}
	workspaceLimit, tempLimit := spec.WorkspaceLimitBytes, spec.TempLimitBytes
	if workspaceLimit == 0 {
		workspaceLimit = defaultWorkspaceVolumeBytes
	}
	if tempLimit == 0 {
		tempLimit = defaultTempVolumeBytes
	}
	if workspaceLimit < 1<<20 || tempLimit < 1<<20 || workspaceLimit > defaultWorkspaceVolumeBytes || tempLimit > defaultTempVolumeBytes {
		return ExecutionResult{}, errors.New("agent workspace and temp volume limits must be between 1 MiB and their configured safe maximums")
	}
	if err := r.createBoundedVolume(parent, spec.WorkspaceVolume, workspaceLimit); err != nil {
		return ExecutionResult{}, fmt.Errorf("provision bounded agent workspace: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, r.removeVolume(spec.WorkspaceVolume)) }()
	if err := r.createBoundedVolume(parent, spec.TempVolume, tempLimit); err != nil {
		return ExecutionResult{}, fmt.Errorf("provision bounded agent temp storage: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, r.removeVolume(spec.TempVolume)) }()
	args, err := BuildRunArgs(spec)
	if err != nil {
		return ExecutionResult{}, err
	}
	if _, err := r.lookup(r.name); err != nil {
		return ExecutionResult{}, fmt.Errorf("%s runtime is no longer available: %w", r.name, err)
	}
	limit := spec.OutputLimitBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	if limit > 16<<20 {
		return ExecutionResult{}, errors.New("container output limit cannot exceed 16 MiB per stream")
	}
	if spec.Timeout < 0 {
		return ExecutionResult{}, errors.New("container execution timeout must not be negative")
	}
	if spec.Timeout == 0 {
		spec.Timeout = 30 * time.Minute
	}
	if spec.Timeout > 24*time.Hour {
		return ExecutionResult{}, errors.New("container execution timeout cannot exceed 24 hours")
	}
	ctx := parent
	var cancel context.CancelFunc
	if spec.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, spec.Timeout)
		defer cancel()
	}
	startedAt := time.Now().UTC()
	startup := r.command(ctx, r.executable, args...)
	startup.Env = filteredRuntimeEnv(spec.Env)
	startupOutput, startupErr := startup.CombinedOutput()
	if startupErr != nil {
		if ctx.Err() != nil {
			result, cancelErr := contextExecutionResult(startedAt, ctx.Err())
			cleanupErr := r.cleanupContainer(spec.Name)
			if cleanupErr != nil {
				return result, errors.Join(cancelErr, fmt.Errorf("cleanup container after canceled startup: %w", cleanupErr))
			}
			return result, cancelErr
		}
		return ExecutionResult{}, fmt.Errorf("start %s agent container %s: %w (%s)", r.name, spec.Name, startupErr, strings.TrimSpace(string(startupOutput)))
	}
	if strings.TrimSpace(string(startupOutput)) == "" {
		cleanupErr := r.cleanupContainer(spec.Name)
		return ExecutionResult{}, errors.Join(errors.New("container runtime started an agent container without returning its identity"), cleanupErr)
	}
	seed := r.command(ctx, r.executable, "exec", spec.Name, "sh", "-c", `cp -R /roundtable/source/. /workspace/ && chmod -R u+rwX /workspace`)
	seed.Env = filteredRuntimeEnv(nil)
	if output, err := seed.CombinedOutput(); err != nil {
		cleanupErr := r.cleanupContainer(spec.Name)
		if ctx.Err() != nil {
			result, cancelErr := contextExecutionResult(startedAt, ctx.Err())
			return result, errors.Join(cancelErr, fmt.Errorf("cleanup: %w", cleanupErr))
		}
		return ExecutionResult{}, fmt.Errorf("seed bounded agent workspace from assigned read-only snapshot: %w (%s); cleanup: %v", err, strings.TrimSpace(string(output)), cleanupErr)
	}
	if err := r.seedBootstrapFiles(ctx, spec); err != nil {
		cleanupErr := r.cleanupContainer(spec.Name)
		return ExecutionResult{}, errors.Join(fmt.Errorf("seed scoped agent configuration: %w", err), cleanupErr)
	}
	result = ExecutionResult{ExitCode: -1, StartedAt: startedAt}
	finished := make(chan struct{})
	cleanupDone := make(chan struct{})
	cleanupResult := make(chan error, 1)
	go func() {
		defer close(cleanupDone)
		select {
		case <-ctx.Done():
			cleanupResult <- r.cleanupContainer(spec.Name)
		case <-finished:
			if ctx.Err() != nil {
				cleanupResult <- r.cleanupContainer(spec.Name)
			}
		}
	}()
	execArgs, err := BuildExecArgs(spec)
	if err != nil {
		close(finished)
		_ = r.cleanupContainer(spec.Name)
		return result, err
	}
	command := r.command(ctx, r.executable, execArgs...)
	stdout, stderr := &boundedCapture{limit: limit}, &boundedCapture{limit: limit}
	command.Stdout, command.Stderr = stdout, stderr
	command.Env = filteredRuntimeEnv(spec.Env)
	err = command.Run()
	result.EndedAt = time.Now().UTC()
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	result.OutputTruncated = stdout.truncated || stderr.truncated
	if ctx.Err() != nil {
		close(finished)
		select {
		case <-cleanupDone:
		case <-time.After(6 * time.Second):
			return result, fmt.Errorf("agent container %s cancellation cleanup timed out", spec.Name)
		}
		if cleanupErr := <-cleanupResult; cleanupErr != nil {
			return result, fmt.Errorf("agent container %s cancellation cleanup failed: %w", spec.Name, cleanupErr)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
			return result, fmt.Errorf("agent container %s exceeded execution timeout: %w", spec.Name, ctx.Err())
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			result.Canceled = true
			return result, fmt.Errorf("agent container %s was canceled: %w", spec.Name, ctx.Err())
		}
		if err != nil {
			return result, fmt.Errorf("%s agent container %s failed: %w", r.name, spec.Name, err)
		}
		return result, nil
	}
	if exit, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exit.ExitCode()
	}
	if pauseErr := r.pauseContainer(spec.Name); pauseErr != nil {
		close(finished)
		cleanupErr := r.cleanupContainer(spec.Name)
		return result, fmt.Errorf("freeze agent container before workspace collection: %w; cleanup: %v", pauseErr, cleanupErr)
	}
	collectErr := r.copyWorkspaceFromContainer(ctx, spec)
	if ctx.Err() != nil {
		close(finished)
		select {
		case <-cleanupDone:
		case <-time.After(6 * time.Second):
			return result, fmt.Errorf("agent container %s cancellation cleanup timed out", spec.Name)
		}
		if cleanupErr := <-cleanupResult; cleanupErr != nil {
			return result, fmt.Errorf("agent container %s cancellation cleanup failed: %w", spec.Name, cleanupErr)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.TimedOut = true
			return result, fmt.Errorf("agent container %s exceeded execution timeout: %w", spec.Name, ctx.Err())
		}
		result.Canceled = true
		return result, fmt.Errorf("agent container %s was canceled: %w", spec.Name, ctx.Err())
	}
	if cleanupErr := r.cleanupContainer(spec.Name); cleanupErr != nil {
		close(finished)
		return result, fmt.Errorf("remove completed agent container: %w", cleanupErr)
	}
	close(finished)
	if collectErr != nil {
		return result, fmt.Errorf("collect bounded agent workspace: %w", collectErr)
	}
	if err != nil {
		return result, fmt.Errorf("%s agent container %s failed: %w", r.name, spec.Name, err)
	}
	result.ExitCode = 0
	return result, nil
}

func safeBootstrapPath(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && filepath.ToSlash(filepath.Clean(name)) == name && name != "." && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00\r\n:")
}

func (r *CLIRuntime) seedBootstrapFiles(ctx context.Context, spec Spec) error {
	for name, contents := range spec.BootstrapFiles {
		if !safeBootstrapPath(name) || len(contents) > 1<<20 {
			return fmt.Errorf("invalid or oversized agent bootstrap file %q", name)
		}
		containerPath := "/tmp/" + name
		command := r.command(ctx, r.executable, "exec", "-i", spec.Name, "sh", "-c", `umask 077; mkdir -p -- "$(dirname -- "$1")" && cat > "$1" && chmod 400 "$1"`, "roundtable-bootstrap", containerPath)
		command.Env = filteredRuntimeEnv(nil)
		command.Stdin = bytes.NewReader(contents)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("write bootstrap file %q: %w (%s)", name, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func contextExecutionResult(startedAt time.Time, contextErr error) (ExecutionResult, error) {
	result := ExecutionResult{ExitCode: -1, StartedAt: startedAt, EndedAt: time.Now().UTC()}
	if errors.Is(contextErr, context.DeadlineExceeded) {
		result.TimedOut = true
		return result, fmt.Errorf("agent container exceeded execution timeout: %w", contextErr)
	}
	result.Canceled = true
	return result, fmt.Errorf("agent container was canceled: %w", contextErr)
}

type boundedCapture struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedCapture) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.Buffer.Write(value)
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func (r *CLIRuntime) cleanupContainer(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, r.executable, "kill", name)
	kill.Env = filteredRuntimeEnv(nil)
	killOutput, killErr := kill.CombinedOutput()
	removeArgs := []string{"rm", "--force"}
	if r.name == "podman" {
		removeArgs = append(removeArgs, "--ignore")
	}
	remove := exec.CommandContext(ctx, r.executable, append(removeArgs, name)...)
	remove.Env = filteredRuntimeEnv(nil)
	removeOutput, removeErr := remove.CombinedOutput()
	if removeErr == nil {
		return nil
	}
	inspect := exec.CommandContext(ctx, r.executable, "inspect", name)
	inspect.Env = filteredRuntimeEnv(nil)
	inspectOutput, inspectErr := inspect.CombinedOutput()
	if inspectErr != nil && containerNotFound(append(append(killOutput, removeOutput...), inspectOutput...)) {
		return nil
	}
	return fmt.Errorf("kill container: %v (%s); remove container: %w (%s); inspect: %v (%s)", killErr, strings.TrimSpace(string(killOutput)), removeErr, strings.TrimSpace(string(removeOutput)), inspectErr, strings.TrimSpace(string(inspectOutput)))
}

func (r *CLIRuntime) pauseContainer(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pause := r.command(ctx, r.executable, "pause", name)
	pause.Env = filteredRuntimeEnv(nil)
	output, err := pause.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pause agent container %s: %w (%s)", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveExecutionVolumes reconciles bounded disposable storage after a crash.
func (r *CLIRuntime) RemoveExecutionVolumes(container string) error {
	if r == nil || r.executable == "" || !safeName.MatchString(container) {
		return errors.New("agent volume cleanup requires a configured runtime and safe container name")
	}
	return errors.Join(r.removeVolume(executionVolumeName("w", container)), r.removeVolume(executionVolumeName("t", container)))
}

func canonicalDirectory(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, ",\x00\r\n") {
		return "", errors.New("must be an absolute, clean path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("must reference an existing directory")
	}
	return resolved, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathsOverlap(a, b string) bool {
	if a == b {
		return true
	}
	rel, err := filepath.Rel(a, b)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func filteredRuntimeEnv(credentials map[string]string) []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "DOCKER_HOST": true, "DOCKER_CONTEXT": true, "DOCKER_CONFIG": true, "CONTAINER_HOST": true, "CONTAINER_CONNECTION": true}
	for key := range credentials {
		allowed[key] = true
	}
	out := make([]string, 0, len(allowed))
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && allowed[name] {
			out = append(out, entry)
		}
	}
	for name, value := range credentials {
		found := false
		for i := range out {
			if strings.HasPrefix(out[i], name+"=") {
				out[i] = name + "=" + value
				found = true
				break
			}
		}
		if !found {
			out = append(out, name+"="+value)
		}
	}
	return out
}
