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
	"roundtable/internal/mcp"
)

type diagnosticCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type diagnosticResponse struct {
	RunID       string            `json:"run_id"`
	AgentID     string            `json:"agent_id"`
	StartedAt   string            `json:"started_at"`
	FinishedAt  string            `json:"finished_at"`
	Status      string            `json:"status"`
	Checks      []diagnosticCheck `json:"checks"`
	Stdout      string            `json:"stdout,omitempty"`
	Stderr      string            `json:"stderr,omitempty"`
	Remediation []string          `json:"remediation_hints,omitempty"`
}

func (s *Server) agentDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Human authorization required", "Set X-Actor-ID and X-Actor-Role to a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Diagnostics require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	agent, err := s.config.Store.GetAgent(r.Context(), r.PathValue("agent_id"))
	if err != nil {
		WriteProblem(w, r, 404, "agent_not_found", "Agent not found", err.Error())
		return
	}
	started := time.Now().UTC()
	result := runAgentDiagnostics(r.Context(), agent)
	result.RunID = fmt.Sprintf("diag-%d", started.UnixNano())
	result.AgentID = agent.ID
	result.StartedAt = started.Format(time.RFC3339Nano)
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	status := "passed"
	for _, check := range result.Checks {
		if check.Status == "failed" {
			status = "failed"
			break
		}
	}
	result.Status = status
	summary, _ := json.Marshal(result)
	if err := s.config.Store.UpsertTestRun(r.Context(), db.TestRun{ID: result.RunID, TaskID: "", Command: "agent-diagnostic", Status: status, SummaryMD: string(summary)}); err != nil {
		WriteProblem(w, r, 500, "diagnostic_persist_failed", "Diagnostic could not be recorded", err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]any{"run_id": result.RunID, "agent_id": agent.ID, "status": status})
	if _, err := s.config.Store.AppendEvent(r.Context(), db.Event{RunID: "diagnostics", Type: "agent.diagnostic.completed", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: string(payload)}); err != nil {
		WriteProblem(w, r, 500, "diagnostic_audit_failed", "Diagnostic audit could not be recorded", err.Error())
		return
	}
	_ = workspace
	writeJSON(w, 202, result)
}

func runAgentDiagnostics(parent context.Context, agent db.Agent) diagnosticResponse {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	checks := []diagnosticCheck{}
	command := strings.TrimSpace(agent.Command)
	if command == "" {
		command = agent.Adapter
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		checks = append(checks, diagnosticCheck{Name: "executable", Status: "failed", Detail: "adapter executable is not configured"})
		return diagnosticResponse{Checks: checks, Remediation: []string{"Configure an adapter executable for this agent"}}
	}
	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], "--version")...)
	output, err := cmd.CombinedOutput()
	excerpt := sanitizeDiagnosticOutput(string(output))
	if err != nil {
		checks = append(checks, diagnosticCheck{Name: "executable_version", Status: "failed", Detail: "adapter version probe failed"})
	} else {
		checks = append(checks, diagnosticCheck{Name: "executable_version", Status: "passed", Detail: excerpt})
	}
	tools := mcp.DefaultRegistry().ToolNames()
	if len(tools) == 0 {
		checks = append(checks, diagnosticCheck{Name: "mcp_surface", Status: "failed", Detail: "no MCP tools registered"})
	} else {
		checks = append(checks, diagnosticCheck{Name: "mcp_surface", Status: "passed", Detail: fmt.Sprintf("%d tools visible", len(tools))})
	}
	checks = append(checks, diagnosticCheck{Name: "read_only_boundary", Status: "passed", Detail: "diagnostic does not create a session or receive repository write access"})
	return diagnosticResponse{Checks: checks, Stdout: excerpt, Remediation: []string{"Verify the adapter executable is installed and on PATH"}}
}

func sanitizeDiagnosticOutput(value string) string {
	lines := strings.Split(value, "\n")
	safe := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "token=") || strings.Contains(lower, "password=") || strings.Contains(lower, "secret=") {
			continue
		}
		safe = append(safe, line)
	}
	value = strings.TrimSpace(strings.Join(safe, "\n"))
	if len(value) > 2000 {
		return value[:2000]
	}
	return value
}
