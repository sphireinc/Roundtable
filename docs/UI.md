# Web UI: Architecture and Scope

The `ui/` directory is a Next.js 15 application using React 19, TypeScript, TanStack Query, and Zustand. The UI communicates with the separate HTTP API; it is not the Go terminal UI. API contract and service behavior are described in the [HTTP API guide](../api/README.md).

## Runtime configuration

The client bundle requires `NEXT_PUBLIC_API_BASE_URL`, `NEXT_PUBLIC_WS_URL`, and `NEXT_PUBLIC_WORKSPACE_ID`. `NEXT_PUBLIC_BUILD_VERSION` is optional and defaults to `dev`; `NEXT_PUBLIC_ENABLE_DEV_MOCKS` is parsed as true only when its value is exactly `true`, but no current route consumes this flag or provides a mock implementation. Next.js inlines `NEXT_PUBLIC_*` variables at build time, so configure them before producing a deployable build. Missing required values yield a visible configuration error instead of silently selecting a workspace or endpoint.

Example development values (replace the workspace ID with one returned by the API):

```dotenv
NEXT_PUBLIC_API_BASE_URL=http://127.0.0.1:8080
NEXT_PUBLIC_WS_URL=ws://127.0.0.1:8080/api/v1/workspaces/workspace-id/events/ws
NEXT_PUBLIC_WORKSPACE_ID=workspace-id
NEXT_PUBLIC_BUILD_VERSION=dev
NEXT_PUBLIC_ENABLE_DEV_MOCKS=false
```

The WebSocket URL should contain the workspace path segment because the context control replaces that segment when the selected workspace changes.

The API client sends `Accept: application/json`, a generated `X-Request-ID`, and `X-Workspace-ID` on each request; JSON writes also send `Content-Type: application/json`. It does not currently attach a bearer token. Do not expose a privileged human token in browser environment variables; deploy behind an appropriate same-origin/authentication boundary or complete the intended authenticated client flow before remote use.

## Implemented routes

The app shell defines navigation links for dashboard, deliberations, proposals, consensus, claims, sessions, memory, policies, approvals, transactions, repository, agents, logs, and settings, along with Search and Notifications actions. In this checkout, only the dashboard route is a product page wired to the API. The root route redirects to the dashboard; style-reference is a design fixture. Other navigation targets are planned links and are not evidence of completed pages.

The dashboard loads `/api/v1/status`, displays node health and component/degraded status, and exposes retry/error states. Its proposals and human-approval cards are still placeholders; they do not fetch counts. The root route redirects to `/dashboard`. The shell includes workspace/branch context controls, node status, build version, and a collapsible navigation sidebar. Sidebar collapse preference is stored in browser local storage as `roundtable.sidebar-collapsed`. Although dashboard copy refers to event reconciliation, the current client only exposes WebSocket connection state and does not reconcile event payloads yet.

## Workspace and branch context controls

- Workspace options load from `GET /api/v1/workspaces`. Selecting a different workspace changes the active client workspace ID and removes cached queries under the old workspace key.
- Repository status loads from `GET /api/v1/workspaces/{workspaceId}/repository` and shows branch, dirty/detached state, abbreviated HEAD, and ahead/behind counts. Loading/error states provide skeleton or retry affordances.
- Branch changes use a two-step human flow: POST a target branch to `/repository/branch-switch/preflight`, render server blockers/impact, then require explicit confirmation before POSTing `/repository/branch-switch` with an idempotency key. A successful switch updates the repository query cache. Detached HEAD, no target, current branch, or pending mutation disables controls as coded.
- The current implementation sends `X-Actor-ID: human` and `X-Actor-Role: human` but the client does not send configured bearer tokens; token-protected deployments therefore need an auth integration before these mutations work.
- Workspace event connection state is displayed in the toolbar. It is not yet a live data-delivery implementation: the socket does not handle message payloads, sequence IDs, snapshot reconciliation, or query invalidation. It retries connection after a fixed two-second delay and is closed on unmount/context change.

## Data fetching and state ownership

The root provider creates one TanStack Query client with a 5-second query stale time and one automatic retry. The API client normalizes non-2xx results into `APIError` problem details, sends an `AbortSignal` through query fetches, handles 204/JSON/text responses, and generates a request ID when available. Workspace/domain query keys should include workspace ID; helpers are available for workspace keys and invalidation. The current workspace list query is global, while repository/health keys are workspace-specific.

Zustand stores only the sidebar-collapsed preference. The selected dashboard workspace ID is local React state initialized from build-time config; it is not persisted across reloads. The server and SQLite remain authoritative for workspaces, branch state, proposals, approvals, and transactions.

## Shared components and visual tokens

`ui/src/components/primitives.tsx` exports Panel, MetricCard, StatusPill/StatusBadge, EntityChip, IconButton, ToolbarButton, Tabs, DenseTable, KeyValueList, EmptyState, ErrorState, Skeleton, CodeSnippet, DiffStat, ProgressMeter, and HealthIndicator. Use these for consistent semantics and states rather than duplicating status or button markup. They are primitives, not a completed feature-page component library.

The base tokens live in `ui/src/styles/tokens.css`:

| Token family | Variables and defaults |
| --- | --- |
| Background/surface | `--rt-color-bg: #11151b`, `--rt-color-surface: #1a2029`, `--rt-color-surface-raised: #202a38`, `--rt-color-border: #2b3543`. |
| Text | `--rt-color-text: #e8edf4`, `--rt-color-text-muted: #91a0b4`, `--rt-color-text-subtle: #64748b`. |
| Semantic/status | Accent `#4e9cff`; success `#43d17a`; warning `#f2b84b`; danger `#f26969`; info `#78c8ff`; focus `#9ecbff`. |
| Diff/chart | Diff add `#1e6b48`, diff remove `#7a3037`; chart 1-4 are blue `#4e9cff`, green `#43d17a`, amber `#f2b84b`, and purple `#c084fc`. |
| Typography | Body `Inter, ui-sans-serif, system-ui, sans-serif`; mono `ui-monospace, SFMono-Regular, Menlo, monospace`. Sizes: page title `clamp(28px, 4vw, 42px)`, section title `23px`, card title `16px`, body `14px`, metadata/status `12px`, table header `11px`. |
| Spacing/radius/elevation | Spacing steps 4/8/12/16/20/24px; radii 5/8/12px; raised shadow `0 10px 30px rgb(0 0 0 / 24%)`. |

Global components, tokens, and layout are in `globals.css`, `tokens.css`, and `app-shell.tsx`. The style-reference route is a design fixture, not a production route.

## Accessibility and responsive behavior

Implemented controls use labels/accessible names, current-page indication, status text, alert/status roles, table header scopes, skeleton hiding, and an sr-only stream announcement. This is not proof of complete keyboard/focus, screen-reader, contrast, reduced-motion, or narrow-screen acceptance across all planned pages; UI task UI-36 and UI-38 remain the broader acceptance references. Test actual interactions at desktop and narrow viewport widths before treating the UI as production-ready.

## Frontend conventions

- API calls go through the typed `APIClient`; server errors are normalized to problem details and retain request IDs.
- TanStack Query owns remote query lifecycle; query keys include workspace context to prevent cross-workspace cache reuse.
- Zustand stores shell-level UI preferences, not authoritative project state.
- The API/database remains authoritative. Browser state is a projection and must not directly mutate repository files.
- The configured WebSocket URL is runtime configuration for live integration, but do not assume every screen currently subscribes or reconciles event streams.

## Local development

From `ui/`, install the package dependencies, set the three required `NEXT_PUBLIC_*` variables, then run `pnpm dev`. Available package scripts are `dev`, `build`, `start`, `typecheck`, `lint`, `test`, `test:e2e`, and `test:e2e:list`; `test:e2e:list` discovers Playwright tests but does not execute them. Unit/component tests use Vitest; browser acceptance uses Playwright. API and UI origins, CORS, WebSocket origin, workspace ID, and build-time variable values must agree. See [Deployment](DEPLOYMENT.md) for the API container and security boundary.
