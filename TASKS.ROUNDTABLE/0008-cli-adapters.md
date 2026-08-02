# 0008 CLI Agent Adapters

## Goal

Implement controlled CLI adapters for existing coding agents.

## Scope

- adapter interface
- codex adapter
- claude adapter
- gemini adapter
- opencode adapter
- generic adapter
- read-only workspace handling
- prompt composition
- MCP manifest injection
- session id capture where possible

## Acceptance criteria

- Adapters can launch agent processes with role-specific context.
- Agents receive generated MCP manifest and task context.
- Adapters store session metadata.
- Agents are restricted to read-only repo access and patch-only mutation contract.

## Risk

High
