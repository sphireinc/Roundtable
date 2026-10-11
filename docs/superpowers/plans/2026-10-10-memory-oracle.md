# Memory Oracle Implementation Plan

**Goal:** Make the configured Memory Oracle usable as a proposal-bound agent that can inspect task history, retain task-scoped summaries, and submit source-cited advisory contradiction warnings.

**Architecture:** Reuse the existing proposal review lifecycle, container supervisor, role-bound MCP capabilities, SQLite event log, and memory store. The Oracle may read only its assigned task/proposal and that task's durable memory; its warning is an auditable event and never changes votes, proposal status, policy, or repository state.

**Tech Stack:** Go, SQLite, MCP, existing container execution supervisor.

**Spec:** `TASKS.ROUNDTABLE/0007-memory-oracle.md`

## Constraints

- No provider host execution fallback; use the existing mandatory container supervisor.
- No Oracle capability to vote, approve, reject, veto, apply patches, or change governance.
- Bind task, proposal, run, role, and agent identity server-side.
- Warnings must cite active task/project memory records; they remain advisory.
- Preserve disposable provider workers; do not enable operational provider resume.

## Tasks

1. Add failing role-capability tests for Memory Oracle task history, scoped memory query/record, and advisory warning reporting; reject voting/admin tools.
2. Implement normalized Memory Oracle role authorization and proposal-bound supervisor eligibility using persisted agent configuration.
3. Add bounded task-history retrieval (task, proposals, decisions, recent task events, active task/project memory) and role-bound MCP tools for reading and reporting.
4. Add the Oracle to proposal review requests without making it a policy approval role; persist warnings as immutable events with validated memory references.
5. Add lifecycle, egress/auth, scope, malformed-report, and non-mutation regression tests; document limits and run full tests/race/build/vet/review.
