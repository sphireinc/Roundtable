package claims

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"roundtable/internal/db"
	"roundtable/internal/repo"
	"roundtable/internal/symbols"
)

var ErrClaimConflict = errors.New("claim conflict")

type Service struct {
	store   *db.Store
	indexer *symbols.Indexer
}

type CreateRequest struct {
	ID           string
	RunID        string
	AgentID      string
	TaskID       string
	ResourceID   string
	ResourceType string
	ResourcePath string
	SymbolName   string
	ClaimType    string
	BaseHash     string
	TTL          time.Duration
	Renewable    bool
	ResumePolicy string
	RationaleMD  string
}

type StatusFilter struct {
	AgentID    string
	ResourceID string
	Status     string
}

type Conflict struct {
	Claim    db.Claim
	Resource db.Resource
	Reason   string
}

type ReconcileRequest struct {
	Root    string
	RunID   string
	AgentID string
	ActorID string
}

func NewService(store *db.Store) *Service {
	return &Service{store: store, indexer: symbols.NewIndexer()}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (db.Claim, error) {
	if err := validateCreateRequest(req); err != nil {
		return db.Claim{}, err
	}

	resource := db.Resource{
		ID:       normalizeResourceID(req.ResourceID, req.ResourceType, req.ResourcePath, req.SymbolName),
		Type:     req.ResourceType,
		Path:     cleanResourcePath(req.ResourceType, req.ResourcePath),
		Symbol:   req.SymbolName,
		Language: symbols.DetectLanguage(req.ResourcePath),
	}
	if err := s.populateResourceSpan(&resource); err != nil {
		return db.Claim{}, err
	}
	if err := s.store.UpsertResource(ctx, resource); err != nil {
		return db.Claim{}, err
	}
	resource, err := s.store.GetResource(ctx, resource.ID)
	if err != nil {
		return db.Claim{}, err
	}

	conflicts, err := s.FindConflicts(ctx, resource, req.ClaimType)
	if err != nil {
		return db.Claim{}, err
	}
	if len(conflicts) > 0 {
		for _, conflict := range conflicts {
			if err := s.recordContention(ctx, req, resource, conflict); err != nil {
				return db.Claim{}, err
			}
		}
		return db.Claim{}, fmt.Errorf("%w: %s", ErrClaimConflict, conflicts[0].Reason)
	}

	expiresAt := time.Now().UTC().Add(effectiveTTL(req.TTL))
	claim := db.Claim{
		ID:           defaultClaimID(req.ID),
		ResourceID:   resource.ID,
		AgentID:      req.AgentID,
		TaskID:       req.TaskID,
		ClaimType:    req.ClaimType,
		BaseHash:     req.BaseHash,
		Status:       "active",
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		Renewable:    req.Renewable,
		ResumePolicy: defaultResumePolicy(req.ResumePolicy),
		RationaleMD:  req.RationaleMD,
	}
	if err := s.store.UpsertClaim(ctx, claim); err != nil {
		return db.Claim{}, err
	}
	saved, err := s.store.GetClaim(ctx, claim.ID)
	if err != nil {
		return db.Claim{}, err
	}
	_ = s.appendEvent(ctx, req.RunID, "claim.created", saved.AgentID, saved.TaskID, map[string]any{
		"claim_id":     saved.ID,
		"resource_id":  saved.ResourceID,
		"claim_type":   saved.ClaimType,
		"expires_at":   saved.ExpiresAt,
		"resourceType": resource.Type,
		"path":         resource.Path,
	})
	return saved, nil
}

func (s *Service) recordContention(ctx context.Context, req CreateRequest, requested db.Resource, conflict Conflict) error {
	id := fmt.Sprintf("contention-%d", time.Now().UnixNano())
	_, err := s.store.DB().ExecContext(ctx, `INSERT INTO claim_contentions (id, resource_id, claimant_id, challenged_claim_id, status, reason, requested_resource_id, requested_agent_id, requested_task_id, requested_mode, requested_path) VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?, ?, ?)`, id, requested.ID, conflict.Claim.ID, conflict.Claim.ID, conflict.Reason, requested.ID, req.AgentID, req.TaskID, req.ClaimType, req.ResourcePath)
	return err
}

func (s *Service) populateResourceSpan(resource *db.Resource) error {
	if resource.Type != "symbol" || resource.Path == "" || resource.Symbol == "" {
		return nil
	}
	found, err := s.indexer.IndexPath(resource.Path)
	if err != nil {
		return nil
	}
	for _, symbol := range found {
		if symbol.Name != resource.Symbol {
			continue
		}
		resource.StartLine = symbol.StartLine
		resource.EndLine = symbol.EndLine
		return nil
	}
	return nil
}

func (s *Service) Release(ctx context.Context, runID, claimID, actorID, reason string) (db.Claim, error) {
	return s.transition(ctx, runID, claimID, actorID, "released", "claim.released", reason)
}

func (s *Service) Extend(ctx context.Context, runID, claimID, actorID string, ttl time.Duration) (db.Claim, error) {
	claim, err := s.store.GetClaim(ctx, claimID)
	if err != nil {
		return db.Claim{}, err
	}
	if claim.Status != "active" {
		return db.Claim{}, fmt.Errorf("claim %s is not active", claimID)
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	claim.ExpiresAt = time.Now().UTC().Add(ttl).Format(time.RFC3339)
	claim.HeartbeatAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpsertClaim(ctx, claim); err != nil {
		return db.Claim{}, err
	}
	saved, err := s.store.GetClaim(ctx, claimID)
	if err != nil {
		return db.Claim{}, err
	}
	return saved, s.appendEvent(ctx, runID, "claim.extended", actorID, saved.TaskID, map[string]any{"claim_id": saved.ID, "expires_at": saved.ExpiresAt})
}

func (s *Service) Revoke(ctx context.Context, runID, claimID, actorID, reason string) (db.Claim, error) {
	return s.transition(ctx, runID, claimID, actorID, "revoked", "claim.revoked", reason)
}

func (s *Service) Suspend(ctx context.Context, runID, claimID, actorID, reason string) (db.Claim, error) {
	return s.transition(ctx, runID, claimID, actorID, "suspended", "claim.suspended", reason)
}

func (s *Service) ExpireDueClaims(ctx context.Context, runID string, now time.Time) ([]db.Claim, error) {
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return nil, err
	}
	var expired []db.Claim
	for _, claim := range claims {
		if claim.Status != "active" {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, claim.ExpiresAt)
		if err != nil {
			continue
		}
		if expiresAt.After(now.UTC()) {
			continue
		}
		updated, err := s.transition(ctx, runID, claim.ID, "system", "expired", "claim.expired", "ttl elapsed")
		if err != nil {
			return nil, err
		}
		expired = append(expired, updated)
	}
	return expired, nil
}

func (s *Service) List(ctx context.Context, filter StatusFilter) ([]db.Claim, error) {
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]db.Claim, 0, len(claims))
	for _, claim := range claims {
		if filter.AgentID != "" && claim.AgentID != filter.AgentID {
			continue
		}
		if filter.ResourceID != "" && claim.ResourceID != filter.ResourceID {
			continue
		}
		if filter.Status != "" && claim.Status != filter.Status {
			continue
		}
		out = append(out, claim)
	}
	return out, nil
}

func (s *Service) FindConflicts(ctx context.Context, requested db.Resource, claimType string) ([]Conflict, error) {
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := s.store.ListResources(ctx)
	if err != nil {
		return nil, err
	}
	resourceByID := make(map[string]db.Resource, len(resources))
	for _, resource := range resources {
		resourceByID[resource.ID] = resource
	}

	var conflicts []Conflict
	for _, claim := range claims {
		if claim.Status != "active" {
			continue
		}
		existingResource, ok := resourceByID[claim.ResourceID]
		if !ok {
			continue
		}
		if !resourcesOverlap(requested, existingResource) {
			continue
		}
		if !claimTypesConflict(claimType, claim.ClaimType) {
			continue
		}
		conflicts = append(conflicts, Conflict{
			Claim:    claim,
			Resource: existingResource,
			Reason:   fmt.Sprintf("%s claim %s already holds overlapping %s resource %s", claim.ClaimType, claim.ID, existingResource.Type, describeResource(existingResource)),
		})
	}
	return conflicts, nil
}

func (s *Service) ReconcileStaleClaims(ctx context.Context, req ReconcileRequest) ([]db.Claim, error) {
	if strings.TrimSpace(req.Root) == "" {
		return nil, errors.New("root is required")
	}
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return nil, err
	}
	suspended := make([]db.Claim, 0)
	for _, claim := range claims {
		if claim.Status != "active" || claim.BaseHash == "" {
			continue
		}
		if req.AgentID != "" && claim.AgentID != req.AgentID {
			continue
		}
		if claim.ClaimType == "read" || claim.ClaimType == "review" {
			continue
		}
		resource, err := s.store.GetResource(ctx, claim.ResourceID)
		if err != nil {
			return nil, err
		}
		currentHash, err := s.currentResourceHash(req.Root, resource)
		if err != nil {
			return nil, err
		}
		resource.CurrentHash = currentHash
		if err := s.store.UpsertResource(ctx, resource); err != nil {
			return nil, err
		}
		if currentHash == "" || currentHash == claim.BaseHash {
			continue
		}
		reason := fmt.Sprintf("stale base hash: claimed %s current %s", claim.BaseHash, currentHash)
		updated, err := s.Suspend(ctx, req.RunID, claim.ID, defaultActorID(req.ActorID), reason)
		if err != nil {
			return nil, err
		}
		suspended = append(suspended, updated)
	}
	return suspended, nil
}

func (s *Service) transition(ctx context.Context, runID, claimID, actorID, status, eventType, reason string) (db.Claim, error) {
	claim, err := s.store.GetClaim(ctx, claimID)
	if err != nil {
		return db.Claim{}, err
	}
	claim.Status = status
	if err := s.store.UpsertClaim(ctx, claim); err != nil {
		return db.Claim{}, err
	}
	saved, err := s.store.GetClaim(ctx, claimID)
	if err != nil {
		return db.Claim{}, err
	}
	_ = s.appendEvent(ctx, runID, eventType, actorID, saved.TaskID, map[string]any{
		"claim_id":    saved.ID,
		"resource_id": saved.ResourceID,
		"status":      saved.Status,
		"reason":      reason,
	})
	return saved, nil
}

func (s *Service) appendEvent(ctx context.Context, runID, eventType, actorID, taskID string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.store.AppendEvent(ctx, db.Event{
		RunID:       runID,
		Type:        eventType,
		ActorID:     actorID,
		TaskID:      taskID,
		PayloadJSON: string(body),
	})
	return err
}

func validateCreateRequest(req CreateRequest) error {
	switch {
	case req.AgentID == "":
		return errors.New("agent id is required")
	case req.TaskID == "":
		return errors.New("task id is required")
	case req.ResourceType == "":
		return errors.New("resource type is required")
	case req.ClaimType == "":
		return errors.New("claim type is required")
	case req.ResourceID == "" && req.ResourcePath == "":
		return errors.New("resource id or resource path is required")
	}
	switch req.ClaimType {
	case "read", "write", "review", "exclusive":
	default:
		return fmt.Errorf("unsupported claim type: %s", req.ClaimType)
	}
	return nil
}

func defaultClaimID(id string) string {
	if id != "" {
		return id
	}
	return "C-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func defaultResumePolicy(value string) string {
	if value == "" {
		return "hold"
	}
	return value
}

func (s *Service) currentResourceHash(root string, resource db.Resource) (string, error) {
	switch resource.Type {
	case "file":
		return repo.FileHash(root, resource.Path)
	case "directory":
		return repo.DirectoryHash(root, resource.Path)
	case "symbol":
		resolved, err := s.resolveSymbolResource(root, resource)
		if err != nil {
			return "", err
		}
		return repo.LineRangeHash(root, resolved.Path, resolved.StartLine, resolved.EndLine)
	default:
		return "", nil
	}
}

func (s *Service) resolveSymbolResource(root string, resource db.Resource) (db.Resource, error) {
	if resource.Path == "" || resource.Symbol == "" {
		return resource, nil
	}
	found, err := s.indexer.IndexPath(filepath.Join(root, filepath.FromSlash(resource.Path)))
	if err != nil {
		return resource, nil
	}
	for _, symbol := range found {
		if symbol.Name != resource.Symbol {
			continue
		}
		resource.StartLine = symbol.StartLine
		resource.EndLine = symbol.EndLine
		return resource, nil
	}
	return resource, nil
}

func defaultActorID(value string) string {
	if strings.TrimSpace(value) == "" {
		return "system"
	}
	return value
}

func effectiveTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 15 * time.Minute
	}
	return ttl
}

func normalizeResourceID(resourceID, resourceType, resourcePath, symbolName string) string {
	if resourceID != "" {
		return resourceID
	}
	cleaned := cleanResourcePath(resourceType, resourcePath)
	if resourceType == "symbol" && symbolName != "" {
		return symbols.ResourceID(cleaned, symbolName)
	}
	return resourceType + ":" + cleaned
}

func cleanResourcePath(resourceType, path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(filepath.ToSlash(path))
	if resourceType == "directory" && cleaned == "." {
		return ""
	}
	return cleaned
}

func resourcesOverlap(a, b db.Resource) bool {
	if a.ID != "" && a.ID == b.ID {
		return true
	}
	if a.Type == "symbol" && b.Type == "file" {
		return a.Path != "" && a.Path == b.Path
	}
	if a.Type == "file" && b.Type == "symbol" {
		return b.Path != "" && a.Path == b.Path
	}
	if a.Type == "symbol" && b.Type == "directory" {
		return pathWithinDir(a.Path, b.Path)
	}
	if a.Type == "directory" && b.Type == "symbol" {
		return pathWithinDir(b.Path, a.Path)
	}
	if a.Type == "symbol" && b.Type == "symbol" {
		return a.Path != "" && a.Path == b.Path && a.Symbol == b.Symbol
	}
	if a.Type == "file" && b.Type == "directory" {
		return pathWithinDir(a.Path, b.Path)
	}
	if a.Type == "directory" && b.Type == "file" {
		return pathWithinDir(b.Path, a.Path)
	}
	if a.Type == "directory" && b.Type == "directory" {
		return pathWithinDir(a.Path, b.Path) || pathWithinDir(b.Path, a.Path)
	}
	return false
}

func pathWithinDir(path, dir string) bool {
	path = cleanResourcePath("file", path)
	dir = cleanResourcePath("directory", dir)
	if path == "" || dir == "" {
		return dir == ""
	}
	if path == dir {
		return true
	}
	return strings.HasPrefix(path, dir+"/")
}

func claimTypesConflict(a, b string) bool {
	if a == "exclusive" || b == "exclusive" {
		return true
	}
	if a == "read" || b == "read" {
		return false
	}
	if a == "review" && b == "review" {
		return false
	}
	if a == "review" && b == "write" {
		return false
	}
	if a == "write" && b == "review" {
		return false
	}
	return a == "write" || b == "write"
}

func describeResource(resource db.Resource) string {
	if resource.Type == "symbol" && resource.Path != "" && resource.Symbol != "" {
		return resource.Path + "#" + resource.Symbol
	}
	if resource.Path != "" {
		return resource.Path
	}
	return resource.ID
}
