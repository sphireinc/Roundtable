# API Schema Reference

Generated from [`api/openapi.yaml`](../openapi.yaml) by `ruby api/scripts/generate-admin-api-guide.rb`. This page lists every component schema and its declared fields; runtime validation may impose additional rules documented by endpoint handlers and [Error Semantics](error-semantics.md).

The endpoint guide links operations to request and response schemas. `required` reflects the OpenAPI contract, not whether a response field may be omitted by every runtime branch. `additionalProperties` is shown when declared.
Nested inline fields use dotted paths; `[]` marks array items and `{value}` marks additional-property values. Referenced component schemas remain separate entries.

### Schema: SecurityCapabilities {#schema-securitycapabilities}

Type: `object`

Required fields: `authentication`, `agent_boundary`, `csrf`, `default_bind`, `roles`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `authentication` | `string` | yes | enum=["local-header-development","bearer-token"] | Configured authentication mode; reports whether either shared bearer token is present, not whether a credential is valid or strong. |
| `agent_boundary` | `string` | yes |  | Human-readable summary of the advertised separation between agent and browser credentials; descriptive only and not an authorization decision. |
| `csrf` | `string` | yes |  | Human-readable summary of the state-changing Origin check; does not indicate that a separate CSRF token is enforced. |
| `default_bind` | `string` | yes | example="127.0.0.1" | Documented default listener address; this is not the actual bound address or proxy exposure. |
| `roles` | array of `string` (item constraints: enum=["view","operate","approve","govern","administer","force-override"]) | yes |  | Role labels advertised by capability discovery; handlers do not uniformly enforce a centralized role hierarchy. |
| `allowed_origins` | array of `string` (item constraints: format="uri") | no |  | Configured browser origins accepted by the HTTP server; presence does not establish WebSocket origin enforcement. |

### Schema: MaintenanceStatus {#schema-maintenancestatus}

Type: `object`

Required fields: `foreign_keys`, `wal`, `protected_immutable_tables`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `foreign_keys` | `boolean` | yes |  | Whether SQLite foreign-key enforcement is enabled on the inspected database connection. |
| `wal` | `boolean` | yes |  | Whether the database reports WAL journal mode. |
| `protected_immutable_tables` | array of `string` | yes |  | Table names excluded from the retention cleanup operation; this is not a complete immutability guarantee for every API or database writer. |

### Schema: RetentionInput {#schema-retentioninput}

Type: `object`

Required fields: `retention_days`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `retention_days` | `integer` | yes | minimum=1; maximum=3650 | Age threshold in calendar days; the retention endpoint deletes notifications older than the computed UTC cutoff. |

### Schema: ComponentHealth {#schema-componenthealth}

Type: `object`

Required fields: `status`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `status` | `string` | yes | enum=["ready","ok","degraded","unavailable","unknown","active","detached"] | Component health or workspace state reported by the current projection; interpretation depends on the component and may not represent process liveness. |
| `details` | object (any value) | no |  | Optional component-specific diagnostic values; keys and value shapes vary by component. |

### Schema: NodeHealthResponse {#schema-nodehealthresponse}

Type: `object`

Required fields: `status`, `version`, `api_version`, `time`, `request_id`, `components`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `status` | `string` | yes | enum=["ok","degraded"] | Aggregate node status; degraded means at least one inspected database or adapter condition is reported degraded or unavailable. |
| `version` | `string` | yes |  | Configured API build/version metadata; defaults to dev when not set. |
| `api_version` | `string` | yes | enum=["v1"] | API route contract version, independent of the build version. |
| `time` | `string` | yes | format="date-time" | UTC timestamp when this health projection was assembled. |
| `request_id` | `string` | yes |  | Request correlation identifier assigned to this HTTP response. |
| `components` | object (values of `ComponentHealth`) | yes |  | Named node subsystem health projections; ready labels do not prove provider subprocess liveness. |
| `degraded_reasons` | array of `string` | no |  | Human-readable reasons contributing to aggregate degraded status; omitted when no reasons were recorded. |

### Schema: WorkspaceHealthResponse {#schema-workspacehealthresponse}

Type: `object`

Required fields: `workspace`, `status`, `components`, `impact`, `request_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace` | `Workspace` | yes |  | Workspace record used to scope this health view. |
| `status` | `string` | yes | enum=["ok","degraded"] | Aggregate status combining node health with whether the selected workspace is detached. |
| `components` | object (values of `ComponentHealth`) | yes |  | Node component reports plus the selected workspace component state. |
| `impact` | `WorkspaceImpact` | yes |  | Counts of selected workspace records considered by the impact projection; not a complete activity or safety assessment. |
| `degraded_reasons` | array of `string` | no |  | Human-readable reasons contributing to degraded status; omitted when no reasons were recorded. |
| `request_id` | `string` | yes |  | Request correlation identifier assigned to this HTTP response. |

### Schema: WorkspaceInput {#schema-workspaceinput}

Type: `object`

Required fields: `root_path`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `display_name` | `string` | no |  |  |
| `root_alias` | `string` | no |  |  |
| `root_path` | `string` | yes |  |  |
| `default_branch` | `string` | no |  |  |

### Schema: WorkspacePatch {#schema-workspacepatch}

Type: `object`

Schema constraints: minProperties=1

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `display_name` | `string` | no |  |  |
| `root_alias` | `string` | no |  |  |
| `default_branch` | `string` | no |  |  |

### Schema: Workspace {#schema-workspace}

Type: `object`

Required fields: `id`, `display_name`, `canonical_repository_identity`, `status`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `revision` | `integer` | no | minimum=1 |  |
| `display_name` | `string` | yes |  |  |
| `root_alias` | `string` | no |  |  |
| `canonical_repository_identity` | `string` | yes |  |  |
| `status` | `string` | yes | enum=["active","detached"] |  |
| `default_branch` | `string` | no |  |  |
| `created_at` | `string` | yes | format="date-time" |  |
| `last_opened_at` | `string` | no | format="date-time" |  |

### Schema: WorkspaceImpact {#schema-workspaceimpact}

Type: `object`

Required fields: `active_sessions`, `active_claims`, `open_proposals`, `active_transactions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `active_sessions` | `integer` | yes |  |  |
| `active_claims` | `integer` | yes |  |  |
| `open_proposals` | `integer` | yes |  |  |
| `active_transactions` | `integer` | yes |  |  |

### Schema: RepositoryChange {#schema-repositorychange}

Type: `object`

Required fields: `path`, `index`, `worktree`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `path` | `string` | yes |  |  |
| `index` | `string` | yes | minLength=1; maxLength=1 |  |
| `worktree` | `string` | yes | minLength=1; maxLength=1 |  |

### Schema: RepositoryRemote {#schema-repositoryremote}

Type: `object`

Required fields: `name`, `url`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `name` | `string` | yes |  |  |
| `url` | `string` | yes |  |  |

### Schema: ProtectedPathSummary {#schema-protectedpathsummary}

Type: `object`

Required fields: `changed`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `changed` | `integer` | yes | minimum=0 |  |
| `paths` | array of `string` | no |  |  |

### Schema: RepositoryStatus {#schema-repositorystatus}

Type: `object`

Required fields: `workspace_id`, `branch`, `head_sha`, `dirty`, `ahead`, `behind`, `detached`, `remotes`, `index`, `protected_paths`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  |  |
| `root_alias` | `string` | no |  |  |
| `branch` | `string` | yes |  |  |
| `head_sha` | `string` | yes |  |  |
| `dirty` | `boolean` | yes |  |  |
| `ahead` | `integer` | yes | minimum=0 |  |
| `behind` | `integer` | yes | minimum=0 |  |
| `detached` | `boolean` | yes |  |  |
| `remotes` | array of `RepositoryRemote` | yes |  |  |
| `index` | array of `RepositoryChange` | yes |  |  |
| `protected_paths` | `ProtectedPathSummary` | yes |  |  |

### Schema: BranchSwitchInput {#schema-branchswitchinput}

Type: object (no additional properties)

Required fields: `branch`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `branch` | `string` | yes | minLength=1 |  |

### Schema: RepositoryBlocker {#schema-repositoryblocker}

Type: `object`

Required fields: `code`, `message`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `code` | `string` | yes |  |  |
| `message` | `string` | yes |  |  |
| `count` | `integer` | no | minimum=0 |  |

### Schema: BranchSwitchPreflight {#schema-branchswitchpreflight}

Type: `object`

Required fields: `workspace_id`, `target_branch`, `allowed`, `blockers`, `repository`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  |  |
| `target_branch` | `string` | yes |  |  |
| `allowed` | `boolean` | yes |  |  |
| `blockers` | array of `RepositoryBlocker` | yes |  |  |
| `repository` | `RepositoryStatus` | yes |  |  |

### Schema: BranchSwitchResponse {#schema-branchswitchresponse}

Type: `object`

Required fields: `workspace`, `before`, `after`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace` | `Workspace` | yes |  |  |
| `before` | `RepositoryStatus` | yes |  |  |
| `after` | `RepositoryStatus` | yes |  |  |

### Schema: RepositoryEntity {#schema-repositoryentity}

Type: `object`

Required fields: `id`, `type`, `path`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `type` | `string` | yes |  |  |
| `path` | `string` | yes |  |  |
| `name` | `string` | no |  |  |
| `language` | `string` | no |  |  |
| `kind` | `string` | no |  |  |
| `start_line` | `integer` | no | minimum=0 |  |
| `end_line` | `integer` | no | minimum=0 |  |
| `claim_status` | `string` | no |  |  |
| `related_proposals` | array of `string` | no |  |  |
| `related_entities` | array of `string` | no |  |  |

### Schema: RepositoryEntities {#schema-repositoryentities}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `RepositoryEntity` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: Agent {#schema-agent}

Type: `object`

Required fields: `agent_id`, `display_name`, `adapter_type`, `enabled`, `health`, `capabilities`, `supports_resume`, `supports_mcp`, `concurrency_limit`, `policy_weight`, `active_session_count`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `agent_id` | `string` | yes |  |  |
| `display_name` | `string` | yes |  |  |
| `adapter_type` | `string` | yes |  |  |
| `enabled` | `boolean` | yes |  |  |
| `health` | `string` | yes |  |  |
| `executable_version` | `string` | no |  |  |
| `capabilities` | object (values of `boolean`) | yes |  |  |
| `supports_resume` | `boolean` | yes |  |  |
| `supports_mcp` | `boolean` | yes |  |  |
| `concurrency_limit` | `integer` | yes | minimum=1 |  |
| `policy_weight` | `integer` | yes | minimum=0 |  |
| `active_session_count` | `integer` | yes | minimum=0 |  |
| `last_heartbeat` | `string` | no | format="date-time" |  |

### Schema: DiagnosticCheck {#schema-diagnosticcheck}

Type: `object`

Required fields: `name`, `status`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `name` | `string` | yes |  |  |
| `status` | `string` | yes | enum=["passed","failed"] |  |
| `detail` | `string` | no |  |  |

### Schema: DiagnosticRun {#schema-diagnosticrun}

Type: `object`

Required fields: `run_id`, `agent_id`, `started_at`, `finished_at`, `status`, `checks`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `run_id` | `string` | yes |  |  |
| `agent_id` | `string` | yes |  |  |
| `started_at` | `string` | yes | format="date-time" |  |
| `finished_at` | `string` | yes | format="date-time" |  |
| `status` | `string` | yes | enum=["passed","failed"] |  |
| `checks` | array of `DiagnosticCheck` | yes |  |  |
| `stdout` | `string` | no |  |  |
| `stderr` | `string` | no |  |  |
| `remediation_hints` | array of `string` | no |  |  |

### Schema: SessionInput {#schema-sessioninput}

Type: object (no additional properties)

Required fields: `agent_id`, `run_id`, `adapter`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `agent_id` | `string` | yes |  |  |
| `run_id` | `string` | yes |  |  |
| `adapter` | `string` | yes |  |  |
| `provider` | `string` | no |  |  |
| `model` | `string` | no |  |  |
| `external_session_id` | `string` | no |  |  |

### Schema: Session {#schema-session}

Type: `object`

Required fields: `id`, `agent_id`, `run_id`, `adapter`, `status`, `started_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `agent_id` | `string` | yes |  |  |
| `run_id` | `string` | yes |  |  |
| `adapter` | `string` | yes |  |  |
| `provider` | `string` | no |  |  |
| `model` | `string` | no |  |  |
| `external_session_id` | `string` | no |  |  |
| `resume_command_template` | `string` | no |  |  |
| `status` | `string` | yes | enum=["starting","active","running","paused","resuming","stopped","terminated"] |  |
| `started_at` | `string` | yes | format="date-time" |  |
| `last_seen_at` | `string` | no | format="date-time" |  |
| `ended_at` | `string` | no | format="date-time" |  |

### Schema: Sessions {#schema-sessions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Session` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: SessionTerminationInput {#schema-sessionterminationinput}

Type: object (no additional properties)

Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `impact_details` | `string` | no |  |  |

### Schema: SessionLog {#schema-sessionlog}

Type: `object`

Required fields: `id`, `event_type`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes |  |  |
| `event_type` | `string` | yes |  |  |
| `payload` | `string` | no |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: SessionLogs {#schema-sessionlogs}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `SessionLog` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: SessionToolCall {#schema-sessiontoolcall}

Type: `object`

Required fields: `id`, `tool`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes |  |  |
| `tool` | `string` | yes |  |  |
| `action` | `string` | no |  |  |
| `status` | `string` | no |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: SessionToolCalls {#schema-sessiontoolcalls}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `SessionToolCall` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PaginatedClaims {#schema-paginatedclaims}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `object` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PaginatedSessionProposals {#schema-paginatedsessionproposals}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `object` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: SessionMetrics {#schema-sessionmetrics}

Type: `object`

Required fields: `session_id`, `usage`, `source`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `session_id` | `string` | yes |  |  |
| `usage` | object (values of `number`) | yes |  |  |
| `source` | `string` | yes |  |  |

### Schema: SessionEnvironment {#schema-sessionenvironment}

Type: `object`

Required fields: `session_id`, `fingerprint`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `session_id` | `string` | yes |  |  |
| `fingerprint` | object (values of `string`) | yes |  |  |

### Schema: DeliberationInput {#schema-deliberationinput}

Type: object (no additional properties)

Required fields: `goal`, `agent_pool`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `goal` | `string` | yes |  |  |
| `agent_pool` | array of `string` | yes | minItems=1 |  |
| `governance_profile` | `string` | no |  |  |
| `initial_resource_scope` | array of `string` | no |  |  |
| `budget_limit` | `integer` | no | minimum=0 |  |
| `time_limit_seconds` | `integer` | no | minimum=0 |  |
| `human_constraints` | array of `string` | no |  |  |
| `moderator_id` | `string` | no |  |  |

### Schema: Deliberation {#schema-deliberation}

Type: `object`

Required fields: `id`, `workspace_id`, `goal`, `status`, `created_by`, `agent_pool`, `participants`, `round`, `unresolved_conflicts`, `linked_proposals`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `goal` | `string` | yes |  |  |
| `status` | `string` | yes | enum=["draft","running","paused","terminated"] |  |
| `created_by` | `string` | yes |  |  |
| `moderator_id` | `string` | no |  |  |
| `agent_pool` | array of `string` | yes |  |  |
| `participants` | array of `string` | yes |  |  |
| `governance_profile` | `string` | no |  |  |
| `initial_resource_scope` | array of `string` | no |  |  |
| `budget_limit` | `integer` | no |  |  |
| `time_limit_seconds` | `integer` | no |  |  |
| `human_constraints` | array of `string` | no |  |  |
| `round` | `integer` | yes | minimum=0 |  |
| `unresolved_conflicts` | array of `string` | yes |  |  |
| `linked_proposals` | array of `string` | yes |  |  |
| `started_at` | `string` | no | format="date-time" |  |
| `ended_at` | `string` | no | format="date-time" |  |
| `created_at` | `string` | yes | format="date-time" |  |
| `updated_at` | `string` | yes | format="date-time" |  |

### Schema: Deliberations {#schema-deliberations}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Deliberation` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: DeliberationMessageInput {#schema-deliberationmessageinput}

Type: `object`

Required fields: `body`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `message_type` | `string` | no | default="human_instruction" |  |
| `body` | `string` | yes |  |  |

### Schema: TranscriptEntry {#schema-transcriptentry}

Type: `object`

Required fields: `id`, `round`, `actor`, `kind`, `visibility_class`, `rendering`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `round` | `integer` | yes | minimum=0 |  |
| `actor` | `string` | yes |  |  |
| `kind` | `string` | yes | enum=["message","event"] |  |
| `message_type` | `string` | no |  |  |
| `summary` | `string` | no |  |  |
| `content` | `string` | no |  |  |
| `tool_references` | array of `string` | no |  |  |
| `claim_references` | array of `string` | no |  |  |
| `proposal_references` | array of `string` | no |  |  |
| `vote_references` | array of `string` | no |  |  |
| `visibility_class` | `string` | yes | enum=["user_visible","operational"] |  |
| `rendering` | object (any value) | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: TranscriptEntries {#schema-transcriptentries}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `TranscriptEntry` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: ProposalInput {#schema-proposalinput}

Type: object (no additional properties)

Required fields: `title`, `summary`, `patch_path`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | no |  |  |
| `deliberation_id` | `string` | no |  |  |
| `proposer_session_id` | `string` | no |  |  |
| `title` | `string` | yes |  |  |
| `summary` | `string` | yes |  |  |
| `patch_path` | `string` | yes |  |  |
| `base_revision` | `string` | no |  |  |
| `risk` | `string` | no | enum=["normal","high"] |  |
| `resource_ids` | array of `string` | no |  |  |

### Schema: Proposal {#schema-proposal}

Type: `object`

Required fields: `proposal_id`, `workspace_id`, `title`, `summary`, `patch_path`, `state`, `vote_state`, `policy_state`, `approval_state`, `transaction_state`, `risk`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `deliberation_id` | `string` | no |  |  |
| `proposer_session_id` | `string` | no |  |  |
| `title` | `string` | yes |  |  |
| `summary` | `string` | yes |  |  |
| `patch_path` | `string` | yes |  |  |
| `base_revision` | `string` | no |  |  |
| `state` | `string` | yes | enum=["pending","in_review","accepted","rejected","withdrawn"] |  |
| `vote_state` | `string` | yes |  |  |
| `policy_state` | `string` | yes |  |  |
| `approval_state` | `string` | yes |  |  |
| `transaction_state` | `string` | yes |  |  |
| `risk` | `string` | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |
| `updated_at` | `string` | yes | format="date-time" |  |

### Schema: Proposals {#schema-proposals}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Proposal` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PatchMetadata {#schema-patchmetadata}

Type: `object`

Required fields: `proposal_id`, `patch_path`, `current_head`, `stale_base`, `file_count`, `added_lines`, `removed_lines`, `binary_files`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | yes |  |  |
| `patch_path` | `string` | yes |  |  |
| `base_revision` | `string` | no |  |  |
| `current_head` | `string` | yes |  |  |
| `stale_base` | `boolean` | yes |  |  |
| `file_count` | `integer` | yes | minimum=0 |  |
| `added_lines` | `integer` | yes | minimum=0 |  |
| `removed_lines` | `integer` | yes | minimum=0 |  |
| `binary_files` | `integer` | yes | minimum=0 |  |

### Schema: PatchFile {#schema-patchfile}

Type: `object`

Required fields: `path`, `operation`, `binary`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `path` | `string` | yes |  |  |
| `old_path` | `string` | no |  |  |
| `new_path` | `string` | no |  |  |
| `operation` | `string` | yes | enum=["create","modify","delete","rename"] |  |
| `binary` | `boolean` | yes |  |  |
| `added_lines` | `integer` | no |  |  |
| `removed_lines` | `integer` | no |  |  |

### Schema: PatchFiles {#schema-patchfiles}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `PatchFile` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PatchDiff {#schema-patchdiff}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `string` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PatchSymbol {#schema-patchsymbol}

Type: `object`

Required fields: `path`, `resource_id`, `kind`, `name`, `start_line`, `end_line`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `path` | `string` | yes |  |  |
| `resource_id` | `string` | yes |  |  |
| `kind` | `string` | yes |  |  |
| `name` | `string` | yes |  |  |
| `start_line` | `integer` | yes |  |  |
| `end_line` | `integer` | yes |  |  |

### Schema: PatchSymbols {#schema-patchsymbols}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `PatchSymbol` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: ValidationInput {#schema-validationinput}

Type: object (no additional properties)

Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `stages` | array of `string` (item constraints: enum=["patch_parse","apply_dry_run","static_checks","tests","policy","stale_base"]) | no |  |  |

### Schema: ValidationRun {#schema-validationrun}

Type: `object`

Required fields: `run_id`, `proposal_id`, `status`, `started_at`, `completed_at`, `stages`, `commands`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `run_id` | `string` | yes |  |  |
| `proposal_id` | `string` | yes |  |  |
| `status` | `string` | yes | enum=["passed","failed"] |  |
| `started_at` | `string` | yes | format="date-time" |  |
| `completed_at` | `string` | yes | format="date-time" |  |
| `stages` | object (any value) | yes |  |  |
| `commands` | object (values of `string`) | yes |  |  |
| `output` | `string` | no |  |  |

### Schema: ClaimInput {#schema-claiminput}

Type: `object`

Required fields: `agent_id`, `task_id`, `resource_type`, `mode`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `claim_id` | `string` | no |  |  |
| `agent_id` | `string` | yes |  |  |
| `session_id` | `string` | no |  |  |
| `task_id` | `string` | yes |  |  |
| `resource_id` | `string` | no |  |  |
| `resource_type` | `string` | yes | enum=["file","directory","symbol","command","schema","endpoint","test_suite","custom"] |  |
| `path` | `string` | no |  |  |
| `symbol` | `string` | no |  |  |
| `mode` | `string` | yes | enum=["exclusive","shared","advisory","execution"] |  |
| `run_id` | `string` | no |  |  |
| `base_hash` | `string` | no |  |  |
| `ttl_seconds` | `integer` | no | minimum=1 |  |
| `rationale` | `string` | no |  |  |

### Schema: ClaimExtendInput {#schema-claimextendinput}

Type: `object`


| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `ttl_seconds` | `integer` | no | minimum=1 |  |

### Schema: Claim {#schema-claim}

Type: `object`

Required fields: `id`, `workspace_id`, `agent_id`, `task_id`, `resource_id`, `resource_type`, `mode`, `state`, `acquired_at`, `lease_expires_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `agent_id` | `string` | yes |  |  |
| `session_id` | `string` | no |  |  |
| `task_id` | `string` | yes |  |  |
| `resource_id` | `string` | yes |  |  |
| `resource_type` | `string` | yes |  |  |
| `path` | `string` | no |  |  |
| `symbol` | `string` | no |  |  |
| `mode` | `string` | yes |  |  |
| `base_hash` | `string` | no |  |  |
| `state` | `string` | yes | enum=["active","released","revoked","suspended","expired"] |  |
| `rationale` | `string` | no |  |  |
| `acquired_at` | `string` | yes | format="date-time" |  |
| `lease_expires_at` | `string` | yes | format="date-time" |  |

### Schema: Claims {#schema-claims}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Claim` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: Contention {#schema-contention}

Type: `object`

Required fields: `id`, `workspace_id`, `resource_id`, `requested_resource_id`, `requested_agent_id`, `requested_task_id`, `requested_mode`, `current_owner_claim_id`, `current_owner_agent_id`, `state`, `reason`, `acquired_at`, `lease_expires_at`, `allowed_resolutions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `resource_id` | `string` | yes |  |  |
| `requested_resource_id` | `string` | yes |  |  |
| `requested_agent_id` | `string` | yes |  |  |
| `requested_task_id` | `string` | yes |  |  |
| `requested_mode` | `string` | yes |  |  |
| `requested_path` | `string` | no |  |  |
| `current_owner_claim_id` | `string` | yes |  |  |
| `current_owner_agent_id` | `string` | yes |  |  |
| `state` | `string` | yes |  |  |
| `reason` | `string` | yes |  |  |
| `acquired_at` | `string` | yes | format="date-time" |  |
| `lease_expires_at` | `string` | yes | format="date-time" |  |
| `allowed_resolutions` | array of `string` | yes |  |  |

### Schema: Contentions {#schema-contentions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Contention` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: Vote {#schema-vote}

Type: `object`

Required fields: `id`, `proposal_id`, `agent_id`, `decision`, `policy_weight`, `policy_version`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `proposal_id` | `string` | yes |  |  |
| `agent_id` | `string` | yes |  |  |
| `session_id` | `string` | no |  |  |
| `decision` | `string` | yes | enum=["approve","reject","abstain","veto"] |  |
| `policy_weight` | `integer` | yes | minimum=1 |  |
| `policy_version` | `string` | yes |  |  |
| `rationale` | `string` | no |  |  |
| `confidence` | `number` | no | minimum=0; maximum=1 |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: Votes {#schema-votes}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Vote` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: VoteInput {#schema-voteinput}

Type: `object`

Required fields: `agent_id`, `decision`, `rationale`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | no |  |  |
| `agent_id` | `string` | yes |  |  |
| `session_id` | `string` | no |  |  |
| `decision` | `string` | yes | enum=["approve","reject","abstain","veto"] |  |
| `rationale` | `string` | yes |  |  |
| `confidence` | `number` | no | minimum=0; maximum=1 |  |

### Schema: VoteResult {#schema-voteresult}

Type: `object`

Required fields: `vote`, `consensus`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `vote` | `Vote` | yes |  |  |
| `consensus` | `Consensus` | yes |  |  |

### Schema: Consensus {#schema-consensus}

Type: `object`

Required fields: `proposal_id`, `outcome`, `approve_weight`, `reject_weight`, `abstain_weight`, `veto_weight`, `quorum`, `threshold`, `votes_cast`, `policy_version`, `explanation`, `calculated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | yes |  |  |
| `outcome` | `string` | yes | enum=["pending","approved","blocked"] |  |
| `approve_weight` | `integer` | yes | minimum=0 |  |
| `reject_weight` | `integer` | yes | minimum=0 |  |
| `abstain_weight` | `integer` | yes | minimum=0 |  |
| `veto_weight` | `integer` | yes | minimum=0 |  |
| `quorum` | `integer` | yes | minimum=1 |  |
| `threshold` | `integer` | yes | minimum=1 |  |
| `votes_cast` | `integer` | yes | minimum=0 |  |
| `policy_version` | `string` | yes |  |  |
| `explanation` | `string` | yes |  |  |
| `calculated_at` | `string` | yes | format="date-time" |  |

### Schema: ConsensusAnalytics {#schema-consensusanalytics}

Type: `object`

Required fields: `window`, `generated_at`, `proposal_count`, `consensus_count`, `success_rate`, `rejection_rate`, `abstention_rate`, `mean_time_to_consensus_seconds`, `p50_time_to_consensus_seconds`, `p95_time_to_consensus_seconds`, `policy_overrides`, `human_interventions`, `quorum_failures`, `agent_participation`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `window` | `object` | yes |  |  |
| `window.from` | `string` | yes | format="date-time" |  |
| `window.to` | `string` | yes | format="date-time" |  |
| `generated_at` | `string` | yes | format="date-time" |  |
| `proposal_count` | `integer` | yes | minimum=0 |  |
| `consensus_count` | `integer` | yes | minimum=0 |  |
| `success_rate` | `number` | yes | minimum=0; maximum=1 |  |
| `rejection_rate` | `number` | yes | minimum=0; maximum=1 |  |
| `abstention_rate` | `number` | yes | minimum=0; maximum=1 |  |
| `mean_time_to_consensus_seconds` | `number` | yes | minimum=0 |  |
| `p50_time_to_consensus_seconds` | `number` | yes | minimum=0 |  |
| `p95_time_to_consensus_seconds` | `number` | yes | minimum=0 |  |
| `policy_overrides` | `integer` | yes | minimum=0 |  |
| `human_interventions` | `integer` | yes | minimum=0 |  |
| `quorum_failures` | `integer` | yes | minimum=0 |  |
| `agent_participation` | array of `AgentParticipation` | yes |  |  |

### Schema: AgentParticipation {#schema-agentparticipation}

Type: `object`

Required fields: `agent_id`, `votes`, `approvals`, `rejections`, `abstentions`, `vetoes`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `agent_id` | `string` | yes |  |  |
| `votes` | `integer` | yes | minimum=0 |  |
| `approvals` | `integer` | yes | minimum=0 |  |
| `rejections` | `integer` | yes | minimum=0 |  |
| `abstentions` | `integer` | yes | minimum=0 |  |
| `vetoes` | `integer` | yes | minimum=0 |  |

### Schema: Policy {#schema-policy}

Type: `object`

Required fields: `id`, `workspace_id`, `name`, `status`, `scope`, `selector`, `severity`, `enforcement_mode`, `human_approval_required`, `metadata`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `name` | `string` | yes |  |  |
| `status` | `string` | yes | enum=["disabled","active"] |  |
| `current_revision_id` | `string` | no |  |  |
| `scope` | `string` | yes |  |  |
| `selector` | object (any value) | yes |  |  |
| `severity` | `string` | yes |  |  |
| `enforcement_mode` | `string` | yes |  |  |
| `human_approval_required` | `boolean` | yes |  |  |
| `metadata` | object (any value) | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |
| `updated_at` | `string` | yes | format="date-time" |  |

### Schema: Policies {#schema-policies}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Policy` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PolicyInput {#schema-policyinput}

Type: `object`

Required fields: `name`, `definition`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | no |  |  |
| `name` | `string` | yes |  |  |
| `scope` | `string` | no |  |  |
| `selector` | object (any value) | no |  |  |
| `severity` | `string` | no |  |  |
| `enforcement_mode` | `string` | no |  |  |
| `human_approval_required` | `boolean` | no |  |  |
| `metadata` | object (any value) | no |  |  |
| `definition` | object (any value) | yes |  |  |

### Schema: PolicyRevision {#schema-policyrevision}

Type: `object`

Required fields: `id`, `policy_id`, `version`, `status`, `definition`, `created_by`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `policy_id` | `string` | yes |  |  |
| `version` | `integer` | yes | minimum=1 |  |
| `status` | `string` | yes | enum=["draft","published"] |  |
| `definition` | object (any value) | yes |  |  |
| `created_by` | `string` | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: PolicyRevisions {#schema-policyrevisions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `PolicyRevision` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: PolicyImpact {#schema-policyimpact}

Type: `object`

Required fields: `active_sessions`, `active_claims`, `open_proposals`, `active_transactions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `active_sessions` | `integer` | yes | minimum=0 |  |
| `active_claims` | `integer` | yes | minimum=0 |  |
| `open_proposals` | `integer` | yes | minimum=0 |  |
| `active_transactions` | `integer` | yes | minimum=0 |  |

### Schema: PolicyTransition {#schema-policytransition}

Type: `object`

Required fields: `policy`, `impact`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `policy` | `Policy` | yes |  |  |
| `impact` | `PolicyImpact` | yes |  |  |

### Schema: PolicySchemaResponse {#schema-policyschemaresponse}

Type: `object`

Required fields: `schema_version`, `schema`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `schema_version` | `string` | yes | const="1" |  |
| `schema` | `PolicyInputSchema` | yes |  |  |

### Schema: PolicyInputSchema {#schema-policyinputschema}

Type: `object`

Required fields: `type`, `required`, `properties`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `type` | `string` | yes | const="object" |  |
| `required` | array of `string` | yes | const=["name","definition"] |  |
| `properties` | `object` | yes |  |  |
| `properties.name` | `object` | yes |  |  |
| `properties.name.type` | `string` | yes | const="string" |  |
| `properties.scope` | `object` | yes |  |  |
| `properties.scope.type` | `string` | yes | const="string" |  |
| `properties.selector` | `object` | yes |  |  |
| `properties.selector.type` | `string` | yes | const="object" |  |
| `properties.severity` | `object` | yes |  |  |
| `properties.severity.type` | `string` | yes | const="string" |  |
| `properties.enforcement_mode` | `object` | yes |  |  |
| `properties.enforcement_mode.type` | `string` | yes | const="string" |  |
| `properties.human_approval_required` | `object` | yes |  |  |
| `properties.human_approval_required.type` | `string` | yes | const="boolean" |  |
| `properties.definition` | `object` | yes |  |  |
| `properties.definition.type` | `string` | yes | const="object" |  |
| `properties.metadata` | `object` | yes |  |  |
| `properties.metadata.type` | `string` | yes | const="object" |  |

### Schema: PolicyValidation {#schema-policyvalidation}

Type: `object`

Required fields: `valid`, `result`, `policy_id`, `policy_revision_id`, `issues`, `simulation`, `deterministic_key`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `valid` | `boolean` | yes |  |  |
| `result` | `string` | yes | enum=["pass","fail"] |  |
| `policy_id` | `string` | yes |  |  |
| `policy_revision_id` | `string` | yes |  |  |
| `issues` | array of `string` | yes |  |  |
| `simulation` | `boolean` | yes | const=true |  |
| `deterministic_key` | `string` | yes |  |  |

### Schema: PolicyEvaluationInput {#schema-policyevaluationinput}

Type: `object`

Required fields: `subject_type`, `subject_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `subject_type` | `string` | yes |  |  |
| `subject_id` | `string` | yes |  |  |
| `evidence` | `array or null` | no |  |  |

### Schema: PolicyEvaluation {#schema-policyevaluation}

Type: `object`

Required fields: `id`, `policy_id`, `policy_revision_id`, `subject_type`, `subject_id`, `result`, `matched_rules`, `evidence`, `remediation`, `human_approval_required`, `deterministic_key`, `simulation`, `evaluated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `policy_id` | `string` | yes |  |  |
| `policy_revision_id` | `string` | yes |  |  |
| `subject_type` | `string` | yes |  |  |
| `subject_id` | `string` | yes |  |  |
| `result` | `string` | yes | enum=["pass","warn"] |  |
| `matched_rules` | array of `string` | yes |  |  |
| `evidence` | `array or null` | yes |  |  |
| `remediation` | array of `string` | yes |  |  |
| `human_approval_required` | `boolean` | yes |  |  |
| `deterministic_key` | `string` | yes |  |  |
| `simulation` | `boolean` | yes |  |  |
| `evaluated_at` | `string` | yes | format="date-time" |  |

### Schema: Approval {#schema-approval}

Type: `object`

Required fields: `id`, `workspace_id`, `subject`, `reason`, `risk`, `status`, `required_role`, `decision_metadata`, `requested_by`, `override_policy`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `proposal_id` | `string` | no |  |  |
| `task_id` | `string` | no |  |  |
| `subject` | `string` | yes |  |  |
| `reason` | `string` | yes |  |  |
| `risk` | `string` | yes |  |  |
| `status` | `string` | yes | enum=["requested","approved","rejected","changes_requested","deferred"] |  |
| `required_role` | `string` | yes |  |  |
| `decision` | `string` | no |  |  |
| `decision_metadata` | object (any value) | yes |  |  |
| `requested_by` | `string` | yes |  |  |
| `decided_by` | `string` | no |  |  |
| `override_policy` | `boolean` | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |
| `updated_at` | `string` | yes | format="date-time" |  |

### Schema: Approvals {#schema-approvals}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Approval` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: ApprovalInput {#schema-approvalinput}

Type: `object`

Required fields: `subject`, `reason`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | no |  |  |
| `proposal_id` | `string` | no |  |  |
| `task_id` | `string` | no |  |  |
| `subject` | `string` | yes |  |  |
| `reason` | `string` | yes |  |  |
| `risk` | `string` | no |  |  |
| `required_role` | `string` | no |  |  |
| `decision_metadata` | object (any value) | no |  |  |
| `override_policy` | `boolean` | no |  |  |

### Schema: ApprovalDecisionInput {#schema-approvaldecisioninput}

Type: `object`


| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `reason` | `string` | no |  |  |
| `decision_metadata` | object (any value) | no |  |  |
| `override` | `boolean` | no |  |  |

### Schema: Transaction {#schema-transaction}

Type: `object`

Required fields: `id`, `workspace_id`, `proposal_id`, `run_id`, `before_git_hash`, `status`, `applied_by`, `metadata`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `proposal_id` | `string` | yes |  |  |
| `run_id` | `string` | yes |  |  |
| `before_git_hash` | `string` | yes |  |  |
| `after_git_hash` | `string` | no |  |  |
| `status` | `string` | yes | enum=["pending","staged","running","applied","cancelled","failed"] |  |
| `applied_by` | `string` | yes |  |  |
| `applied_at` | `string` | no | format="date-time" |  |
| `rollback_patch_path` | `string` | no |  |  |
| `metadata` | object (any value) | yes |  |  |

### Schema: Transactions {#schema-transactions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Transaction` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: TransactionPhase {#schema-transactionphase}

Type: `object`

Required fields: `id`, `transaction_id`, `phase`, `status`, `actor`, `inputs`, `outputs`, `recovery_state`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `transaction_id` | `string` | yes |  |  |
| `phase` | `string` | yes |  |  |
| `status` | `string` | yes |  |  |
| `actor` | `string` | yes |  |  |
| `reason` | `string` | no |  |  |
| `request_id` | `string` | no |  |  |
| `inputs` | object (any value) | yes |  |  |
| `outputs` | object (any value) | yes |  |  |
| `log_ref` | `string` | no |  |  |
| `failure_code` | `string` | no |  |  |
| `recovery_state` | `string` | yes |  |  |
| `started_at` | `string` | no | format="date-time" |  |
| `ended_at` | `string` | no | format="date-time" |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: TransactionPhases {#schema-transactionphases}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `TransactionPhase` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: TransactionRecovery {#schema-transactionrecovery}

Type: `object`

Required fields: `transaction_id`, `status`, `repository_state`, `actions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `transaction_id` | `string` | yes |  |  |
| `status` | `string` | yes |  |  |
| `repository_state` | `string` | yes |  |  |
| `actions` | array of `string` | yes |  |  |
| `blocked_reason` | `string` | no |  |  |

### Schema: RunControl {#schema-runcontrol}

Type: `object`

Required fields: `run_id`, `workspace_id`, `goal`, `state`, `active_agent_count`, `active_deliberations`, `open_proposals`, `pending_approvals`, `blockers`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `run_id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `goal` | `string` | yes |  |  |
| `state` | `string` | yes | enum=["stopped","starting","active","paused-intake","draining","stopping","degraded"] |  |
| `active_agent_count` | `integer` | yes | minimum=0 |  |
| `active_deliberations` | `integer` | yes | minimum=0 |  |
| `open_proposals` | `integer` | yes | minimum=0 |  |
| `pending_approvals` | `integer` | yes | minimum=0 |  |
| `blockers` | `integer` | yes | minimum=0 |  |
| `updated_at` | `string` | yes | format="date-time" |  |

### Schema: RunStartInput {#schema-runstartinput}

Type: `object`


| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `run_id` | `string` | no |  |  |
| `goal` | `string` | no |  |  |

### Schema: RunTransition {#schema-runtransition}

Type: `object`

Required fields: `run_id`, `state`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `run_id` | `string` | yes |  |  |
| `state` | `string` | yes |  |  |

### Schema: DashboardSummary {#schema-dashboardsummary}

Type: `object`

Required fields: `workspace_id`, `generated_at`, `measurement_window`, `agents_online`, `proposal_queue`, `consensus_success_rate`, `policy_pass_rate`, `transaction_manager`, `mcp_enforcement`, `memory_oracle`, `run_state`, `repository`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  |  |
| `generated_at` | `string` | yes | format="date-time" |  |
| `measurement_window` | `object` | yes |  |  |
| `measurement_window.from` | `string` | yes | format="date-time" |  |
| `measurement_window.to` | `string` | yes | format="date-time" |  |
| `agents_online` | `integer` | yes | minimum=0 |  |
| `proposal_queue` | `object` | yes |  |  |
| `proposal_queue.pending` | `integer` | yes |  |  |
| `proposal_queue.in_review` | `integer` | yes |  |  |
| `proposal_queue.approved` | `integer` | yes |  |  |
| `proposal_queue.rejected` | `integer` | yes |  |  |
| `consensus_success_rate` | `number` | yes | minimum=0; maximum=1 |  |
| `policy_pass_rate` | `number` | yes | minimum=0; maximum=1 |  |
| `transaction_manager` | `string` | yes |  |  |
| `mcp_enforcement` | `string` | yes |  |  |
| `memory_oracle` | `string` | yes |  |  |
| `run_state` | `string` | yes |  |  |
| `repository` | object (any value) | yes |  |  |

### Schema: ActivityItem {#schema-activityitem}

Type: `object`

Required fields: `id`, `type`, `category`, `entity_type`, `severity`, `title`, `metadata`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes |  |  |
| `type` | `string` | yes |  |  |
| `category` | `string` | yes |  |  |
| `actor_id` | `string` | no |  |  |
| `entity_type` | `string` | yes |  |  |
| `entity_id` | `string` | no |  |  |
| `severity` | `string` | yes |  |  |
| `title` | `string` | yes |  |  |
| `metadata` | object (any value) | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: ActivityFeed {#schema-activityfeed}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `ActivityItem` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: Memory {#schema-memory}

Type: `object`

Required fields: `id`, `scope`, `kind`, `title`, `body_md`, `tags`, `provenance`, `confidence`, `reliability`, `importance`, `pinned`, `revision`, `status`, `resynthesis_status`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | no |  |  |
| `scope` | `string` | yes |  |  |
| `kind` | `string` | yes |  |  |
| `title` | `string` | yes |  |  |
| `body_md` | `string` | yes |  |  |
| `tags` | array of `string` | yes |  |  |
| `provenance` | object (any value) | yes |  |  |
| `source_event_id` | `integer` | no | format="int64" |  |
| `source_session_id` | `string` | no |  |  |
| `source_proposal_id` | `string` | no |  |  |
| `confidence` | `number` | yes | minimum=0; maximum=1 |  |
| `reliability` | `number` | yes | minimum=0; maximum=1 |  |
| `importance` | `integer` | yes | minimum=0; maximum=100 |  |
| `pinned` | `boolean` | yes |  |  |
| `revision` | `integer` | yes | minimum=1 |  |
| `status` | `string` | yes | enum=["active","archived","merged"] |  |
| `resynthesis_status` | `string` | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |
| `updated_at` | `string` | yes | format="date-time" |  |

### Schema: Memories {#schema-memories}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Memory` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: MemoryInput {#schema-memoryinput}

Type: `object`

Required fields: `title`, `body_md`, `scope`, `kind`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `scope` | `string` | yes |  |  |
| `kind` | `string` | yes |  |  |
| `title` | `string` | yes |  |  |
| `body_md` | `string` | yes |  |  |
| `tags` | array of `string` | no |  |  |
| `provenance` | object (any value) | no |  |  |
| `source_event_id` | `integer` | no | format="int64" |  |
| `source_session_id` | `string` | no |  |  |
| `source_proposal_id` | `string` | no |  |  |
| `confidence` | `number` | no | minimum=0; maximum=1 |  |
| `reliability` | `number` | no | minimum=0; maximum=1 |  |
| `importance` | `integer` | no | minimum=0; maximum=100 |  |

### Schema: MemoryRevisionInput {#schema-memoryrevisioninput}

Type: `object`

Required fields: `body_md`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `body_md` | `string` | yes |  |  |
| `title` | `string` | no |  |  |
| `reason` | `string` | no |  |  |

### Schema: MemoryRevision {#schema-memoryrevision}

Type: `object`

Required fields: `id`, `memory_id`, `revision`, `body_md`, `provenance`, `created_by`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `memory_id` | `string` | yes |  |  |
| `revision` | `integer` | yes | minimum=1 |  |
| `body_md` | `string` | yes |  |  |
| `provenance` | object (any value) | yes |  |  |
| `created_by` | `string` | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: MemoryRevisions {#schema-memoryrevisions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `MemoryRevision` | yes |  |  |

### Schema: MemoryDiff {#schema-memorydiff}

Type: `object`

Required fields: `memory_id`, `from_revision`, `to_revision`, `changed`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `memory_id` | `string` | yes |  |  |
| `from_revision` | `string` | yes |  |  |
| `to_revision` | `string` | yes |  |  |
| `changed` | `boolean` | yes |  |  |
| `current_body_md` | `string` | no |  |  |

### Schema: MemoryGraph {#schema-memorygraph}

Type: `object`

Required fields: `memory_id`, `aliases`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `memory_id` | `string` | yes |  |  |
| `aliases` | array of object (any value) | yes |  |  |

### Schema: MemorySourceChain {#schema-memorysourcechain}

Type: `object`

Required fields: `memory_id`, `edges`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `memory_id` | `string` | yes |  |  |
| `edges` | array of object (any value) | yes |  |  |

### Schema: Notification {#schema-notification}

Type: `object`

Required fields: `id`, `workspace_id`, `recipient_id`, `severity`, `category`, `title`, `body`, `actionable`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  |  |
| `workspace_id` | `string` | yes |  |  |
| `recipient_id` | `string` | yes |  |  |
| `severity` | `string` | yes | enum=["info","warning","error","critical"] |  |
| `category` | `string` | yes | enum=["approval_required","claim_contention","transaction_failure","policy_intervention","agent_session_failure","repository_degraded","system_warning"] |  |
| `title` | `string` | yes |  |  |
| `body` | `string` | yes |  |  |
| `entity_type` | `string` | no |  |  |
| `entity_id` | `string` | no |  |  |
| `read_at` | `string` | no | format="date-time" |  |
| `actionable` | `boolean` | yes |  |  |
| `resolved_at` | `string` | no | format="date-time" |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: Notifications {#schema-notifications}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Notification` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: NotificationCounts {#schema-notificationcounts}

Type: `object`

Required fields: `workspace_id`, `unread`, `actionable`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  |  |
| `unread` | `integer` | yes | minimum=0 |  |
| `actionable` | `integer` | yes | minimum=0 |  |

### Schema: OperationalLog {#schema-operationallog}

Type: `object`

Required fields: `id`, `type`, `component`, `severity`, `payload`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes | format="int64" |  |
| `type` | `string` | yes |  |  |
| `component` | `string` | yes |  |  |
| `severity` | `string` | yes |  |  |
| `actor_id` | `string` | no |  |  |
| `request_id` | `string` | no |  |  |
| `agent_id` | `string` | no |  |  |
| `session_id` | `string` | no |  |  |
| `proposal_id` | `string` | no |  |  |
| `transaction_id` | `string` | no |  |  |
| `payload` | object (any value) | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: OperationalLogs {#schema-operationallogs}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `OperationalLog` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: AuditEvent {#schema-auditevent}

Type: `object`

Required fields: `id`, `actor_id`, `action`, `entity_type`, `entity_id`, `payload`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes | format="int64" |  |
| `actor_id` | `string` | yes |  |  |
| `action` | `string` | yes |  |  |
| `entity_type` | `string` | yes |  |  |
| `entity_id` | `string` | yes |  |  |
| `request_id` | `string` | no |  |  |
| `reason` | `string` | no |  |  |
| `payload` | object (any value) | yes |  |  |
| `created_at` | `string` | yes | format="date-time" |  |

### Schema: AuditEvents {#schema-auditevents}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `AuditEvent` | yes |  |  |
| `next_cursor` | `string or null` | no |  |  |

### Schema: EffectiveSettings {#schema-effectivesettings}

Type: `object`

Required fields: `workspace_id`, `version`, `sections`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  |  |
| `version` | `integer` | yes | minimum=0 |  |
| `sections` | object (values of `EffectiveSettingsSection`) | yes |  |  |

### Schema: EffectiveSettingsSection {#schema-effectivesettingssection}

Type: `object`

Required fields: `values`, `source`, `version`, `restart_required`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `values` | object (any value) | yes |  |  |
| `source` | `string` | yes | enum=["default","workspace"] |  |
| `version` | `integer` | yes | minimum=0 |  |
| `restart_required` | `boolean` | yes |  |  |

### Schema: SettingsSchema {#schema-settingsschema}

Type: `object`

Required fields: `sections`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `sections` | object (any value) | yes |  |  |

### Schema: SettingsUpdate {#schema-settingsupdate}

Type: `object`


| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `expected_version` | `integer` | no | minimum=1 |  |
| `values` | object (any value) | no |  |  |
| `section` | `string` | no |  |  |

### Schema: SettingsPreflight {#schema-settingspreflight}

Type: `object`

Required fields: `allowed`, `version`, `confirmation_token`, `impact`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `allowed` | `boolean` | yes |  |  |
| `version` | `integer` | yes |  |  |
| `confirmation_token` | `string` | yes |  |  |
| `impact` | object (any value) | yes |  |  |

### Schema: LiveEventEnvelope {#schema-liveeventenvelope}

Type: `object`

Required fields: `event_id`, `workspace_id`, `sequence`, `type`, `occurred_at`, `payload`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `event_id` | `integer` | yes | format="int64" |  |
| `workspace_id` | `string` | yes |  |  |
| `sequence` | `integer` | yes | format="int64" |  |
| `type` | `string` | yes |  |  |
| `occurred_at` | `string` | yes | format="date-time" |  |
| `actor` | `string` | no |  |  |
| `entity_type` | `string` | no |  |  |
| `entity_id` | `string` | no |  |  |
| `payload` | object (any value) | yes |  |  |

### Schema: LiveEventControl {#schema-liveeventcontrol}

Type: `object`

Required fields: `kind`, `workspace_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `kind` | `string` | yes | enum=["hello","ping","resync_required"] |  |
| `schema_version` | `string` | no |  |  |
| `workspace_id` | `string` | yes |  |  |
| `current_sequence` | `integer` | no | format="int64" |  |
| `resumable` | `boolean` | no |  |  |
| `reason` | `string` | no |  |  |

### Schema: EventVersionMarker {#schema-eventversionmarker}

Type: `object`

Required fields: `count`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `count` | `integer` | yes | minimum=0 |  |
| `latest_at` | `string` | no | format="date-time" |  |

### Schema: EventSnapshot {#schema-eventsnapshot}

Type: `object`

Required fields: `workspace_id`, `sequence`, `generated_at`, `retention`, `version_markers`, `snapshot`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  |  |
| `sequence` | `integer` | yes | format="int64"; minimum=0 |  |
| `generated_at` | `string` | yes | format="date-time" |  |
| `retention` | `object` | yes |  |  |
| `retention.mode` | `string` | yes |  |  |
| `retention.resume_by` | `string` | yes |  |  |
| `retention.max_replay_events` | `integer` | yes | minimum=1 |  |
| `retention.resync_endpoint` | `string` | yes |  |  |
| `version_markers` | object (values of `EventVersionMarker`) | yes |  |  |
| `snapshot` | `object` | yes |  |  |
| `snapshot.workspace` | `Workspace` | yes |  |  |
| `snapshot.counts` | object (values of `integer`) | yes |  |  |
| `snapshot.run_state` | `string` | yes |  |  |

### Schema: ContentionResolutionInput {#schema-contentionresolutioninput}

Type: `object`

Required fields: `resolution`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `resolution` | `string` | yes | enum=["keep_owner","transfer","split","narrow","extend_lease","reject_contender"] |  |
| `reason` | `string` | no |  |  |

### Schema: ContentionResolution {#schema-contentionresolution}

Type: `object`

Required fields: `contention_id`, `state`, `resolution`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `contention_id` | `string` | yes |  |  |
| `state` | `string` | yes | enum=["resolved"] |  |
| `resolution` | `string` | yes |  |  |

### Schema: HealthResponse {#schema-healthresponse}

Type: `object`

Required fields: `status`, `version`, `request_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `status` | `string` | yes | enum=["ok"] |  |
| `version` | `string` | yes |  |  |
| `request_id` | `string` | yes |  |  |

### Schema: StatusResponse {#schema-statusresponse}

Type: `object`

Required fields: `status`, `version`, `api_version`, `time`, `request_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `status` | `string` | yes |  |  |
| `version` | `string` | yes |  |  |
| `api_version` | `string` | yes | enum=["v1"] |  |
| `time` | `string` | yes | format="date-time" |  |
| `request_id` | `string` | yes |  |  |

### Schema: Problem {#schema-problem}

Type: `object`

Required fields: `type`, `title`, `status`, `request_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `type` | `string` | yes | format="uri" |  |
| `title` | `string` | yes |  |  |
| `status` | `integer` | yes |  |  |
| `detail` | `string` | no |  |  |
| `instance` | `string` | no |  |  |
| `request_id` | `string` | yes |  |  |
| `code` | `string` | no |  |  |
| `metadata` | object (any value) | no |  |  |
