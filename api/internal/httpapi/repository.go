package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"roundtable/internal/db"
)

type repositoryStatusResponse struct {
	WorkspaceID string               `json:"workspace_id"`
	RootAlias   string               `json:"root_alias,omitempty"`
	Branch      string               `json:"branch"`
	HeadSHA     string               `json:"head_sha"`
	Dirty       bool                 `json:"dirty"`
	Ahead       int                  `json:"ahead"`
	Behind      int                  `json:"behind"`
	Detached    bool                 `json:"detached"`
	Remotes     []repositoryRemote   `json:"remotes"`
	Index       []repositoryChange   `json:"index"`
	Protected   protectedPathSummary `json:"protected_paths"`
}

type repositoryRemote struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type repositoryChange struct {
	Path     string `json:"path"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
}

type protectedPathSummary struct {
	Changed int      `json:"changed"`
	Paths   []string `json:"paths,omitempty"`
}

type branchSwitchInput struct {
	Branch string `json:"branch"`
}

type branchSwitchPreflightResponse struct {
	WorkspaceID string                   `json:"workspace_id"`
	Target      string                   `json:"target_branch"`
	Allowed     bool                     `json:"allowed"`
	Blockers    []repositoryBlocker      `json:"blockers"`
	Status      repositoryStatusResponse `json:"repository"`
}

type repositoryBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int    `json:"count,omitempty"`
}

type branchSwitchResponse struct {
	Workspace workspaceResponse        `json:"workspace"`
	Before    repositoryStatusResponse `json:"before"`
	After     repositoryStatusResponse `json:"after"`
}

var repositorySwitchMu sync.Mutex

func (s *Server) repositoryStatus(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	status, err := inspectRepository(r.Context(), workspace)
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "repository_status_unavailable", "Repository status unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) branchSwitchPreflight(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	input, err := decodeBranchInput(r)
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_branch", "Invalid branch request", err.Error())
		return
	}
	status, err := inspectRepository(r.Context(), workspace)
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "repository_status_unavailable", "Repository status unavailable", err.Error())
		return
	}
	preflight, err := s.branchPreflight(r.Context(), workspace, input.Branch, status)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "branch_preflight_failed", "Branch preflight failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preflight)
}

func (s *Server) branchSwitch(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, http.StatusForbidden, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency key required", "Branch switching requires Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	input, err := decodeBranchInput(r)
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_branch", "Invalid branch request", err.Error())
		return
	}
	repositorySwitchMu.Lock()
	defer repositorySwitchMu.Unlock()
	before, err := inspectRepository(r.Context(), workspace)
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "repository_status_unavailable", "Repository status unavailable", err.Error())
		return
	}
	preflight, err := s.branchPreflight(r.Context(), workspace, input.Branch, before)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "branch_preflight_failed", "Branch preflight failed", err.Error())
		return
	}
	if !preflight.Allowed {
		writeJSON(w, http.StatusConflict, preflight)
		return
	}
	if err := git(r.Context(), workspace.RootPath, "switch", input.Branch); err != nil {
		WriteProblem(w, r, http.StatusConflict, "branch_switch_failed", "Branch switch failed", err.Error())
		return
	}
	after, err := inspectRepository(r.Context(), workspace)
	if err != nil {
		_ = git(context.Background(), workspace.RootPath, "switch", before.Branch)
		WriteProblem(w, r, http.StatusConflict, "repository_status_unavailable", "Repository status unavailable", err.Error())
		return
	}
	updated, err := s.config.Store.RecordRepositoryBranchSwitch(r.Context(), workspace.ID, r.Header.Get("X-Actor-ID"), requestID(r.Context()), before.Branch, after.Branch, workspace.Revision)
	if err != nil {
		_ = git(context.Background(), workspace.RootPath, "switch", before.Branch)
		if errors.Is(err, db.ErrRevisionConflict) {
			WriteProblem(w, r, http.StatusConflict, "workspace_revision_conflict", "Workspace changed", err.Error())
			return
		}
		WriteProblem(w, r, http.StatusInternalServerError, "branch_switch_record_failed", "Branch switch could not be recorded", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, branchSwitchResponse{Workspace: toWorkspaceResponse(updated), Before: before, After: after})
}

func decodeBranchInput(r *http.Request) (branchSwitchInput, error) {
	var input branchSwitchInput
	if err := decodeJSON(r, &input); err != nil {
		return input, err
	}
	input.Branch = strings.TrimSpace(input.Branch)
	if input.Branch == "" || input.Branch == "HEAD" || strings.ContainsAny(input.Branch, "\r\n") || filepath.IsAbs(input.Branch) || strings.Contains(input.Branch, "..") || strings.HasPrefix(input.Branch, "-") {
		return input, errors.New("branch must be a safe repository branch name")
	}
	return input, nil
}

func humanAuthorized(r *http.Request) bool {
	if !authorized(r) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("X-Actor-Role"))) {
	case "human", "admin", "chair", "view", "operate", "approve", "govern", "administer", "force-override":
		return true
	default:
		return false
	}
}

func (s *Server) branchPreflight(ctx context.Context, workspace db.Workspace, target string, status repositoryStatusResponse) (branchSwitchPreflightResponse, error) {
	impact, err := s.config.Store.WorkspaceImpact(ctx, workspace.ID)
	if err != nil {
		return branchSwitchPreflightResponse{}, err
	}
	blockers := make([]repositoryBlocker, 0)
	if status.Dirty {
		blockers = append(blockers, repositoryBlocker{Code: "dirty_worktree", Message: "Repository has uncommitted changes"})
	}
	if impact.ActiveTransactions > 0 {
		blockers = append(blockers, repositoryBlocker{Code: "active_transactions", Message: "A transaction is active", Count: impact.ActiveTransactions})
	}
	if impact.ActiveClaims > 0 {
		blockers = append(blockers, repositoryBlocker{Code: "active_claims", Message: "Active resource claims must be resolved", Count: impact.ActiveClaims})
	}
	if impact.OpenProposals > 0 {
		blockers = append(blockers, repositoryBlocker{Code: "open_proposals", Message: "Open proposals must be resolved", Count: impact.OpenProposals})
	}
	if impact.ActiveSessions > 0 {
		blockers = append(blockers, repositoryBlocker{Code: "active_sessions", Message: "Active sessions must be resolved", Count: impact.ActiveSessions})
	}
	if status.Branch == target {
		blockers = append(blockers, repositoryBlocker{Code: "already_on_branch", Message: "Repository is already on the target branch"})
	}
	return branchSwitchPreflightResponse{WorkspaceID: workspace.ID, Target: target, Allowed: len(blockers) == 0, Blockers: blockers, Status: status}, nil
}

func inspectRepository(ctx context.Context, workspace db.Workspace) (repositoryStatusResponse, error) {
	root := workspace.RootPath
	branch, err := gitOutput(ctx, root, "branch", "--show-current")
	if err != nil {
		return repositoryStatusResponse{}, err
	}
	head, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return repositoryStatusResponse{}, err
	}
	detached := strings.TrimSpace(branch) == ""
	if detached {
		branch = "HEAD"
	}
	porcelain, err := gitOutput(ctx, root, "status", "--porcelain=v1")
	if err != nil {
		return repositoryStatusResponse{}, err
	}
	changes := parseRepositoryChanges(porcelain)
	remotes, err := repositoryRemotes(ctx, root)
	if err != nil {
		return repositoryStatusResponse{}, err
	}
	ahead, behind := 0, 0
	if !detached {
		if counts, countErr := gitOutput(ctx, root, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); countErr == nil {
			_, _ = fmt.Sscanf(strings.TrimSpace(counts), "%d %d", &behind, &ahead)
		}
	}
	protected := protectedPathSummary{}
	for _, change := range changes {
		if isProtectedPath(change.Path) {
			protected.Changed++
			protected.Paths = append(protected.Paths, change.Path)
		}
	}
	return repositoryStatusResponse{WorkspaceID: workspace.ID, RootAlias: workspace.RootAlias, Branch: strings.TrimSpace(branch), HeadSHA: strings.TrimSpace(head), Dirty: len(changes) > 0, Ahead: ahead, Behind: behind, Detached: detached, Remotes: remotes, Index: changes, Protected: protected}, nil
}

func git(ctx context.Context, root string, args ...string) error {
	_, err := gitOutput(ctx, root, args...)
	return err
}
func gitOutput(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func repositoryRemotes(ctx context.Context, root string) ([]repositoryRemote, error) {
	out, err := gitOutput(ctx, root, "remote", "-v")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var remotes []repositoryRemote
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		remotes = append(remotes, repositoryRemote{Name: fields[0], URL: fields[1]})
	}
	return remotes, nil
}

func parseRepositoryChanges(value string) []repositoryChange {
	var changes []repositoryChange
	for _, line := range strings.Split(strings.TrimSpace(value), "\n") {
		if len(line) < 4 {
			continue
		}
		status := line[:2]
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			path = strings.TrimSpace(strings.SplitN(path, " -> ", 2)[1])
		}
		changes = append(changes, repositoryChange{Path: path, Index: string(status[0]), Worktree: string(status[1])})
	}
	return changes
}

func isProtectedPath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	return path == ".roundtable" || strings.HasPrefix(path, ".roundtable/") || path == ".github" || strings.HasPrefix(path, ".github/") || path == "TASKS.ROUNDTABLE" || strings.HasPrefix(path, "TASKS.ROUNDTABLE/")
}
