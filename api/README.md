# Roundtable HTTP API

The HTTP API is a standalone Go control-plane server under `api/`. It serves the versioned REST contract plus the `/metrics` Prometheus endpoint. The authoritative route/request/response contract is [`openapi.yaml`](openapi.yaml); the generated [endpoint guide](docs/admin-api-guide.md) maps operations to their [schema reference](docs/schema-reference.md), which catalogs every declared component field. The API shares database tables with the Go runtime when configured to use the same SQLite file, but it is a separate process with its own HTTP auth, workspace boundary, and event delivery implementation.

## Capability groups

| Area | Capability |
| --- | --- |
| Health/security | Node and workspace health, component degradation, security-capability discovery, Prometheus metrics. |
| Workspaces/repository | Workspace registry, root validation, repository status/entities, guarded branch-switch preflight and switch. |
| Agents/sessions | Agent registry/enablement/diagnostics and session lifecycle, logs, tool calls, claims, proposals, metrics, environment projections. |
| Deliberation/proposals | Deliberation lifecycle/transcript, proposals, patch file/diff/symbol impact, validation, votes, and consensus. |
| Governance | Policy catalog/revisions/evaluations, human approval queue, resource claims/contentions, transaction phases/recovery. |
| Runtime visibility | Run controls/status, dashboard summary, activity feed, event snapshot/WebSocket, notifications, operational logs, audit list/export. |
| Settings/maintenance | Effective settings/schema/preflight/update and controlled SQLite health/checkpoint/backup/retention operations. |

The OpenAPI contract is substantially broader than the current browser implementation; see [UI scope](../docs/UI.md). The API's existence does not mean a corresponding page is finished or the Go CLI runtime automatically performs the operation.

## Reading Map

Use the generated endpoint/schema guides for declared shapes and the references below for actual handler behavior. A contract permission, readiness label, accepted request, or persisted status is not independent evidence of runtime execution or privilege isolation.

| Topic | Detailed reference |
| --- | --- |
| Native/container setup | [Compose](#start-with-docker-compose), [flags/environment](#process-flags-and-environment), [startup/lifecycle](#startup-bind-and-process-lifecycle-details), [embedded configuration](#embedded-server-configuration). |
| Credentials and browser access | [Authorization](#authentication-and-authorization), [Origin/CORS](#browser-origin-and-request-conventions), [security discovery](#security-capability-discovery), [decoder/actor/retry boundaries](docs/error-semantics.md). |
| Workspace and repository | [Registry/lifecycle](#workspace-registry-and-lifecycle), [status/branch switching](#repository-status-and-branch-control), [repository entities/symbols](../docs/SYMBOLS.md#http-repository-entity-discovery). |
| Deliberation | [Creation/lifecycle/messages](#deliberation-administration), [transcript](#deliberation-transcript-projection), [runtime turns and run controls](../docs/ORCHESTRATION.md). |
| Agent and session administration | [Adapters/diagnostics](../docs/ADAPTERS.md), [session controls/observability](../docs/SESSIONS.md). |
| Governance and proposals | [Policy/evaluation/votes](../POLICIES.ROUNDTABLE.md), [claims/contentions](../docs/CLAIMS.md), [approvals](../docs/HUMAN_APPROVALS.md), [proposals/patches/transactions/recovery](../docs/TRANSACTIONS.md), [validation stages](../docs/TEST_EXECUTION.md). |
| Memory | [Search, lifecycle, revisions, provenance, and merge](../docs/MEMORY_ORACLE.md). |
| Current-state displays | [Health](#health-and-readiness-boundaries), [dashboard](#dashboard-summary-semantics), [activity](#activity-feed-projection), [analytics](#consensus-analytics). |
| Streaming and notifications | [Snapshot/WebSocket protocol](#event-snapshot-and-websocket-protocol), [notification selection/counts/acknowledgment](docs/notifications.md). |
| Logs, audit, and instrumentation | [Operational logs/audit export](#operational-logs-and-audit-exports), [HTTP logs/metrics](#request-logging-and-metrics). |
| Configuration and database operations | [Workspace settings](docs/settings.md), [maintenance](docs/maintenance.md), [persistence](../docs/DATABASE.md). |
| Integration and documentation | [Pagination/search](docs/pagination.md), [endpoint guide](docs/admin-api-guide.md), [schema reference](docs/schema-reference.md), [generation/checks](#documentation-maintenance), [browser scope](../docs/UI.md). |

## Start with Docker Compose

From repository root:

```sh
docker compose -f api/docker-compose.yml up --build
```

The compose service publishes `${ROUNDTABLE_API_PORT:-8080}:8080`, mounts the project `.roundtable/` directory at `/app/.roundtable`, and mounts the checkout read-only at `/workspace`. The server process is configured to use the workspace and database paths from its image/command. Do not mount the authoritative project read-write into an agent container.

For native development:

```sh
GOCACHE=/tmp/roundtable-go-cache go run ./api/cmd/server \
  -addr 127.0.0.1:8080 \
  -workspace-root . \
  -database .roundtable/roundtable.db
```

## Process flags and environment

| Flag | Default | Environment/default source | Purpose |
| --- | --- | --- | --- |
| `-addr` | `127.0.0.1:8080` | none | HTTP listen address. Non-loopback is rejected unless `-allow-remote` is also set. |
| `-workspace-root` | `.` | none | Root directory the API may register/serve as a workspace. |
| `-database` | `.roundtable/roundtable.db` | none | SQLite file path, relative to process working directory when not absolute. |
| `-human-token` | empty | `ROUNDTABLE_HUMAN_TOKEN` | Bearer token for human/admin control surface. |
| `-agent-token` | empty | `ROUNDTABLE_AGENT_TOKEN` | Separate bearer token for agent/orchestrator control surface. |
| `-allow-remote` | `false` | none | Explicit opt-in to non-loopback binds. This does not add TLS, a proxy, network policy, or stronger identity. |

`ROUNDTABLE_API_VERSION` sets version metadata; an empty value becomes `dev`. The server library's `AllowedOrigins` defaults to `http://localhost:3000` and `http://127.0.0.1:3000`. The standalone `main` does not expose an allowed-origin flag/environment variable, so a deployment requiring other browser origins needs an explicit server configuration change or trusted same-origin proxy design.

## Startup, bind, and process lifecycle details

### Embedded Server Configuration

`httpapi.NewServer(Config)` is the library entry point used by the standalone executable and tests. These fields are constructor inputs, not additional YAML keys or automatically read environment variables:

| Field | Constructor behavior and scope |
| --- | --- |
| `Version` | Exactly empty becomes `dev`; other values are retained as version metadata, not API-route version selection. |
| `Logger` | Nil becomes `slog.Default()`; a supplied logger controls HTTP request-log output. No constructor-level redaction, log rotation, level flag, or log-file destination is added. |
| `Store` | Supplied database store; the constructor does not open/migrate SQLite or reject nil. Individual handlers differ in nil-store handling, so nil is not a general supported stateless-service mode. |
| `AllowedWorkspaceRoots` | Registration allowlist, with symlink resolution during root validation. Empty means no roots can be registered, not unrestricted filesystem access. The standalone executable supplies one root; embedded callers can supply several. |
| `AllowedOrigins` | A zero-length slice, including nil or an explicitly empty slice, is replaced with the two localhost UI origins. This cannot disable defaults by passing an empty slice. Matching is exact after configured-entry trimming; the constructor does not validate origin syntax or expose a wildcard mode. |
| `HumanToken` | Shared human credential, retained as supplied. No generation, hashing-at-rest, expiry, rotation, or per-workspace token scope is supplied. |
| `AgentToken` | Shared agent credential, retained as supplied. Context classification does not repair the handler authorization limitation documented below. |
| `MaintenanceDirectory` | Backup parent directory. Exactly empty uses `.roundtable` in the backup handler; the standalone executable supplies the database path's parent. It is not a workspace-root restriction or arbitrary client-selected destination. |

Each new server allocates a fresh in-memory idempotency map and zeroed HTTP counters. Constructing another server with the same database does not share those maps/counters. `Handler()` constructs the HTTP routing/middleware surface; it does not itself create a listener, register workspaces, launch agents, or perform a runtime coordinator startup. The native executable separately opens the database and calls `Serve`. Constructor defaults are not evidence that environment variables, runtime configuration revisions, or browser settings have been synchronized.

The native bind guard splits `-addr` as host/port and parses the host as a numeric IP. `127.0.0.1:8080` and `[::1]:8080` pass the loopback test; `localhost:8080`, `:8080`, and `0.0.0.0:8080` do not, even if a hostname resolves to loopback. Unless `-allow-remote` is set, rejection prints `refusing non-loopback bind; pass -allow-remote explicitly` and exits with code 2 before opening SQLite. The opt-in skips that check; malformed addresses can still fail when serving begins.

Database and workspace-root defaults are independent process-relative paths. Setting `-workspace-root /some/repo` does not automatically move the default database into that repository. `-database` also sets the parent directory used as the maintenance directory. The process does not read `.roundtable/config.yaml` to derive these HTTP-service flags. Explicit token flags override their environment defaults, including an explicitly empty flag value. Environment changes do not hot-reload a running server.

After database initialization, the native process installs interrupt/SIGTERM cancellation and starts the HTTP server. The server sets a five-second `ReadHeaderTimeout`, but does not configure general read, write, or idle timeouts in this constructor. On context cancellation it requests shutdown with a separate five-second context and ignores that shutdown call's returned error. This is not proof that every long-running operation or hijacked WebSocket completed gracefully. There is no HTTPS listener, certificate flag, worker-limit flag, or request-timeout flag exposed by this executable.

### Container-specific configuration

The image command uses `-addr 0.0.0.0:8080 -allow-remote -workspace-root /workspace -database /app/.roundtable/roundtable.db`. These replace native defaults deliberately so the container port is reachable. Compose maps `${ROUNDTABLE_API_PORT:-8080}:8080` without a host IP restriction; it is not a loopback-only publication. Empty token defaults plus that publication can expose development header mode beyond the local machine. Set credentials and constrain network exposure before starting it on a shared host; `-allow-remote` is not authentication or TLS.

Compose has restart policy `unless-stopped`, but no configured healthcheck, resource limit, or dependency on a UI/runtime service. The image builds with Go 1.26 on Debian Bookworm and `CGO_ENABLED=1`; its runtime image installs CA certificates and Git. It does not install agent CLIs, Node, Python, or a general test-toolchain environment. No `USER` directive switches away from the image's default root user. The checkout mount is read-only, so repository reads can work while operations requiring writes, such as branch switching, can fail on that mount even when policy/preflight allows them. The writable `.roundtable` mount is separate persistent state, not authority to rewrite the read-only checkout.

Container startup does not register a workspace automatically or launch the coordinator. Verify the intended workspace registry/root and database file separately after startup. Changing published port, mounts, or token environment affects container deployment, not the browser's already-built public URL settings.

## Authentication and authorization

When either bearer token is configured, every non-OPTIONS request must present a matching `Authorization: Bearer ...` token. The human token maps the request to human identity; the agent token maps it to agent identity. Tokens are compared in constant time. Unknown bearer tokens return 401. The API does not provide a login/session service or token rotation endpoint; provision, store, rotate, and revoke tokens outside the repository and restart the API after changing them.

Mutation handlers may additionally require `X-Actor-ID` and `X-Actor-Role`; documented roles are `view`, `operate`, `approve`, `govern`, `administer`, and `force-override`, with `human`, `admin`, and `chair` accepted as aliases by current helper code. Do not infer fine-grained separation from role names alone: many current handlers check the supplied human-role header rather than authenticated identity kind or a unique permission. Some agent/orchestrator handlers use `X-Roundtable-Orchestrator: true` and actor headers. Consult the operation implementation and the boundary limitation below before granting a token access; OpenAPI permission labels do not independently enforce authorization.

If both bearer tokens are absent, local development mode accepts actor headers and is not an authentication boundary. Keep this mode bound to loopback. A remote bind is unsafe without configured credentials and a protected transport/network boundary. Do not put the human token in browser code, `NEXT_PUBLIC_*` variables, shell history, checked-in `.env` files, or logs.

### Token and actor parsing details

Authorization is trimmed, then recognized only with the exact case-sensitive prefix `Bearer `; the extracted token is trimmed too. Lowercase `bearer`, another authentication scheme, or an empty bearer value behaves as missing credentials. With either configured token, that produces `authentication_required`. A nonempty unrecognized bearer token produces `invalid_token`, including in development mode when no tokens are configured. OPTIONS bypasses authentication for CORS preflight.

Human-token matching is checked before agent-token matching. If both configured secrets are identical, that value authenticates as human, not agent; use distinct tokens to preserve the intended boundary. Constant-time comparison applies only after checking nonempty values and equal lengths. This is a shared-secret mechanism, not per-user login, cryptographic binding to `X-Actor-ID`, or a scoped token catalog.

`X-Actor-ID` is trimmed attribution supplied by the caller, not an identity derived from the token. Actor role is trimmed and lowercased in middleware identity. A valid human token preserves that role; a valid agent token forces the **context identity's** role/kind to `agent`. A human-kind request with a nonempty unrecognized role is rejected as `unknown_role`. An empty role can pass middleware authentication but fail a handler's additional actor requirements. In no-token development mode, exact `X-Roundtable-Orchestrator: true` or normalized role `agent` chooses agent kind; this header is not a secret.

**The current handler-level human boundary does not consistently consume that context identity.** `humanAuthorized` checks a nonblank actor header and an allowlisted role from the original `X-Actor-Role` header. Middleware does not rewrite that header when an agent token is accepted. Consequently, a valid agent token accompanied by a human-role header and actor ID can satisfy this helper even though its context identity remains agent. The identity-aware `agentIdentity` helper is defined but not used by the current HTTP handlers. Do not claim agent credentials are universally excluded from human-only controls or expose them as narrowly scoped read-only credentials. This documents an implementation defect; token separation alone does not repair it. Restrict both credentials to trusted participants until authorization is made identity-aware and verified.

The accepted human role labels and aliases are a helper allowlist, not proof of a separate permission boundary for every operation. Read handler-specific authorization and keep the token's broad authority in mind. Do not expose a human secret merely to label requests as `view`.

## Browser origin and request conventions

The default allowed origins are the localhost UI origins above. CORS returns allowed methods/headers for a matching Origin; the security middleware rejects state-changing requests with a non-allowlisted Origin. `OPTIONS` is handled as preflight. This is an origin check, not an alternative to bearer authentication.

Requests and errors use request IDs; errors use `application/problem+json`. Mutating endpoints may require `Idempotency-Key`; workspace/configuration updates may use `If-Match` revision values. The API client should preserve returned request/correlation IDs and only retry according to [Error Semantics](docs/error-semantics.md). Collection pagination is endpoint-specific, with offset, keyset, decimal-ID, and unpaginated surfaces as described in [Pagination](docs/pagination.md).

The current Next.js API client sends `X-Request-ID` and `X-Workspace-ID` but does not attach a bearer token. Do not expose this API directly to a browser when it requires a privileged bearer credential; use a deliberately designed authenticated proxy/session boundary or finish the UI auth flow first.

### Security capability discovery

`GET /api/v1/security/capabilities` passes through common authentication; it is not a public bootstrap exemption. The response contains `authentication`, `agent_boundary`, `csrf`, `default_bind`, `roles`, and optional `allowed_origins`. Authentication is labeled `bearer-token` whenever either configured token is nonempty, otherwise `local-header-development`. This reports configuration presence, not token strength, expiry, external identity-provider integration, or a successful credential test.

`default_bind` is always the literal `127.0.0.1`, not the actual listener address, proxy exposure, container port mapping, or connection peer. `allowed_origins` is a copy of the configured server allowlist; its presence does not imply WebSocket origin enforcement. The `csrf` and `agent_boundary` fields are descriptive strings, not a machine-readable permission decision or proof that a deployment is safe for remote access.

Advertised roles are `view`, `operate`, `approve`, `govern`, `administer`, and `force-override`. The common human-role parser additionally accepts legacy `human`, `admin`, and `chair`, and permits an empty human role. It rejects other nonempty human roles with 403 `unknown_role`. These labels are not a centrally enforced role hierarchy: individual handlers determine their action-specific authorization, and many check human-versus-agent identity rather than a granular capability. Do not infer that `view` is universally read-only or that only `administer` can perform every maintenance operation from this discovery list.

In bearer mode, the token selects human-versus-agent kind, but actor ID remains supplied attribution. Human requests retain the supplied normalized role; an agent token forces role `agent`. In local development mode, role `agent` or exact header `X-Roundtable-Orchestrator: true` selects agent kind. Neither an actor name nor a browser-provided role establishes a verified external identity. Discovery does not enumerate endpoint permissions, workspace entitlements, runtime MCP policy, transaction acceptance gates, or filesystem sandbox guarantees.

### Exact CORS and request-body behavior

Allowed origins are exact string matches after trimming configured allowlist entries; there is no wildcard, hostname suffix, or automatic port equivalence. CORS preflight returns 204 for an absent/allowed Origin and 405 for a disallowed Origin. Allowed responses advertise GET/POST/PATCH/PUT/DELETE/OPTIONS, the configured header list, and a 600-second preflight cache. The middleware does not set `Access-Control-Allow-Credentials` or `Access-Control-Expose-Headers`; browser code may therefore be unable to read a cross-origin response's `X-Request-ID` header even though it can read a request ID in the JSON body.

The state-changing Origin check covers POST, PATCH, PUT, and DELETE only when an Origin header is present and nonempty after trimming. It does not require Origin on nonbrowser requests, inspect Referer, or verify an `X-CSRF-Token` value. Listing that header in the CORS allowlist does not implement a synchronizer-token mechanism. A disallowed read origin is not necessarily denied by this check; browser CORS still governs whether that origin can read the response. WebSocket upgrade origin rules are separate.

Handlers using the shared `decodeJSON` helper decode through a one-mebibyte `io.LimitReader`, reject unknown fields for typed structs, and require EOF after the first decoded value. This is not a general schema validator: map-based inputs retain arbitrary keys, required/domain values need handler checks, and JSON `null` can decode without establishing a populated object. The helper does not check content type itself. The byte limit truncates the reader rather than uniformly returning a dedicated 413; oversized/truncated input can produce decode errors, while bytes beyond the limited reader are not examined. Consult each operation's documented error semantics rather than assuming every malformed request has one universal code.

## Live event delivery

Notification selection, counts, acknowledgment, and retention boundaries are documented in [Notifications](docs/notifications.md). Marking a notification read is distinct from resolving its associated governance action.

The API exposes workspace event snapshots and a workspace WebSocket. Clients should load a snapshot, subscribe, track sequence IDs, and resynchronize when the server reports a gap or reconnect indicates stale state. The HTTP event service is distinct from the Go runtime's in-process event bus and local MCP socket. See the endpoint schemas and event-related operation descriptions in `openapi.yaml`.

## Database and maintenance

Workspace configuration revisions, defaults, preflight, confirmation, and their runtime limitations are documented in [Workspace Settings](docs/settings.md). They are distinct from CLI YAML configuration and process flags.

The API opens SQLite with the same WAL/foreign-key/busy-timeout setup as `internal/db`. If API and Go runtime share `.roundtable/roundtable.db`, coordinate maintenance and backups with all writers. Maintenance operations are fixed server-side actions, not arbitrary SQL; consult [Maintenance](docs/maintenance.md). Preserve database sidecars while writers are active and verify backup restore procedures.

## Request Logging and Metrics

Observability is implemented in `api/internal/httpapi/observability_metrics.go`. Each observed request records an `http_request` info log after its handler returns, including request ID, correlation ID, supplied workspace header, method, URL path, observed HTTP status, and whole-millisecond duration. It does not log the query string, request/response bodies, authorization header, actor, bytes transferred, SQL timing, or provider execution details in this middleware. Header/path fields remain caller-controlled attribution, not a verified workspace identity or complete audit trail.

`X-Correlation-ID` is copied verbatim when nonempty; otherwise a new ID is generated and returned in the response header. It is separate from `X-Request-ID`, and is not persisted here as a durable domain event. There is no trace propagation/sampling implementation or correlation-value validation in this middleware. The default CORS header lists do not include/expose this correlation header, so cross-origin browser clients should not assume they can set or read it.

### Counter Scope and Timing

Three atomic counters are held by the server instance: handled requests, observed statuses at least 400, and summed request duration in nanoseconds. They reset with a new server instance/process and are not shared with replicas or persisted in SQLite. There are no route/workspace/status labels, latency histogram, percentile calculation, in-flight gauge, byte counter, or export of process/Go runtime metrics from this code.

Counter updates occur after handler return. A long-lived WebSocket contributes only when its handler ends, and duration includes its connection lifetime rather than only handshake latency. Hijacking does not independently populate the wrapper's status, so a default 200 can be observed when no status was captured. Panics are not recovered or counted by a deferred instrumentation block here. CORS sits outside observability, so OPTIONS requests answered by that outer middleware do not reach these counters. A `/metrics` request reads counters before its own post-handler increment. Independent atomic reads are not one snapshot of all counters.

### Metrics Exposition Limitation

`GET /metrics` advertises `text/plain; version=0.0.4; charset=utf-8` and declares these names in HELP/TYPE lines:

- `roundtable_http_requests_total`: completed observed requests.
- `roundtable_http_errors_total`: observed responses with status 400 or greater.
- `roundtable_http_request_duration_nanoseconds_total`: accumulated observed duration.

**Current compatibility defect:** the writer emits each value as a bare numeric line rather than a sample containing its metric name. The declared names therefore do not have correctly named sample lines in the current response. Do not treat a successful HTTP scrape as proof that a Prometheus collector can ingest these counters. This documentation records the existing output; it does not repair the exporter. Validate collector parsing before relying on alerts or dashboards.

The endpoint passes through common HTTP authentication when tokens are configured; it is not an unauthenticated exception merely because it lacks `/api/v1`. Plan scraper credentials within the same service trust boundary. Request logs and metrics are not substitutes for persisted governance audit/events, and their absence is not proof that a state-changing operation never ran.

## Health and readiness boundaries

All three health routes pass through the common HTTP middleware, including authentication when tokens are configured. Their successful HTTP status is not a readiness verdict; inspect the JSON `status`, component statuses, and optional `degraded_reasons`.

| Route | Response and checks |
| --- | --- |
| `GET /api/v1/health` | HTTP 200 with literal `status: "ok"`, configured build `version` (default `dev`), and `request_id`. No database, repository, agent, or coordinator probe is performed. |
| `GET /api/v1/status` | HTTP 200 with aggregate `status`, version, literal `api_version: "v1"`, current UTC RFC3339Nano `time`, request ID, and component map. Aggregate status is `degraded` whenever a degradation reason exists, otherwise `ok`. |
| `GET /api/v1/workspaces/{id}/health` | Resolves the workspace, counts its impact records, and combines node components with a workspace component. Returns HTTP 200 for an ordinary degraded result; workspace lookup and impact-query failures use error responses instead. |

### Node component interpretation

`event_stream` and `transaction_manager` are unconditionally marked `ready`; this handler does not test subscriber delivery, replay, transaction execution, locks, or coordinator activity. `repository_index` is also unconditionally `ready`, with `details.configured_roots` equal to the number of configured allowed roots. That count does not establish that roots exist, are accessible, contain repositories, or have a fresh index.

Database status is determined by `SELECT 1` followed by `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`. A missing store is `unavailable`; connectivity failure is `unavailable`; a migration-query failure after successful connectivity is `degraded` with `details.connectivity: "ready"`. A successful migration query reports `ready` and `details.migration_version`, even if that value is zero or differs from the expected application schema. This is not an integrity check, writable-transaction test, schema completeness check, backup validation, or migration compatibility assertion. Use the separate maintenance operations for their documented checks.

`agent_adapters` uses all persisted agent rows, not a workspace-filtered set. Only exact stored statuses `ready` and `idle` count as ready. The aggregate reports `details.configured` and `details.ready`; zero agents is considered `ready`, while any other stored status makes a nonempty set `degraded`. The check does not filter disabled agents or inspect sessions, heartbeat age, installed commands, provider credentials, MCP sockets, or running processes. A list-query failure is `degraded`; a missing database store makes this component `unknown`. Actual command diagnostics are a separate operation described in the [adapter reference](../docs/ADAPTERS.md).

### Workspace component and impact counts

Workspace health inherits the node's global degradation reasons, so an unrelated agent row can degrade every workspace response. Exact workspace status `active` maps to component status `ready`. Any other status is copied into the component and adds the literal reason `workspace is detached`, even if the stored value is not `detached`. The handler does not check that the workspace directory or current Git branch is usable.

The `impact` object counts exact workspace-ID matches independently:

- `active_sessions`: sessions whose stored status is `active` or `running`.
- `active_claims`: claims whose stored status is `active`, without checking expiry.
- `open_proposals`: proposals whose stored status is `pending` or `in_review`.
- `active_transactions`: transactions whose stored status is `pending` or `running`.

These four queries are not one transactional snapshot, do not include legacy rows lacking that workspace ID, and do not themselves change the aggregate health status. Counts can describe stale records rather than live work. A query failure returns HTTP 500 with code `workspace_impact_failed` before node health is assembled. Workspace responses include workspace metadata, status, components, impact, optional reasons, and request ID, but not the node response's version, API version, or timestamp fields.

For monitoring, distinguish process liveness (`/health`), this limited status projection (`/status`), workspace lifecycle state, and independently verified operational readiness. HTTP-only probes cannot detect a degraded JSON result, and an `ok` result does not prove agents can deliberate, propose, test, or mutate through the transaction manager.

## Event snapshot and WebSocket protocol

`GET /api/v1/workspaces/{id}/events/ws` requires a nonblank actor identity in `X-Actor-ID`, falling back to the `actor_id` query parameter. This is in addition to common authentication, not a replacement for configured credentials. Missing identity returns 401 `authentication_required`. The upgrader's `CheckOrigin` accepts every origin; do not assume the mutation-origin policy protects this GET upgrade. Deployments must enforce their intended WebSocket origin boundary separately.

The optional `last_event_id` query value is a base-10 signed 64-bit integer that must be nonnegative. Omission means zero, not "live events only". Invalid values return 400 `invalid_event_cursor` before upgrade. There is no opaque cursor, timestamp cursor, or workspace-local consecutive sequence: both `event_id` and `sequence` are the global SQLite event ID. IDs skipped by another workspace are normal and do not alone prove lost delivery.

After upgrade, the server sends a JSON `hello` with `schema_version: "1"`, workspace ID, current persisted sequence, and `resumable: true`. It then subscribes to the process-local bus with buffer capacity 64 and queries historical events whose IDs exceed the supplied cursor, ascending by ID. Workspace membership means either the event's `run_id` equals the workspace ID or that run has a matching `runs.workspace_id`. Other legacy/global events are not automatically included.

### Replay and recovery

The replay query reads at most 1,001 records. When more than 1,000 qualify, the server sends `kind: "resync_required"`, reason `cursor_not_retained`, and the previously read current sequence, then closes without sending that historical batch. Despite the reason's name, this checks pending replay size, not actual deletion or oldest-retained ID. A cursor beyond the current maximum is accepted and produces no historical events; there is no future-cursor validation. Historical query failure after upgrade closes the connection without a structured problem response.

Subscription occurs before the historical query, so an event can appear in both replay and the live buffer. Clients must deduplicate IDs and must not assume replay/live ordering is strictly increasing. The bus is best-effort, silently drops notifications when a subscriber buffer is full, and does not bridge separate API/runtime processes merely because they share SQLite. The server does not acknowledge client cursors, persist subscriber offsets, automatically retry delivery, or emit a bus-overflow gap message.

Every 20 seconds the server sends an application-level JSON message `kind: "ping"` with workspace ID and the currently persisted workspace sequence. It is not a WebSocket ping control frame. A sequence advance can prompt durable reconciliation, but does not identify which domain state changed. Errors reading the maximum sequence are ignored and can report zero. Writes have a five-second deadline refreshed for each ordinary event or periodic ping. The read loop discards received messages; there is no documented command protocol, read deadline, configured read-size limit, or application pong requirement here. Transport read/write errors end the connection rather than reconnecting it.

Event envelopes have no `kind` field: distinguish them by their event fields from `hello`, `ping`, and `resync_required`. They include ID/sequence, workspace ID, event type, stored creation time as `occurred_at`, optional actor/entity fields, and payload. `entity_type` is the prefix before the first dot in the event type. `entity_id` is the first string payload value found in this order: `proposal_id`, `claim_id`, `vote_id`, `approval_id`, `transaction_id`, `policy_id`, `session_id`. It is not a universal entity lookup. Payload decoding and heuristic redaction are applied; malformed/non-object payloads can fall back to a redacted `text` value. Redaction is not proof that arbitrary secret content is safe to publish.

### Snapshot contents and consistency

`GET /api/v1/workspaces/{id}/events/snapshot` returns workspace metadata, global event-ID sequence, UTC RFC3339Nano generation time, retention descriptors, domain version markers, and a small state summary. Retention reports `mode: "append_only_bounded_replay"`, `resume_by: "last_event_id"`, `max_replay_events: 1000`, and this workspace's snapshot path. These fields describe replay behavior; they do not configure cleanup or establish a retention duration.

The snapshot is **not** a complete export of tasks, claims, proposals, or other records. Its `snapshot` contains the workspace, domain counts, and latest workspace run status (default `stopped` when the lookup fails or finds nothing). Fetch the relevant collection endpoints to rebuild domain state. Version markers contain total row counts and optional latest timestamps, not revision tokens or content hashes:

- Events use the same direct-workspace/run-membership filter as replay and latest creation time.
- Sessions include only sessions whose run belongs to the workspace; their timestamp is the maximum of each row's `last_seen_at` with `started_at` as a null fallback.
- Claims and proposals use exact workspace ID and latest creation time, including terminal rows.
- Transactions use exact workspace ID and latest applied time, with null applied times treated as empty strings.
- Deliberations and policies use exact workspace ID and latest update time.
- Notifications use exact workspace ID and latest creation time, without recipient, acknowledgement, or actionability filtering.

Each marker query is independent. Failed queries omit that domain from both markers and counts rather than failing the whole HTTP response. Sequence, workspace metadata, run state, and markers are not read in one transaction; `generated_at` is not a snapshot isolation guarantee. A count or creation-time marker may remain unchanged after a record edit. Reconnect using the last processed event ID, deduplicate replay, and refresh the affected domain collections when reconciling; do not treat a successful snapshot or a connected socket as complete state synchronization.

## Operational logs and audit exports

`GET /api/v1/workspaces/{id}/logs/operational` projects persisted `events` rows; it does not read the HTTP logger, agent stdout/stderr, filesystem logs, or test log files. Workspace membership uses direct `run_id == workspace_id` or a run belonging to the workspace. `GET /api/v1/workspaces/{id}/audit` instead reads `audit_events` with exact workspace ID. These are separate ledgers with different coverage, not interchangeable evidence that every operation was recorded atomically.

Both lists default to 50 rows, accept integer `limit` from 1 through 200, and return newest IDs first. Invalid limits return 400 `invalid_limit`. Optional `cursor` is a positive decimal ID; invalid values return 400 `invalid_cursor`. Subsequent pages select IDs strictly below that cursor. Responses contain `items` and `next_cursor`, with null when no extra row was found. Cursors do not bind filters or establish a database snapshot. Queries and scan failures use `operational_log_list_failed` or `audit_list_failed`; the loops do not separately check the final `rows.Err()`, so a late iteration error can be indistinguishable from a shorter successful page.

### Filters and projected fields

Operational filters are `component`, `agent_id`, `session_id`, `proposal_id`, `transaction_id`, `request_id`, `severity`, `from`, and `to`. Component uses SQL `LIKE` against event type with the supplied value plus `.%`. Other identity/severity filters search raw payload text for a compact JSON substring beginning with the key and supplied value, rather than parsing JSON or comparing exact values. A value can match a longer prefix; whitespace-formatted JSON can fail to match; `%` and `_` retain SQL wildcard behavior. Parameters are bound, but that does not make these filters semantic JSON equality checks. Returned default severity `info` does not imply an explicit payload severity exists for filtering.

Operational records include event ID, type, component (type prefix before the first dot), severity (string payload value or `info`), optional actor/request/agent/session/proposal/transaction IDs, redacted payload, and stored creation time. Payload IDs must be strings to populate the projected fields. Actor comes from the event row; the other identities come from payload content, not verified foreign-key relationships.

Audit filters `actor`, `action`, `request_id`, and `entity_id` use exact stored-column equality. Audit records include ID, actor, action, entity type/ID, optional request ID/reason, redacted payload, and creation time. Actor, entity ID, and reason receive text redaction; action, entity type, and request ID are not separately text-redacted in this handler. Both surfaces use heuristic payload redaction, falling back to a redacted `text` field for malformed or non-object JSON. This is not a complete secret-detection or authorization boundary.

For either surface, `from` and `to` are inclusive bounds and are accepted only if parseable as RFC3339Nano. Invalid values are silently ignored, not rejected. Valid values are compared as stored SQL timestamp text without normalization to one timezone/precision. Do not assume equivalent timestamp spellings or reversed bounds are validated or chronologically normalized.

### Export formats and limits

`GET /api/v1/workspaces/{id}/audit/export` applies the same audit filters, orders newest first, and selects at most 1,000 rows. It does not apply list `limit` or `cursor` and supplies no continuation token or truncation count. Therefore it is not an exhaustive workspace audit backup. `format` is lowercased but not trimmed:

- `csv` returns `text/csv` with columns `id,actor_id,action,entity_type,entity_id,request_id,reason,created_at`. Payload is omitted. Standard CSV quoting is used, but spreadsheet formula prefixes are not neutralized; treat downloaded cells as untrusted text.
- `ndjson` returns `application/x-ndjson`, one full audit record per line, including redacted payload.
- Any other value, including omission or misspelling, returns a JSON object containing `items` and literal `bounded: true`. That flag is always true, not evidence that truncation actually occurred.

There is no attachment filename, compression option, export job, stable snapshot token, or streaming database iterator exposed to clients: rows are collected in memory before writing. Export query/scan failures return `audit_export_failed`; final row-iteration errors and CSV/NDJSON write errors are not checked. A successful HTTP status alone cannot establish a complete download.

After writing the response, the handler attempts to insert `audit.exported` with the supplied actor header, current request ID, selected row count, and lowercased format value. Insert failure is ignored. The new record is not in that export's already-selected batch, and a successful download does not prove its export audit record was persisted. Unknown formats are recorded with their supplied lowercased value even though JSON was returned.

## Consensus analytics

`GET /api/v1/workspaces/{id}/analytics/consensus` derives aggregates from stored proposals, consensus snapshots, votes, audit rows, and human approvals. It is not live agent telemetry, provider usage, or a measurement of transaction success. Optional trimmed `from` and `to` values must parse as RFC3339Nano and are normalized to UTC. Defaults are computed together as now and now minus 24 hours; supplying only one bound leaves the other original default unchanged. The interval must have `from < to` and span at most 31 days, otherwise HTTP 400 `invalid_analytics_window` is returned. Future windows are not independently prohibited.

Queries use an inclusive lower and exclusive upper bound, formatted as SQLite-style UTC timestamp text. Mixed stored timestamp formats are not normalized by these predicates. The response includes the normalized window and a separate UTC `generated_at`; queries are independent, not a transactionally consistent snapshot.

| Field | Actual calculation |
| --- | --- |
| `proposal_count` | Proposals with exact workspace ID created inside the window. |
| `consensus_count` | Number of `approved` or `blocked` consensus snapshot rows in the window joined to this workspace's proposals, not distinct proposals. Proposal creation may precede the window. |
| `success_rate` / `rejection_rate` | Approved/blocked snapshot counts divided by consensus count. Values are fractions, not percentages; both are zero with no qualifying terminal snapshots. |
| `quorum_failures` | Number of snapshots with exact status `pending`, not diagnosed quorum failures or unique stalled proposals. |
| `abstention_rate` | Exact `abstain` vote rows divided by all qualifying vote rows, including other/unrecognized vote values. Zero when no votes qualify. |
| `mean_time_to_consensus_seconds` | Mean of snapshot creation time minus proposal creation time for both approved and blocked snapshots, using SQLite `julianday`. No clamp, first-terminal selection, or deduplication. |
| `p50_time_to_consensus_seconds` / `p95_time_to_consensus_seconds` | Sorted duration sample at rounded index `(n - 1) * fraction`, not interpolated percentiles. All duration fields remain zero without terminal samples. |
| `human_interventions` | Workspace audit rows in the window whose action matches `human.%` or `%approval%`. Actor kind is not checked; this is an action-name heuristic. |
| `policy_overrides` | All local human approvals with status `approved`, `override_policy = 1`, and update time in the window. **This query is not workspace-filtered**, so the count can include other workspaces. |

`agent_participation` is sorted by agent ID and includes only agents with qualifying votes on proposals belonging to the workspace. Each item counts all vote rows as `votes`, with exact `approve`, `reject`, `abstain`, and `veto` categories. It does not require proposal creation within the window, verify agent readiness, normalize unknown vote values, weight votes, or include agents that never voted. Repeated persisted snapshots/votes are counted as rows rather than independent deliberations or unique participants.

Primary proposal, snapshot, and vote query/scan failures return HTTP 500 `analytics_failed`. Final row-iteration errors are not separately checked. Human-intervention and override query errors are ignored, leaving zero counts indistinguishable from no matches. There is no pagination, cache configuration, grouping interval, confidence interval, or minimum sample threshold. Read these metrics with their underlying row scopes before using them to compare workspaces or infer governance effectiveness.

## Activity feed projection

The workspace activity endpoint projects persisted `events` rows, newest global event ID first. Workspace membership uses direct event `run_id` equality with the workspace ID or membership of that run in the workspace. It does not merge `audit_events`, dedicated session events, notifications, HTTP request logs, or provider output. Missing activity therefore does not establish that no operation occurred.

Optional `entity`, `category`, and `severity` query values are trimmed and lowercased; `actor` is trimmed but case-sensitive. Filters are applied in memory **after** normalization and redaction, not as database predicates. Category and entity type both equal the event-type prefix before its first dot, so they are currently redundant selection dimensions. Stored event-type prefixes are not lowercased, meaning an unusually capitalized prefix may not match the normalized query. Actor comparison uses the redacted actor value, not necessarily the original stored ID. Unknown filter values simply produce an empty selection.

Each item contains `id`, `type`, `category`, optional `actor_id`, `entity_type`, optional `entity_id`, `severity`, `title`, `metadata`, and `created_at`. Metadata is a recursively redacted object; malformed JSON and valid non-object JSON become an empty object, unlike the operational-log fallback `text` payload. Severity defaults to `info`; a string metadata value is lowercased but not trimmed or validated, and an explicitly empty string remains empty.

Entity ID is the first string metadata value found in this order: `proposal_id`, `claim_id`, `approval_id`, `transaction_id`, `policy_id`, `session_id`, `contention_id`, `vote_id`. Selection is heuristic and differs from the WebSocket envelope's precedence. An empty first string can prevent a later populated key from being selected. Titles have explicit friendly labels for `proposal.created`, `claim.acquired`, `vote.cast`, `policy.passed`, `approval.required`, `transaction.committed`, `session.resumed`, and `memory.updated`; other event names are title-cased after replacing dots with spaces, then underscores are replaced with spaces. A title is display copy, not a validated lifecycle transition.

The handler loads and normalizes the full matching workspace event history before applying the shared offset pagination described in [Pagination](docs/pagination.md). Default limit is 50, accepted limits are 1 through 200, and the response contains `items` plus optional continuation cursor. The page size does not bound database scanning or memory consumption. Filters, redaction, and concurrent inserts can shift offset pages; this is not the WebSocket's event-ID resume protocol. Query/scan failures return HTTP 500 `activity_list_failed`; final row-iteration errors are not separately checked. No time-range, full-text search, entity-ID filter, live subscription, or audit-export guarantee is implemented by this handler.

## Dashboard summary semantics

The workspace dashboard summary is a mixed-scope projection, not a complete readiness probe. It returns workspace ID, UTC `generated_at`, a fixed preceding-24-hour `measurement_window`, agent count, proposal queue, two rate fractions, runtime labels, latest run state, and repository information. There are no configurable window query parameters in this handler.

`agents_online` counts **global** enabled agent rows with exact status `ready` or `idle`. It does not count active sessions or test heartbeats/process availability. This differs from node health, which includes disabled agent rows in its aggregate. Proposal queue counts are exact workspace-ID matches across all creation times: `pending`, `in_review`, `rejected`, and an `approved` bucket combining stored `accepted` and `approved`. Other statuses are excluded. The advertised measurement window does not constrain these queue counts or agent count.

`consensus_success_rate` divides approved terminal consensus snapshot rows by approved-plus-blocked snapshot rows created within the window and joined to workspace proposals. Repeated snapshots are separate samples, not distinct proposals. `policy_pass_rate` divides evaluation rows whose exact result is `pass` by all evaluation rows in the window joined to policies belonging to this workspace. Other results remain in the denominator. Both are fractions from zero to one for ordinary data, not percentages; zero can mean no samples, not proven failure. Timestamp predicates compare formatted SQLite-style UTC text without normalizing mixed stored timestamp formats.

`transaction_manager: "ready"`, `mcp_enforcement: "orchestrator-only"`, and `memory_oracle: "ready"` are fixed labels. The handler does not verify transaction execution, MCP authorization, memory service availability, or coordinator ownership. `run_state` is the latest workspace run status by start time, defaulting to `stopped` when empty or unavailable; it is not a running-process check. Repository inspection is attempted separately, with failures represented only as `{status: "unavailable"}` rather than exposing the inspection error.

All aggregate SQL query errors are ignored, so a HTTP 200 response can contain zero/default values caused by unavailable data. Queries and repository inspection are not one atomic snapshot. The response includes no per-counter error, sample count, query freshness, or degraded-reason field. Use the dedicated health, analytics, run, and repository endpoints for their documented detail; do not infer operational readiness or a complete 24-hour performance assessment from this summary alone.

## Workspace Registry and Lifecycle

Workspace list returns all stored workspaces, including detached rows, ordered by stored display name then ID. It is unpaginated: `items` is always an array and `next_cursor` is null; limit/cursor query values do not restrict this handler. Nil store returns HTTP 503 `store_unavailable`; query/scan/iteration errors return HTTP 500 `workspace_list_failed`. Detail uses the trimmed URL workspace ID, not `X-Workspace-ID`. Most lookup errors become HTTP 404 `workspace_not_found`, including database failures; missing store is separately HTTP 503.

Projections contain ID, display name, optional root alias, canonical repository identity, status, optional default branch, creation time, optional last-opened time, and revision. Root path and stored update time are not exposed. Reading a workspace does not update its last-opened time, verify its directory still exists, or establish a live runtime process. The shared lookup does not reject detached rows, so detachment is not universal endpoint-access revocation.

### Registration and Root Validation

Creation uses generic actor authorization, not a human-role check, and has no handler-level idempotency-key requirement. JSON accepts `display_name`, `root_alias`, `root_path`, and `default_branch`; malformed/unknown typed fields return HTTP 400 `invalid_json`. Root is required after a blank check, but its original text is used for path resolution. Relative roots resolve from API process working directory. The handler makes the path absolute, resolves symlinks, and requires an existing directory inside or equal to a symlink-resolved configured allowed root. Unresolvable allowlist entries are skipped; no configured roots permits no registration. Failure returns HTTP 400 `invalid_workspace_root`.

This validates directories, not Git repositories: it does not inspect HEAD, discover remotes, initialize Git, load runtime YAML, or launch a coordinator. Display name defaults to the canonical directory basename when blank; display name, alias, and default branch are trimmed. Default branch is metadata and is neither validated as a Git ref nor checked out. Canonical identity is `path-sha256:` plus SHA-256 of the canonical root path, not a remote repository identity or content hash. Moving the checkout can change that identity.

ID is `ws-` plus eight random bytes encoded as hexadecimal, with Unix-nanosecond fallback if randomness fails. Creation starts active at revision 1 and current last-opened time. Store insertion, audit, and outbox share a SQL transaction; all returned store errors use HTTP 409 `workspace_create_conflict`, not necessarily duplicate-root errors. Root path is unique in the database. HTTP 201 projects the in-memory input without reloading database defaults, so its `created_at` can be empty even though a later GET returns the database timestamp. No configuration scaffold, sessions, claims, or filesystem artifacts are created.

### Metadata Update and Detachment

PATCH uses the same typed input and generic actor check. Nonblank trimmed display name, alias, and default branch replace their fields; empty/null values cannot clear existing values. Accepted `root_path` is ignored: this endpoint cannot relocate or revalidate a workspace root. Even an empty object updates last-opened time, increments revision, and writes audit/outbox. Status and canonical identity remain unchanged, including on a detached workspace. A default-branch metadata edit does not execute Git switching.

For PATCH and detach, absent/blank `If-Match` falls back to the revision loaded by the handler. Nonempty input must be a plain integer after whitespace trimming; quoted entity tags, weak tags, and wildcard syntax are not accepted by this helper. Invalid input becomes revision -1. This differs from settings' quote-stripping parser. Version mismatch uses HTTP 409 `workspace_revision_conflict`; other update errors use `workspace_update_conflict`, including invalid positive-revision checks. There is no mandatory caller-supplied version or handler-level idempotency header.

Detach uses generic actor authorization, ignores request-body options, and first calculates workspace impact. Failure uses HTTP 500 `workspace_impact_failed`. **Nonzero impact does not block detachment**: the handler changes status to `detached` and increments revision without stopping sessions, releasing claims, resolving proposals, cancelling transactions, deleting rows/files, or shutting down a runtime. Counts use the exact state filters described under branch preflight and are not atomic with the later update. Repeated detachment can advance revision again. Success is HTTP 200 with `workspace` and pre-update `impact`; errors other than revision conflict use HTTP 409 `workspace_detach_conflict`. No HTTP attach/reactivate action is provided here.

Both versioned writes check affected rows and transactionally persist workspace audit/outbox with the new revision. Audit/outbox insertion failures roll back the SQL mutation. Outbox IDs are `workspace:<id>:<revision>:<action>`, where action is `workspace.created`, `workspace.updated`, or `workspace.detached`; payload contains workspace ID, revision, and action. Publication is separate from committing that row. PATCH/detach reload after commit, so a reload error can return a conflict response despite a committed change. Inspect revision, status, audit, and outbox before retrying an uncertain result.

## Repository Status and Branch Control

Repository status inspects the selected workspace root using local Git commands: `branch --show-current`, `rev-parse HEAD`, `status --porcelain=v1`, `remote -v`, and, for an attached branch, `rev-list --left-right --count @{upstream}...HEAD`. It does not fetch remotes. Missing upstream/count errors leave ahead/behind at zero; zero therefore does not prove synchronization. Failure of the other required commands returns HTTP 409 `repository_status_unavailable`, including an unborn repository without a resolvable HEAD.

The response includes workspace ID, optional root alias, branch, HEAD SHA, dirty flag, ahead/behind, detached flag, remotes, `index` change entries, and protected-path summary. Detached status uses literal branch `HEAD`. The `index` collection includes worktree and untracked changes, not only staged files. Each item has path and one-character index/worktree status. Remotes are deduplicated by name using the first `remote -v` entry; separate fetch/push URLs are not retained, and URLs are not redacted. Avoid embedding credentials in remote URLs.

Status parsing is line-based, not NUL-delimited. It trims the entire porcelain output before parsing, which can remove the leading blank status column from the first worktree-only entry and misalign that entry's status/path. Rename paths use text after the first ` -> `; quoted/escaped filenames are not decoded, and embedded newlines or arrow-like names are not robustly represented. Dirty is derived from parsed entries. Empty change/remote slices can serialize as null. This projection is not an exact Git status machine interface.

Protected paths are a fixed lexical classification of `.roundtable`, `.github`, and `TASKS.ROUNDTABLE`, including descendants. The summary counts matching parsed changes and optionally lists paths. It is not a configurable protected-branch policy, a separate switch blocker, or proof that all sensitive files have been detected.

### Branch Input and Preflight

Preflight and switch require human authorization plus common authentication; switch additionally requires a nonblank trimmed `Idempotency-Key`. Input is JSON `{ "branch": "name" }`, trimmed before use. Empty values, exact `HEAD`, CR/LF, absolute paths, any `..` substring, and leading `-` are rejected with HTTP 400 `invalid_branch`. This is a limited safety check, not complete `git check-ref-format` validation. No shell is used: commands invoke the Git executable with argument arrays. There is no configured command timeout beyond request-context cancellation.

Preflight inspects current repository state and separately queries workspace impact. Its response is HTTP 200 with workspace ID, target branch, allowed boolean, blocker array, and repository projection, even when blocked. It does not verify target existence, fetch it, check remote tracking, or issue a reservation token. Impact-query failure returns HTTP 500 `branch_preflight_failed`.

| Blocker | Exact condition |
| --- | --- |
| `dirty_worktree` | At least one parsed repository change. No automatic stash or cleanup. |
| `active_transactions` | Workspace-tagged transactions in `pending` or `running`; `staged` is excluded. |
| `active_claims` | Workspace-tagged claims in `active`, without expiry checks. |
| `open_proposals` | Workspace-tagged proposals in `pending` or `in_review`. |
| `active_sessions` | Workspace-tagged sessions in `active` or `running`; `starting` and `paused` are excluded. |
| `already_on_branch` | Current reported branch equals target. Switching to the current branch is blocked rather than a no-op success. |

Counts use each table's stored `workspace_id`, not inferred associations; untagged legacy records can be excluded. Queries are independent, not a single atomic impact snapshot. Blocker counts are included only when nonzero. No active deliberation, detached-HEAD, protected-branch, or behind-upstream blocker is separately imposed.

### Switch Persistence and Failure Recovery

Switch acquires one process-global mutex across all workspaces, re-inspects status, and repeats preflight. This mutex does not coordinate other API processes, external Git writers, runtime patch application, or new sessions/claims created after preflight. A blocked switch returns HTTP 409 with the preflight JSON object, **not** the usual problem object. Allowed execution runs `git switch <target>` without force, create-branch, fetch, or stash options; Git failure returns HTTP 409 `branch_switch_failed`.

After Git succeeds, repository status is inspected again. The store then updates the active workspace at the revision loaded for this request: sets default branch to the observed branch, increments revision, updates open/update times, and atomically inserts `repository.branch_switched` audit and event-outbox records. This is a database transaction only, not an atomic Git/database transaction. The outbox ID is `repository:<workspace-id>:<revision>:branch-switched`, with before/after branches and new revision. The handler does not require a caller-provided expected revision or reuse the earlier preflight response.

If post-switch inspection or recording fails, the handler attempts `git switch <before-branch>` using a background context, with no timeout and ignored rollback errors. For a detached starting state the recorded before branch is literal `HEAD`; this is not preservation of the original detached commit. Do not assume failure restored the repository. Revision conflict returns HTTP 409 `workspace_revision_conflict`; other recording failure returns HTTP 500 `branch_switch_record_failed`; post-switch inspection failure uses `repository_status_unavailable`. A store reload can fail after its transaction commits, so a recording error can coexist with committed audit/outbox/workspace state and an attempted Git rollback.

Success returns HTTP 200 with updated workspace and before/after repository projections. No session restart, resource reindex, or proposal migration occurs here. The handler itself does not cache idempotency responses; repeating a successful request can encounter `already_on_branch`. For an uncertain response inspect Git branch/HEAD, workspace revision/default branch, audit, and outbox before retrying or restoring anything.

## Deliberation administration

The HTTP deliberation catalog is persisted control-plane state, not the runtime's turn scheduler. List loads workspace-owned IDs oldest creation time then ID, reads each detail projection, silently skips individual detail failures, and then applies shared offset pagination. The outer query checks scan and final iteration errors (`deliberation_list_failed`). Detail requires exact workspace membership; lookup failures become HTTP 404 `deliberation_not_found`.

Creation requires human authorization and a nonblank trimmed `Idempotency-Key`, in addition to common authentication. Input fields are `goal`, `agent_pool`, `governance_profile`, `initial_resource_scope`, `budget_limit`, `time_limit_seconds`, `human_constraints`, and `moderator_id`. Goal/moderator and each agent ID are trimmed. Goal and a nonempty pool are required; blank pool entries are rejected. Each pool agent must exist, but enabled status, adapter readiness, pool uniqueness, moderator existence/membership, resource validity, profile existence, and positive budget/time limits are not checked. Duplicate participants can fail insertion rather than being deduplicated. Optional configuration is stored as metadata, not applied as execution limits by this handler.

Creation generates `deliberation-<UnixNano>`, stores status `draft`, inserts participants and round 0 with status `pending`, and writes `deliberation.created` audit in one SQL transaction. Round 0 receives a start timestamp even though it is pending. Begin failure returns HTTP 500 `deliberation_create_failed`; insert/audit/commit failures return HTTP 409 `deliberation_create_conflict`. Invalid JSON/required fields use HTTP 400 `invalid_deliberation`; failed agent lookups use HTTP 404 `agent_not_found`. Audit stores the configuration metadata without field redaction and uses the incoming `X-Request-ID`, which can differ from the effective generated request ID used for the main event. After commit, reload and event errors are ignored; success returns HTTP 201. No session or worker is launched.

### Lifecycle controls

Lifecycle changes require human authorization and a nonblank trimmed idempotency header. They do not decode a request body or consume a supplied reason, constraints, or expected version.

| Action | Allowed stored source statuses | Target status |
| --- | --- | --- |
| `start` | `draft`, `paused` | `running` |
| `pause` | `running` | `paused` |
| `resume` | `paused` | `running` |
| `terminate` | `draft`, `running`, `paused` | `terminated` |

Unknown actions return HTTP 400 `invalid_transition`; disallowed source status returns HTTP 409 `illegal_deliberation_transition`. First transition to running sets `started_at` only if SQL null; terminate sets `ended_at`; all changes update `updated_at`. The status update has a previous-status predicate, but affected rows are not checked, so a concurrent change can cause no update while an audit is still written. Update and before/after audit payload commit together. Begin failure uses HTTP 500 `deliberation_transition_failed`; write/commit failure uses HTTP 409 `deliberation_transition_conflict`.

Post-commit reload and main event errors are ignored. Success returns HTTP 200 with the projection; event run ID is the deliberation ID. These actions do not change round rows, invoke adapters, pause/kill sessions, release claims, allocate agent turns, or enforce stored budgets. A `running` response is not evidence of a running agent process.

### Human message insertion

Message insertion requires human authorization but no handler-level idempotency header. Only `running` or `paused` deliberations accept input; other statuses return HTTP 409 `deliberation_not_active`. Input accepts `body` and `message_type`; malformed JSON or blank trimmed body returns HTTP 400 `invalid_message`. Original body whitespace is retained and limited text redaction is applied before storage. Exactly empty type defaults to `human_instruction`; arbitrary other strings, including whitespace-only types, are retained without enum validation.

The handler inserts a timestamp-derived message ID, null agent ID, supplied actor, and sequence `MAX(sequence)+1` starting at 0. Sequence-query errors are ignored; allocation is not a caller-controlled concurrency protocol. Message and audit commit together, but audit payload always labels type `human_instruction`, even for another supplied type. Begin failure uses HTTP 500 `message_failed`; write/commit failure uses HTTP 409 `message_conflict`. A post-commit `deliberation.message_added` event is attempted with ignored errors. HTTP 201 includes ID, deliberation ID, type, redacted body, sequence, and UTC RFC3339Nano creation time, not actor or agent ID. Repeated submissions create messages rather than deduplicating; insertion does not invoke agents or guarantee consumption of an instruction.

### Detail projection limitations

The response exposes goal (stored title), status, creator/moderator, configuration metadata, participants, round, conflicts, linked proposals, and timestamps. Participants are sorted by agent ID, and `agent_pool` is reconstructed from that list rather than preserving creation order. Round is the maximum stored round number, not necessarily a running round. Unresolved conflicts are text-redacted summaries of exact `open` rows ordered by creation time, without conflict IDs or resolution actions. **Linked proposals are not populated by this reader**, so the field can be null even when related proposal data exists.

Participant/conflict query and scan errors and round lookup errors are ignored, allowing partial/default values in an otherwise successful response. Metadata decoding errors are ignored; numeric budget/time fields are reconstructed from JSON numbers, not evaluated as remaining limits. Resource scope and human constraints receive limited text redaction when projected, but goal, governance profile, actor/moderator IDs, and stored creation audit metadata do not receive that treatment. Empty non-optional slices can serialize as null; optional empty configuration fields are omitted. Neither the projection nor limited redaction provides a comprehensive secrets filter or an execution-health assessment.

## Deliberation transcript projection

Transcript list and entry-detail endpoints first resolve a workspace-owned deliberation. They merge its `deliberation_messages` with general events whose run ID is the deliberation ID. They do not include every event for the workspace/run, dedicated session logs, provider conversation history, or unrecorded tool output. The result is a display projection, not a complete conversation export.

Messages initially load by sequence then ID, but the merged result is re-sorted by **stored timestamp text ascending**, with prefixed entry ID as the tie-breaker. This can differ from message sequence, chronological order across differently formatted timestamps, and numeric event-ID order (`event:10` sorts before `event:2` on equal timestamps). Every entry receives the deliberation's current `round`, not a historically recorded per-message round.

Message IDs are `message:<stored-id>`; event IDs are `event:<decimal-id>`. Message entries have kind `message`, message type, content, and visibility class `user_visible`. Message types containing case-insensitive `chain_of_thought` or `hidden_reasoning` are excluded. This name-based exclusion does not detect internal reasoning hidden in an otherwise ordinary message body. Actor, message type, and body receive limited text redaction, not comprehensive secret scanning.

Event entries have kind `event`, type as `summary`, visibility class `operational`, and no full payload content. References are extracted only from nonempty string payload keys `tool`, `claim`, `proposal`, `vote`, or their `_id` alternatives; an `_id` value overrides its corresponding unsuffixed key. Arrays and arbitrary nested references are ignored. Each recognized reference is a single-element array, with text redaction applied. Reference labels are not checked for entity existence or access rights.

All entries carry `rendering: {format: "markdown", html_allowed: false, links: "untrusted"}`. These are client rendering instructions, not server-side HTML sanitization or safe-link validation. Renderers must enforce them and treat returned Markdown and links as untrusted.

The list loads the entire projection before shared offset pagination (default 50, limit 1 through 200). Entry detail also constructs the entire projection, then matches either an exact prefixed ID or an unprefixed suffix. An ambiguous unprefixed ID returns the first matching entry in merged order; use full prefixed IDs. A missing entry returns 404 `transcript_entry_not_found`. Loading errors containing the phrase `not found` are classified as 404 `deliberation_not_found`; other errors return 500 `transcript_failed`. This string-based classification is not a typed distinction between missing data and every possible storage failure.

## Documentation maintenance

The endpoint guide is generated from `openapi.yaml`:

```sh
ruby api/scripts/generate-admin-api-guide.rb
ruby api/scripts/verify-openapi.rb
```

The generator writes both `api/docs/admin-api-guide.md` and `api/docs/schema-reference.md`. CI runs the generator before assembling the docs site so published endpoint-to-schema links reflect the current contract.

The scripts use Ruby's standard YAML/JSON libraries and resolve the default specification relative to their own directory. The verifier accepts an optional first argument naming another YAML file; the generator always reads the repository's `api/openapi.yaml`. Regeneration overwrites its output pages, so make durable shared-copy changes in the generator rather than hand-editing those pages.

`verify-openapi.rb` is a lightweight structural gate, not a full OpenAPI/JSON Schema validator. It checks exact version `3.1.0`, presence of components/schemas, path prefixes (`/api/v1/` or exact `/metrics`), and operation IDs/nonempty responses for GET/POST/PATCH/PUT/DELETE. It walks references under `paths` and checks direct `#/components/schemas/` targets exist. It does not validate component-only reference chains, external references, schema semantics, examples, response bodies, unique operation IDs, security enforcement, or actual Go route coverage. Other HTTP methods are not subject to its operation-level checks. A reported path/schema count is an inventory count, not runtime conformance proof.

Generated permission labels are inferred from HTTP method and declared parameter metadata, not discovered by executing authorization. Schema labels summarize declared shapes; they do not establish that the handler's runtime response matches those shapes. Keep implementation-grounded behavior references and endpoint tests alongside the generated inventory, especially where current placeholders or compatibility defects differ from intended contract semantics.

The generated guide is an endpoint inventory, not a substitute for request/response schemas in OpenAPI or the implementation's state-machine tests. API tasks/design docs under `TASKS.ROUNDTABLE/` may describe intended capabilities beyond the live server.
