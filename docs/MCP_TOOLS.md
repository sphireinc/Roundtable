# MCP Tool Surface

Roundtable exposes a local MCP-compatible server. Agents should use MCP tools rather than directly modifying files.

## Generated files

`roundtable init` and `roundtable mcp inspect --write` create:

```text
.roundtable/mcp/AGENT_MCP_MANIFEST.md
.roundtable/mcp/tools.schema.json
.roundtable/mcp/server.json
```

These files are generated from the actual MCP tool registry and should not be hand-edited.

## Tool categories

### Table

- `table.get_state`
- `table.watch`

### Tasks

- `task.list`
- `task.get`
- `task.create`
- `task.update_status`

### Resources and claims

- `resource.search`
- `resource.get`
- `resource.claim`
- `resource.release`
- `resource.claim_status`

### Repository read-only tools

- `repo.read_file`
- `repo.search`
- `repo.symbols`
- `repo.dependency_context`

There is intentionally no `repo.write_file` tool.

### Proposals and patches

- `proposal.create`
- `proposal.get`
- `proposal.list`
- `proposal.attach_patch`
- `proposal.request_review`
- `patch.validate`
- `patch.apply`
- `patch.reject`

`patch.apply` is only callable by the orchestrator or Chair under policy. Ordinary agents should not call it.

### Voting and decisions

- `vote.cast`
- `vote.list`
- `decision.record`

### Testing

- `test.suggest`
- `test.run`
- `test.get_result`

### Memory

- `memory.query`
- `memory.record`
- `memory.summarize`
- `memory.mark_stale`

### Security and human approval

- `security.review`
- `human.request_approval`
- `human.approval_status`

## Required agent startup behavior

On startup or resume, every agent must call:

1. `table.get_state`
2. `memory.query` for task-relevant context
3. `task.get` for its assigned task
4. `resource.claim_status` for any active claims it believes it owns

## Example claim

```json
{
  "resource_type": "symbol",
  "resource_id": "src/auth/session.go#ValidateRefreshToken",
  "claim_type": "write",
  "task_id": "T-0007",
  "ttl_seconds": 900,
  "rationale": "Need to update refresh token validation behavior."
}
```

## Example proposal

```json
{
  "task_id": "T-0007",
  "title": "Add refresh token replay prevention",
  "summary_md": "Adds hash-at-rest refresh token rotation and tests replay rejection.",
  "affected_resources": [
    "src/auth/session.go#ValidateRefreshToken",
    "tests/auth/session_test.go"
  ],
  "risk": "high",
  "patch": "diff --git ...",
  "expected_tests": ["go test ./..."]
}
```
