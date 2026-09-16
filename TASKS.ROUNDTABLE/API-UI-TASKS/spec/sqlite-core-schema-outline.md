# SQLite Core Schema Outline

This is a logical outline, not a drop-in migration.

Core current-state tables:
- `workspaces`
- `agents`
- `sessions`
- `deliberations`
- `proposals`
- `claims`
- `consensus`
- `policies`
- `policy_active_versions`
- `approvals`
- `transactions`
- `memory_items`
- `notifications`
- `configuration`

Immutable/history tables:
- `deliberation_events`
- `proposal_patch_artifacts`
- `proposal_files`
- `claim_contentions`
- `votes`
- `consensus_snapshots`
- `policy_revisions`
- `policy_evaluations`
- `transaction_phases`
- `memory_revisions`
- `memory_provenance_edges`
- `domain_events`
- `audit_events`
- `event_outbox`
- `diagnostic_runs`

Important indexes:
- workspace + status + updated time on all queue pages;
- proposal state/vote/policy/approval/transaction state;
- claim canonical resource key + state;
- session agent/status/heartbeat;
- deliberation status/updated;
- transaction state/started;
- audit/event occurred_at + entity IDs;
- memory FTS virtual table + tag relation;
- policy scope/enabled;
- notification actionable/acknowledged.
