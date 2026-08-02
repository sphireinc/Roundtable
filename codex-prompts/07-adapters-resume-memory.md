# Prompt 07: Adapters, Resume, Memory Oracle

Implement CLI adapters, durable session resume, and Memory Oracle.

Requirements:

- adapter interface
- codex/claude/gemini/opencode/generic adapters
- session id capture framework
- external_resume_command persistence
- `roundtable sessions`
- `roundtable resume`
- resume briefing generation
- memory query/record/summarize/mark_stale
- Memory Oracle role behavior

Acceptance:

- sessions persist
- resume reconstructs run state
- stale claims become suspended
- memory can be queried from CLI/MCP
