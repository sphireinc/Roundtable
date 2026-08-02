# 0004 Patch Proposals and Transactions

## Goal

Implement proposal submission, patch validation, consensus gates, and orchestrator-applied transactions.

## Scope

- proposals table
- proposal_resources table
- votes table
- decisions table
- transactions table
- patch storage on disk
- patch validation against claims
- git base hash validation
- apply through orchestrator only

## Acceptance criteria

- Agents can submit unified diffs as proposals.
- Proposal resources must be covered by active claims.
- Proposals cannot touch unclaimed resources.
- Accepted patches are applied by the orchestrator and assigned transaction ids.
- Transaction records include before/after git hashes.

## Risk

High
