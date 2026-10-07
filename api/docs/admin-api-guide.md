# Roundtable Admin API guide

This guide is generated from [`api/openapi.yaml`](../openapi.yaml). Regenerate it with `ruby api/scripts/generate-admin-api-guide.rb` after changing the contract. All routes use `/api/v1` except the Prometheus `/metrics` endpoint.

## Shared contract

- Ordinary handler responses are JSON; structured handler errors use RFC 7807 `application/problem+json` with `request_id` and `code`. Audit exports also support CSV/NDJSON, metrics use text, and WebSocket upgrades use their own protocol. Router, middleware, and transport failures need not share the handler error shape.
- Request middleware assigns `X-Request-ID` to ordinary HTTP responses; pass `X-Correlation-ID` for request-log correlation. Hijacked WebSocket handshakes and downstream persisted events have separate propagation boundaries; do not assume every protocol message or event carries these IDs.
- Mutations commonly require `X-Actor-ID` and, for human-only actions, `X-Actor-Role`. When bearer tokens are configured, requests must also use the matching human or agent token; header-only mode is for loopback development, not remote authentication.
- Mutations that can be retried declare `Idempotency-Key`; body-bearing mutations replay the original response, while empty-body lifecycle transitions are re-evaluated.
- Pagination is endpoint-specific: offset, keyset, and decimal-ID cursors coexist, and some collections are unpaginated. Consult [Pagination](pagination.md), preserve returned cursors, and check documented implementation mismatches before assuming a universal protocol.
- Permission and idempotency columns summarize OpenAPI metadata; check each handler for runtime authorization details. Many governance writes group state and audit/outbox records in a SQLite transaction, but this is not guaranteed for every route or filesystem mutation.
- Browser state-changing requests with an Origin header must match an allowed CORS origin. Default origins and loopback binding are described in `api/README.md`.

## Endpoint inventory

| Method | Path | Operation | Permission / boundary | Idempotency | Request schema | Response schemas |
|---|---|---|---|---|---|---|
| `GET` | `/api/v1/health` | `getHealth` | view | not required | none | 200: [`HealthResponse`](schema-reference.md#schema-healthresponse) |
| `GET` | `/api/v1/status` | `getStatus` | view | not required | none | 200: [`NodeHealthResponse`](schema-reference.md#schema-nodehealthresponse) |
| `GET` | `/api/v1/security/capabilities` | `getSecurityCapabilities` | view | not required | none | 200: [`SecurityCapabilities`](schema-reference.md#schema-securitycapabilities) |
| `GET` | `/metrics` | `getMetrics` | Prometheus scrape | not required | none | 200: string |
| `GET` | `/api/v1/maintenance` | `getMaintenanceStatus` | view | not required | none | 200: [`MaintenanceStatus`](schema-reference.md#schema-maintenancestatus) |
| `POST` | `/api/v1/maintenance/integrity-check` | `runIntegrityCheck` | role-gated human | required | none | none declared |
| `POST` | `/api/v1/maintenance/checkpoint` | `checkpointDatabase` | role-gated human | required | none | none declared |
| `POST` | `/api/v1/maintenance/backup` | `createDatabaseBackup` | role-gated human | required | none | none declared |
| `POST` | `/api/v1/maintenance/retention` | `runRetentionCleanup` | role-gated human | required | [`RetentionInput`](schema-reference.md#schema-retentioninput) | none declared |
| `GET` | `/api/v1/workspaces/{id}/health` | `getWorkspaceHealth` | view | not required | none | 200: [`WorkspaceHealthResponse`](schema-reference.md#schema-workspacehealthresponse) |
| `GET` | `/api/v1/workspaces` | `listWorkspaces` | view | not required | none | 200: inline object {`items`: array of `Workspace`!, `next_cursor`: string or null} |
| `POST` | `/api/v1/workspaces` | `createWorkspace` | orchestrator or human | not required | [`WorkspaceInput`](schema-reference.md#schema-workspaceinput) | 201: [`Workspace`](schema-reference.md#schema-workspace) |
| `GET` | `/api/v1/workspaces/{id}` | `getWorkspace` | view | not required | none | 200: [`Workspace`](schema-reference.md#schema-workspace) |
| `PATCH` | `/api/v1/workspaces/{id}` | `updateWorkspace` | orchestrator or human | not required | [`WorkspacePatch`](schema-reference.md#schema-workspacepatch) | 200: [`Workspace`](schema-reference.md#schema-workspace) |
| `DELETE` | `/api/v1/workspaces/{id}` | `detachWorkspace` | orchestrator or human | not required | none | 200: inline object {`workspace`: `Workspace`!, `impact`: `WorkspaceImpact`!} |
| `GET` | `/api/v1/workspaces/{id}/repository` | `getRepositoryStatus` | view | not required | none | 200: [`RepositoryStatus`](schema-reference.md#schema-repositorystatus) |
| `POST` | `/api/v1/workspaces/{id}/repository/branch-switch/preflight` | `preflightRepositoryBranchSwitch` | role-gated human | not required | [`BranchSwitchInput`](schema-reference.md#schema-branchswitchinput) | 200: [`BranchSwitchPreflight`](schema-reference.md#schema-branchswitchpreflight) |
| `POST` | `/api/v1/workspaces/{id}/repository/branch-switch` | `switchRepositoryBranch` | role-gated human | required | [`BranchSwitchInput`](schema-reference.md#schema-branchswitchinput) | 200: [`BranchSwitchResponse`](schema-reference.md#schema-branchswitchresponse) |
| `GET` | `/api/v1/workspaces/{id}/repository/entities` | `listRepositoryEntities` | view | not required | none | 200: [`RepositoryEntities`](schema-reference.md#schema-repositoryentities) |
| `GET` | `/api/v1/workspaces/{id}/repository/entities/{entity_id}` | `getRepositoryEntity` | view | not required | none | 200: [`RepositoryEntity`](schema-reference.md#schema-repositoryentity) |
| `GET` | `/api/v1/workspaces/{id}/agents` | `listAgents` | view | not required | none | 200: inline object {`items`: array of `Agent`!, `next_cursor`: string or null} |
| `GET` | `/api/v1/workspaces/{id}/agents/{agent_id}` | `getAgent` | view | not required | none | 200: [`Agent`](schema-reference.md#schema-agent) |
| `POST` | `/api/v1/workspaces/{id}/agents/{agent_id}/enable` | `enableAgent` | role-gated human | required | none | 200: [`Agent`](schema-reference.md#schema-agent) |
| `POST` | `/api/v1/workspaces/{id}/agents/{agent_id}/disable` | `disableAgent` | role-gated human | required | none | 200: [`Agent`](schema-reference.md#schema-agent) |
| `POST` | `/api/v1/workspaces/{id}/agents/{agent_id}/diagnostics` | `runAgentDiagnostics` | role-gated human | required | none | 202: [`DiagnosticRun`](schema-reference.md#schema-diagnosticrun) |
| `GET` | `/api/v1/workspaces/{id}/sessions` | `listSessions` | view | not required | none | 200: [`Sessions`](schema-reference.md#schema-sessions) |
| `POST` | `/api/v1/workspaces/{id}/sessions` | `createSession` | role-gated human | required | [`SessionInput`](schema-reference.md#schema-sessioninput) | 201: [`Session`](schema-reference.md#schema-session) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}` | `getSession` | view | not required | none | 200: [`Session`](schema-reference.md#schema-session) |
| `POST` | `/api/v1/workspaces/{id}/sessions/{session_id}/pause` | `pauseSession` | role-gated human | not required | none | 200: [`Session`](schema-reference.md#schema-session) |
| `POST` | `/api/v1/workspaces/{id}/sessions/{session_id}/resume` | `resumeSession` | role-gated human | required | none | 200: [`Session`](schema-reference.md#schema-session) |
| `POST` | `/api/v1/workspaces/{id}/sessions/{session_id}/stop` | `stopSession` | role-gated human | not required | none | 200: [`Session`](schema-reference.md#schema-session) |
| `POST` | `/api/v1/workspaces/{id}/sessions/{session_id}/terminate` | `terminateSession` | role-gated human | required | [`SessionTerminationInput`](schema-reference.md#schema-sessionterminationinput) | 200: [`Session`](schema-reference.md#schema-session) |
| `POST` | `/api/v1/workspaces/{id}/sessions/{session_id}/heartbeat` | `heartbeatSession` | orchestrator or human | not required | none | 200: [`Session`](schema-reference.md#schema-session) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}/logs` | `listSessionLogs` | view | not required | none | 200: [`SessionLogs`](schema-reference.md#schema-sessionlogs) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}/tool-calls` | `listSessionToolCalls` | view | not required | none | 200: [`SessionToolCalls`](schema-reference.md#schema-sessiontoolcalls) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}/claims` | `listSessionClaims` | view | not required | none | 200: [`PaginatedClaims`](schema-reference.md#schema-paginatedclaims) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}/proposals` | `listSessionProposals` | view | not required | none | 200: [`PaginatedSessionProposals`](schema-reference.md#schema-paginatedsessionproposals) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}/metrics` | `getSessionMetrics` | view | not required | none | 200: [`SessionMetrics`](schema-reference.md#schema-sessionmetrics) |
| `GET` | `/api/v1/workspaces/{id}/sessions/{session_id}/environment` | `getSessionEnvironment` | view | not required | none | 200: [`SessionEnvironment`](schema-reference.md#schema-sessionenvironment) |
| `GET` | `/api/v1/workspaces/{id}/deliberations` | `listDeliberations` | view | not required | none | 200: [`Deliberations`](schema-reference.md#schema-deliberations) |
| `POST` | `/api/v1/workspaces/{id}/deliberations` | `createDeliberation` | role-gated human | required | [`DeliberationInput`](schema-reference.md#schema-deliberationinput) | 201: [`Deliberation`](schema-reference.md#schema-deliberation) |
| `GET` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}` | `getDeliberation` | view | not required | none | 200: [`Deliberation`](schema-reference.md#schema-deliberation) |
| `POST` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/start` | `startDeliberation` | role-gated human | required | none | 200: [`Deliberation`](schema-reference.md#schema-deliberation) |
| `POST` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/pause` | `pauseDeliberation` | role-gated human | required | none | 200: [`Deliberation`](schema-reference.md#schema-deliberation) |
| `POST` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/resume` | `resumeDeliberation` | role-gated human | required | none | 200: [`Deliberation`](schema-reference.md#schema-deliberation) |
| `POST` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/terminate` | `terminateDeliberation` | role-gated human | required | none | 200: [`Deliberation`](schema-reference.md#schema-deliberation) |
| `POST` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/messages` | `addDeliberationMessage` | role-gated human | not required | [`DeliberationMessageInput`](schema-reference.md#schema-deliberationmessageinput) | none declared |
| `GET` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/transcript` | `listDeliberationTranscript` | view | not required | none | 200: [`TranscriptEntries`](schema-reference.md#schema-transcriptentries) |
| `GET` | `/api/v1/workspaces/{id}/deliberations/{deliberation_id}/transcript/{entry_id}` | `getDeliberationTranscriptEntry` | view | not required | none | 200: [`TranscriptEntry`](schema-reference.md#schema-transcriptentry) |
| `GET` | `/api/v1/workspaces/{id}/proposals` | `listProposals` | view | not required | none | 200: [`Proposals`](schema-reference.md#schema-proposals) |
| `POST` | `/api/v1/workspaces/{id}/proposals/internal` | `createInternalProposal` | orchestrator or human | required | [`ProposalInput`](schema-reference.md#schema-proposalinput) | 201: [`Proposal`](schema-reference.md#schema-proposal) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}` | `getProposal` | view | not required | none | 200: [`Proposal`](schema-reference.md#schema-proposal) |
| `POST` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/{action}` | `transitionProposal` | role-gated human | required | none | 200: [`Proposal`](schema-reference.md#schema-proposal) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/patch` | `getPatchMetadata` | view | not required | none | 200: [`PatchMetadata`](schema-reference.md#schema-patchmetadata) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/patch/files` | `listPatchFiles` | view | not required | none | 200: [`PatchFiles`](schema-reference.md#schema-patchfiles) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/patch/diff` | `listPatchDiffChunks` | view | not required | none | 200: [`PatchDiff`](schema-reference.md#schema-patchdiff) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/patch/symbol-impact` | `listPatchSymbolImpact` | view | not required | none | 200: [`PatchSymbols`](schema-reference.md#schema-patchsymbols) |
| `POST` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/validation` | `rerunProposalValidation` | role-gated human | required | [`ValidationInput`](schema-reference.md#schema-validationinput) | 202: [`ValidationRun`](schema-reference.md#schema-validationrun) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/votes` | `listProposalVotes` | view | not required | none | 200: [`Votes`](schema-reference.md#schema-votes) |
| `POST` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/votes` | `castProposalVote` | orchestrator or human | required | [`VoteInput`](schema-reference.md#schema-voteinput) | 201: [`VoteResult`](schema-reference.md#schema-voteresult) |
| `GET` | `/api/v1/workspaces/{id}/proposals/{proposal_id}/consensus` | `getProposalConsensus` | view | not required | none | 200: [`Consensus`](schema-reference.md#schema-consensus) |
| `GET` | `/api/v1/workspaces/{id}/analytics/consensus` | `getConsensusAnalytics` | view | not required | none | 200: [`ConsensusAnalytics`](schema-reference.md#schema-consensusanalytics) |
| `GET` | `/api/v1/workspaces/{id}/policies` | `listPolicies` | view | not required | none | 200: [`Policies`](schema-reference.md#schema-policies) |
| `POST` | `/api/v1/workspaces/{id}/policies` | `createPolicy` | role-gated human | required | [`PolicyInput`](schema-reference.md#schema-policyinput) | 201: [`Policy`](schema-reference.md#schema-policy) |
| `GET` | `/api/v1/workspaces/{id}/policies/{policy_id}` | `getPolicy` | view | not required | none | 200: [`Policy`](schema-reference.md#schema-policy) |
| `GET` | `/api/v1/workspaces/{id}/policies/{policy_id}/revisions` | `listPolicyRevisions` | view | not required | none | 200: [`PolicyRevisions`](schema-reference.md#schema-policyrevisions) |
| `GET` | `/api/v1/workspaces/{id}/policies/{policy_id}/schema` | `getPolicySchema` | view | not required | none | 200: [`PolicySchemaResponse`](schema-reference.md#schema-policyschemaresponse) |
| `POST` | `/api/v1/workspaces/{id}/policies/{policy_id}/validate` | `validatePolicy` | orchestrator or human | not required | none | 200: [`PolicyValidation`](schema-reference.md#schema-policyvalidation) |
| `POST` | `/api/v1/workspaces/{id}/policies/{policy_id}/simulate` | `simulatePolicy` | orchestrator or human | not required | [`PolicyEvaluationInput`](schema-reference.md#schema-policyevaluationinput) | 200: [`PolicyEvaluation`](schema-reference.md#schema-policyevaluation) |
| `POST` | `/api/v1/workspaces/{id}/policies/{policy_id}/evaluate` | `evaluatePolicy` | role-gated human | required | [`PolicyEvaluationInput`](schema-reference.md#schema-policyevaluationinput) | 201: [`PolicyEvaluation`](schema-reference.md#schema-policyevaluation) |
| `POST` | `/api/v1/workspaces/{id}/policies/{policy_id}/{action}` | `transitionPolicy` | role-gated human | required | none | 200: [`PolicyTransition`](schema-reference.md#schema-policytransition) |
| `GET` | `/api/v1/workspaces/{id}/approvals` | `listHumanApprovals` | view | not required | none | 200: [`Approvals`](schema-reference.md#schema-approvals) |
| `POST` | `/api/v1/workspaces/{id}/approvals` | `requestHumanApproval` | orchestrator or human | required | [`ApprovalInput`](schema-reference.md#schema-approvalinput) | 201: [`Approval`](schema-reference.md#schema-approval) |
| `GET` | `/api/v1/workspaces/{id}/approvals/{approval_id}` | `getHumanApproval` | view | not required | none | 200: [`Approval`](schema-reference.md#schema-approval) |
| `POST` | `/api/v1/workspaces/{id}/approvals/{approval_id}/{action}` | `decideHumanApproval` | role-gated human | required | [`ApprovalDecisionInput`](schema-reference.md#schema-approvaldecisioninput) | 200: [`Approval`](schema-reference.md#schema-approval) |
| `GET` | `/api/v1/workspaces/{id}/transactions` | `listTransactions` | view | not required | none | 200: [`Transactions`](schema-reference.md#schema-transactions) |
| `GET` | `/api/v1/workspaces/{id}/transactions/{transaction_id}` | `getTransaction` | view | not required | none | 200: [`Transaction`](schema-reference.md#schema-transaction) |
| `GET` | `/api/v1/workspaces/{id}/transactions/{transaction_id}/phases` | `listTransactionPhases` | view | not required | none | 200: [`TransactionPhases`](schema-reference.md#schema-transactionphases) |
| `GET` | `/api/v1/workspaces/{id}/transactions/{transaction_id}/recovery` | `getTransactionRecovery` | view | not required | none | 200: [`TransactionRecovery`](schema-reference.md#schema-transactionrecovery) |
| `GET` | `/api/v1/workspaces/{id}/runs/status` | `getRunStatus` | view | not required | none | 200: [`RunControl`](schema-reference.md#schema-runcontrol) |
| `POST` | `/api/v1/workspaces/{id}/runs/{action}` | `transitionRun` | role-gated human | required | [`RunStartInput`](schema-reference.md#schema-runstartinput) | 200: [`RunTransition`](schema-reference.md#schema-runtransition) |
| `GET` | `/api/v1/workspaces/{id}/dashboard/summary` | `getDashboardSummary` | view | not required | none | 200: [`DashboardSummary`](schema-reference.md#schema-dashboardsummary) |
| `GET` | `/api/v1/workspaces/{id}/activity` | `listActivityFeed` | view | not required | none | 200: [`ActivityFeed`](schema-reference.md#schema-activityfeed) |
| `GET` | `/api/v1/workspaces/{id}/events/ws` | `connectWorkspaceEvents` | view | not required | none | none declared |
| `GET` | `/api/v1/workspaces/{id}/events/snapshot` | `getWorkspaceEventSnapshot` | view | not required | none | 200: [`EventSnapshot`](schema-reference.md#schema-eventsnapshot) |
| `GET` | `/api/v1/workspaces/{id}/memories` | `listMemories` | view | not required | none | 200: [`Memories`](schema-reference.md#schema-memories) |
| `POST` | `/api/v1/workspaces/{id}/memories` | `createMemoryNote` | orchestrator or human | required | [`MemoryInput`](schema-reference.md#schema-memoryinput) | 201: [`Memory`](schema-reference.md#schema-memory) |
| `GET` | `/api/v1/workspaces/{id}/memories/{memory_id}` | `getMemory` | view | not required | none | 200: [`Memory`](schema-reference.md#schema-memory) |
| `POST` | `/api/v1/workspaces/{id}/memories/{memory_id}/revisions` | `createMemoryRevision` | orchestrator or human | required | [`MemoryRevisionInput`](schema-reference.md#schema-memoryrevisioninput) | 200: [`Memory`](schema-reference.md#schema-memory) |
| `GET` | `/api/v1/workspaces/{id}/memories/{memory_id}/revisions` | `listMemoryRevisions` | view | required | none | 200: [`MemoryRevisions`](schema-reference.md#schema-memoryrevisions) |
| `GET` | `/api/v1/workspaces/{id}/memories/{memory_id}/diff` | `getMemoryRevisionDiff` | view | not required | none | 200: [`MemoryDiff`](schema-reference.md#schema-memorydiff) |
| `GET` | `/api/v1/workspaces/{id}/memories/{memory_id}/graph` | `getMemoryGraph` | view | not required | none | 200: [`MemoryGraph`](schema-reference.md#schema-memorygraph) |
| `GET` | `/api/v1/workspaces/{id}/memories/{memory_id}/source-chain` | `getMemorySourceChain` | view | not required | none | 200: [`MemorySourceChain`](schema-reference.md#schema-memorysourcechain) |
| `POST` | `/api/v1/workspaces/{id}/memories/{memory_id}/{action}` | `transitionMemory` | orchestrator or human | required | none | 200: [`Memory`](schema-reference.md#schema-memory) |
| `GET` | `/api/v1/workspaces/{id}/notifications` | `listNotifications` | view | not required | none | 200: [`Notifications`](schema-reference.md#schema-notifications) |
| `GET` | `/api/v1/workspaces/{id}/notifications/counts` | `getNotificationCounts` | view | not required | none | 200: [`NotificationCounts`](schema-reference.md#schema-notificationcounts) |
| `POST` | `/api/v1/workspaces/{id}/notifications/{notification_id}/ack` | `acknowledgeNotification` | orchestrator or human | required | none | 200: [`Notification`](schema-reference.md#schema-notification) |
| `GET` | `/api/v1/workspaces/{id}/logs/operational` | `listOperationalLogs` | view | not required | none | 200: [`OperationalLogs`](schema-reference.md#schema-operationallogs) |
| `GET` | `/api/v1/workspaces/{id}/audit` | `listAuditEvents` | view | not required | none | 200: [`AuditEvents`](schema-reference.md#schema-auditevents) |
| `GET` | `/api/v1/workspaces/{id}/audit/export` | `exportAuditEvents` | view | not required | none | none declared |
| `GET` | `/api/v1/workspaces/{id}/settings` | `getEffectiveSettings` | view | not required | none | 200: [`EffectiveSettings`](schema-reference.md#schema-effectivesettings) |
| `GET` | `/api/v1/workspaces/{id}/settings/schema` | `getSettingsSchema` | view | not required | none | 200: [`SettingsSchema`](schema-reference.md#schema-settingsschema) |
| `POST` | `/api/v1/workspaces/{id}/settings/preflight` | `preflightSettingsUpdate` | orchestrator or human | not required | [`SettingsUpdate`](schema-reference.md#schema-settingsupdate) | 200: [`SettingsPreflight`](schema-reference.md#schema-settingspreflight) |
| `PATCH` | `/api/v1/workspaces/{id}/settings/{section}` | `updateSettingsSection` | orchestrator or human | required | [`SettingsUpdate`](schema-reference.md#schema-settingsupdate) | 200: [`EffectiveSettings`](schema-reference.md#schema-effectivesettings) |
| `POST` | `/api/v1/workspaces/{id}/transactions/{transaction_id}/{action}` | `transitionTransaction` | role-gated human | required | none | 200: [`Transaction`](schema-reference.md#schema-transaction) |
| `GET` | `/api/v1/workspaces/{id}/claims` | `listClaims` | view | not required | none | 200: [`Claims`](schema-reference.md#schema-claims) |
| `POST` | `/api/v1/workspaces/{id}/claims/internal` | `createInternalClaim` | orchestrator or human | required | [`ClaimInput`](schema-reference.md#schema-claiminput) | 201: [`Claim`](schema-reference.md#schema-claim) |
| `GET` | `/api/v1/workspaces/{id}/claims/{claim_id}` | `getClaim` | view | not required | none | 200: [`Claim`](schema-reference.md#schema-claim) |
| `POST` | `/api/v1/workspaces/{id}/claims/{claim_id}/{action}` | `transitionClaim` | role-gated human | required | [`ClaimExtendInput`](schema-reference.md#schema-claimextendinput) | 200: [`Claim`](schema-reference.md#schema-claim) |
| `GET` | `/api/v1/workspaces/{id}/claims/contentions` | `listClaimContentions` | view | not required | none | 200: [`Contentions`](schema-reference.md#schema-contentions) |
| `POST` | `/api/v1/workspaces/{id}/claims/contentions/{contention_id}/resolve` | `resolveClaimContention` | role-gated human | required | [`ContentionResolutionInput`](schema-reference.md#schema-contentionresolutioninput) | 200: [`ContentionResolution`](schema-reference.md#schema-contentionresolution) |

## Lifecycle operations

The API exposes explicit action endpoints for deliberations, proposals, policies, approvals, sessions, claims, runs, and transactions. Each endpoint's accepted action, current-state preconditions, and conflict response are defined by its OpenAPI operation and handler. The API control-plane state model is distinct from the local Go proposal/transaction service; do not apply one component's state diagram to the other.

For proposal apply, transaction recovery, and compensation semantics, consult the corresponding transaction operation schema and handler. Compensation creates a separate proposal from the rollback artifact; it does not silently rewrite history.

## Local fixtures

The realistic dashboard payload pack is in [`api/examples/dashboard-fixtures.json`](../examples/dashboard-fixtures.json). Start the API with `go run ./api/cmd/server -addr 127.0.0.1:8080 -workspace-root .`, then use the examples as response fixtures; no fixture contains secrets or absolute repository paths.
