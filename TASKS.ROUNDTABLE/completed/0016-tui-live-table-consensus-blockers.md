# 0016: TUI Live Table Consensus and Blockers

## Goal

Show useful proposal ballot and blocked-task detail in the Live Table without presenting a UI tally as an authoritative governance decision.

## Acceptance criteria

- Display a raw tally for the newest proposal, grouping approve/approve-with-notes, revise/reject, veto, and other values.
- Display one blocked task's ID/title and indicate any additional blocked tasks.
- Keep policy evaluation and transaction authority in existing governance workflows; the display is informational only.
- Load only an aggregate for the newest proposal, not the full vote/rationale history.
- Index proposal votes and verify the aggregate query uses the index.
- Document tally meanings and limitations.
- Tests, race detector, build, vet, independent review, and whitespace checks pass.

## Completion evidence

- TUI regression coverage verifies blocked task detail and compact raw vote tally, including an unrecognized vote.
- Database tests verify proposal-scoped aggregate counts and `EXPLAIN QUERY PLAN` use of `idx_votes_proposal_id`.
- Migration version 6 creates the index for existing databases.
- Full-suite tests, race detection, vet, build, and diff checks passed; independent review findings were addressed.

## Parent task status

This completes only the Live Table consensus/blocker detail criterion from `0005-tui.md`. Selection-driven Inspector views remain open.
