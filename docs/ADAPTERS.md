# CLI Agent Adapters

Roundtable uses existing CLI coding agents where possible.

## Adapter interface

```go
type Adapter interface {
    Name() string
    Capabilities(ctx context.Context) AdapterCapabilities
    Start(ctx context.Context, req StartAgentRequest) (*AgentSession, error)
    Resume(ctx context.Context, req ResumeAgentRequest) (*AgentSession, error)
    Stop(ctx context.Context, sessionID string) error
}
```

## Required capabilities

```go
type AdapterCapabilities struct {
    SupportsResume            bool
    SupportsMCP               bool
    SupportsReadOnlyWorkspace bool
    SupportsSessionCapture    bool
}
```

## Adapter config example

```yaml
adapters:
  codex:
    command: "codex"
    supports_resume: true
    resume_pattern: "codex resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true

  claude:
    command: "claude"
    supports_resume: true
    resume_pattern: "claude --resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true

  gemini:
    command: "gemini"
    supports_resume: true
    resume_pattern: "gemini resume {{external_session_id}}"
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true

  opencode:
    command: "opencode"
    supports_resume: false
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: false

  generic:
    command: "sh"
    supports_resume: false
    supports_mcp: false
    supports_readonly_workspace: true
    captures_session_id: false
```

## Launch contract

Each agent receives:

- role-specific prompt
- `AGENTS.ROUNDTABLE.md`
- `PROJECT.ROUNDTABLE.md`
- `POLICIES.ROUNDTABLE.md`
- `.roundtable/mcp/AGENT_MCP_MANIFEST.md`
- current task
- current table snapshot
- memory briefing
- read-only repository path

## Mutation contract

Agents may not directly mutate the repo.

Agents must submit unified diffs to `proposal.create`.

The adapter must either:

1. provide a read-only repo mount, or
2. sandbox the agent and export only a patch artifact, or
3. detect and reject unauthorized writes before anything reaches the authoritative repo.

## Session capture

Adapters should capture external session ids when visible in stdout/stderr, command output, metadata files, or adapter integration APIs.

Persist:

- agent id
- adapter name
- external session id
- external resume command
- run id
- task id
- role
- working directory
- MCP socket
- last heartbeat

## Resume briefing

On resume, Roundtable injects a canonical briefing. External session context is not authoritative.

Example:

```md
# Roundtable Resume Briefing

You are resuming as Implementer-1.

Run: RUN-20260706-0142
External session: 019edf2b-3879-7160-8625-c8e80205ccd5

Current task:
T-0007 Add refresh-token rotation

Your active claims:
- symbol:src/auth/session.go#ValidateRefreshToken
- file:tests/auth/session_test.go

Pending proposals:
- P-0012 from you: needs revision
  Reason: Security vetoed raw token logging.

Latest decisions:
- D-0008: Tokens must never be logged.
- D-0009: Login response shape must remain unchanged.

Required next action:
Revise P-0012 to remove token logging, update tests, and resubmit.
```
