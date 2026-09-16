# Route → Data Dependency Map

| Route | Primary queries | Primary mutations | Live event families |
|---|---|---|---|
| `/dashboard` | summary, proposals, claims, activity, memory, governance | start/pause run, approval actions | proposal.*, claim.*, vote.*, policy.*, transaction.*, session.*, memory.* |
| `/deliberations` | deliberations | create deliberation | deliberation.* |
| `/deliberations/:id` | deliberation detail/messages | human input, pause, resume, terminate | deliberation.*, proposal.*, claim.* |
| `/proposals` | proposal list | limited queue actions | proposal.*, vote.*, policy.*, transaction.* |
| `/proposals/:id` | proposal, patch, checks, votes | approve/reject/request changes, rerun checks | proposal.*, vote.*, policy.*, transaction.* |
| `/consensus` | active votes + analytics | human vote/override only if permitted | vote.*, consensus.* |
| `/claims` | claim list/contentions | create/release/force-release/extend/resolve | claim.* |
| `/sessions` | session list | pause/stop/terminate/resume | session.* |
| `/sessions/:id` | session detail/logs | lifecycle actions | session.*, claim.*, proposal.* |
| `/memory` | memory list/search | pin/edit/archive/merge/resynthesize | memory.* |
| `/policies` | policy list | create/clone/enable/disable | policy.* |
| `/policies/:id` | policy + revisions | validate/simulate/publish | policy.* |
| `/approvals` | attention/approval list | approve/reject/request changes | approval.*, proposal.*, transaction.* |
| `/transactions` | transaction list | cancel/retry allowed phases | transaction.* |
| `/transactions/:id` | transaction timeline | retry/cancel/compensating proposal | transaction.* |
| `/repository` | repository status/index | governed human branch action | repository.*, index.* |
| `/agents` | agent list | enable/disable/health check | agent.*, session.* |
| `/agents/:id` | adapter diagnostics | health test, test session | agent.*, session.* |
| `/logs` | logs/audit search | acknowledgement/export | log.*, audit.* |
| `/settings/*` | configuration schema/effective values | update section | config.* |
