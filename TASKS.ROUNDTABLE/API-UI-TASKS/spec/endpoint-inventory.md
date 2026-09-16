# Endpoint Inventory

The exact names can be adapted to the existing router, but the capability set should remain intact.

## Node
- `GET /api/v1/health`
- `GET /api/v1/status`

## Workspaces
- `GET /api/v1/workspaces`
- `POST /api/v1/workspaces`
- `GET /api/v1/workspaces/{workspace_id}`
- `PATCH /api/v1/workspaces/{workspace_id}`
- `POST /api/v1/workspaces/{workspace_id}/detach`

## Dashboard / run
- `GET /api/v1/workspaces/{workspace_id}/dashboard`
- `GET /api/v1/workspaces/{workspace_id}/run`
- `POST /api/v1/workspaces/{workspace_id}/run/start`
- `POST /api/v1/workspaces/{workspace_id}/run/pause-intake`
- `POST /api/v1/workspaces/{workspace_id}/run/resume-intake`
- `POST /api/v1/workspaces/{workspace_id}/run/stop`

## Repository
- `GET /api/v1/workspaces/{workspace_id}/repository`
- `POST /api/v1/workspaces/{workspace_id}/repository/branch-switch/preflight`
- `POST /api/v1/workspaces/{workspace_id}/repository/branch-switch`
- `GET /api/v1/workspaces/{workspace_id}/repository/entities`
- `GET /api/v1/workspaces/{workspace_id}/repository/entities/{entity_id}`

## Agents
- `GET /api/v1/workspaces/{workspace_id}/agents`
- `GET /api/v1/workspaces/{workspace_id}/agents/{agent_id}`
- `POST /api/v1/workspaces/{workspace_id}/agents/{agent_id}/enable`
- `POST /api/v1/workspaces/{workspace_id}/agents/{agent_id}/disable`
- `POST /api/v1/workspaces/{workspace_id}/agents/{agent_id}/diagnostics`

## Sessions
- `GET /api/v1/workspaces/{workspace_id}/sessions`
- `GET /api/v1/workspaces/{workspace_id}/sessions/{session_id}`
- `POST /api/v1/workspaces/{workspace_id}/sessions`
- `POST /api/v1/workspaces/{workspace_id}/sessions/{session_id}/pause`
- `POST /api/v1/workspaces/{workspace_id}/sessions/{session_id}/resume`
- `POST /api/v1/workspaces/{workspace_id}/sessions/{session_id}/stop`
- `POST /api/v1/workspaces/{workspace_id}/sessions/{session_id}/terminate`
- `GET /api/v1/workspaces/{workspace_id}/sessions/{session_id}/events`

## Deliberations
- `GET /api/v1/workspaces/{workspace_id}/deliberations`
- `POST /api/v1/workspaces/{workspace_id}/deliberations`
- `GET /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}`
- `POST /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}/start`
- `POST /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}/pause`
- `POST /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}/resume`
- `POST /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}/terminate`
- `POST /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}/human-input`
- `GET /api/v1/workspaces/{workspace_id}/deliberations/{deliberation_id}/events`

## Proposals / patches
- `GET /api/v1/workspaces/{workspace_id}/proposals`
- `GET /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}`
- `GET /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/files`
- `GET /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/diff`
- `GET /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/checks`
- `POST /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/checks/rerun`
- `POST /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/request-changes`
- `POST /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/cancel`

## Claims
- `GET /api/v1/workspaces/{workspace_id}/claims`
- `POST /api/v1/workspaces/{workspace_id}/claims`
- `GET /api/v1/workspaces/{workspace_id}/claims/{claim_id}`
- `POST /api/v1/workspaces/{workspace_id}/claims/{claim_id}/release`
- `POST /api/v1/workspaces/{workspace_id}/claims/{claim_id}/force-release`
- `POST /api/v1/workspaces/{workspace_id}/claims/{claim_id}/extend`
- `GET /api/v1/workspaces/{workspace_id}/claim-contentions`
- `POST /api/v1/workspaces/{workspace_id}/claim-contentions/{contention_id}/resolve`

## Consensus
- `GET /api/v1/workspaces/{workspace_id}/consensus/active`
- `GET /api/v1/workspaces/{workspace_id}/consensus/analytics`
- `GET /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/votes`
- `GET /api/v1/workspaces/{workspace_id}/proposals/{proposal_id}/consensus`

## Policies
- `GET /api/v1/workspaces/{workspace_id}/policies`
- `POST /api/v1/workspaces/{workspace_id}/policies`
- `GET /api/v1/workspaces/{workspace_id}/policies/{policy_id}`
- `PATCH /api/v1/workspaces/{workspace_id}/policies/{policy_id}/draft`
- `POST /api/v1/workspaces/{workspace_id}/policies/{policy_id}/validate`
- `POST /api/v1/workspaces/{workspace_id}/policies/{policy_id}/simulate`
- `POST /api/v1/workspaces/{workspace_id}/policies/{policy_id}/publish`
- `POST /api/v1/workspaces/{workspace_id}/policies/{policy_id}/enable`
- `POST /api/v1/workspaces/{workspace_id}/policies/{policy_id}/disable`
- `GET /api/v1/workspaces/{workspace_id}/policies/{policy_id}/revisions`

## Approvals
- `GET /api/v1/workspaces/{workspace_id}/approvals`
- `GET /api/v1/workspaces/{workspace_id}/approvals/{approval_id}`
- `POST /api/v1/workspaces/{workspace_id}/approvals/{approval_id}/approve`
- `POST /api/v1/workspaces/{workspace_id}/approvals/{approval_id}/reject`
- `POST /api/v1/workspaces/{workspace_id}/approvals/{approval_id}/request-changes`
- `POST /api/v1/workspaces/{workspace_id}/approvals/{approval_id}/defer`

## Transactions
- `GET /api/v1/workspaces/{workspace_id}/transactions`
- `GET /api/v1/workspaces/{workspace_id}/transactions/{transaction_id}`
- `POST /api/v1/workspaces/{workspace_id}/transactions/{transaction_id}/cancel`
- `POST /api/v1/workspaces/{workspace_id}/transactions/{transaction_id}/retry`
- `POST /api/v1/workspaces/{workspace_id}/transactions/{transaction_id}/compensating-proposal`

## Memory
- `GET /api/v1/workspaces/{workspace_id}/memory`
- `POST /api/v1/workspaces/{workspace_id}/memory`
- `GET /api/v1/workspaces/{workspace_id}/memory/{memory_id}`
- `PATCH /api/v1/workspaces/{workspace_id}/memory/{memory_id}`
- `POST /api/v1/workspaces/{workspace_id}/memory/{memory_id}/pin`
- `POST /api/v1/workspaces/{workspace_id}/memory/{memory_id}/unpin`
- `POST /api/v1/workspaces/{workspace_id}/memory/{memory_id}/archive`
- `POST /api/v1/workspaces/{workspace_id}/memory/{memory_id}/restore`
- `POST /api/v1/workspaces/{workspace_id}/memory/merge`
- `POST /api/v1/workspaces/{workspace_id}/memory/{memory_id}/resynthesize`
- `GET /api/v1/workspaces/{workspace_id}/memory/{memory_id}/revisions`

## Activity / notifications / logs
- `GET /api/v1/workspaces/{workspace_id}/activity`
- `GET /api/v1/workspaces/{workspace_id}/notifications`
- `POST /api/v1/workspaces/{workspace_id}/notifications/{notification_id}/ack`
- `GET /api/v1/workspaces/{workspace_id}/logs`
- `GET /api/v1/workspaces/{workspace_id}/audit`
- `POST /api/v1/workspaces/{workspace_id}/logs/export`

## Settings
- `GET /api/v1/settings/schema`
- `GET /api/v1/settings`
- `PATCH /api/v1/settings/{section}`
- `POST /api/v1/settings/{section}/impact`

## Live events
- `GET/WS /api/v1/workspaces/{workspace_id}/events/ws`
- `GET /api/v1/workspaces/{workspace_id}/events/resync`
