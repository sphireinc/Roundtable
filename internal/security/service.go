package security

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
)

type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

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

func (s *Service) Review(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	resourceID := stringArg(args, "resource_id")
	if proposalID == "" && resourceID == "" {
		return nil, errors.New("proposal_id or resource_id is required")
	}

	reviewID := stringArgDefault(args, "review_id", generatedID("SR"))
	reviewerID := stringArgDefault(args, "reviewer_id", "security")
	status := stringArg(args, "status")
	summaryMD := stringArg(args, "summary_md")
	var findings []Finding
	taskID := stringArg(args, "task_id")

	if proposalID != "" {
		proposal, err := s.store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, err
		}
		if taskID == "" {
			taskID = proposal.TaskID
		}
		if status == "" {
			patchText, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(proposal.PatchPath)))
			if err != nil {
				return nil, err
			}
			patchInfo, err := repo.ParseTouchedFiles(string(patchText))
			if err != nil {
				return nil, err
			}
			autoStatus, autoSummary, autoFindings := s.autoReview(proposal.Risk, patchInfo.Files, string(patchText))
			status = autoStatus
			if summaryMD == "" {
				summaryMD = autoSummary
			}
			findings = autoFindings
		}
	}

	if status == "" {
		status = "approved"
	}
	if summaryMD == "" {
		summaryMD = "Security review recorded."
	}

	review := db.SecurityReview{
		ID:           reviewID,
		ProposalID:   proposalID,
		ResourceID:   resourceID,
		TaskID:       taskID,
		ReviewerID:   reviewerID,
		Status:       status,
		SummaryMD:    summaryMD,
		FindingsJSON: mustJSON(findings),
	}
	if err := s.store.UpsertSecurityReview(ctx, review); err != nil {
		return nil, err
	}
	saved, err := s.store.GetSecurityReview(ctx, review.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"review": saved, "findings": findings}, nil
}

func (s *Service) LatestForProposal(ctx context.Context, proposalID string) (db.SecurityReview, []db.SecurityReview, error) {
	reviews, err := s.store.ListSecurityReviews(ctx, proposalID)
	if err != nil {
		return db.SecurityReview{}, nil, err
	}
	if len(reviews) == 0 {
		return db.SecurityReview{}, reviews, nil
	}
	return reviews[len(reviews)-1], reviews, nil
}

func (s *Service) ReviewRequired(proposalRisk string, policyResult policy.Result) bool {
	if proposalRisk == "high" || proposalRisk == "critical" {
		return true
	}
	if len(policyResult.HighRiskPaths) > 0 {
		return true
	}
	for _, profile := range policyResult.Profiles {
		if profile == "dependency_change" || profile == "migration" {
			return true
		}
	}
	return false
}

func ReviewSatisfied(review db.SecurityReview, humanOverride bool) bool {
	switch review.Status {
	case "approved":
		return true
	case "veto":
		return humanOverride
	default:
		return false
	}
}

func (s *Service) autoReview(risk string, touchedFiles []string, patchText string) (string, string, []Finding) {
	findings := append(secretFindings(patchText), commandFindings(s.policy, patchText)...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		return findings[i].Code < findings[j].Code
	})
	for _, finding := range findings {
		if finding.Severity == "critical" {
			return "veto", "Security review vetoed the proposal due to critical findings.", findings
		}
	}
	if len(findings) > 0 {
		return "needs_changes", "Security review found issues that require changes before apply.", findings
	}
	if risk == "high" || risk == "critical" || len(touchedFiles) > 0 {
		return "approved", "Security review completed with no blocking findings.", findings
	}
	return "approved", "No security issues detected.", findings
}

func secretFindings(patchText string) []Finding {
	var findings []Finding
	lines := strings.Split(patchText, "\n")
	for _, raw := range lines {
		if !strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "+++") {
			continue
		}
		line := strings.ToLower(raw[1:])
		if !(strings.Contains(line, "log") || strings.Contains(line, "print") || strings.Contains(line, "console.")) {
			continue
		}
		if strings.Contains(line, "token") || strings.Contains(line, "secret") || strings.Contains(line, "password") || strings.Contains(line, "api_key") || strings.Contains(line, "private_key") {
			findings = append(findings, Finding{
				Code:     "secret_logging",
				Severity: "critical",
				Message:  "Added line appears to log or print sensitive credentials or tokens.",
			})
		}
	}
	return findings
}

func commandFindings(engine *policy.Engine, patchText string) []Finding {
	var findings []Finding
	rules := engine.Rules()
	lines := strings.Split(patchText, "\n")
	for _, raw := range lines {
		if !strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "+++") {
			continue
		}
		line := strings.ToLower(raw[1:])
		for _, command := range rules.DangerousCommands {
			if strings.Contains(line, strings.ToLower(command)) {
				findings = append(findings, Finding{
					Code:     "dangerous_command",
					Severity: "high",
					Message:  fmt.Sprintf("Added content references dangerous command pattern %q.", command),
				})
			}
		}
	}
	return findings
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func generatedID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func stringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func stringArgDefault(args map[string]any, key, fallback string) string {
	if value := stringArg(args, key); value != "" {
		return value
	}
	return fallback
}
