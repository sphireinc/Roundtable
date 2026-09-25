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

## Authentication and authorization

When either bearer token is configured, every non-OPTIONS request must present a matching `Authorization: Bearer ...` token. The human token maps the request to human identity; the agent token maps it to agent identity. Tokens are compared in constant time. Unknown bearer tokens return 401. The API does not provide a login/session service or token rotation endpoint; provision, store, rotate, and revoke tokens outside the repository and restart the API after changing them.

Mutation handlers may additionally require `X-Actor-ID` and `X-Actor-Role`; documented roles are `view`, `operate`, `approve`, `govern`, `administer`, and `force-override`, with `human`, `admin`, and `chair` accepted as aliases by current helper code. Do not infer fine-grained separation from role names alone: many current handlers use a shared human-vs-agent check rather than enforcing a unique permission for each role. Some agent/orchestrator handlers use `X-Roundtable-Orchestrator: true` and actor headers. Consult the operation implementation and OpenAPI security requirements before granting a token access.

If both bearer tokens are absent, local development mode accepts actor headers and is not an authentication boundary. Keep this mode bound to loopback. A remote bind is unsafe without configured credentials and a protected transport/network boundary. Do not put the human token in browser code, `NEXT_PUBLIC_*` variables, shell history, checked-in `.env` files, or logs.

## Browser origin and request conventions

The default allowed origins are the localhost UI origins above. CORS returns allowed methods/headers for a matching Origin; the security middleware rejects state-changing requests with a non-allowlisted Origin. `OPTIONS` is handled as preflight. This is an origin check, not an alternative to bearer authentication.

Requests and errors use request IDs; errors use `application/problem+json`. Mutating endpoints may require `Idempotency-Key`; workspace/configuration updates may use `If-Match` revision values. The API client should preserve returned request/correlation IDs and only retry according to [Error Semantics](docs/error-semantics.md). Collections use opaque cursor pagination as described in [Pagination](docs/pagination.md).

The current Next.js API client sends `X-Request-ID` and `X-Workspace-ID` but does not attach a bearer token. Do not expose this API directly to a browser when it requires a privileged bearer credential; use a deliberately designed authenticated proxy/session boundary or finish the UI auth flow first.

## Live event delivery

The API exposes workspace event snapshots and a workspace WebSocket. Clients should load a snapshot, subscribe, track sequence IDs, and resynchronize when the server reports a gap or reconnect indicates stale state. The HTTP event service is distinct from the Go runtime's in-process event bus and local MCP socket. See the endpoint schemas and event-related operation descriptions in `openapi.yaml`.

## Database and maintenance

The API opens SQLite with the same WAL/foreign-key/busy-timeout setup as `internal/db`. If API and Go runtime share `.roundtable/roundtable.db`, coordinate maintenance and backups with all writers. Maintenance operations are fixed server-side actions, not arbitrary SQL; consult [Maintenance](docs/maintenance.md). Preserve database sidecars while writers are active and verify backup restore procedures.

## Documentation maintenance

The endpoint guide is generated from `openapi.yaml`:

```sh
ruby api/scripts/generate-admin-api-guide.rb
ruby api/scripts/verify-openapi.rb
```

The generator writes both `api/docs/admin-api-guide.md` and `api/docs/schema-reference.md`. CI runs the generator before assembling the docs site so published endpoint-to-schema links reflect the current contract.

The generated guide is an endpoint inventory, not a substitute for request/response schemas in OpenAPI or the implementation's state-machine tests. API tasks/design docs under `TASKS.ROUNDTABLE/` may describe intended capabilities beyond the live server.
