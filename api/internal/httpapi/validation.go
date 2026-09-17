package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"roundtable/internal/db"
	"roundtable/internal/proposals"
)

type validationInput struct {
	Stages []string `json:"stages"`
}
type validationResponse struct {
	RunID       string            `json:"run_id"`
	ProposalID  string            `json:"proposal_id"`
	Status      string            `json:"status"`
	StartedAt   string            `json:"started_at"`
	CompletedAt string            `json:"completed_at"`
	Stages      map[string]any    `json:"stages"`
	Commands    map[string]string `json:"commands"`
	Output      string            `json:"output,omitempty"`
}

func (s *Server) validateProposalAPI(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Validation reruns require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	proposal, err := s.readProposal(r.Context(), workspace.ID, r.PathValue("proposal_id"))
	if err != nil {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
		return
	}
	var input validationInput
	if r.Body != nil {
		_ = decodeJSON(r, &input)
	}
	if len(input.Stages) == 0 {
		input.Stages = []string{"patch_parse", "apply_dry_run", "policy", "stale_base"}
	}
	allowed := map[string]bool{"patch_parse": true, "apply_dry_run": true, "static_checks": true, "tests": true, "policy": true, "stale_base": true}
	for _, stage := range input.Stages {
		if !allowed[stage] {
			WriteProblem(w, r, 400, "invalid_validation_stage", "Invalid validation stage", stage)
			return
		}
	}
	started := time.Now().UTC()
	result := validationResponse{RunID: fmt.Sprintf("validation-%d", started.UnixNano()), ProposalID: proposal.ProposalID, Status: "passed", StartedAt: started.Format(time.RFC3339Nano), Stages: map[string]any{}, Commands: map[string]string{}}
	service := proposals.NewService(workspace.RootPath, s.config.Store, nil)
	validation, validateErr := service.PatchValidate(r.Context(), map[string]any{"proposal_id": proposal.ProposalID})
	if validateErr != nil {
		result.Status = "failed"
		result.Output = redactText(validateErr.Error())
	} else {
		for _, stage := range input.Stages {
			result.Stages[stage] = validationStageResult(stage, validation, workspace, proposal)
		}
	}
	if validateErr == nil {
		for _, stage := range input.Stages {
			if passed, ok := result.Stages[stage].(map[string]any)["passed"].(bool); ok && !passed {
				result.Status = "failed"
			}
		}
	}
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	summary, _ := json.Marshal(result.Stages)
	run := db.TestRun{ID: result.RunID, ProposalID: proposal.ProposalID, Command: "proposal validation stages", Status: result.Status, SummaryMD: redactText(string(summary))}
	if err := s.config.Store.UpsertTestRun(r.Context(), run); err != nil {
		WriteProblem(w, r, 500, "validation_persist_failed", "Unable to persist validation run", err.Error())
		return
	}
	writeJSON(w, 202, result)
}

func validationStageResult(stage string, validation map[string]any, workspace db.Workspace, proposal proposalResponse) map[string]any {
	out := map[string]any{"passed": false}
	switch stage {
	case "patch_parse":
		out["passed"] = validation["patch_parse_error"] == "" && validation["patch_exists"] == true
	case "apply_dry_run":
		out["passed"] = validation["temp_apply_error"] == ""
	case "policy":
		if policy, ok := validation["policy"].(map[string]any); ok {
			out["passed"] = policy["approvable"] == true || policy["summary"] == "no policy blockers"
		} else {
			out["passed"] = validation["claims_valid"] == true
		}
	case "stale_base":
		status, _ := inspectRepository(context.Background(), workspace)
		out["current_head"] = status.HeadSHA
		out["base_revision"] = proposal.BaseRevision
		out["passed"] = proposal.BaseRevision == "" || status.HeadSHA == proposal.BaseRevision
	case "static_checks":
		output, err := boundedCommand(workspace.RootPath, "git", "diff", "--check")
		out["passed"], out["output"] = err == nil, redactText(output)
	case "tests":
		output, err := boundedCommand(workspace.RootPath, "go", "test", "./...")
		out["passed"], out["output"] = err == nil, redactText(output)
	}
	return out
}
func boundedCommand(dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if len(output) > 8192 {
		output = output[:8192]
	}
	return string(output), err
}
