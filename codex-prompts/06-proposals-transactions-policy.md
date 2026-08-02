# Prompt 06: Proposals, Transactions, Policy

Implement patch proposals, policy-weighted consensus, and transaction application.

Requirements:

- proposal.create stores patch on disk
- proposal_resources populated
- votes recorded
- policy engine evaluates thresholds
- high-risk changes require Human
- Security veto blocks
- transaction manager validates claims/base hashes
- applies patch via orchestrator only
- records before/after git hashes
- rollback patch generated if possible

Acceptance:

- proposal touching unclaimed resource is rejected
- accepted proposal becomes TX id
- Security veto blocks apply
- Human can override where policy permits
