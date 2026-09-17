package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"roundtable/internal/repo"
	"roundtable/internal/symbols"
)

type patchMetadata struct {
	ProposalID   string `json:"proposal_id"`
	PatchPath    string `json:"patch_path"`
	BaseRevision string `json:"base_revision,omitempty"`
	CurrentHead  string `json:"current_head"`
	StaleBase    bool   `json:"stale_base"`
	FileCount    int    `json:"file_count"`
	AddedLines   int    `json:"added_lines"`
	RemovedLines int    `json:"removed_lines"`
	BinaryFiles  int    `json:"binary_files"`
}
type patchFile struct {
	Path         string `json:"path"`
	OldPath      string `json:"old_path,omitempty"`
	NewPath      string `json:"new_path,omitempty"`
	Operation    string `json:"operation"`
	Binary       bool   `json:"binary"`
	AddedLines   int    `json:"added_lines"`
	RemovedLines int    `json:"removed_lines"`
}
type patchSymbol struct {
	Path       string `json:"path"`
	ResourceID string `json:"resource_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

func (s *Server) patchMetadataAPI(w http.ResponseWriter, r *http.Request) {
	workspace, workspaceErr := s.workspace(r)
	if workspaceErr != nil {
		writeWorkspaceError(w, r, workspaceErr)
		return
	}
	value, info, _, err := s.loadPatch(r)
	if err != nil {
		writePatchError(w, r, err)
		return
	}
	status, _ := inspectRepository(r.Context(), workspace)
	response := patchMetadata{ProposalID: value.ProposalID, PatchPath: value.PatchPath, BaseRevision: value.BaseRevision, CurrentHead: status.HeadSHA, StaleBase: value.BaseRevision != "" && status.HeadSHA != value.BaseRevision, FileCount: len(info.Files)}
	for _, file := range info.FileChanges {
		if file.IsDelete {
			response.RemovedLines++
		}
	}
	for _, ranges := range info.Changes {
		response.AddedLines += len(ranges)
	}
	writeJSON(w, 200, response)
}

func (s *Server) patchFilesAPI(w http.ResponseWriter, r *http.Request) {
	_, info, raw, err := s.loadPatch(r)
	if err != nil {
		writePatchError(w, r, err)
		return
	}
	items := make([]patchFile, 0, len(info.FileChanges))
	for _, change := range info.FileChanges {
		path := change.NewPath
		if path == "" {
			path = change.OldPath
		}
		items = append(items, patchFile{Path: path, OldPath: change.OldPath, NewPath: change.NewPath, Operation: patchOperation(change), Binary: isBinaryPatch(raw, path)})
	}
	writePage(w, r, items)
}

func (s *Server) patchDiffAPI(w http.ResponseWriter, r *http.Request) {
	_, _, raw, err := s.loadPatch(r)
	if err != nil {
		writePatchError(w, r, err)
		return
	}
	lines := strings.Split(redactText(raw), "\n")
	writePage(w, r, lines)
}

func (s *Server) patchSymbolsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	_, info, _, err := s.loadPatch(r)
	if err != nil {
		writePatchError(w, r, err)
		return
	}
	indexer := symbols.NewIndexer()
	items := make([]patchSymbol, 0)
	for _, file := range info.Files {
		safe, safeErr := safeWorkspacePath(workspace.RootPath, file)
		if safeErr != nil {
			continue
		}
		found, indexErr := indexer.IndexPath(safe)
		if indexErr != nil {
			continue
		}
		for _, symbol := range found {
			items = append(items, patchSymbol{Path: symbol.Path, ResourceID: symbol.ResourceID, Kind: symbol.Kind, Name: symbol.Name, StartLine: symbol.StartLine, EndLine: symbol.EndLine})
		}
	}
	writePage(w, r, items)
}

func (s *Server) loadPatch(r *http.Request) (proposalResponse, repo.PatchInfo, string, error) {
	workspace, err := s.workspace(r)
	if err != nil {
		return proposalResponse{}, repo.PatchInfo{}, "", err
	}
	proposal, err := s.readProposal(r.Context(), workspace.ID, r.PathValue("proposal_id"))
	if err != nil {
		return proposalResponse{}, repo.PatchInfo{}, "", fmt.Errorf("proposal not found: %w", err)
	}
	path, err := safeWorkspacePath(workspace.RootPath, proposal.PatchPath)
	if err != nil {
		return proposalResponse{}, repo.PatchInfo{}, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return proposalResponse{}, repo.PatchInfo{}, "", err
	}
	info, err := repo.ParseTouchedFiles(string(data))
	if err != nil {
		return proposalResponse{}, repo.PatchInfo{}, "", err
	}
	return proposal, info, string(data), nil
}
func safeWorkspacePath(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("invalid repository-relative path")
	}
	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	full, err := filepath.Abs(filepath.Join(root, cleaned))
	if err != nil {
		return "", err
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if full != base && !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace root")
	}
	return full, nil
}
func patchOperation(change repo.FileChange) string {
	if change.IsRename {
		return "rename"
	}
	if change.IsNew {
		return "create"
	}
	if change.IsDelete {
		return "delete"
	}
	return "modify"
}
func isBinaryPatch(raw, path string) bool {
	marker := "Binary files "
	return strings.Contains(raw, marker+"a/"+path) || strings.Contains(raw, marker+"b/"+path)
}
func writePatchError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(strings.ToLower(err.Error()), "proposal not found") {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
		return
	}
	WriteProblem(w, r, 400, "patch_unavailable", "Patch unavailable", err.Error())
}
