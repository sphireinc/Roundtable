package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"roundtable/internal/agentauth"
)

var implementedTools = map[string]bool{
	"agent.turn_request": true, "agent.turn_start": true, "agent.turn_complete": true,
	"table.get_state": true, "table.watch": true, "patch.validate": true, "patch.reject": true, "patch.apply": true,
	"proposal.create": true, "proposal.get": true, "proposal.list": true, "proposal.attach_patch": true,
	"proposal.request_review": true, "vote.cast": true, "vote.list": true, "decision.record": true,
	"human.request_approval": true, "human.approval_status": true, "security.review": true,
	"task.list": true, "task.get": true, "task.create": true, "task.update_status": true,
	"memory.query": true, "memory.record": true, "memory.summarize": true, "memory.mark_stale": true,
	"memory.task_history": true, "memory.oracle_report": true, "proposal.patch_read": true,
	"test.suggest": true, "test.run": true, "test.get_result": true, "test.evidence.submit": true, "repo.read_file": true, "repo.search": true,
	"resource.claim": true, "resource.release": true, "resource.claim_status": true, "resource.get": true,
	"resource.search": true, "repo.symbols": true,
}

func NewStandardServer(runtime *Runtime) *sdkmcp.Server {
	return newStandardServer(runtime, nil, "")
}

// NewAgentStandardServer builds a per-execution tool surface. Calls are checked
// again against the capability on every invocation, so this list is not the
// authorization boundary by itself.
func NewAgentStandardServer(runtime *Runtime, token string) (*sdkmcp.Server, error) {
	if runtime == nil || runtime.agentAuth == nil {
		return nil, fmt.Errorf("agent capability authority is unavailable")
	}
	identity, err := runtime.agentAuth.Resolve(token)
	if err != nil {
		return nil, err
	}
	return newStandardServer(runtime, runtime.CallAgent, token, identity.Role), nil
}

type agentToolCaller func(context.Context, string, string, map[string]any) (map[string]any, error)

func newStandardServer(runtime *Runtime, call agentToolCaller, token string, roles ...string) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "roundtable", Version: "0.1.0"}, nil)
	for _, registered := range DefaultRegistry().Tools() {
		if !implementedTools[registered.Name] {
			continue
		}
		if len(roles) > 0 && !agentauth.AllowedTool(roles[0], registered.Name) {
			continue
		}
		tool := &sdkmcp.Tool{Name: registered.Name, Description: registered.Description, InputSchema: registered.InputSchema}
		name := registered.Name
		resolvedSchema, schemaErr := resolveToolSchema(registered.InputSchema)
		server.AddTool(tool, func(ctx context.Context, request *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			if schemaErr != nil {
				return toolError(fmt.Errorf("registered tool schema is invalid")), nil
			}
			args := map[string]any{}
			if len(request.Params.Arguments) > 0 {
				if err := json.Unmarshal(request.Params.Arguments, &args); err != nil || args == nil {
					return toolError(fmt.Errorf("tool arguments must be a JSON object")), nil
				}
			}
			if err := resolvedSchema.Validate(args); err != nil {
				return toolError(fmt.Errorf("tool arguments do not match the declared input schema")), nil
			}
			var result map[string]any
			var err error
			if call != nil {
				result, err = call(ctx, token, name, args)
			} else {
				result, err = runtime.Call(ctx, name, args)
			}
			if err != nil {
				return toolError(err), nil
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				return toolError(fmt.Errorf("encode tool result: %w", err)), nil
			}
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(encoded)}}, StructuredContent: result}, nil
		})
	}
	return server
}

func resolveToolSchema(raw map[string]any) (*jsonschema.Resolved, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
}

func toolError(err error) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}}, IsError: true}
}
