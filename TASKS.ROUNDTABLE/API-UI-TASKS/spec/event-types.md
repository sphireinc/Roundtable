# Live Event Type Families

Events are versioned by schema, not by embedding version suffixes in every event name.

- `workspace.updated`
- `repository.status_changed`
- `repository.degraded`
- `run.state_changed`
- `agent.health_changed`
- `agent.config_changed`
- `session.created`
- `session.state_changed`
- `session.heartbeat`
- `session.resumed`
- `deliberation.created`
- `deliberation.state_changed`
- `deliberation.message_added`
- `proposal.created`
- `proposal.updated`
- `proposal.state_changed`
- `proposal.check_completed`
- `claim.acquired`
- `claim.contested`
- `claim.released`
- `claim.expired`
- `vote.cast`
- `consensus.updated`
- `consensus.finalized`
- `policy.evaluation_completed`
- `policy.changed`
- `approval.required`
- `approval.decided`
- `transaction.created`
- `transaction.phase_changed`
- `transaction.committed`
- `transaction.failed`
- `memory.created`
- `memory.updated`
- `memory.archived`
- `notification.created`
- `notification.acknowledged`
- `config.changed`
- `audit.appended`

High-frequency heartbeats may be coalesced before delivery to browsers.
