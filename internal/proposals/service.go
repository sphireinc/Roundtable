package proposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"roundtable/internal/db"
	"roundtable/internal/policy"
	"roundtable/internal/repo"
	"roundtable/internal/security"
	"roundtable/internal/symbols"
)

type Service struct {
	root   string
	store  *db.Store
	policy *policy.Engine
}

func NewService(root string, store *db.Store, evaluator *policy.Engine) *Service {
	if evaluator == nil {
		evaluator = policy.DefaultEngine()
	}
	return &Service{root: root, store: store, policy: evaluator}
}

func (s *Service) PatchValidate(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	if proposalID == "" {
		return nil, errors.New("proposal_id is required")
	}
	proposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	proposalResources, err := s.store.ListProposalResources(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	affectedResources := make([]string, 0, len(proposalResources))
	for _, resource := range proposalResources {
		affectedResources = append(affectedResources, resource.ResourceID)
	}

	staleClaims, claimError := s.validateProposalClaims(ctx, proposal.AgentID, affectedResources)
	patchExists := true
	patchPath := filepath.Join(s.root, filepath.FromSlash(proposal.PatchPath))
	if _, err := os.Stat(patchPath); err != nil {
		patchExists = false
	}
	patchText := ""
	if patchExists {
		data, err := os.ReadFile(patchPath)
		if err == nil {
			patchText = string(data)
		}
	}
	patchInfo, patchParseErr := repo.PatchInfo{}, error(nil)
	resourceCoverageErr := error(nil)
	tempApplyErr := error(nil)
	if patchExists {
		patchInfo, patchParseErr = repo.ParseTouchedFiles(patchText)
		if patchParseErr == nil {
			resourceCoverageErr = s.validatePatchResources(ctx, proposalResources, patchInfo)
			if resourceCoverageErr == nil {
				tempRoot, err := os.MkdirTemp("", "roundtable-patch-*")
				if err == nil {
					defer os.RemoveAll(tempRoot)
					if err := repo.CopyWorkspace(s.root, tempRoot); err != nil {
						tempApplyErr = err
					} else {
						tempApplyErr = repo.ApplyPatch(tempRoot, patchText)
					}
				} else {
					tempApplyErr = err
				}
			}
		}
	}

	votes, err := s.store.ListVotes(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	policyResult := s.policy.EvaluateProposal(proposal.Risk, patchInfo.Files, votes)
	humanApprovals, err := s.store.ListHumanApprovals(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	approvedHuman, humanSatisfied := satisfyingHumanApproval(policyResult, humanApprovals)
	latestSecurityReview, securityReviews, err := s.latestSecurityReview(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	securityRequired := security.NewService(s.root, s.store, s.policy).ReviewRequired(proposal.Risk, policyResult)
	securitySatisfied := !securityRequired
	if securityRequired {
		securitySatisfied = security.ReviewSatisfied(latestSecurityReview, humanSatisfied && approvedHuman.OverridePolicy)
	}

	valid := claimError == nil && patchExists && patchParseErr == nil && resourceCoverageErr == nil && tempApplyErr == nil
	return map[string]any{
		"proposal":                  proposal,
		"proposal_resources":        proposalResources,
		"patch_exists":              patchExists,
		"patch_files":               patchInfo.Files,
		"patch_parse_error":         errString(patchParseErr),
		"claims_valid":              claimError == nil,
		"claim_error":               errString(claimError),
		"stale_claims":              staleClaims,
		"resource_coverage":         resourceCoverageErr == nil,
		"resource_error":            errString(resourceCoverageErr),
		"temp_apply_error":          errString(tempApplyErr),
		"policy":                    policyResult,
		"human_approvals":           humanApprovals,
		"human_approval":            approvedHuman,
		"human_approval_applied":    humanSatisfied,
		"security_reviews":          securityReviews,
		"security_review":           latestSecurityReview,
		"security_review_required":  securityRequired,
		"security_review_satisfied": securitySatisfied,
		"valid":                     valid,
	}, nil
}

func (s *Service) PatchReject(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	reason := stringArg(args, "reason_md")
	if proposalID == "" || reason == "" {
		return nil, errors.New("proposal_id and reason_md are required")
	}
	proposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	proposal.Status = "rejected"
	if err := s.store.UpsertProposal(ctx, proposal); err != nil {
		return nil, err
	}
	decision := db.Decision{
		ID:          stringArgDefault(args, "decision_id", generatedID("D")),
		ProposalID:  proposalID,
		TaskID:      proposal.TaskID,
		Decision:    "rejected",
		RationaleMD: reason,
		DecidedBy:   stringArgDefault(args, "decided_by", "orchestrator"),
	}
	if err := s.store.UpsertDecision(ctx, decision); err != nil {
		return nil, err
	}
	saved, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	recorded, err := s.store.GetDecision(ctx, decision.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proposal": saved, "decision": recorded}, nil
}

func (s *Service) PatchApply(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	if proposalID == "" {
		return nil, errors.New("proposal_id is required")
	}
	validation, err := s.PatchValidate(ctx, map[string]any{"proposal_id": proposalID})
	if err != nil {
		return nil, err
	}
	if valid, _ := validation["valid"].(bool); !valid {
		return nil, errors.New("proposal validation failed")
	}
	policyResult, ok := validation["policy"].(policy.Result)
	if !ok {
		return nil, errors.New("policy evaluation unavailable")
	}
	approvedHuman, _ := validation["human_approval"].(db.HumanApproval)
	humanSatisfied, _ := validation["human_approval_applied"].(bool)
	if !policyResult.Approvable && !humanSatisfied {
		return nil, fmt.Errorf("proposal not approvable: %s", policyResult.Summary)
	}
	securityRequired, _ := validation["security_review_required"].(bool)
	securitySatisfied, _ := validation["security_review_satisfied"].(bool)
	if securityRequired && !securitySatisfied {
		return nil, errors.New("proposal not approvable: security review required")
	}
	latestSecurityReview, _ := validation["security_review"].(db.SecurityReview)

	proposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	patchPath := filepath.Join(s.root, filepath.FromSlash(proposal.PatchPath))
	patchData, err := os.ReadFile(patchPath)
	if err != nil {
		return nil, err
	}
	patchText := string(patchData)
	patchInfo, err := repo.ParseTouchedFiles(patchText)
	if err != nil {
		return nil, err
	}
	rollbackEntries, err := repo.CreateRollbackArtifact(s.root, patchInfo.Files)
	if err != nil {
		return nil, err
	}
	tempRoot, err := os.MkdirTemp("", "roundtable-apply-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempRoot)
	if err := repo.CopyWorkspace(s.root, tempRoot); err != nil {
		return nil, err
	}
	if err := repo.ApplyPatch(tempRoot, patchText); err != nil {
		return nil, err
	}
	beforeHash, err := repo.RepoStateHash(s.root)
	if err != nil {
		return nil, err
	}
	if err := repo.ApplyPatch(s.root, patchText); err != nil {
		return nil, err
	}
	afterHash, err := repo.RepoStateHash(s.root)
	if err != nil {
		return nil, err
	}

	proposal.Status = "accepted"
	if err := s.store.UpsertProposal(ctx, proposal); err != nil {
		return nil, err
	}
	txID := stringArgDefault(args, "transaction_id", generatedID("TX"))
	rollbackPath := filepath.Join(s.root, ".roundtable/patches", txID+"-rollback.json")
	if err := repo.WriteRollbackArtifact(rollbackPath, rollbackEntries); err != nil {
		return nil, err
	}

	tx := db.Transaction{
		ID:                txID,
		ProposalID:        proposalID,
		RunID:             stringArgDefault(args, "run_id", "RUN-UNSPECIFIED"),
		BeforeGitHash:     beforeHash,
		AfterGitHash:      afterHash,
		Status:            "applied",
		AppliedBy:         stringArgDefault(args, "applied_by", "orchestrator"),
		AppliedAt:         time.Now().UTC().Format(time.RFC3339),
		RollbackPatchPath: filepath.ToSlash(filepath.Join(".roundtable/patches", txID+"-rollback.json")),
		MetadataJSON:      mustJSON(map[string]any{"mode": "applied", "policy_summary": policyResult.Summary, "patch_files": patchInfo.Files, "human_approval_id": approvedHuman.ID, "security_review_id": latestSecurityReview.ID}),
	}
	if err := s.store.UpsertTransaction(ctx, tx); err != nil {
		return nil, err
	}
	savedProposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	savedTx, err := s.store.GetTransaction(ctx, tx.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proposal": savedProposal, "transaction": savedTx, "policy": policyResult, "human_approval": approvedHuman, "security_review": latestSecurityReview}, nil
}

func (s *Service) ProposalCreate(ctx context.Context, args map[string]any) (map[string]any, error) {
	taskID := stringArg(args, "task_id")
	agentID := stringArg(args, "agent_id")
	title := stringArg(args, "title")
	summary := stringArg(args, "summary_md")
	risk := stringArgDefault(args, "risk", "normal")
	patch := stringArg(args, "patch")
	if taskID == "" || agentID == "" || title == "" || summary == "" || patch == "" {
		return nil, errors.New("task_id, agent_id, title, summary_md, and patch are required")
	}
	patchInfo, err := repo.ParseTouchedFiles(patch)
	if err != nil {
		return nil, err
	}

	affectedResources, err := stringSliceArg(args, "affected_resources")
	if err != nil {
		return nil, err
	}
	affectedResources = mergeAffectedResources(affectedResources, patchInfo.Files)
	if _, err := s.store.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	if err := s.validateClaimOwnership(ctx, agentID, affectedResources); err != nil {
		return nil, err
	}

	proposalID := stringArgDefault(args, "proposal_id", generatedID("P"))
	patchPath := filepath.Join(s.root, ".roundtable/patches", proposalID+".diff")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(patchPath, []byte(patch), 0o644); err != nil {
		return nil, err
	}

	proposal := db.Proposal{
		ID:        proposalID,
		TaskID:    taskID,
		AgentID:   agentID,
		Title:     title,
		SummaryMD: summary,
		PatchPath: filepath.ToSlash(filepath.Join(".roundtable/patches", proposalID+".diff")),
		Status:    stringArgDefault(args, "status", "pending"),
		Risk:      risk,
	}
	if err := s.store.UpsertProposal(ctx, proposal); err != nil {
		return nil, err
	}
	if err := s.ensureResourceRecords(ctx, affectedResources); err != nil {
		return nil, err
	}
	if err := s.store.ReplaceProposalResources(ctx, proposalID, affectedResources); err != nil {
		return nil, err
	}
	saved, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	resources, err := s.store.ListProposalResources(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proposal": saved, "proposal_resources": resources}, nil
}

func (s *Service) ProposalGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	if proposalID == "" {
		return nil, errors.New("proposal_id is required")
	}
	proposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	resources, err := s.store.ListProposalResources(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proposal": proposal, "proposal_resources": resources}, nil
}

func (s *Service) ProposalAttachPatch(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	patch := stringArg(args, "patch")
	if proposalID == "" || patch == "" {
		return nil, errors.New("proposal_id and patch are required")
	}
	proposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	patchInfo, err := repo.ParseTouchedFiles(patch)
	if err != nil {
		return nil, err
	}
	affectedResources, err := stringSliceArg(args, "affected_resources")
	if err != nil {
		return nil, err
	}
	affectedResources = mergeAffectedResources(affectedResources, patchInfo.Files)
	if err := s.validateClaimOwnership(ctx, proposal.AgentID, affectedResources); err != nil {
		return nil, err
	}

	patchPath := proposal.PatchPath
	if patchPath == "" {
		patchPath = filepath.ToSlash(filepath.Join(".roundtable/patches", proposalID+".diff"))
		proposal.PatchPath = patchPath
	}
	absPatchPath := filepath.Join(s.root, filepath.FromSlash(patchPath))
	if err := os.MkdirAll(filepath.Dir(absPatchPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(absPatchPath, []byte(patch), 0o644); err != nil {
		return nil, err
	}
	if err := s.store.UpsertProposal(ctx, proposal); err != nil {
		return nil, err
	}
	if err := s.ensureResourceRecords(ctx, affectedResources); err != nil {
		return nil, err
	}
	if err := s.store.ReplaceProposalResources(ctx, proposalID, affectedResources); err != nil {
		return nil, err
	}
	saved, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	resources, err := s.store.ListProposalResources(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proposal": saved, "proposal_resources": resources}, nil
}

func (s *Service) ProposalRequestReview(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	if proposalID == "" {
		return nil, errors.New("proposal_id is required")
	}
	proposal, err := s.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	proposalResources, err := s.store.ListProposalResources(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	patchFiles, err := s.proposalPatchFiles(proposal)
	if err != nil {
		return nil, err
	}
	votes, err := s.store.ListVotes(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	policyResult := s.policy.EvaluateProposal(proposal.Risk, patchFiles, votes)
	targetRoles := s.reviewRolesForProposal(proposal.Risk, policyResult)
	reviewers, err := s.reviewersForRoles(ctx, targetRoles, proposal.AgentID)
	if err != nil {
		return nil, err
	}
	if proposal.Status == "" || proposal.Status == "pending" {
		proposal.Status = "in_review"
		if err := s.store.UpsertProposal(ctx, proposal); err != nil {
			return nil, err
		}
		proposal, err = s.store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, err
		}
	}
	securityRequired := security.NewService(s.root, s.store, s.policy).ReviewRequired(proposal.Risk, policyResult)
	return map[string]any{
		"proposal":                 proposal,
		"proposal_resources":       proposalResources,
		"review_roles":             targetRoles,
		"reviewers":                reviewers,
		"policy":                   policyResult,
		"human_approval_required":  policyResult.NeedsHuman,
		"security_review_required": securityRequired,
	}, nil
}

func (s *Service) ProposalList(ctx context.Context, args map[string]any) (map[string]any, error) {
	taskID := stringArg(args, "task_id")
	status := stringArg(args, "status")
	proposals, err := s.store.ListProposals(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]db.Proposal, 0, len(proposals))
	for _, proposal := range proposals {
		if taskID != "" && proposal.TaskID != taskID {
			continue
		}
		if status != "" && proposal.Status != status {
			continue
		}
		filtered = append(filtered, proposal)
	}
	return map[string]any{"proposals": filtered}, nil
}

func (s *Service) VoteCast(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	agentID := stringArg(args, "agent_id")
	voteValue := stringArg(args, "vote")
	reason := stringArg(args, "reason_md")
	if proposalID == "" || agentID == "" || voteValue == "" || reason == "" {
		return nil, errors.New("proposal_id, agent_id, vote, and reason_md are required")
	}
	if _, err := s.store.GetProposal(ctx, proposalID); err != nil {
		return nil, err
	}
	vote := db.Vote{
		ID:         stringArgDefault(args, "vote_id", generatedID("V")),
		ProposalID: proposalID,
		AgentID:    agentID,
		Vote:       voteValue,
		Confidence: floatArgDefault(args, "confidence", 0),
		ReasonMD:   reason,
	}
	if err := s.store.UpsertVote(ctx, vote); err != nil {
		return nil, err
	}
	saved, err := s.store.GetVote(ctx, vote.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"vote": saved}, nil
}

func (s *Service) VoteList(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	if proposalID == "" {
		return nil, errors.New("proposal_id is required")
	}
	votes, err := s.store.ListVotes(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"votes": votes}, nil
}

func (s *Service) DecisionRecord(ctx context.Context, args map[string]any) (map[string]any, error) {
	decisionValue := stringArg(args, "decision")
	rationale := stringArg(args, "rationale_md")
	decidedBy := stringArg(args, "decided_by")
	if decisionValue == "" || rationale == "" || decidedBy == "" {
		return nil, errors.New("decision, rationale_md, and decided_by are required")
	}
	decision := db.Decision{
		ID:          stringArgDefault(args, "decision_id", generatedID("D")),
		ProposalID:  stringArg(args, "proposal_id"),
		TaskID:      stringArg(args, "task_id"),
		Decision:    decisionValue,
		RationaleMD: rationale,
		DecidedBy:   decidedBy,
	}
	if err := s.store.UpsertDecision(ctx, decision); err != nil {
		return nil, err
	}
	saved, err := s.store.GetDecision(ctx, decision.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"decision": saved}, nil
}

func (s *Service) validateProposalClaims(ctx context.Context, agentID string, affectedResources []string) ([]db.Claim, error) {
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[string]db.Claim)
	var stale []db.Claim
	for _, claim := range claims {
		if claim.AgentID != agentID || claim.Status != "active" {
			continue
		}
		if claim.ClaimType == "read" || claim.ClaimType == "review" {
			continue
		}
		if claim.BaseHash != "" {
			resource, err := s.store.GetResource(ctx, claim.ResourceID)
			if err != nil {
				return nil, err
			}
			currentHash, err := s.currentResourceHash(resource)
			if err != nil {
				return nil, err
			}
			resource.CurrentHash = currentHash
			if err := s.store.UpsertResource(ctx, resource); err != nil {
				return nil, err
			}
			if currentHash != "" && currentHash != claim.BaseHash {
				claim.Status = "suspended"
				if err := s.store.UpsertClaim(ctx, claim); err != nil {
					return nil, err
				}
				stale = append(stale, claim)
				continue
			}
		}
		active[claim.ResourceID] = claim
	}
	if len(stale) > 0 {
		return stale, fmt.Errorf("stale claim base hash: %s", stale[0].ResourceID)
	}
	for _, resourceID := range affectedResources {
		if _, ok := active[resourceID]; !ok {
			return nil, fmt.Errorf("unclaimed affected resource: %s", resourceID)
		}
	}
	return nil, nil
}

func (s *Service) validateClaimOwnership(ctx context.Context, agentID string, affectedResources []string) error {
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return err
	}
	active := make(map[string]db.Claim)
	for _, claim := range claims {
		if claim.AgentID != agentID || claim.Status != "active" {
			continue
		}
		if claim.ClaimType == "read" || claim.ClaimType == "review" {
			continue
		}
		active[claim.ResourceID] = claim
	}
	for _, resourceID := range affectedResources {
		if _, ok := active[resourceID]; !ok {
			return fmt.Errorf("unclaimed affected resource: %s", resourceID)
		}
	}
	return nil
}

func (s *Service) validatePatchResources(ctx context.Context, proposalResources []db.ProposalResource, patchInfo repo.PatchInfo) error {
	fileResources := map[string]struct{}{}
	directoryResources := map[string]struct{}{}
	symbolResources := map[string][]db.Resource{}
	for _, proposalResource := range proposalResources {
		resource, err := s.store.GetResource(ctx, proposalResource.ResourceID)
		if err != nil {
			return err
		}
		switch resource.Type {
		case "file":
			fileResources[filepath.ToSlash(resource.Path)] = struct{}{}
		case "directory":
			directoryResources[filepath.ToSlash(resource.Path)] = struct{}{}
		case "symbol":
			resolved, err := s.resolveSymbolResource(resource)
			if err != nil {
				return err
			}
			symbolResources[filepath.ToSlash(resolved.Path)] = append(symbolResources[filepath.ToSlash(resolved.Path)], resolved)
		}
	}
	fileChanges := patchInfo.FileChanges
	if len(fileChanges) == 0 {
		for _, path := range patchInfo.Files {
			fileChanges = append(fileChanges, repo.FileChange{OldPath: path, NewPath: path})
		}
	}
	for _, change := range fileChanges {
		paths := touchedPaths(change)
		if pathCoveredByFilesOrDirectories(paths, fileResources, directoryResources) {
			continue
		}
		if change.IsNew || change.IsDelete || change.IsRename {
			return fmt.Errorf("patch operation requires file or directory claim coverage: %s", describeFileChange(change))
		}
		symbolCovered := false
		for _, path := range paths {
			if path == "" {
				continue
			}
			symbolsForFile := symbolResources[path]
			if len(symbolsForFile) == 0 {
				continue
			}
			changesForPath := patchInfo.Changes[path]
			if len(changesForPath) == 0 {
				continue
			}
			allCovered := true
			for _, lineChange := range changesForPath {
				if !changeCoveredBySymbols(lineChange, symbolsForFile) {
					allCovered = false
					return fmt.Errorf("patch touches lines outside claimed symbols in %s at %d-%d", path, lineChange.StartLine, lineChange.EndLine)
				}
			}
			if allCovered {
				symbolCovered = true
				break
			}
		}
		if !symbolCovered {
			return fmt.Errorf("patch touches file not covered by proposal resources: %s", describeFileChange(change))
		}
	}
	return nil
}

func (s *Service) ensureResourceRecords(ctx context.Context, resourceIDs []string) error {
	for _, resourceID := range resourceIDs {
		if _, err := s.store.GetResource(ctx, resourceID); err == nil {
			continue
		}
		resourceType, resourcePath, symbolName := parseResourceID(resourceID)
		resource := db.Resource{
			ID:       resourceID,
			Type:     resourceType,
			Path:     resourcePath,
			Symbol:   symbolName,
			Language: symbols.DetectLanguage(resourcePath),
		}
		if err := s.store.UpsertResource(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func mergeAffectedResources(explicit []string, patchFiles []string) []string {
	seen := map[string]struct{}{}
	fileCovered := map[string]struct{}{}
	directoryCovered := map[string]struct{}{}
	merged := make([]string, 0, len(explicit)+len(patchFiles))
	for _, resourceID := range explicit {
		if resourceID == "" {
			continue
		}
		if _, ok := seen[resourceID]; ok {
			continue
		}
		seen[resourceID] = struct{}{}
		resourceType, resourcePath, _ := parseResourceID(resourceID)
		if resourceType == "file" || resourceType == "symbol" {
			fileCovered[filepath.ToSlash(resourcePath)] = struct{}{}
		}
		if resourceType == "directory" {
			directoryCovered[filepath.ToSlash(resourcePath)] = struct{}{}
		}
		merged = append(merged, resourceID)
	}
	for _, file := range patchFiles {
		slashFile := filepath.ToSlash(file)
		if _, ok := fileCovered[slashFile]; ok {
			continue
		}
		if coveredByDirectory(slashFile, directoryCovered) {
			continue
		}
		resourceID := "file:" + slashFile
		if _, ok := seen[resourceID]; ok {
			continue
		}
		seen[resourceID] = struct{}{}
		merged = append(merged, resourceID)
	}
	return merged
}

func parseResourceID(resourceID string) (resourceType, resourcePath, symbolName string) {
	switch {
	case strings.HasPrefix(resourceID, "symbol:"):
		rest := strings.TrimPrefix(resourceID, "symbol:")
		parts := strings.SplitN(rest, "#", 2)
		resourceType = "symbol"
		resourcePath = filepath.ToSlash(parts[0])
		if len(parts) == 2 {
			symbolName = parts[1]
		}
		return
	case strings.Contains(resourceID, ":"):
		parts := strings.SplitN(resourceID, ":", 2)
		return parts[0], filepath.ToSlash(parts[1]), ""
	default:
		return "file", filepath.ToSlash(resourceID), ""
	}
}

func generatedID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func stringArg(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case string:
		return v
	default:
		return ""
	}
}

func stringArgDefault(args map[string]any, key, fallback string) string {
	if value := stringArg(args, key); value != "" {
		return value
	}
	return fallback
}

func floatArgDefault(args map[string]any, key string, fallback float64) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		n, err := v.Float64()
		if err == nil {
			return n
		}
	}
	return fallback
}

func stringSliceArg(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}
	switch values := raw.(type) {
	case []string:
		return values, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain only strings", key)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
}

func (s *Service) resolveSymbolResource(resource db.Resource) (db.Resource, error) {
	if resource.Type != "symbol" || resource.Path == "" || resource.Symbol == "" {
		return resource, nil
	}
	if resource.StartLine > 0 && resource.EndLine >= resource.StartLine {
		return resource, nil
	}
	indexer := symbols.NewIndexer()
	fullPath := filepath.Join(s.root, filepath.FromSlash(resource.Path))
	found, err := indexer.IndexPath(fullPath)
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

func coveredByDirectory(path string, directories map[string]struct{}) bool {
	for dir := range directories {
		if dir == "" || path == dir || strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

func changeCoveredBySymbols(change repo.LineRange, resources []db.Resource) bool {
	for _, resource := range resources {
		if resource.StartLine <= 0 || resource.EndLine < resource.StartLine {
			continue
		}
		if rangesOverlap(change.StartLine, change.EndLine, resource.StartLine, resource.EndLine) {
			return true
		}
	}
	return false
}

func pathCoveredByFilesOrDirectories(paths []string, files map[string]struct{}, directories map[string]struct{}) bool {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := files[path]; ok {
			return true
		}
		if coveredByDirectory(path, directories) {
			return true
		}
	}
	return false
}

func touchedPaths(change repo.FileChange) []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, 2)
	for _, path := range []string{filepath.ToSlash(change.OldPath), filepath.ToSlash(change.NewPath)} {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func describeFileChange(change repo.FileChange) string {
	switch {
	case change.IsRename:
		return fmt.Sprintf("rename %s -> %s", change.OldPath, change.NewPath)
	case change.IsNew:
		return fmt.Sprintf("create %s", change.NewPath)
	case change.IsDelete:
		return fmt.Sprintf("delete %s", change.OldPath)
	case change.NewPath != "" && change.NewPath != change.OldPath:
		return fmt.Sprintf("%s -> %s", change.OldPath, change.NewPath)
	case change.OldPath != "":
		return change.OldPath
	default:
		return change.NewPath
	}
}

func rangesOverlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart <= bEnd && bStart <= aEnd
}

func (s *Service) proposalPatchFiles(proposal db.Proposal) ([]string, error) {
	patchPath := filepath.Join(s.root, filepath.FromSlash(proposal.PatchPath))
	patchData, err := os.ReadFile(patchPath)
	if err != nil {
		return nil, err
	}
	patchInfo, err := repo.ParseTouchedFiles(string(patchData))
	if err != nil {
		return nil, err
	}
	return patchInfo.Files, nil
}

func (s *Service) reviewRolesForProposal(risk string, policyResult policy.Result) []string {
	roles := map[string]struct{}{
		"Reviewer": {},
	}
	for _, profile := range policyResult.Profiles {
		rule, ok := s.policy.Rules().Consensus[profile]
		if !ok {
			continue
		}
		for _, role := range rule.RequiredRoles {
			if strings.TrimSpace(role) != "" {
				roles[role] = struct{}{}
			}
		}
	}
	if risk == "high" || risk == "critical" || len(policyResult.HighRiskPaths) > 0 {
		roles["Security"] = struct{}{}
	}
	if _, ok := roles["Security"]; ok {
		if rule, ok := s.policy.Rules().Consensus["security"]; ok {
			for _, role := range rule.RequiredRoles {
				if strings.TrimSpace(role) != "" {
					roles[role] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(roles))
	for role := range roles {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}

func (s *Service) reviewersForRoles(ctx context.Context, roles []string, excludeAgentID string) ([]map[string]any, error) {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	var reviewers []map[string]any
	for _, role := range roles {
		matched := false
		for _, agent := range agents {
			if !agent.IsEnabled || agent.ID == excludeAgentID || !strings.EqualFold(agent.Role, role) {
				continue
			}
			reviewers = append(reviewers, map[string]any{
				"role":     role,
				"agent_id": agent.ID,
				"adapter":  agent.Adapter,
				"status":   agent.Status,
			})
			matched = true
		}
		if !matched {
			reviewers = append(reviewers, map[string]any{
				"role": role,
			})
		}
	}
	return reviewers, nil
}

func satisfyingHumanApproval(policyResult policy.Result, approvals []db.HumanApproval) (db.HumanApproval, bool) {
	if !policyResult.NeedsHuman {
		return db.HumanApproval{}, false
	}
	if policyResult.Blockers > policyResult.Vetoes {
		return db.HumanApproval{}, false
	}
	for i := len(approvals) - 1; i >= 0; i-- {
		approval := approvals[i]
		if approval.Status != "approved" {
			continue
		}
		if policyResult.Vetoes > 0 && !approval.OverridePolicy {
			continue
		}
		return approval, true
	}
	return db.HumanApproval{}, false
}

func (s *Service) latestSecurityReview(ctx context.Context, proposalID string) (db.SecurityReview, []db.SecurityReview, error) {
	reviews, err := s.store.ListSecurityReviews(ctx, proposalID)
	if err != nil {
		return db.SecurityReview{}, nil, err
	}
	if len(reviews) == 0 {
		return db.SecurityReview{}, reviews, nil
	}
	return reviews[len(reviews)-1], reviews, nil
}

func (s *Service) currentResourceHash(resource db.Resource) (string, error) {
	switch resource.Type {
	case "file":
		return repo.FileHash(s.root, resource.Path)
	case "directory":
		return repo.DirectoryHash(s.root, resource.Path)
	case "symbol":
		resolved, err := s.resolveSymbolResource(resource)
		if err != nil {
			return "", err
		}
		return repo.LineRangeHash(s.root, resolved.Path, resolved.StartLine, resolved.EndLine)
	default:
		return "", nil
	}
}
