package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"roundtable/internal/symbols"
)

type repositoryEntity struct {
	ID               string   `json:"id"`
	Type             string   `json:"type"`
	Path             string   `json:"path"`
	Name             string   `json:"name,omitempty"`
	Language         string   `json:"language,omitempty"`
	Kind             string   `json:"kind,omitempty"`
	StartLine        int      `json:"start_line,omitempty"`
	EndLine          int      `json:"end_line,omitempty"`
	ClaimStatus      string   `json:"claim_status,omitempty"`
	RelatedProposals []string `json:"related_proposals,omitempty"`
	RelatedEntities  []string `json:"related_entities,omitempty"`
}

type repositoryEntitiesResponse struct {
	Items      []repositoryEntity `json:"items"`
	NextCursor *string            `json:"next_cursor"`
}

func (s *Server) repositoryEntities(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	entities, err := indexRepository(workspace.RootPath)
	if err != nil {
		WriteProblem(w, r, http.StatusConflict, "repository_index_failed", "Repository index unavailable", err.Error())
		return
	}
	if err := s.enrichEntityGovernance(r, entities); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "repository_entity_context_failed", "Repository entity context unavailable", err.Error())
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	typeFilter := strings.TrimSpace(r.URL.Query().Get("type"))
	pathFilter, err := canonicalRelativePath(r.URL.Query().Get("path"))
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_repository_path", "Invalid repository path", err.Error())
		return
	}
	filtered := entities[:0]
	for _, entity := range entities {
		if typeFilter != "" && entity.Type != typeFilter {
			continue
		}
		if pathFilter != "" && entity.Path != pathFilter && !strings.HasPrefix(entity.Path, pathFilter+"/") {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entity.ID+" "+entity.Path+" "+entity.Name+" "+entity.Kind), query) {
			continue
		}
		filtered = append(filtered, entity)
	}
	start, err := cursorOffset(r.URL.Query().Get("cursor"))
	if err != nil {
		WriteProblem(w, r, http.StatusBadRequest, "invalid_cursor", "Invalid cursor", err.Error())
		return
	}
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 200 {
			WriteProblem(w, r, 400, "invalid_limit", "Invalid limit", "limit must be between 1 and 200")
			return
		}
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	var next *string
	if end < len(filtered) {
		value := strconv.Itoa(end)
		next = &value
	}
	writeJSON(w, http.StatusOK, repositoryEntitiesResponse{Items: filtered[start:end], NextCursor: next})
}

func (s *Server) repositoryEntity(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	entityID := strings.TrimSpace(r.PathValue("entity_id"))
	if entityID == "" || strings.ContainsAny(entityID, "\r\n") {
		WriteProblem(w, r, 400, "invalid_entity_id", "Invalid entity id", "entity_id is required")
		return
	}
	entities, err := indexRepository(workspace.RootPath)
	if err != nil {
		WriteProblem(w, r, 409, "repository_index_failed", "Repository index unavailable", err.Error())
		return
	}
	if err := s.enrichEntityGovernance(r, entities); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "repository_entity_context_failed", "Repository entity context unavailable", err.Error())
		return
	}
	for _, entity := range entities {
		if entity.ID == entityID || strings.TrimPrefix(entity.ID, "file:") == entityID || strings.TrimPrefix(entity.ID, "directory:") == entityID || strings.TrimPrefix(entity.ID, "symbol:") == entityID {
			writeJSON(w, http.StatusOK, entity)
			return
		}
	}
	WriteProblem(w, r, http.StatusNotFound, "entity_not_found", "Repository entity not found", entityID)
}

func (s *Server) enrichEntityGovernance(r *http.Request, entities []repositoryEntity) error {
	if s.config.Store == nil {
		return nil
	}
	claims, err := s.config.Store.ListClaims(r.Context())
	if err != nil {
		return err
	}
	claimStatus := make(map[string]string, len(claims))
	for _, claim := range claims {
		if claim.Status == "active" {
			claimStatus[claim.ResourceID] = claim.Status
		}
	}
	proposals, err := s.config.Store.ListProposals(r.Context())
	if err != nil {
		return err
	}
	proposalResources := make(map[string][]string)
	for _, proposal := range proposals {
		if proposal.Status != "pending" && proposal.Status != "in_review" {
			continue
		}
		resources, resourceErr := s.config.Store.ListProposalResources(r.Context(), proposal.ID)
		if resourceErr != nil {
			return resourceErr
		}
		for _, resource := range resources {
			proposalResources[resource.ResourceID] = append(proposalResources[resource.ResourceID], proposal.ID)
		}
	}
	for i := range entities {
		entities[i].ClaimStatus = claimStatus[entities[i].ID]
		entities[i].RelatedProposals = append([]string(nil), proposalResources[entities[i].ID]...)
	}
	return nil
}

func indexRepository(root string) ([]repositoryEntity, error) {
	var entities []repositoryEntity
	indexer := symbols.NewIndexer()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			if rel == ".git" || rel == ".roundtable" || rel == "node_modules" || strings.HasPrefix(rel, ".git/") {
				return filepath.SkipDir
			}
			entities = append(entities, repositoryEntity{ID: "directory:" + rel, Type: "directory", Path: rel, RelatedEntities: []string{"directory:."}})
			return nil
		}
		if isSecretLikePath(rel) {
			return nil
		}
		language := symbols.DetectLanguage(rel)
		kind := classifyRepositoryFile(rel)
		entities = append(entities, repositoryEntity{ID: "file:" + rel, Type: kind, Path: rel, Language: language, RelatedEntities: []string{"directory:" + filepath.ToSlash(filepath.Dir(rel))}})
		if language != "" {
			found, err := indexer.IndexPath(path)
			if err != nil {
				return nil
			}
			for _, symbol := range found {
				entities = append(entities, repositoryEntity{ID: symbol.ResourceID, Type: "symbol", Path: rel, Name: symbol.Name, Language: symbol.Language, Kind: symbol.Kind, StartLine: symbol.StartLine, EndLine: symbol.EndLine, RelatedEntities: []string{"file:" + rel}})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository: %w", err)
	}
	return entities, nil
}

func classifyRepositoryFile(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, "_test.go") || strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".spec.ts") || strings.HasSuffix(lower, ".spec.tsx"):
		return "test_suite"
	case strings.Contains(lower, "openapi") || strings.HasSuffix(lower, ".schema.json") || strings.HasSuffix(lower, ".schema.yaml") || strings.HasSuffix(lower, ".schema.yml"):
		return "schema"
	case strings.HasPrefix(lower, "cmd/") || strings.HasPrefix(lower, "scripts/"):
		return "command"
	case strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".json"):
		return "schema"
	default:
		return "file"
	}
}

func isSecretLikePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, ".env") || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential")
}

func canonicalRelativePath(value string) (string, error) {
	value = strings.TrimSpace(filepath.ToSlash(value))
	if value == "" {
		return "", nil
	}
	if filepath.IsAbs(value) || value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.Contains(value, "/../") || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("path must be repository-relative")
	}
	return filepath.ToSlash(filepath.Clean(value)), nil
}

func cursorOffset(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return 0, errors.New("cursor must be a non-negative integer")
	}
	return n, nil
}
