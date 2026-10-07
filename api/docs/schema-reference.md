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
| `display_name` | `string` | no |  | Human-readable workspace label; when blank during creation, the API derives it from the validated root directory name. |
| `root_alias` | `string` | no |  | Optional short operator-facing alias for the registered root; it does not change filesystem resolution. |
| `root_path` | `string` | yes |  | Filesystem directory to register as the workspace root; the server validates and canonicalizes it against its allowed-root configuration. |
| `default_branch` | `string` | no |  | Optional recorded default-branch metadata; setting it does not switch the repository or verify the branch exists. |

### Schema: WorkspacePatch {#schema-workspacepatch}

Type: `object`

Schema constraints: minProperties=1

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `display_name` | `string` | no |  | Replacement display label when the supplied value is nonblank; an empty value leaves the stored label unchanged. |
| `root_alias` | `string` | no |  | Replacement operator-facing alias when nonblank; an empty value leaves the stored alias unchanged. |
| `default_branch` | `string` | no |  | Replacement default-branch metadata when nonblank; does not switch the repository's current branch. |

### Schema: Workspace {#schema-workspace}

Type: `object`

Required fields: `id`, `display_name`, `canonical_repository_identity`, `status`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Stable identifier assigned when the workspace is registered. |
| `revision` | `integer` | no | minimum=1 | Monotonic workspace revision used by conditional updates such as If-Match; it is not a repository commit number. |
| `display_name` | `string` | yes |  | Human-readable workspace label. |
| `root_alias` | `string` | no |  | Optional short alias for display and operator selection. |
| `canonical_repository_identity` | `string` | yes |  | SHA-256 identifier derived from the canonical filesystem root path; moving the checkout can change this identity, and it is not derived from a Git remote URL. |
| `status` | `string` | yes | enum=["active","detached"] | Registry lifecycle state; detached means the workspace is no longer available for normal active workspace operations. |
| `default_branch` | `string` | no |  | Recorded default-branch metadata; does not imply the repository is currently checked out on this branch. |
| `created_at` | `string` | yes | format="date-time" | Workspace registration timestamp in RFC3339 date-time form. |
| `last_opened_at` | `string` | no | format="date-time" | Timestamp set at workspace creation and update by the current handlers; ordinary reads do not refresh it, and it is omitted when not recorded. |

### Schema: WorkspaceImpact {#schema-workspaceimpact}

Type: `object`

Required fields: `active_sessions`, `active_claims`, `open_proposals`, `active_transactions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `active_sessions` | `integer` | yes |  | Count of workspace sessions whose status is exactly active or running; not a subprocess liveness probe. |
| `active_claims` | `integer` | yes |  | Count of workspace claims with exact active status; expiry is not equivalent to a claim being transitioned out of active status. |
| `open_proposals` | `integer` | yes |  | Count of workspace proposals with exact pending or in_review status; other statuses are excluded. |
| `active_transactions` | `integer` | yes |  | Count of workspace transactions with exact pending or running status. |

### Schema: RepositoryChange {#schema-repositorychange}

Type: `object`

Required fields: `path`, `index`, `worktree`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `path` | `string` | yes |  | Repository-relative path reported by Git status; rename records use the destination path parsed by the handler. |
| `index` | `string` | yes | minLength=1; maxLength=1 | Single-character Git index status code for the staged side of this path. |
| `worktree` | `string` | yes | minLength=1; maxLength=1 | Single-character Git worktree status code for the unstaged side of this path. |

### Schema: RepositoryRemote {#schema-repositoryremote}

Type: `object`

Required fields: `name`, `url`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `name` | `string` | yes |  | Git remote name in the inspected repository. |
| `url` | `string` | yes |  | Remote URL reported by Git; may contain sensitive host or repository information. |

### Schema: ProtectedPathSummary {#schema-protectedpathsummary}

Type: `object`

Required fields: `changed`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `changed` | `integer` | yes | minimum=0 | Number of changed paths matching the API's protected-path prefixes (.roundtable, .github, and TASKS.ROUNDTABLE). |
| `paths` | array of `string` | no |  | Repository-relative changed paths counted as protected; omitted when no such paths were found. |

### Schema: RepositoryStatus {#schema-repositorystatus}

Type: `object`

Required fields: `workspace_id`, `branch`, `head_sha`, `dirty`, `ahead`, `behind`, `detached`, `remotes`, `index`, `protected_paths`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  | Identifier of the workspace whose registered repository was inspected. |
| `root_alias` | `string` | no |  | Optional display alias copied from the workspace registry. |
| `branch` | `string` | yes |  | Current branch name, or literal HEAD when Git reports detached HEAD. |
| `head_sha` | `string` | yes |  | Full object ID of the current HEAD commit. |
| `dirty` | `boolean` | yes |  | Whether Git porcelain status reported any staged or worktree changes. |
| `ahead` | `integer` | yes | minimum=0 | Commit count by which HEAD is ahead of its configured upstream; zero when detached or when upstream comparison fails. |
| `behind` | `integer` | yes | minimum=0 | Commit count by which HEAD is behind its configured upstream; zero when detached or when upstream comparison fails. |
| `detached` | `boolean` | yes |  | Whether the repository HEAD is detached. |
| `remotes` | array of `RepositoryRemote` | yes |  | Configured Git remotes, with duplicate fetch/push entries collapsed by remote name. |
| `index` | array of `RepositoryChange` | yes |  | Per-path index and worktree status codes from Git porcelain output. |
| `protected_paths` | `ProtectedPathSummary` | yes |  | Summary of changed paths under the API's protected-path prefixes. |

### Schema: BranchSwitchInput {#schema-branchswitchinput}

Type: object (no additional properties)

Required fields: `branch`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `branch` | `string` | yes | minLength=1 | Target local branch name for preflight or switch; handler rejects unsafe path-like, option-like, empty, or newline-containing values. |

### Schema: RepositoryBlocker {#schema-repositoryblocker}

Type: `object`

Required fields: `code`, `message`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `code` | `string` | yes |  | Stable machine-readable reason this preflight condition blocks a switch. |
| `message` | `string` | yes |  | Human-readable summary of the blocking repository or workspace condition. |
| `count` | `integer` | no | minimum=0 | Count associated with a record-based blocker; omitted when the blocker is boolean or does not expose a count. |

### Schema: BranchSwitchPreflight {#schema-branchswitchpreflight}

Type: `object`

Required fields: `workspace_id`, `target_branch`, `allowed`, `blockers`, `repository`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  | Workspace whose repository and governance state were checked. |
| `target_branch` | `string` | yes |  | Validated target branch name considered by this preflight. |
| `allowed` | `boolean` | yes |  | True only when the current preflight produced no dirty-worktree, active-transaction, active-claim, open-proposal, active-session, or already-on-branch blocker. |
| `blockers` | array of `RepositoryBlocker` | yes |  | Current reasons that prevent switching; this is a point-in-time assessment and must be recomputed before mutation. |
| `repository` | `RepositoryStatus` | yes |  | Repository state inspected during this preflight. |

### Schema: BranchSwitchResponse {#schema-branchswitchresponse}

Type: `object`

Required fields: `workspace`, `before`, `after`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace` | `Workspace` | yes |  | Updated workspace registry record after the branch switch was recorded. |
| `before` | `RepositoryStatus` | yes |  | Repository status captured immediately before the switch. |
| `after` | `RepositoryStatus` | yes |  | Repository status inspected after the switch completed. |

### Schema: RepositoryEntity {#schema-repositoryentity}

Type: `object`

Required fields: `id`, `type`, `path`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Stable entity identifier, generally derived from the entity category and repository path or symbol identity. |
| `type` | `string` | yes |  | Repository entity category used by the discovery projection. |
| `path` | `string` | yes |  | Repository-relative path associated with the entity. |
| `name` | `string` | no |  | Entity name when a symbol or named repository artifact is available. |
| `language` | `string` | no |  | Detected source language for entities backed by recognized source files. |
| `kind` | `string` | no |  | Parser/indexer classification for a discovered symbol; absent when the entity is not a symbol. |
| `start_line` | `integer` | no | minimum=0 | One-based starting line for a discovered symbol when available; zero can indicate no indexed span. |
| `end_line` | `integer` | no | minimum=0 | One-based ending line for a discovered symbol when available; zero can indicate no indexed span. |
| `claim_status` | `string` | no |  | Claim status associated with this entity when a matching resource record exists. |
| `related_proposals` | array of `string` | no |  | Proposal identifiers linked to this repository entity by the API projection. |
| `related_entities` | array of `string` | no |  | Identifiers of other entities linked by the current repository discovery projection. |

### Schema: RepositoryEntities {#schema-repositoryentities}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `RepositoryEntity` | yes |  | Repository entities returned for the requested workspace and page. |
| `next_cursor` | `string or null` | no |  | Opaque continuation cursor for the next page, or null when no further page is available. |

### Schema: Agent {#schema-agent}

Type: `object`

Required fields: `agent_id`, `display_name`, `adapter_type`, `enabled`, `health`, `capabilities`, `supports_resume`, `supports_mcp`, `concurrency_limit`, `policy_weight`, `active_session_count`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `agent_id` | `string` | yes |  | Identifier of the global agent registry row returned through the workspace-scoped API route. |
| `display_name` | `string` | yes |  | Configured display name stored on the agent record. |
| `adapter_type` | `string` | yes |  | Adapter name stored on the agent record; capability fields below are currently projected by exact built-in name checks. |
| `enabled` | `boolean` | yes |  | Persisted agent enablement flag; does not prove that an external process is running. |
| `health` | `string` | yes |  | Stored agent status, with an empty value projected as unknown; not an executable or heartbeat probe. |
| `executable_version` | `string` | no |  | Optional executable version field; the current agent response projection does not populate it. |
| `capabilities` | object (values of `boolean`) | yes |  | Capability labels currently derived from exact adapter-name cases, not dynamically inspected from configured adapter metadata or enforced as OS restrictions. |
| `supports_resume` | `boolean` | yes |  | Hardcoded response capability: true for codex, claude, and gemini; false for other adapter names, regardless of custom configuration. |
| `supports_mcp` | `boolean` | yes |  | Hardcoded response capability: true for codex, claude, gemini, and opencode; false for other adapter names. |
| `concurrency_limit` | `integer` | yes | minimum=1 | Advertised as 1 by the current handler; this response value does not enforce session scheduling or process concurrency. |
| `policy_weight` | `integer` | yes | minimum=0 | Advertised as 1 by the current handler; this is not a dynamically loaded policy weight or enforced scheduling limit. |
| `active_session_count` | `integer` | yes | minimum=0 | Global count of this agent's sessions whose stored status is exactly active or running; it is not workspace-filtered or checked against process liveness. |
| `last_heartbeat` | `string` | no | format="date-time" | Optional heartbeat timestamp; not populated by the current agent response projection. |

### Schema: DiagnosticCheck {#schema-diagnosticcheck}

Type: `object`

Required fields: `name`, `status`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `name` | `string` | yes |  | Diagnostic check identifier, such as executable_version, mcp_surface, or read_only_boundary. |
| `status` | `string` | yes | enum=["passed","failed"] | Outcome of this individual probe; a passed informational check is not proof of provider process execution. |
| `detail` | `string` | no |  | Optional probe result or sanitized failure summary; omitted when the check has no detail. |

### Schema: DiagnosticRun {#schema-diagnosticrun}

Type: `object`

Required fields: `run_id`, `agent_id`, `started_at`, `finished_at`, `status`, `checks`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `run_id` | `string` | yes |  | Timestamp-derived diagnostic record ID; it is stored as a test-run ID and is not the coordinator run ID. |
| `agent_id` | `string` | yes |  | Global agent registry identifier probed by this diagnostic request. |
| `started_at` | `string` | yes | format="date-time" | UTC timestamp captured immediately before running the diagnostic checks. |
| `finished_at` | `string` | yes | format="date-time" | UTC timestamp captured after the checks complete. |
| `status` | `string` | yes | enum=["passed","failed"] | Aggregate diagnostic outcome; failed if any individual check reports failed. |
| `checks` | array of `DiagnosticCheck` | yes |  | Results for the adapter version executable probe, MCP registry visibility check, and declared read-only boundary. |
| `stdout` | `string` | no |  | Optional bounded excerpt of combined adapter version-probe output after limited line-based secret filtering. |
| `stderr` | `string` | no |  | Optional standard-error field; the current diagnostic implementation does not populate it. |
| `remediation_hints` | array of `string` | no |  | Fixed suggested follow-up text; hints are not a complete diagnosis or proof that the suggested remediation is appropriate. |

### Schema: SessionInput {#schema-sessioninput}

Type: object (no additional properties)

Required fields: `agent_id`, `run_id`, `adapter`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `agent_id` | `string` | yes |  | Existing global agent identifier to associate with the created session. |
| `run_id` | `string` | yes |  | Caller-supplied run identifier stored on the session; the handler does not verify that the run belongs to this workspace. |
| `adapter` | `string` | yes |  | Caller-supplied adapter label; it is not checked against the agent's configured adapter or launched as a process. |
| `provider` | `string` | no |  | Optional provider label retained as session metadata; does not select or invoke a provider executable. |
| `model` | `string` | no |  | Optional model label retained as session metadata; does not verify model availability. |
| `external_session_id` | `string` | no |  | Optional external CLI session identifier supplied by the caller; not discovered by this endpoint. |

### Schema: Session {#schema-session}

Type: `object`

Required fields: `id`, `agent_id`, `run_id`, `adapter`, `status`, `started_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Timestamp-derived session identifier assigned by the API; it is not the external provider session ID. |
| `agent_id` | `string` | yes |  | Global agent registry identifier associated with this session. |
| `run_id` | `string` | yes |  | Run identifier copied from the creation request; not independently resolved or verified against workspace membership. |
| `adapter` | `string` | yes |  | Adapter label copied from the creation request; no CLI process is launched. |
| `provider` | `string` | no |  | Optional provider metadata copied from the request; omitted when empty. |
| `model` | `string` | no |  | Optional model metadata copied from the request; omitted when empty. |
| `external_session_id` | `string` | no |  | Optional externally managed session identifier; omitted when none was supplied. |
| `resume_command_template` | `string` | no |  | Generic `<adapter> resume <external_session_id>` display template; not taken from adapter configuration and not executed by the API. |
| `status` | `string` | yes | enum=["starting","active","running","paused","resuming","stopped","terminated"] | Persisted session lifecycle label; state transitions are control-plane records and do not themselves start, pause, resume, or kill a process. |
| `started_at` | `string` | yes | format="date-time" | Session creation timestamp in UTC RFC3339Nano form. |
| `last_seen_at` | `string` | no | format="date-time" | Most recent stored session activity/heartbeat timestamp; does not establish process liveness. |
| `ended_at` | `string` | no | format="date-time" | Stored end timestamp when a transition sets one; omitted when absent. |

### Schema: Sessions {#schema-sessions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Session` | yes |  | Workspace-associated session rows returned for this page. |
| `next_cursor` | `string or null` | no |  | Opaque base64url-encoded offset cursor for the next page, or null when no further page is available. |

### Schema: SessionTerminationInput {#schema-sessionterminationinput}

Type: object (no additional properties)

Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `impact_details` | `string` | no |  | Optional human-supplied impact acknowledgment; termination requires nonblank text when the agent has active claims, but the handler does not persist or analyze this text. |

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
| `goal` | `string` | yes |  | Required deliberation goal; trimmed and stored as the deliberation title and metadata goal. |
| `agent_pool` | array of `string` | yes | minItems=1 | Required nonempty list of existing global agent IDs; values are trimmed and persisted as participant rows, without checking enablement or launching agents. |
| `governance_profile` | `string` | no |  | Optional profile label retained in deliberation metadata; this endpoint does not itself load or enforce the named policy profile. |
| `initial_resource_scope` | array of `string` | no |  | Optional resource identifiers retained as deliberation metadata; not a filesystem permission boundary. |
| `budget_limit` | `integer` | no | minimum=0 | Optional nonnegative budget metadata; persistence does not enforce spending or token limits. |
| `time_limit_seconds` | `integer` | no | minimum=0 | Optional nonnegative time-limit metadata; persistence does not schedule automatic timeout or termination. |
| `human_constraints` | array of `string` | no |  | Optional human-provided constraints retained in metadata; this endpoint does not independently evaluate compliance. |
| `moderator_id` | `string` | no |  | Optional moderator label stored with the deliberation; it is not authenticated or launched as an agent. |

### Schema: Deliberation {#schema-deliberation}

Type: `object`

Required fields: `id`, `workspace_id`, `goal`, `status`, `created_by`, `agent_pool`, `participants`, `round`, `unresolved_conflicts`, `linked_proposals`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Timestamp-derived deliberation identifier; the record is workspace-scoped. |
| `workspace_id` | `string` | yes |  | Workspace registry identifier that scopes reads and lifecycle changes for this deliberation. |
| `goal` | `string` | yes |  | Trimmed goal text stored as the deliberation title and returned as its goal. |
| `status` | `string` | yes | enum=["draft","running","paused","terminated"] | Persisted control-plane lifecycle state; it does not prove that agents are executing or enforce budget/time metadata. |
| `created_by` | `string` | yes |  | Actor ID attributed to the creation request; it is caller-supplied identity metadata. |
| `moderator_id` | `string` | no |  | Optional moderator label from creation metadata; omitted when empty and not proof of an active moderator process. |
| `agent_pool` | array of `string` | yes |  | Agent IDs projected from the deliberation participant rows; currently the same set as participants. |
| `participants` | array of `string` | yes |  | Persisted participant agent IDs, returned in sorted order; participation does not launch or authenticate agents. |
| `governance_profile` | `string` | no |  | Optional profile label recovered from metadata; omitted when absent and not evidence that the profile was applied. |
| `initial_resource_scope` | array of `string` | no |  | Optional resource identifiers recovered from metadata; omitted when absent and not an access-control boundary. |
| `budget_limit` | `integer` | no |  | Optional budget value recovered from metadata; zero may be omitted and no spend enforcement is implied. |
| `time_limit_seconds` | `integer` | no |  | Optional time limit recovered from metadata; zero may be omitted and no automatic timeout is implemented by this record. |
| `human_constraints` | array of `string` | no |  | Optional constraint strings recovered from metadata with text redaction; omitted when absent. |
| `round` | `integer` | yes | minimum=0 | Maximum round number stored for this deliberation, defaulting to zero; it is not a count of completed discussions. |
| `unresolved_conflicts` | array of `string` | yes |  | Summaries of conflicts whose stored status is exactly open, in creation order, with text redaction applied. |
| `linked_proposals` | array of `string` | yes |  | Proposal identifiers associated with this deliberation; the current response reader does not populate this field, so it may serialize as null/empty despite being required by the schema. |
| `started_at` | `string` | no | format="date-time" | Timestamp set on the first transition into running; subsequent resumes preserve the original start time. |
| `ended_at` | `string` | no | format="date-time" | Timestamp set when the deliberation is terminated; omitted before termination. |
| `created_at` | `string` | yes | format="date-time" | UTC timestamp recorded when the deliberation row was created. |
| `updated_at` | `string` | yes | format="date-time" | UTC timestamp last written by a lifecycle transition. |

### Schema: Deliberations {#schema-deliberations}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Deliberation` | yes |  | Workspace deliberations ordered by creation time and ID before in-memory pagination. |
| `next_cursor` | `string or null` | no |  | Opaque base64url-encoded offset cursor for the next page, or null when no further page is available. |

### Schema: DeliberationMessageInput {#schema-deliberationmessageinput}

Type: object (no additional properties)

Required fields: `body`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `message_type` | `string` | no | default="human_instruction" | Optional message category; an empty or omitted value becomes human_instruction, while other nonempty labels are stored without enum validation. |
| `body` | `string` | yes |  | Required nonblank message content; the handler applies text redaction before persisting and returning it. |

### Schema: TranscriptEntry {#schema-transcriptentry}

Type: `object`

Required fields: `id`, `round`, `actor`, `kind`, `visibility_class`, `rendering`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Source-prefixed message:<id> or event:<numeric-id>; the detail endpoint also accepts the unprefixed source ID. |
| `round` | `integer` | yes | minimum=0 | Current deliberation round copied onto every projected transcript row, not a historical per-entry round lookup. |
| `actor` | `string` | yes |  | Redacted actor label from the message or event record. |
| `kind` | `string` | yes | enum=["message","event"] | Source record category: human/agent deliberation message or operational event. |
| `message_type` | `string` | no |  | Redacted message category for message entries; omitted for event entries. |
| `summary` | `string` | no |  | Redacted event type for operational event entries; omitted for message entries. |
| `content` | `string` | no |  | Redacted message body for message entries; operational event payloads are not exposed as transcript content. |
| `tool_references` | array of `string` | no |  | References heuristically extracted from selected string fields in an event JSON payload; not a complete tool-call trace. |
| `claim_references` | array of `string` | no |  | Claim references heuristically extracted from event payload claim/claim_id string fields. |
| `proposal_references` | array of `string` | no |  | Proposal references heuristically extracted from event payload proposal/proposal_id string fields. |
| `vote_references` | array of `string` | no |  | Vote references heuristically extracted from event payload vote/vote_id string fields. |
| `visibility_class` | `string` | yes | enum=["user_visible","operational"] | Fixed projection label: messages are user_visible and event records are operational; it is not a complete authorization or redaction policy. |
| `rendering` | object (any value) | yes |  | Fixed rendering hints currently declaring Markdown format, HTML disallowed, and links untrusted. |
| `created_at` | `string` | yes | format="date-time" | Source record creation timestamp used to merge messages and events into transcript order. |

### Schema: TranscriptEntries {#schema-transcriptentries}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `TranscriptEntry` | yes |  | Merged message and operational-event transcript entries sorted by creation time, then entry ID. |
| `next_cursor` | `string or null` | no |  | Opaque base64url-encoded offset cursor for the next page, or null when no further page is available. |

### Schema: ProposalInput {#schema-proposalinput}

Type: object (no additional properties)

Required fields: `title`, `summary`, `patch_path`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | no |  | Optional caller-supplied proposal identifier; when empty the server generates a timestamp-based ID. Repeating the same request with its idempotency key replays the middleware-cached response; a conflicting identifier can still fail at persistence. |
| `deliberation_id` | `string` | no |  | Optional deliberation association stored as supplied; creation does not verify that a corresponding deliberation exists. |
| `proposer_session_id` | `string` | no |  | Optional session identifier recorded as proposal provenance; creation does not validate that the session exists. |
| `title` | `string` | yes |  | Required nonblank proposal title; surrounding whitespace is trimmed and the returned value is text-redacted. |
| `summary` | `string` | yes |  | Required nonblank proposal summary; redaction is applied before persistence and again when read. |
| `patch_path` | `string` | yes |  | Required repository-relative patch artifact path. Creation rejects absolute paths and any path containing '..'; patch reads apply workspace-root containment checks. |
| `base_revision` | `string` | no |  | Optional repository revision the proposed patch is based on; stored as evidence and compared with current HEAD by patch metadata, not enforced during proposal creation. |
| `risk` | `string` | no | enum=["normal","high"] | Proposal risk label; omission defaults to normal. The handler stores other supplied strings without validating the documented enum. |
| `resource_ids` | array of `string` | no |  | Optional resource identifiers associated with the proposal in the same database transaction; referenced resources are not validated here. |

### Schema: Proposal {#schema-proposal}

Type: `object`

Required fields: `proposal_id`, `workspace_id`, `title`, `summary`, `patch_path`, `state`, `vote_state`, `policy_state`, `approval_state`, `transaction_state`, `risk`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | yes |  | Stable proposal identifier assigned on creation or supplied by the caller. |
| `workspace_id` | `string` | yes |  | Workspace that owns the proposal; reads are scoped to this workspace. |
| `deliberation_id` | `string` | no |  | Optional recorded deliberation association; empty values are omitted from JSON. |
| `proposer_session_id` | `string` | no |  | Optional recorded proposer session; empty values are omitted from JSON. |
| `title` | `string` | yes |  | Proposal title after text redaction on read. |
| `summary` | `string` | yes |  | Proposal summary after text redaction on read. |
| `patch_path` | `string` | yes |  | Workspace-relative patch artifact path after text redaction on read. |
| `base_revision` | `string` | no |  | Optional revision recorded as the patch base; empty values are omitted. |
| `state` | `string` | yes | enum=["pending","in_review","accepted","rejected","withdrawn"] | Lifecycle state stored on the proposal; supported human actions move pending to in_review, rejected, or withdrawn, and in_review to rejected or withdrawn. |
| `vote_state` | `string` | yes |  | Stored vote projection state, defaulting to pending when the database value is null; this read field is not recalculated as part of proposal retrieval. |
| `policy_state` | `string` | yes |  | Stored policy projection state, defaulting to pending when the database value is null. |
| `approval_state` | `string` | yes |  | Stored approval projection state, defaulting to pending when the database value is null. |
| `transaction_state` | `string` | yes |  | Stored transaction projection state, defaulting to not_started when the database value is null. |
| `risk` | `string` | yes |  | Risk label persisted at creation; the current handler defaults omission to normal but does not validate the input against the documented values. |
| `created_at` | `string` | yes | format="date-time" | Proposal creation timestamp returned from its stored record. |
| `updated_at` | `string` | yes | format="date-time" | Most recent lifecycle update timestamp, falling back to created_at when the stored update value is null. |

### Schema: Proposals {#schema-proposals}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Proposal` | yes |  | Proposals owned by the selected workspace, ordered by creation time and ID before pagination. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next response page, or null when no further page is available. |

### Schema: PatchMetadata {#schema-patchmetadata}

Type: `object`

Required fields: `proposal_id`, `patch_path`, `current_head`, `stale_base`, `file_count`, `added_lines`, `removed_lines`, `binary_files`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | yes |  | Proposal whose patch artifact was loaded. |
| `patch_path` | `string` | yes |  | Stored workspace-relative path used to read the patch artifact. |
| `base_revision` | `string` | no |  | Recorded base revision; omitted when empty. |
| `current_head` | `string` | yes |  | HEAD revision observed from repository inspection; inspection errors are ignored and can therefore produce an empty value. |
| `stale_base` | `boolean` | yes |  | True only when a nonempty recorded base revision differs from the observed HEAD; this is an informational comparison, not a freshness guarantee. |
| `file_count` | `integer` | yes | minimum=0 | Number of unique touched file paths parsed from the patch. |
| `added_lines` | `integer` | yes | minimum=0 | Current parser projection counts parsed change ranges (hunk headers), not individual added lines; treat as approximate metadata. |
| `removed_lines` | `integer` | yes | minimum=0 | Current projection increments once per deleted file, not once per removed line; it is not a removed-line total. |
| `binary_files` | `integer` | yes | minimum=0 | Reserved binary-file count; the current metadata handler leaves this at zero. |

### Schema: PatchFile {#schema-patchfile}

Type: `object`

Required fields: `path`, `operation`, `binary`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `path` | `string` | yes |  | Primary file path, using the new path when present and otherwise the old path. |
| `old_path` | `string` | no |  | Old path from the parsed patch header; omitted when absent. |
| `new_path` | `string` | no |  | New path from the parsed patch header; omitted when absent. |
| `operation` | `string` | yes | enum=["create","modify","delete","rename"] | Operation inferred from parsed new, deleted, and rename header flags; otherwise reported as modify. |
| `binary` | `boolean` | yes |  | Whether the raw patch contains a matching conventional 'Binary files a/... b/...' marker; this is a textual heuristic. |
| `added_lines` | `integer` | no |  | Currently returned as zero; per-file added-line counts are not populated by this handler. |
| `removed_lines` | `integer` | no |  | Currently returned as zero; per-file removed-line counts are not populated by this handler. |

### Schema: PatchFiles {#schema-patchfiles}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `PatchFile` | yes |  | File-change headers parsed from the patch artifact; patch application or semantic validity is not established by this listing. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next response page, or null when no further page is available. |

### Schema: PatchDiff {#schema-patchdiff}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `string` | yes |  | Redacted patch text split into newline-delimited strings; pagination is applied to those lines. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next response page, or null when no further page is available. |

### Schema: PatchSymbol {#schema-patchsymbol}

Type: `object`

Required fields: `path`, `resource_id`, `kind`, `name`, `start_line`, `end_line`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `path` | `string` | yes |  | Indexed workspace path for the symbol, limited to currently existing files touched by the patch. |
| `resource_id` | `string` | yes |  | Resource identifier produced by the symbol indexer for this symbol. |
| `kind` | `string` | yes |  | Language/indexer-specific symbol kind. |
| `name` | `string` | yes |  | Symbol name reported by the indexer. |
| `start_line` | `integer` | yes |  | One-based start line from indexing the current workspace file, not a projected post-patch location. |
| `end_line` | `integer` | yes |  | One-based end line from indexing the current workspace file, not a projected post-patch location. |

### Schema: PatchSymbols {#schema-patchsymbols}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `PatchSymbol` | yes |  | Symbols discovered in current workspace contents of parsed touched files; unsafe paths and indexing failures are skipped, and proposed patch contents are not indexed. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next response page, or null when no further page is available. |

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

Type: object (no additional properties)

Required fields: `agent_id`, `task_id`, `resource_type`, `mode`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `claim_id` | `string` | no |  | Optional claim identifier; omission generates a timestamp-based claim ID. |
| `agent_id` | `string` | yes |  | Required nonblank agent identity recorded as the claim owner. |
| `session_id` | `string` | no |  | Accepted by the request decoder but currently not passed to claim persistence or returned by the handler. |
| `task_id` | `string` | yes |  | Required nonblank task identifier associated with the claim. |
| `resource_id` | `string` | no |  | Optional explicit resource key. If omitted, the service derives one from resource_type and path, or from resource_type and symbol for symbol claims; a resource_id or path is required. |
| `resource_type` | `string` | yes | enum=["file","directory","symbol","command","schema","endpoint","test_suite","custom"] | Required nonblank resource category. The service does not enforce this documented enum, so other nonempty categories may be stored; file, directory, and symbol receive specialized resource handling. |
| `path` | `string` | no |  | Optional workspace-relative resource path. When nonempty, the HTTP handler rejects paths escaping the selected workspace; resource_id may be supplied instead. |
| `symbol` | `string` | no |  | Optional symbol name used to derive a symbol resource ID and resolve symbol-specific resource metadata. |
| `mode` | `string` | yes | enum=["read","write","review","exclusive"] | Required claim mode. The service accepts read, write, review, or exclusive; the former OpenAPI values shared, advisory, and execution are not accepted by this implementation. |
| `run_id` | `string` | no |  | Optional run identifier forwarded to claim creation and related events. |
| `base_hash` | `string` | no |  | Optional caller-provided resource revision/hash evidence stored on the claim; creation does not verify it against current contents. |
| `ttl_seconds` | `integer` | no | minimum=1 | Optional requested lease duration in seconds; omission or a nonpositive value falls back to a 15-minute lease. |
| `rationale` | `string` | no |  | Optional rationale stored after text redaction. |

### Schema: ClaimExtendInput {#schema-claimextendinput}

Type: `object`


| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `ttl_seconds` | `integer` | no | minimum=1 | Requested new lease duration in seconds. Missing, invalid, or nonpositive values currently reach the service as zero and use its 15-minute default. |

### Schema: Claim {#schema-claim}

Type: `object`

Required fields: `id`, `workspace_id`, `agent_id`, `task_id`, `resource_id`, `resource_type`, `mode`, `state`, `acquired_at`, `lease_expires_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Stable claim identifier. |
| `workspace_id` | `string` | yes |  | Workspace to which the API binds this claim after creation; list and detail reads are scoped by this value. |
| `agent_id` | `string` | yes |  | Agent identity that owns the claim. |
| `session_id` | `string` | no |  | The response model has an optional session field, but the current HTTP mapper never populates it, so this field is omitted from current claim responses. |
| `task_id` | `string` | yes |  | Task associated with the claim and used by claim transition operations. |
| `resource_id` | `string` | yes |  | Canonical resource identifier protected by this claim. |
| `resource_type` | `string` | yes |  | Resource category loaded from the resource record, not copied directly from the claim row. |
| `path` | `string` | no |  | Resource path loaded from the resource record; omitted when empty. |
| `symbol` | `string` | no |  | Resource symbol name loaded from the resource record; omitted when empty. |
| `mode` | `string` | yes |  | Stored claim type/mode (read, write, review, or exclusive). |
| `base_hash` | `string` | no |  | Optional hash evidence supplied at claim creation; omitted when empty. |
| `state` | `string` | yes | enum=["active","released","revoked","suspended","expired"] | Persisted claim status. Creation returns active; leases can later be released or expire during maintenance/reconciliation. |
| `rationale` | `string` | no |  | Text-redacted rationale; omitted when empty. |
| `acquired_at` | `string` | yes | format="date-time" | Claim creation time returned as the acquisition timestamp. |
| `lease_expires_at` | `string` | yes | format="date-time" | Lease expiration timestamp; creation defaults to 15 minutes when no positive TTL is provided. |

### Schema: Claims {#schema-claims}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Claim` | yes |  | Claims associated with the selected workspace, ordered by the underlying store before page slicing. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next page, or null when there are no further items. |

### Schema: Contention {#schema-contention}

Type: `object`

Required fields: `id`, `workspace_id`, `resource_id`, `requested_resource_id`, `requested_agent_id`, `requested_task_id`, `requested_mode`, `current_owner_claim_id`, `current_owner_agent_id`, `state`, `reason`, `acquired_at`, `lease_expires_at`, `allowed_resolutions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Contention record identifier created when an attempted claim conflicts with an existing claim. |
| `workspace_id` | `string` | yes |  | Workspace inferred from the challenged owner claim and used to scope the contention. |
| `resource_id` | `string` | yes |  | Resource identifier from the attempted claim that encountered a conflict; it can differ from the incumbent claim's resource for overlapping resources. |
| `requested_resource_id` | `string` | yes |  | Resource identifier requested by the contender; it may equal resource_id for a direct conflict. |
| `requested_agent_id` | `string` | yes |  | Agent identity that attempted to acquire the conflicting claim. |
| `requested_task_id` | `string` | yes |  | Task identifier supplied for the attempted claim. |
| `requested_mode` | `string` | yes |  | Claim mode requested by the contender. |
| `requested_path` | `string` | no |  | Optional resource path supplied by the contender; omitted when empty. |
| `current_owner_claim_id` | `string` | yes |  | Existing claim challenged by the request. |
| `current_owner_agent_id` | `string` | yes |  | Agent currently associated with the challenged claim. |
| `state` | `string` | yes |  | Contention status, normally open until a supported human resolution records it as resolved. |
| `reason` | `string` | yes |  | Conflict explanation recorded by claim conflict detection. |
| `acquired_at` | `string` | yes | format="date-time" | Contention creation timestamp. |
| `lease_expires_at` | `string` | yes | format="date-time" | Expiration timestamp of the challenged owner claim; this is not the contender's requested lease. |
| `allowed_resolutions` | array of `string` | yes |  | Currently advertised values are keep_owner, transfer, extend_lease, and reject_contender. keep_owner and reject_contender close the contention without changing the incumbent claim; transfer assigns its owner/task/mode to the requester; extend_lease adds 15 minutes. The resolver also recognizes split and narrow but rejects them because resource-specific semantics are not implemented. |

### Schema: Contentions {#schema-contentions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Contention` | yes |  | Contentions associated with owner claims in the selected workspace, optionally filtered by resource_id and ordered by creation time and ID. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next page, or null when there are no further items. |

### Schema: Vote {#schema-vote}

Type: `object`

Required fields: `id`, `proposal_id`, `agent_id`, `decision`, `policy_weight`, `policy_version`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Vote row identifier; a caller-supplied identifier can update an existing row with the same ID. |
| `proposal_id` | `string` | yes |  | Proposal receiving this vote. |
| `agent_id` | `string` | yes |  | Agent identity attributed to the vote; the server derives policy weight from the stored agent role, or uses weight 1 when unavailable. |
| `session_id` | `string` | no |  | Optional session attributed to the vote; omitted when empty. |
| `decision` | `string` | yes | enum=["approve","reject","abstain","veto"] | Normalized decision used in consensus aggregation. |
| `policy_weight` | `integer` | yes | minimum=1 | Weight stored when the vote is cast: security role 3, architect or reviewer role 2, and all other or missing roles 1. |
| `policy_version` | `string` | yes |  | Policy label recorded for the vote; the current implementation uses default-v1. |
| `rationale` | `string` | no |  | Text-redacted rationale; omitted when empty. |
| `confidence` | `number` | no | minimum=0; maximum=1 | Optional caller-provided confidence; the server does not currently enforce the documented numeric range. |
| `created_at` | `string` | yes | format="date-time" | Stored vote creation timestamp used for stable list ordering. |

### Schema: Votes {#schema-votes}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Vote` | yes |  | Votes for the workspace-scoped proposal, ordered by creation timestamp and ID before pagination. |
| `next_cursor` | `string or null` | no |  | Opaque cursor for the next response page, or null when no further page is available. |

### Schema: VoteInput {#schema-voteinput}

Type: object (no additional properties)

Required fields: `agent_id`, `decision`, `rationale`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | no |  | Optional vote row ID; when omitted a timestamp-based ID is generated. Reusing an ID updates that vote row. |
| `agent_id` | `string` | yes |  | Required nonblank agent identifier used for attribution and role-based weight lookup; existence is not required for a default weight of 1. |
| `session_id` | `string` | no |  | Optional session attribution stored with the vote; surrounding whitespace is trimmed. |
| `decision` | `string` | yes | enum=["approve","reject","abstain","veto"] | Required decision; the handler trims and lowercases it before checking the four supported values. |
| `rationale` | `string` | yes |  | Required nonblank rationale, text-redacted before persistence and in the response. |
| `confidence` | `number` | no | minimum=0; maximum=1 | Optional confidence value persisted with the vote; the handler currently does not enforce the documented 0-to-1 range. |

### Schema: VoteResult {#schema-voteresult}

Type: `object`

Required fields: `vote`, `consensus`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `vote` | `Vote` | yes |  | The recorded or updated vote with server-derived policy weight and version. |
| `consensus` | `Consensus` | yes |  | Consensus recalculated from stored votes in the same transaction as this vote. |

### Schema: Consensus {#schema-consensus}

Type: `object`

Required fields: `proposal_id`, `outcome`, `approve_weight`, `reject_weight`, `abstain_weight`, `veto_weight`, `quorum`, `threshold`, `votes_cast`, `policy_version`, `explanation`, `calculated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `proposal_id` | `string` | yes |  | Proposal whose stored votes were aggregated. |
| `outcome` | `string` | yes | enum=["pending","approved","blocked"] | Approved requires approve weight at least the threshold, no rejects or vetoes, and votes cast at least quorum; any reject or veto blocks; otherwise outcome is pending. |
| `approve_weight` | `integer` | yes | minimum=0 | Sum of stored policy weights for approve votes. |
| `reject_weight` | `integer` | yes | minimum=0 | Sum of stored policy weights for reject votes. |
| `abstain_weight` | `integer` | yes | minimum=0 | Sum of stored policy weights for abstain votes; abstentions contribute to votes_cast but not approval. |
| `veto_weight` | `integer` | yes | minimum=0 | Sum of stored policy weights for veto votes; any veto blocks approval. |
| `quorum` | `integer` | yes | minimum=1 | Minimum number of vote rows required for approval; currently fixed at 2 and counts abstentions. |
| `threshold` | `integer` | yes | minimum=1 | Minimum aggregate approve weight required for approval; currently fixed at 2. |
| `votes_cast` | `integer` | yes | minimum=0 | Number of stored vote rows across all decisions, including abstain. |
| `policy_version` | `string` | yes |  | Policy version from the last row observed during aggregation; it is not a single validated version for the entire vote set. |
| `explanation` | `string` | yes |  | Human-readable summary assembled from the aggregate weights and outcome. |
| `calculated_at` | `string` | yes | format="date-time" | UTC timestamp when this response calculation ran; it is not the time of the most recent vote. |

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
| `id` | `string` | yes |  | Stable policy identifier, supplied at creation or generated by the server. |
| `workspace_id` | `string` | yes |  | Owning workspace used to scope policy reads and lifecycle changes. |
| `name` | `string` | yes |  | Display name; creation requires a nonblank value. |
| `status` | `string` | yes | enum=["disabled","active"] | Enforcement lifecycle state; new policies and clones start disabled, and enable/publish activate them. |
| `current_revision_id` | `string` | no |  | Identifier of the current draft or published revision; omitted when no value is stored. |
| `scope` | `string` | yes |  | Scope label stored with the policy; omission on creation defaults to workspace. |
| `selector` | object (any value) | yes |  | Arbitrary JSON selector attributes persisted with the policy; interpretation depends on policy evaluation rules. |
| `severity` | `string` | yes |  | Severity label; omission on creation defaults to normal. |
| `enforcement_mode` | `string` | yes |  | Enforcement mode label; omission on creation defaults to advisory. |
| `human_approval_required` | `boolean` | yes |  | Policy setting indicating whether evaluation requires human approval. |
| `metadata` | object (any value) | yes |  | Arbitrary JSON metadata stored separately from the policy revision definition. |
| `created_at` | `string` | yes | format="date-time" | Policy creation timestamp. |
| `updated_at` | `string` | yes | format="date-time" | Timestamp of the latest policy-level update; draft/revision lifecycle actions may update this value. |

### Schema: Policies {#schema-policies}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Policy` | yes |  | Workspace policies ordered by creation time and ID before pagination. |
| `next_cursor` | `string or null` | no |  | Opaque pagination cursor or null when no further page is available. |

### Schema: PolicyInput {#schema-policyinput}

Type: object (no additional properties)

Required fields: `name`, `definition`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | no |  | Optional policy identifier; omission generates a timestamp-based identifier. |
| `name` | `string` | yes |  | Required nonblank policy name. |
| `scope` | `string` | no |  | Optional policy scope label; defaults to workspace when empty. |
| `selector` | object (any value) | no |  | Optional arbitrary JSON selector, stored separately from the versioned definition. |
| `severity` | `string` | no |  | Optional severity label; defaults to normal when empty. |
| `enforcement_mode` | `string` | no |  | Optional enforcement mode; defaults to advisory when empty. |
| `human_approval_required` | `boolean` | no |  | Whether policy evaluations should require human approval; defaults to false. |
| `metadata` | object (any value) | no |  | Optional arbitrary JSON metadata attached to the policy record. |
| `definition` | object (any value) | yes |  | Required versioned policy-rule document; an empty object is persisted when decoded as null. |

### Schema: PolicyRevision {#schema-policyrevision}

Type: `object`

Required fields: `id`, `policy_id`, `version`, `status`, `definition`, `created_by`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Revision identifier, conventionally the policy ID followed by its revision number. |
| `policy_id` | `string` | yes |  | Policy owning this immutable revision record. |
| `version` | `integer` | yes | minimum=1 | Monotonically assigned revision number within the policy. |
| `status` | `string` | yes | enum=["draft","published"] | Revision lifecycle status; publishing a policy marks its current revision published. |
| `definition` | object (any value) | yes |  | JSON policy definition captured for this revision. |
| `created_by` | `string` | yes |  | Actor ID recorded when the revision was created or cloned. |
| `created_at` | `string` | yes | format="date-time" | Revision creation timestamp. |

### Schema: PolicyRevisions {#schema-policyrevisions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `PolicyRevision` | yes |  | Revisions for the workspace-scoped policy, ordered by descending version. |
| `next_cursor` | `string or null` | no |  | Opaque pagination cursor or null when no further page is available. |

### Schema: PolicyImpact {#schema-policyimpact}

Type: `object`

Required fields: `active_sessions`, `active_claims`, `open_proposals`, `active_transactions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `active_sessions` | `integer` | yes | minimum=0 | Count of sessions whose stored status is active or running. |
| `active_claims` | `integer` | yes | minimum=0 | Count of claims whose stored status is active. |
| `open_proposals` | `integer` | yes | minimum=0 | Count of proposals whose status is pending or in_review. |
| `active_transactions` | `integer` | yes | minimum=0 | Count of transactions whose status is pending or running. |

### Schema: PolicyTransition {#schema-policytransition}

Type: `object`

Required fields: `policy`, `impact`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `policy` | `Policy` | yes |  | Policy representation after the requested lifecycle action. |
| `impact` | `PolicyImpact` | yes |  | Current global activity counts returned alongside policy transitions; query failures may leave individual counts at zero. |

### Schema: PolicySchemaResponse {#schema-policyschemaresponse}

Type: `object`

Required fields: `schema_version`, `schema`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `schema_version` | `string` | yes | const="1" | Version identifier for this introspection schema. |
| `schema` | `PolicyInputSchema` | yes |  | JSON Schema-like shape describing the accepted policy creation input. |

### Schema: PolicyInputSchema {#schema-policyinputschema}

Type: `object`

Required fields: `type`, `required`, `properties`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `type` | `string` | yes | const="object" | The policy input root must be a JSON object. |
| `required` | array of `string` | yes | const=["name","definition"] | Required top-level policy input keys. |
| `properties` | `object` | yes |  |  |
| `properties.name` | `object` | yes |  | Name field type declaration. |
| `properties.name.type` | `string` | yes | const="string" | Name is a string. |
| `properties.scope` | `object` | yes |  | Scope field type declaration. |
| `properties.scope.type` | `string` | yes | const="string" | Scope is a string. |
| `properties.selector` | `object` | yes |  | Selector field type declaration. |
| `properties.selector.type` | `string` | yes | const="object" | Selector is an object. |
| `properties.severity` | `object` | yes |  | Severity field type declaration. |
| `properties.severity.type` | `string` | yes | const="string" | Severity is a string. |
| `properties.enforcement_mode` | `object` | yes |  | Enforcement mode field type declaration. |
| `properties.enforcement_mode.type` | `string` | yes | const="string" | Enforcement mode is a string. |
| `properties.human_approval_required` | `object` | yes |  | Human approval setting type declaration. |
| `properties.human_approval_required.type` | `string` | yes | const="boolean" | Human approval requirement is a boolean. |
| `properties.definition` | `object` | yes |  | Versioned definition field type declaration. |
| `properties.definition.type` | `string` | yes | const="object" | The definition is an object. |
| `properties.metadata` | `object` | yes |  | Metadata field type declaration. |
| `properties.metadata.type` | `string` | yes | const="object" | Metadata is an object. |

### Schema: PolicyValidation {#schema-policyvalidation}

Type: `object`

Required fields: `valid`, `result`, `policy_id`, `policy_revision_id`, `issues`, `simulation`, `deterministic_key`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `valid` | `boolean` | yes |  | Whether validation found no blocking issues. |
| `result` | `string` | yes | enum=["pass","fail"] | Coarse validation outcome corresponding to valid. |
| `policy_id` | `string` | yes |  | Policy evaluated by the validation operation. |
| `policy_revision_id` | `string` | yes |  | Revision used to resolve the policy definition. |
| `issues` | array of `string` | yes |  | Validation issue messages; empty when no issues are reported. |
| `simulation` | `boolean` | yes | const=true | Always true for this validation response; validation does not itself apply policy effects. |
| `deterministic_key` | `string` | yes |  | Stable key derived by the evaluator for correlating equivalent evaluation inputs. |

### Schema: PolicyEvaluationInput {#schema-policyevaluationinput}

Type: `object`

Required fields: `subject_type`, `subject_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `subject_type` | `string` | yes |  | Required type/category of the subject being evaluated. |
| `subject_id` | `string` | yes |  | Required identifier of the subject being evaluated. |
| `evidence` | `array or null` | no |  | Optional evidence strings supplied to the evaluator; omitted or null means no evidence list. |

### Schema: PolicyEvaluation {#schema-policyevaluation}

Type: `object`

Required fields: `id`, `policy_id`, `policy_revision_id`, `subject_type`, `subject_id`, `result`, `matched_rules`, `evidence`, `remediation`, `human_approval_required`, `deterministic_key`, `simulation`, `evaluated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Evaluation record identifier; simulation results may not be persisted. |
| `policy_id` | `string` | yes |  | Policy evaluated. |
| `policy_revision_id` | `string` | yes |  | Policy revision whose definition was evaluated. |
| `subject_type` | `string` | yes |  | Type/category of the evaluated subject. |
| `subject_id` | `string` | yes |  | Identifier of the evaluated subject. |
| `result` | `string` | yes | enum=["pass","warn"] | Evaluation outcome; this API projection currently exposes pass or warn. |
| `matched_rules` | array of `string` | yes |  | Rule identifiers or labels reported as matched by the evaluator. |
| `evidence` | `array or null` | yes |  | Evidence carried into the result; null is distinct from an empty array. |
| `remediation` | array of `string` | yes |  | Remediation guidance emitted by matched policy rules. |
| `human_approval_required` | `boolean` | yes |  | Whether the policy result requires a human approval step. |
| `deterministic_key` | `string` | yes |  | Deterministic correlation key for equivalent evaluation inputs. |
| `simulation` | `boolean` | yes |  | True when the operation evaluates without persisting or applying effects; false for a committed evaluation. |
| `evaluated_at` | `string` | yes | format="date-time" | UTC time at which the policy evaluation ran. |

### Schema: Approval {#schema-approval}

Type: `object`

Required fields: `id`, `workspace_id`, `subject`, `reason`, `risk`, `status`, `required_role`, `decision_metadata`, `requested_by`, `override_policy`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Approval request identifier, supplied by the caller or generated by the server. |
| `workspace_id` | `string` | yes |  | Workspace associated with the approval through its proposal. |
| `proposal_id` | `string` | no |  | Optional associated proposal; omitted when no proposal is recorded. |
| `task_id` | `string` | no |  | Optional task identifier recorded with the request; omitted when empty. |
| `subject` | `string` | yes |  | Required subject/title of the approval request. |
| `reason` | `string` | yes |  | Required request rationale; text-redacted before persistence. |
| `risk` | `string` | yes |  | Risk copied from the associated proposal, defaulting to normal when no proposal risk is available. |
| `status` | `string` | yes | enum=["requested","approved","rejected","changes_requested","deferred"] | Request lifecycle state; decisions are only accepted while status is requested. |
| `required_role` | `string` | yes |  | Currently fixed to human in the response projection. |
| `decision` | `string` | no |  | Decision label returned when present; omitted by the current response scanner unless populated by another writer. |
| `decision_metadata` | object (any value) | yes |  | Metadata map returned by the scanner; currently initialized as an empty object rather than hydrated from stored decision metadata. |
| `requested_by` | `string` | yes |  | Actor ID recorded when the request was created. |
| `decided_by` | `string` | no |  | Actor ID that made the decision; omitted while empty. |
| `override_policy` | `boolean` | yes |  | Whether this approval request explicitly permits a policy override; exercising it also requires the override permission header. |
| `created_at` | `string` | yes | format="date-time" | Approval request creation timestamp. |
| `updated_at` | `string` | yes | format="date-time" | Timestamp updated when a decision is recorded. |

### Schema: Approvals {#schema-approvals}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Approval` | yes |  | Workspace-associated approval requests ordered by creation time and ID. |
| `next_cursor` | `string or null` | no |  | Opaque pagination cursor or null when no further page is available. |

### Schema: ApprovalInput {#schema-approvalinput}

Type: object (no additional properties)

Required fields: `subject`, `reason`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | no |  | Optional approval ID; omission generates a timestamp-based ID. |
| `proposal_id` | `string` | no |  | Optional proposal association; when provided it must identify a proposal in the current workspace. |
| `task_id` | `string` | no |  | Optional task identifier recorded on the approval request. |
| `subject` | `string` | yes |  | Required nonblank approval subject. |
| `reason` | `string` | yes |  | Required nonblank request rationale; redacted before persistence. |
| `risk` | `string` | no |  | Accepted by the request decoder but currently not persisted from this input; response risk is derived from the associated proposal or defaults to normal. |
| `required_role` | `string` | no |  | Accepted by the request decoder but current response projection sets required_role to human. |
| `decision_metadata` | object (any value) | no |  | Arbitrary metadata included in the audit payload; current approval response does not hydrate this value. |
| `override_policy` | `boolean` | no |  | Explicitly permits a later policy override; actual override still requires the override permission header. |

### Schema: ApprovalDecisionInput {#schema-approvaldecisioninput}

Type: object (no additional properties)

Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `reason` | `string` | no |  | Optional decision rationale; reject and request-changes actions require a nonblank value and the value is redacted before storage. |
| `decision_metadata` | object (any value) | no |  | Arbitrary metadata serialized into the audit event; it is not currently copied into the approval response. |
| `override` | `boolean` | no |  | Requests a policy override; the approval must have override_policy enabled and the caller must send X-Override-Permission: true. |

### Schema: Transaction {#schema-transaction}

Type: `object`

Required fields: `id`, `workspace_id`, `proposal_id`, `run_id`, `before_git_hash`, `status`, `applied_by`, `metadata`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Transaction identifier, commonly assigned by the transaction service. |
| `workspace_id` | `string` | yes |  | Workspace owning the proposal associated with this transaction. |
| `proposal_id` | `string` | yes |  | Proposal whose patch is being managed by the transaction. |
| `run_id` | `string` | yes |  | Run identifier associated with transaction execution. |
| `before_git_hash` | `string` | yes |  | Repository revision recorded before application. |
| `after_git_hash` | `string` | no |  | Repository revision observed after application; omitted when not available. |
| `status` | `string` | yes | enum=["pending","staged","running","applied","cancelled","failed"] | Transaction lifecycle state as persisted by the transaction manager. |
| `applied_by` | `string` | yes |  | Actor recorded as responsible for applying the transaction. |
| `applied_at` | `string` | no | format="date-time" | Application timestamp; omitted before successful application. |
| `rollback_patch_path` | `string` | no |  | Workspace-relative rollback patch artifact path when compensation is available; omitted when empty. |
| `metadata` | object (any value) | yes |  | Arbitrary transaction metadata decoded from the stored JSON object. |

### Schema: Transactions {#schema-transactions}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Transaction` | yes |  | Transactions associated with proposals in the selected workspace, ordered by transaction ID. |
| `next_cursor` | `string or null` | no |  | Opaque pagination cursor or null when no further page is available. |

### Schema: TransactionPhase {#schema-transactionphase}

Type: `object`

Required fields: `id`, `transaction_id`, `phase`, `status`, `actor`, `inputs`, `outputs`, `recovery_state`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Transaction phase record identifier. |
| `transaction_id` | `string` | yes |  | Transaction that owns this phase record. |
| `phase` | `string` | yes |  | Operation phase label, such as stage, apply, validate, cancel, or retry. |
| `status` | `string` | yes |  | Outcome/status recorded for this phase. |
| `actor` | `string` | yes |  | Actor associated with the phase operation. |
| `reason` | `string` | no |  | Optional redacted reason supplied for the operation. |
| `request_id` | `string` | no |  | Optional request correlation identifier. |
| `inputs` | object (any value) | yes |  | Arbitrary structured input evidence recorded for the phase. |
| `outputs` | object (any value) | yes |  | Arbitrary structured output evidence recorded for the phase. |
| `log_ref` | `string` | no |  | Optional reference to detailed phase logs. |
| `failure_code` | `string` | no |  | Optional machine-readable failure category. |
| `recovery_state` | `string` | yes |  | Recovery progress or disposition recorded for the phase. |
| `started_at` | `string` | no | format="date-time" | Optional phase start timestamp. |
| `ended_at` | `string` | no | format="date-time" | Optional phase end timestamp. |
| `created_at` | `string` | yes | format="date-time" | Phase record creation timestamp. |

### Schema: TransactionPhases {#schema-transactionphases}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `TransactionPhase` | yes |  | Recorded phases for a transaction, in the order provided by the phase endpoint. |
| `next_cursor` | `string or null` | no |  | Opaque pagination cursor or null when no further page is available. |

### Schema: TransactionRecovery {#schema-transactionrecovery}

Type: `object`

Required fields: `transaction_id`, `status`, `repository_state`, `actions`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `transaction_id` | `string` | yes |  | Transaction inspected by the recovery operation. |
| `status` | `string` | yes |  | Recovery assessment status reported by the transaction service. |
| `repository_state` | `string` | yes |  | Observed repository state relevant to whether recovery can proceed. |
| `actions` | array of `string` | yes |  | Recovery actions reported as available or performed; this list is advisory output, not an authorization grant. |
| `blocked_reason` | `string` | no |  | Reason recovery is unavailable or blocked, when one is reported. |

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
| `workspace_id` | `string` | yes |  | Workspace whose proposal queue, policy rate, run state, and repository status are summarized. |
| `generated_at` | `string` | yes | format="date-time" | UTC time at which the dashboard projection was assembled. |
| `measurement_window` | `object` | yes |  | Rolling 24-hour UTC interval used for consensus and policy rates; queue counts are current-state counts, not limited to this interval. |
| `measurement_window.from` | `string` | yes | format="date-time" | Inclusive lower boundary of the rate calculation interval. |
| `measurement_window.to` | `string` | yes | format="date-time" | Exclusive upper boundary of the rate calculation interval, captured as generated_at. |
| `agents_online` | `integer` | yes | minimum=0 | Global enabled-agent count with status ready or idle; this count is not scoped to the selected workspace and does not probe process liveness. |
| `proposal_queue` | `object` | yes |  | Workspace proposal counts grouped by stored lifecycle status; accepted is included in the approved count. |
| `proposal_queue.pending` | `integer` | yes |  | Workspace proposals with status pending. |
| `proposal_queue.in_review` | `integer` | yes |  | Workspace proposals with status in_review. |
| `proposal_queue.approved` | `integer` | yes |  | Workspace proposals with status accepted or approved. |
| `proposal_queue.rejected` | `integer` | yes |  | Workspace proposals with status rejected. |
| `consensus_success_rate` | `number` | yes | minimum=0; maximum=1 | Approved consensus snapshots divided by workspace snapshots with approved or blocked status inside the measurement window; zero when no finalized snapshots exist. |
| `policy_pass_rate` | `number` | yes | minimum=0; maximum=1 | Passing workspace policy evaluations divided by all workspace evaluations in the measurement window; zero when there are no evaluations. |
| `transaction_manager` | `string` | yes |  | Currently hardcoded to ready; this dashboard field does not probe transaction execution or locks. |
| `mcp_enforcement` | `string` | yes |  | Currently hardcoded to orchestrator-only; it is an informational label, not a runtime policy verification. |
| `memory_oracle` | `string` | yes |  | Currently hardcoded to ready; this dashboard field does not probe memory service behavior. |
| `run_state` | `string` | yes |  | State from the most recently started workspace run, or stopped when no state is returned. |
| `repository` | object (any value) | yes |  | Repository inspection result for the workspace; on inspection failure the handler returns an object with status unavailable. The detailed shape depends on repository status projection. |

### Schema: ActivityItem {#schema-activityitem}

Type: `object`

Required fields: `id`, `type`, `category`, `entity_type`, `severity`, `title`, `metadata`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes |  | Global event row ID; activity is returned newest-first by this ID. |
| `type` | `string` | yes |  | Persisted event type string. |
| `category` | `string` | yes |  | Prefix of type before the first dot; also used as the activity entity_type projection. |
| `actor_id` | `string` | no |  | Text-redacted event actor ID; omitted when empty. |
| `entity_type` | `string` | yes |  | Currently identical to category, not a separately resolved domain entity type. |
| `entity_id` | `string` | no |  | First string value found in selected metadata keys (proposal, claim, approval, transaction, policy, session, contention, vote IDs), text-redacted; omitted if unavailable. |
| `severity` | `string` | yes |  | Lowercased severity string from event metadata, or info when absent or not a string. |
| `title` | `string` | yes |  | Friendly title for selected known event types; other event names are transformed from dotted/underscored identifiers. |
| `metadata` | object (any value) | yes |  | JSON object payload after heuristic redaction; malformed or non-object payloads yield an empty object in this activity projection. |
| `created_at` | `string` | yes | format="date-time" | Persisted event creation timestamp. |

### Schema: ActivityFeed {#schema-activityfeed}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `ActivityItem` | yes |  | Workspace-matching events in descending event ID order, with optional entity, category, actor, and severity filters applied in memory. |
| `next_cursor` | `string or null` | no |  | Opaque offset cursor for the next page; this endpoint loads and filters the matching event rows before pagination. |

### Schema: Memory {#schema-memory}

Type: `object`

Description: Current memory projection. Workspace-owned and legacy shared rows are readable within the workspace; optional source identifiers and workspace_id are omitted when empty. Title/body and selected provenance data are redacted heuristically on output. Confidence, reliability, and importance range metadata describe intended scale but creation currently does not enforce those bounds.
Required fields: `id`, `scope`, `kind`, `title`, `body_md`, `tags`, `provenance`, `confidence`, `reliability`, `importance`, `pinned`, `revision`, `status`, `resynthesis_status`, `created_at`, `updated_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Stable memory identifier; created notes use an M-prefixed time-derived identifier and canonical merges create a new identifier. |
| `workspace_id` | `string` | no |  | Owning workspace; omitted for legacy shared rows. |
| `scope` | `string` | yes |  | Caller-supplied scope label; creation does not validate or normalize it. |
| `kind` | `string` | yes |  | Caller-supplied memory kind label; creation does not validate or normalize it. |
| `title` | `string` | yes |  | Memory title after heuristic text redaction on output. |
| `body_md` | `string` | yes |  | Markdown body after heuristic redaction before persistence and on output. |
| `tags` | array of `string` | yes |  | Tag strings; missing or invalid stored JSON is returned as an empty array. |
| `provenance` | object (any value) | yes |  | Caller-provided provenance JSON object; missing or invalid stored JSON is returned as an empty object and response values are redacted. |
| `source_event_id` | `integer` | no | format="int64" | Optional originating event row ID; omitted when zero or absent. |
| `source_session_id` | `string` | no |  | Optional source session identifier; omitted when empty. |
| `source_proposal_id` | `string` | no |  | Optional source proposal identifier; omitted when empty. |
| `confidence` | `number` | yes | minimum=0; maximum=1 | Confidence score; defaults to 0.5 on creation. Bounds are documented but not enforced by the current handler. |
| `reliability` | `number` | yes | minimum=0; maximum=1 | Reliability score; defaults to 0.5 on creation. Bounds are documented but not enforced by the current handler. |
| `importance` | `integer` | yes | minimum=0; maximum=100 | Importance score; omitted input currently persists as 0 despite the database column default. Bounds are not enforced by the handler. |
| `pinned` | `boolean` | yes |  | Whether the note is pinned; changed through the pin and unpin actions. |
| `revision` | `integer` | yes | minimum=1 | Current revision number; newly created notes start at 1 and successful revision writes increment it after an expected_revision check. |
| `status` | `string` | yes | enum=["active","archived","merged"] | Lifecycle state. Listing always excludes archived rows; restore changes status to active; merges mark source records merged. |
| `resynthesis_status` | `string` | yes |  | Resynthesis lifecycle marker; the resynthesize action sets requested but does not execute a worker. |
| `created_at` | `string` | yes | format="date-time" | Database creation timestamp. |
| `updated_at` | `string` | yes | format="date-time" | Database update timestamp, changed by lifecycle actions and revision writes. |

### Schema: Memories {#schema-memories}

Type: `object`

Description: One ascending keyset-paginated memory page; next_cursor is null when no further row was observed.
Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Memory` | yes |  | Memory rows matching the list filters, ordered by created_at then id ascending. |
| `next_cursor` | `string or null` | no |  | Raw-base64url keyset cursor derived from the last returned created_at and id when an additional row exists. |

### Schema: MemoryInput {#schema-memoryinput}

Type: object (no additional properties)

Description: Strict JSON request for note creation. Only title and body_md are validated as non-blank; scope and kind are accepted as empty when omitted. Confidence/reliability default to 0.5 and importance defaults to 0 in current handler behavior.
Required fields: `title`, `body_md`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `scope` | `string` | no |  | Optional caller-defined scope label; no enum or non-empty validation is currently applied. |
| `kind` | `string` | no |  | Optional caller-defined kind label; no enum or non-empty validation is currently applied. |
| `title` | `string` | yes | minLength=1 | Required non-blank title; trimmed only for validation, not before persistence. |
| `body_md` | `string` | yes | minLength=1 | Required non-blank markdown body; text-redacted before persistence. |
| `tags` | array of `string` | no |  | Optional tag list; omitted/null is returned as an empty array. |
| `provenance` | object (any value) | no |  | Optional free-form provenance object; returned values are heuristically redacted. |
| `source_event_id` | `integer` | no | format="int64" | Optional source event row ID; zero is persisted as SQL null. |
| `source_session_id` | `string` | no |  | Optional source session ID; empty string is persisted as SQL null. |
| `source_proposal_id` | `string` | no |  | Optional source proposal ID; empty string is persisted as SQL null. |
| `confidence` | `number` | no | minimum=0; maximum=1 | Optional intended 0..1 score; omission defaults to 0.5. The handler does not enforce the range. |
| `reliability` | `number` | no | minimum=0; maximum=1 | Optional intended 0..1 score; omission defaults to 0.5. The handler does not enforce the range. |
| `importance` | `integer` | no | minimum=0; maximum=100 | Optional intended 0..100 score; omission currently persists as 0. The handler does not enforce the range. |

### Schema: MemoryRevisionInput {#schema-memoryrevisioninput}

Type: object (no additional properties)

Description: Strict JSON revision request using optimistic concurrency. expected_revision must be positive and equal the current revision; stale writes return 409.
Required fields: `body_md`, `expected_revision`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `body_md` | `string` | yes | minLength=1 | Required non-blank replacement markdown body; text-redacted before persistence. |
| `title` | `string` | no |  | Optional replacement title; an empty value leaves the current title unchanged. |
| `reason` | `string` | no |  | Accepted but currently ignored; stored revision provenance uses the fixed reason revision. |
| `expected_revision` | `integer` | yes | minimum=1 | Required current revision number used to reject stale edits; must be at least 1. |

### Schema: MemoryRevision {#schema-memoryrevision}

Type: `object`

Description: Immutable revision record returned in ascending revision order.
Required fields: `id`, `memory_id`, `revision`, `body_md`, `provenance`, `created_by`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Revision row identifier, conventionally memory ID followed by -r and the revision number. |
| `memory_id` | `string` | yes |  | Memory whose history contains this revision. |
| `revision` | `integer` | yes | minimum=1 | Monotonically increasing revision number. |
| `body_md` | `string` | yes |  | Persisted markdown body after heuristic text redaction and redacted again on output. |
| `provenance` | object (any value) | yes |  | Revision provenance JSON; current writes record the fixed reason revision. |
| `created_by` | `string` | yes |  | Actor ID that submitted the revision. |
| `created_at` | `string` | yes | format="date-time" | Revision creation timestamp. |

### Schema: MemoryRevisions {#schema-memoryrevisions}

Type: `object`

Description: Complete unpaginated memory revision history.
Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `MemoryRevision` | yes |  | Immutable revision records in ascending revision order. |

### Schema: MemoryDiff {#schema-memorydiff}

Type: `object`

Description: Current implementation is a placeholder projection and does not calculate a content diff; changed is always true.
Required fields: `memory_id`, `from_revision`, `to_revision`, `changed`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `memory_id` | `string` | yes |  | Requested memory identifier. |
| `from_revision` | `string` | yes |  | Requested source revision label, default 1; echoed without lookup. |
| `to_revision` | `string` | yes |  | Requested target revision label, default latest; echoed without lookup. |
| `changed` | `boolean` | yes |  | Currently always true; not computed by comparing revisions. |
| `current_body_md` | `string` | no |  | Current memory body after heuristic text redaction; this is not a patch or unified diff. |

### Schema: MemoryGraph {#schema-memorygraph}

Type: `object`

Description: Alias records connecting this memory ID to canonical merge records.
Required fields: `memory_id`, `aliases`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `memory_id` | `string` | yes |  | Memory whose alias relationships were queried. |
| `aliases` | array of object (no additional properties) | yes |  | Rows where memory_id appears as alias_id or canonical_id, ordered by created_at. |
| `aliases[].alias_id` | `string` | yes |  | Redirected source memory ID. |
| `aliases[].canonical_id` | `string` | yes |  | Canonical memory ID for the alias. |
| `aliases[].reason` | `string` | yes |  | Merge reason after heuristic text redaction. |
| `aliases[].created_by` | `string` | yes |  | Actor ID after heuristic text redaction. |
| `aliases[].created_at` | `string` | yes | format="date-time" | Alias row creation time. |

### Schema: MemorySourceChain {#schema-memorysourcechain}

Type: `object`

Description: Structured provenance edges for a memory, separate from free-form provenance JSON.
Required fields: `memory_id`, `edges`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `memory_id` | `string` | yes |  | Memory whose source relationships were queried. |
| `edges` | array of object (no additional properties) | yes |  | Provenance edges ordered by created_at. |
| `edges[].source_type` | `string` | yes |  | Category of the source record. |
| `edges[].source_id` | `string` | yes |  | Source record identifier after heuristic text redaction. |
| `edges[].relation` | `string` | yes |  | Relationship label connecting the source to this memory. |
| `edges[].created_at` | `string` | yes | format="date-time" | Provenance edge creation time. |

### Schema: Notification {#schema-notification}

Type: `object`

Required fields: `id`, `workspace_id`, `recipient_id`, `severity`, `category`, `title`, `body`, `actionable`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `string` | yes |  | Stable notification identifier. |
| `workspace_id` | `string` | yes |  | Workspace that owns the notification. |
| `recipient_id` | `string` | yes |  | Recipient identity or wildcard recipient; text-redacted before returning. |
| `severity` | `string` | yes | enum=["info","warning","error","critical"] | Stored notification severity label. |
| `category` | `string` | yes | enum=["approval_required","claim_contention","transaction_failure","policy_intervention","agent_session_failure","repository_degraded","system_warning"] | Stored attention category used by exact-match list filters. |
| `title` | `string` | yes |  | Notification headline after text redaction. |
| `body` | `string` | yes |  | Notification detail after text redaction. |
| `entity_type` | `string` | no |  | Optional associated entity category; omitted when empty. |
| `entity_id` | `string` | no |  | Optional associated entity identifier; omitted when empty. |
| `read_at` | `string` | no | format="date-time" | Acknowledgement timestamp; acknowledgement preserves the first timestamp and does not resolve the underlying condition. |
| `actionable` | `boolean` | yes |  | Whether the notification is marked as actionable; actionable counts include only unread items whose resolved_at is null. |
| `resolved_at` | `string` | no | format="date-time" | Underlying notification resolution timestamp; omitted when unresolved. Reading/acknowledging does not set this field. |
| `created_at` | `string` | yes | format="date-time" | Notification creation timestamp, used with ID for descending keyset pagination. |

### Schema: Notifications {#schema-notifications}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `Notification` | yes |  | Workspace notifications ordered by creation time and ID descending after recipient/category/severity/read/actionable filters. |
| `next_cursor` | `string or null` | no |  | Raw-base64url cursor encoding the last returned created_at and ID separated by a newline; null when no further page is available. |

### Schema: NotificationCounts {#schema-notificationcounts}

Type: `object`

Required fields: `workspace_id`, `unread`, `actionable`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  | Workspace whose notifications were counted. |
| `unread` | `integer` | yes | minimum=0 | Count of unread notifications for the selected recipient scope. |
| `actionable` | `integer` | yes | minimum=0 | Count of unread notifications with actionable=true and resolved_at unset for the selected recipient scope. |

### Schema: OperationalLog {#schema-operationallog}

Type: `object`

Required fields: `id`, `type`, `component`, `severity`, `payload`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes | format="int64" | Global persisted event row ID. |
| `type` | `string` | yes |  | Stored event type. |
| `component` | `string` | yes |  | Prefix of type before the first dot; the component filter matches event-type prefixes. |
| `severity` | `string` | yes |  | String severity extracted from the payload, defaulting to info when absent or non-string. |
| `actor_id` | `string` | no |  | Text-redacted actor ID; omitted when empty. |
| `request_id` | `string` | no |  | Text-redacted string extracted from payload; omitted when absent or non-string. |
| `agent_id` | `string` | no |  | Text-redacted string extracted from payload; omitted when absent or non-string. |
| `session_id` | `string` | no |  | Text-redacted string extracted from payload; omitted when absent or non-string. |
| `proposal_id` | `string` | no |  | Text-redacted string extracted from payload; omitted when absent or non-string. |
| `transaction_id` | `string` | no |  | Text-redacted string extracted from payload; omitted when absent or non-string. |
| `payload` | object (any value) | yes |  | Decoded JSON event payload after heuristic redaction; invalid or non-object payloads become an object containing redacted text. |
| `created_at` | `string` | yes | format="date-time" | Persisted event creation timestamp. |

### Schema: OperationalLogs {#schema-operationallogs}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `OperationalLog` | yes |  | Workspace events in descending global event ID order, projected from the events table; this is not the HTTP logger or a complete process log. |
| `next_cursor` | `string or null` | no |  | Decimal global event ID of the last returned item when another page exists; send it back as cursor to request older IDs. |

### Schema: AuditEvent {#schema-auditevent}

Type: `object`

Required fields: `id`, `actor_id`, `action`, `entity_type`, `entity_id`, `payload`, `created_at`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `id` | `integer` | yes | format="int64" | Global audit row ID. |
| `actor_id` | `string` | yes |  | Text-redacted actor identifier. |
| `action` | `string` | yes |  | Recorded audit action label. |
| `entity_type` | `string` | yes |  | Recorded entity category associated with the audited action. |
| `entity_id` | `string` | yes |  | Text-redacted identifier of the audited entity. |
| `request_id` | `string` | no |  | Optional request correlation ID; omitted when absent. |
| `reason` | `string` | no |  | Optional text-redacted rationale; omitted when empty. |
| `payload` | object (any value) | yes |  | Decoded audit JSON payload after heuristic redaction; invalid or non-object content is represented as redacted text. |
| `created_at` | `string` | yes | format="date-time" | Audit row creation timestamp. |

### Schema: AuditEvents {#schema-auditevents}

Type: `object`

Required fields: `items`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `items` | array of `AuditEvent` | yes |  | Workspace audit rows in descending ID order, filtered by exact actor/action/request/entity values and valid time bounds. |
| `next_cursor` | `string or null` | no |  | Decimal audit row ID of the last returned item when another page exists; send it back to request older IDs. |

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
| `event_id` | `integer` | yes | format="int64" | Global SQLite event row ID; IDs belonging to other workspaces can create gaps in this workspace's stream. |
| `workspace_id` | `string` | yes |  | Workspace whose membership filter selected this event. |
| `sequence` | `integer` | yes | format="int64" | Currently identical to event_id (global event row ID), not a workspace-local consecutive counter. |
| `type` | `string` | yes |  | Persisted event type string. |
| `occurred_at` | `string` | yes | format="date-time" | Persisted event creation timestamp projected as the occurrence time. |
| `actor` | `string` | no |  | Text-redacted actor ID; omitted when empty. |
| `entity_type` | `string` | no |  | Prefix of type before its first dot; this is a naming heuristic, not a foreign-key lookup. |
| `entity_id` | `string` | no |  | First string value found among selected payload keys (proposal_id, claim_id, vote_id, approval_id, transaction_id, policy_id, session_id), text-redacted; omitted if none is found. |
| `payload` | object (any value) | yes |  | Parsed and heuristically redacted JSON object. Invalid JSON falls back to an object with redacted text; redaction is not a guarantee that every secret format is removed. |

### Schema: LiveEventControl {#schema-liveeventcontrol}

Type: `object`

Required fields: `kind`, `workspace_id`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `kind` | `string` | yes | enum=["hello","ping","resync_required"] | Control message discriminator. hello is sent after upgrade, ping periodically reports sequence progress, and resync_required ends replay when the backlog exceeds the server limit. |
| `schema_version` | `string` | no |  | Protocol schema version, currently 1 and present on hello. |
| `workspace_id` | `string` | yes |  | Workspace associated with the control message. |
| `current_sequence` | `integer` | no | format="int64" | Current global event ID included by hello and resync_required; ping uses the separate sequence field. |
| `sequence` | `integer` | no | format="int64" | Current global event ID included by ping controls; it is not a workspace-local sequence. |
| `resumable` | `boolean` | no |  | Hello currently reports true to indicate that last_event_id replay is supported, subject to the bounded replay limit. |
| `reason` | `string` | no |  | Resync explanation; currently cursor_not_retained when replay query finds more than 1,000 events, which indicates backlog size rather than verified event deletion. |

### Schema: EventVersionMarker {#schema-eventversionmarker}

Type: `object`

Required fields: `count`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `count` | `integer` | yes | minimum=0 | Number of rows counted for the named domain marker; each domain query is independent. |
| `latest_at` | `string` | no | format="date-time" | Latest timestamp selected for that domain's marker; omitted when the source query has no timestamp. |

### Schema: EventSnapshot {#schema-eventsnapshot}

Type: `object`

Required fields: `workspace_id`, `sequence`, `generated_at`, `retention`, `version_markers`, `snapshot`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `workspace_id` | `string` | yes |  | Workspace whose bounded event stream and selected domain counts are summarized. |
| `sequence` | `integer` | yes | format="int64"; minimum=0 | Largest persisted global event ID belonging to the workspace filter, or zero when no matching event is present or the lookup fails. |
| `generated_at` | `string` | yes | format="date-time" | UTC time when the snapshot response was assembled; component queries are not wrapped in one database transaction. |
| `retention` | `object` | yes |  | Fixed descriptors for WebSocket event replay behavior; these values do not configure data retention or event deletion. |
| `retention.mode` | `string` | yes |  | Currently append_only_bounded_replay. |
| `retention.resume_by` | `string` | yes |  | Currently last_event_id; resume cursor is the global SQLite event ID. |
| `retention.max_replay_events` | `integer` | yes | minimum=1 | Currently 1,000 replayed events; a larger pending replay produces resync_required and closes the socket. |
| `retention.resync_endpoint` | `string` | yes |  | Workspace snapshot endpoint path suggested for reconciliation; this response is a partial summary, not a complete domain export. |
| `version_markers` | object (values of `EventVersionMarker`) | yes |  | Available per-domain row counts and latest timestamps. Failed marker queries omit that domain; markers are not content hashes or full revision tokens. |
| `snapshot` | `object` | yes |  | Partial state projection containing workspace metadata, selected domain counts, and latest run state; fetch collection endpoints for full records. |
| `snapshot.workspace` | `Workspace` | yes |  | Workspace registry record at the time it was read. |
| `snapshot.counts` | object (values of `integer`) | yes |  | Counts for domains whose independent marker queries succeeded; values can be omitted when their queries fail. |
| `snapshot.run_state` | `string` | yes |  | State of the latest workspace run, defaulting to stopped when no state is returned. |

### Schema: ContentionResolutionInput {#schema-contentionresolutioninput}

Type: object (no additional properties)

Required fields: `resolution`
Additional properties: forbidden.

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `resolution` | `string` | yes | enum=["keep_owner","transfer","split","narrow","extend_lease","reject_contender"] | Required resolution choice. keep_owner, transfer, extend_lease, and reject_contender are implemented; split and narrow are recognized but return HTTP 409 as unsupported. |
| `reason` | `string` | no |  | Optional explanation, text-redacted and recorded in the audit event; it does not affect which resolution is applied. |

### Schema: ContentionResolution {#schema-contentionresolution}

Type: `object`

Required fields: `contention_id`, `state`, `resolution`

| Property | Type | Required | Constraints | Description |
|---|---|---|---|---|
| `contention_id` | `string` | yes |  | Identifier of the contention marked resolved. |
| `state` | `string` | yes | enum=["resolved"] | Successful resolution always returns resolved; attempting to resolve a non-open contention returns a conflict instead. |
| `resolution` | `string` | yes |  | Resolution applied to the incumbent claim and contention record. |

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
