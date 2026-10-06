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

The native bind guard splits `-addr` as host/port and parses the host as a numeric IP. `127.0.0.1:8080` and `[::1]:8080` pass the loopback test; `localhost:8080`, `:8080`, and `0.0.0.0:8080` do not, even if a hostname resolves to loopback. Unless `-allow-remote` is set, rejection prints `refusing non-loopback bind; pass -allow-remote explicitly` and exits with code 2 before opening SQLite. The opt-in skips that check; malformed addresses can still fail when serving begins.

Database and workspace-root defaults are independent process-relative paths. Setting `-workspace-root /some/repo` does not automatically move the default database into that repository. `-database` also sets the parent directory used as the maintenance directory. The process does not read `.roundtable/config.yaml` to derive these HTTP-service flags. Explicit token flags override their environment defaults, including an explicitly empty flag value. Environment changes do not hot-reload a running server.

After database initialization, the native process installs interrupt/SIGTERM cancellation and starts the HTTP server. The server sets a five-second `ReadHeaderTimeout`, but does not configure general read, write, or idle timeouts in this constructor. On context cancellation it requests shutdown with a separate five-second context and ignores that shutdown call's returned error. This is not proof that every long-running operation or hijacked WebSocket completed gracefully. There is no HTTPS listener, certificate flag, worker-limit flag, or request-timeout flag exposed by this executable.

### Container-specific configuration

The image command uses `-addr 0.0.0.0:8080 -allow-remote -workspace-root /workspace -database /app/.roundtable/roundtable.db`. These replace native defaults deliberately so the container port is reachable. Compose maps `${ROUNDTABLE_API_PORT:-8080}:8080` without a host IP restriction; it is not a loopback-only publication. Empty token defaults plus that publication can expose development header mode beyond the local machine. Set credentials and constrain network exposure before starting it on a shared host; `-allow-remote` is not authentication or TLS.

Compose has restart policy `unless-stopped`, but no configured healthcheck, resource limit, or dependency on a UI/runtime service. The image builds with Go 1.26 on Debian Bookworm and `CGO_ENABLED=1`; its runtime image installs CA certificates and Git. It does not install agent CLIs, Node, Python, or a general test-toolchain environment. No `USER` directive switches away from the image's default root user. The checkout mount is read-only, so repository reads can work while operations requiring writes, such as branch switching, can fail on that mount even when policy/preflight allows them. The writable `.roundtable` mount is separate persistent state, not authority to rewrite the read-only checkout.

Container startup does not register a workspace automatically or launch the coordinator. Verify the intended workspace registry/root and database file separately after startup. Changing published port, mounts, or token environment affects container deployment, not the browser's already-built public URL settings.

## Authentication and authorization

When either bearer token is configured, every non-OPTIONS request must present a matching `Authorization: Bearer ...` token. The human token maps the request to human identity; the agent token maps it to agent identity. Tokens are compared in constant time. Unknown bearer tokens return 401. The API does not provide a login/session service or token rotation endpoint; provision, store, rotate, and revoke tokens outside the repository and restart the API after changing them.

Mutation handlers may additionally require `X-Actor-ID` and `X-Actor-Role`; documented roles are `view`, `operate`, `approve`, `govern`, `administer`, and `force-override`, with `human`, `admin`, and `chair` accepted as aliases by current helper code. Do not infer fine-grained separation from role names alone: many current handlers use a shared human-vs-agent check rather than enforcing a unique permission for each role. Some agent/orchestrator handlers use `X-Roundtable-Orchestrator: true` and actor headers. Consult the operation implementation and OpenAPI security requirements before granting a token access.

If both bearer tokens are absent, local development mode accepts actor headers and is not an authentication boundary. Keep this mode bound to loopback. A remote bind is unsafe without configured credentials and a protected transport/network boundary. Do not put the human token in browser code, `NEXT_PUBLIC_*` variables, shell history, checked-in `.env` files, or logs.

### Token and actor parsing details

Authorization is trimmed, then recognized only with the exact case-sensitive prefix `Bearer `; the extracted token is trimmed too. Lowercase `bearer`, another authentication scheme, or an empty bearer value behaves as missing credentials. With either configured token, that produces `authentication_required`. A nonempty unrecognized bearer token produces `invalid_token`, including in development mode when no tokens are configured. OPTIONS bypasses authentication for CORS preflight.

Human-token matching is checked before agent-token matching. If both configured secrets are identical, that value authenticates as human, not agent; use distinct tokens to preserve the intended boundary. Constant-time comparison applies only after checking nonempty values and equal lengths. This is a shared-secret mechanism, not per-user login, cryptographic binding to `X-Actor-ID`, or a scoped token catalog.

`X-Actor-ID` is trimmed attribution supplied by the caller, not an identity derived from the token. Actor role is trimmed and lowercased. A valid human token preserves the supplied role; a valid agent token forces role/kind to `agent`, preventing that token from becoming human through a role header. A human-kind request with a nonempty unrecognized role is rejected as `unknown_role`. An empty role can pass middleware authentication but fail a handler's additional actor requirements. In no-token development mode, exact `X-Roundtable-Orchestrator: true` or normalized role `agent` chooses agent kind; this header is not a secret.

The accepted human role labels and aliases are a helper allowlist, not proof of a separate permission boundary for every operation. Read handler-specific authorization and keep the token's broad authority in mind. Do not expose a human secret merely to label requests as `view`.

## Browser origin and request conventions

The default allowed origins are the localhost UI origins above. CORS returns allowed methods/headers for a matching Origin; the security middleware rejects state-changing requests with a non-allowlisted Origin. `OPTIONS` is handled as preflight. This is an origin check, not an alternative to bearer authentication.

Requests and errors use request IDs; errors use `application/problem+json`. Mutating endpoints may require `Idempotency-Key`; workspace/configuration updates may use `If-Match` revision values. The API client should preserve returned request/correlation IDs and only retry according to [Error Semantics](docs/error-semantics.md). Collections use opaque cursor pagination as described in [Pagination](docs/pagination.md).

The current Next.js API client sends `X-Request-ID` and `X-Workspace-ID` but does not attach a bearer token. Do not expose this API directly to a browser when it requires a privileged bearer credential; use a deliberately designed authenticated proxy/session boundary or finish the UI auth flow first.

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

## Documentation maintenance

The endpoint guide is generated from `openapi.yaml`:

```sh
ruby api/scripts/generate-admin-api-guide.rb
ruby api/scripts/verify-openapi.rb
```

The generator writes both `api/docs/admin-api-guide.md` and `api/docs/schema-reference.md`. CI runs the generator before assembling the docs site so published endpoint-to-schema links reflect the current contract.

The generated guide is an endpoint inventory, not a substitute for request/response schemas in OpenAPI or the implementation's state-machine tests. API tasks/design docs under `TASKS.ROUNDTABLE/` may describe intended capabilities beyond the live server.
