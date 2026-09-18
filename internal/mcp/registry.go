package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"roundtable/internal/config"
)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type Registry struct {
	tools []Tool
}

func DefaultRegistry() *Registry {
	return &Registry{
		tools: []Tool{
			tool("decision.record", "Record a proposal or task decision.", schema([]string{"decision", "rationale_md", "decided_by"}, optionalProperties(
				prop("proposal_id", "string"),
				prop("task_id", "string"),
			)...)),
			tool("human.approval_status", "Check the status of a requested human approval.", requiredSchema("approval_id")),
			tool("human.request_approval", "Request explicit human approval for a high-risk change or override.", schema([]string{"subject", "reason_md"}, optionalProperties(
				prop("approval_id", "string"),
				prop("proposal_id", "string"),
				prop("task_id", "string"),
				prop("requested_by", "string"),
				prop("status", "string"),
				prop("decision_md", "string"),
				prop("decided_by", "string"),
				prop("override_policy", "boolean"),
			)...)),
			tool("memory.mark_stale", "Mark a memory entry stale after it has been contradicted by a newer decision.", requiredSchema("memory_id", "reason_md")),
			tool("memory.query", "Query persistent project memory and prior decisions.", schema([]string{"query"}, optionalProperties(
				prop("scope", "string"),
				prop("kind", "string"),
				prop("limit", "integer"),
			)...)),
			tool("memory.record", "Record a durable memory entry.", schema([]string{"scope", "kind", "title", "body_md"}, optionalProperties(
				prop("source_event_id", "integer"),
				prop("importance", "integer"),
			)...)),
			tool("memory.summarize", "Summarize memory or decisions for a task, proposal, or project scope.", schema(nil, optionalProperties(
				prop("task_id", "string"),
				prop("proposal_id", "string"),
				prop("scope", "string"),
			)...)),
			tool("patch.apply", "Apply a validated patch through the orchestrator only.", requiredSchema("proposal_id")),
			tool("patch.reject", "Reject a patch proposal after validation or policy failure.", requiredSchema("proposal_id", "reason_md")),
			tool("patch.validate", "Validate a patch proposal against current repo and claim state.", requiredSchema("proposal_id")),
			tool("proposal.attach_patch", "Attach or replace a stored patch for an existing proposal.", requiredSchema("proposal_id", "patch")),
			tool("proposal.create", "Submit a patch proposal. The orchestrator validates claims and consensus before applying.", schema([]string{"task_id", "agent_id", "title", "summary_md", "affected_resources", "risk", "patch"}, optionalProperties(
				prop("expected_tests", "array", map[string]any{"items": map[string]any{"type": "string"}}),
				prop("rollback_notes", "string"),
			)...)),
			tool("proposal.get", "Fetch a proposal by id.", requiredSchema("proposal_id")),
			tool("proposal.list", "List proposals for the current run or task.", schema(nil, optionalProperties(
				prop("task_id", "string"),
				prop("status", "string"),
			)...)),
			tool("proposal.request_review", "Request review on a proposal from relevant roles.", requiredSchema("proposal_id")),
			tool("repo.dependency_context", "Return read-only dependency context for a path or manifest.", requiredSchema("path")),
			tool("repo.read_file", "Read a repository file through the read-only tool surface.", requiredSchema("path")),
			tool("repo.search", "Search repository content through the read-only tool surface.", schema([]string{"query"}, optionalProperties(
				prop("path", "string"),
			)...)),
			tool("repo.symbols", "List indexed symbols for a repository path.", schema(nil, optionalProperties(
				prop("path", "string"),
				prop("language", "string"),
			)...)),
			tool("resource.claim", "Claim a file, symbol, directory, command, endpoint, schema, or other resource before proposing changes.", schema([]string{"resource_type", "resource_id", "claim_type", "task_id"}, optionalProperties(
				prop("ttl_seconds", "integer"),
				prop("rationale", "string"),
			)...)),
			tool("resource.claim_status", "Inspect active, suspended, or expired claims for a resource or agent.", schema(nil, optionalProperties(
				prop("resource_id", "string"),
				prop("agent_id", "string"),
			)...)),
			tool("resource.get", "Fetch a resource by id.", requiredSchema("resource_id")),
			tool("resource.release", "Release a previously granted claim.", requiredSchema("claim_id")),
			tool("resource.search", "Search known resources and claims.", schema([]string{"query"}, optionalProperties(
				prop("resource_type", "string"),
			)...)),
			tool("security.review", "Run or record security review logic for a proposal or resource change.", schema(nil, optionalProperties(
				prop("review_id", "string"),
				prop("proposal_id", "string"),
				prop("resource_id", "string"),
				prop("task_id", "string"),
				prop("reviewer_id", "string"),
				prop("status", "string"),
				prop("summary_md", "string"),
			)...)),
			tool("table.get_state", "Return current Roundtable state including tasks, agents, claims, proposals, votes, decisions, blockers, and required human approvals.", emptySchema()),
			tool("table.watch", "Stream the live event feed from Roundtable.", emptySchema()),
			tool("task.create", "Create a task in the active run.", schema([]string{"title", "body_md"}, optionalProperties(
				prop("priority", "integer"),
				prop("risk", "string"),
				prop("assigned_agent_id", "string"),
			)...)),
			tool("task.get", "Fetch a task by id.", requiredSchema("task_id")),
			tool("task.list", "List tasks for the current run.", emptySchema()),
			tool("task.update_status", "Update a task status or assignment.", schema([]string{"task_id"}, optionalProperties(
				prop("status", "string"),
				prop("assigned_agent_id", "string"),
			)...)),
			tool("test.get_result", "Fetch a stored test result.", requiredSchema("test_run_id")),
			tool("test.run", "Run an allowlisted, read-only test command under orchestrator control; shell control characters and repository write primitives are rejected.", schema([]string{"command"}, optionalProperties(
				prop("proposal_id", "string"),
				prop("task_id", "string"),
			)...)),
			tool("test.suggest", "Suggest relevant test commands for a proposal or task.", schema(nil, optionalProperties(
				prop("proposal_id", "string"),
				prop("task_id", "string"),
			)...)),
			tool("vote.cast", "Cast a role-aware vote on a proposal.", schema([]string{"proposal_id", "agent_id", "vote", "reason_md"}, optionalProperties(
				prop("confidence", "number"),
			)...)),
			tool("vote.list", "List votes for a proposal.", requiredSchema("proposal_id")),
		},
	}
}

func (r *Registry) Tools() []Tool {
	out := append([]Tool(nil), r.tools...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) ToolNames() []string {
	tools := r.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func (r *Registry) ToolsSchemaJSON() (string, error) {
	return prettyJSON(map[string]any{"tools": r.Tools()})
}

func (r *Registry) Manifest(cfg config.Config) string {
	var b strings.Builder
	b.WriteString("# Roundtable MCP Manifest\n\n")
	b.WriteString("Generated by `roundtable init` / `roundtable mcp inspect --write`.\n\n")
	b.WriteString(fmt.Sprintf("Server: %s://%s\n", cfg.MCP.Transport, cfg.MCP.SocketPath))
	b.WriteString("Protocol: MCP-compatible local tool server\n\n")
	b.WriteString("## Mandatory Rules\n\n")
	b.WriteString("You are operating inside Roundtable.\n\n")
	b.WriteString("You may inspect the repository read-only.\n\n")
	b.WriteString("You must not directly modify repository files.\n\n")
	b.WriteString("All changes must be submitted as patch proposals through MCP.\n\n")
	b.WriteString("Before proposing a patch, you must claim every affected resource.\n\n")
	b.WriteString("If a resource is claimed by another agent, do not touch it unless your role and task assignment permit review of that resource.\n\n")
	b.WriteString("Roundtable state is authoritative. Your external CLI session memory is not authoritative.\n\n")
	b.WriteString("## Startup / Resume Checklist\n\n")
	b.WriteString("1. Call `table.get_state`.\n")
	b.WriteString("2. Call `memory.query` for task-relevant history.\n")
	b.WriteString("3. Call `task.get` for your assigned task.\n")
	b.WriteString("4. Call `resource.claim_status` for any claims you believe you own.\n")
	b.WriteString("5. Continue only after reconciling your memory with the table.\n\n")
	b.WriteString("## Common Tools\n\n")
	for _, name := range []string{
		"table.get_state",
		"task.list",
		"task.get",
		"resource.search",
		"resource.claim",
		"resource.release",
		"repo.read_file",
		"repo.search",
		"repo.symbols",
		"proposal.create",
		"vote.cast",
		"test.run",
		"memory.query",
		"security.review",
		"human.request_approval",
	} {
		b.WriteString("- `" + name + "`\n")
	}
	b.WriteString("\nThere is intentionally no direct repository write tool.\n")
	return b.String()
}

func ServerConfigJSON(cfg config.Config) (string, error) {
	return prettyJSON(map[string]string{
		"name":              "roundtable",
		"transport":         cfg.MCP.Transport,
		"socket_path":       cfg.MCP.SocketPath,
		"manifest_path":     ".roundtable/mcp/AGENT_MCP_MANIFEST.md",
		"tools_schema_path": ".roundtable/mcp/tools.schema.json",
	})
}

func prettyJSON(v any) (string, error) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func tool(name, description string, schema map[string]any) Tool {
	return Tool{Name: name, Description: description, InputSchema: schema}
}

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func requiredSchema(required ...any) map[string]any {
	names := make([]string, 0, len(required))
	for _, name := range required {
		names = append(names, name.(string))
	}
	return schema(names)
}

func schema(required []string, optional ...propertySpec) map[string]any {
	out := emptySchema()
	properties := map[string]any{}
	for _, name := range required {
		switch name {
		case "affected_resources":
			properties[name] = map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			}
		default:
			properties[name] = map[string]any{"type": "string"}
		}
	}
	for _, prop := range optional {
		properties[prop.Name] = prop.Schema
	}
	out["properties"] = properties
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

type propertySpec struct {
	Name   string
	Schema map[string]any
}

func prop(name, typ string, extras ...map[string]any) propertySpec {
	schema := map[string]any{"type": typ}
	for _, extra := range extras {
		for k, v := range extra {
			schema[k] = v
		}
	}
	return propertySpec{Name: name, Schema: schema}
}

func optionalProperties(props ...propertySpec) []propertySpec {
	return props
}
