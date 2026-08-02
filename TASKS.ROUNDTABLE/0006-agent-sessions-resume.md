# 0006 Agent Sessions and Resume

## Goal

Persist external CLI agent sessions and support `roundtable resume`.

## Scope

- agent_sessions table
- agent_session_events table
- adapter_capabilities table
- run_snapshots table
- external_session_id
- external_resume_command
- resume briefing generation
- claim reconciliation on resume

## Acceptance criteria

- Roundtable stores adapter-specific session ids.
- Roundtable can print resume commands.
- `roundtable resume` rehydrates state and restarts/resumes agents where supported.
- Agents receive a canonical resume briefing.
- Stale claims are suspended and reconciled.

## Risk

Normal
