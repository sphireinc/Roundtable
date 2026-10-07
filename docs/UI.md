# Web UI: Architecture and Scope

The `ui/` directory is a Next.js application whose current manifest requests Next `^16.3.8`, using React 19, TypeScript, TanStack Query, and Zustand. The UI communicates with the separate HTTP API; it is not the Go terminal UI. API contract and service behavior are described in the [HTTP API guide](../api/README.md).

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

The checked-in [UI Compose deployment](DEPLOYMENT.md#ui-container-configuration) currently passes these values only to the running container, after its Dockerfile has already built the Next.js bundle. Runtime environment changes cannot update statically inlined public values; the Compose file's blank workspace default and non-workspace WebSocket path are also invalid for the current UI/API integration. Use the documented native build flow or first add explicit container build-argument wiring.

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

### Workspace list and cache transition

Workspace options use the global query key `["workspaces"]`; the control consumes only the returned `items` and does not follow pagination or request search/status filters. Display labels combine `display_name` with `root_alias` or ID. An empty list renders a `No registered workspaces` option retaining the selected ID rather than choosing another workspace. The active-workspace warning checks whether the selected item's status is exact `active`; a missing item, loading list, or failed list can also satisfy that warning condition. It is not by itself proof that the server detached a workspace.

Selecting a nonempty different ID removes queries whose keys begin `["workspace", oldId]`, then updates dashboard-local React state. It does not persist that choice, change environment variables, mutate the server's active workspace, clear the global workspace list query, or clear branch target/error/preflight/dialog state. The selector is not disabled while a branch request is pending. Switching contexts during that flow can leave local preflight state from the previous workspace; cancel and rerun preflight in the intended context rather than treating an existing dialog as universally current.

### Branch request and confirmation details

The target is free text rather than a loaded branch picker. Input is disabled while repository data is absent, HEAD is detached, or an action is pending. The review button checks trimmed target, equality to the currently displayed branch, and pending state; it does not independently enforce every input-disable condition. The handler trims the target and sends `{branch: target}` to preflight with human actor headers. It displays returned blocker messages and nonzero counts, not a complete server impact object. Confirmation is offered only when `allowed` is true.

Confirmation sends `{branch: preflight.target_branch}` to the workspace currently captured by that render. It does not submit a preflight token, expected revision, or client-side stale-state check. Server-side revalidation remains necessary. Each confirmation attempt generates a new `crypto.randomUUID()` idempotency key; there is no timestamp fallback or retained key for an ambiguous retry. A lost response followed by another confirmation is therefore not the same idempotent request merely because the branch target is unchanged.

On success, the returned `after` repository status replaces only `["workspace", workspaceId, "repository"]`; target text and preflight are cleared and the dialog closes. Other workspace queries are not broadly invalidated. Failures show the thrown error's message or a generic fallback, without the full problem detail/request ID. They preserve target/preflight/dialog state, allowing manual dismissal or cancellation. Mutation requests pass no abort signal and do not use a TanStack mutation retry policy.

### Stream URL and lifecycle details

The stream hook replaces the first substring matching `/workspaces/<segment>/` with the URI-encoded selected workspace ID. If that pattern is absent, the configured URL remains unchanged even when selection changes; the hook does not append a workspace route or validate that the socket belongs to the selected workspace. It opens a browser WebSocket without a custom subprotocol or bearer-header injection.

Connection construction/open/close/error handlers update toolbar state. A close schedules another attempt after two seconds; construction failure also schedules a retry. There is no exponential backoff, jitter, retry limit, heartbeat timeout, online/offline listener, or user-controlled reconnect action. Cleanup cancels its pending timer and closes the current socket. It does not send a saved event cursor or handle messages at all. A `connected` label means the socket opened, not that its workspace identity, data freshness, authorization, or event continuity was verified.

## Data fetching and state ownership

The root provider creates one TanStack Query client with a 5-second query stale time and one automatic retry. The API client normalizes non-2xx results into `APIError` problem details, sends an `AbortSignal` through query fetches, handles 204/JSON/text responses, and generates a request ID when available. Workspace/domain query keys should include workspace ID; helpers are available for workspace keys and invalidation. The current workspace list query is global, while repository/health keys are workspace-specific.

Zustand stores only the sidebar-collapsed preference. The selected dashboard workspace ID is local React state initialized from build-time config; it is not persisted across reloads. The server and SQLite remain authoritative for workspaces, branch state, proposals, approvals, and transactions.

### Configuration validation and URL resolution

The three required public values are trimmed and checked only for nonemptiness. Whitespace-only values count as missing. The loader does not validate URL syntax, require HTTP/WebSocket schemes, check endpoint reachability, or verify that the workspace exists. A malformed API base can fail later when constructing a request URL; a malformed WebSocket URL can fail at connection construction. The build-version value is trimmed and falls back to `dev` when empty. The mock flag is not trimmed or case-folded: `True` or ` true ` does not enable it.

Requests use `new URL(path, apiBaseUrl)`, not string concatenation. Current endpoint paths begin with `/`, so they replace any pathname in the configured base URL. For example, a base ending in `/proxy/` does not automatically prefix `/api/v1/status` with `/proxy`. Configure proxy routing accordingly. There is no workspace fallback/discovery in configuration loading and no server-secret configuration layer in this client.

### API client response and failure contract

The client exposes GET, POST, PATCH, and DELETE helpers. POST alone accepts caller-supplied extra headers, used by the branch flow for actor and idempotency metadata. The client then sets its own `Accept`, request ID, and workspace ID headers, overwriting matching values supplied through that extra-header argument. POST/PATCH stringify the body and set JSON content type when a body is present. There is no PUT helper, automatic bearer-token attachment, explicitly configured cookie credential mode, request timeout, or internal retry loop.

Each invocation generates a fresh request ID using `crypto.randomUUID` when available, otherwise `req-` plus the current millisecond timestamp. The fallback is not a uniqueness guarantee under concurrent requests. An error response header or parsed problem body can replace this generated ID in the reported problem. Idempotency keys are separate from request IDs; adding a request ID does not make a write idempotent.

For successful responses, status 204 returns `undefined`. A content type containing exact lowercase `application/json` is decoded with `response.json`; other types return text. There is no schema validation, envelope unwrapping, pagination traversal, date conversion, or domain-model coercion. The generic TypeScript return type is an assertion, not evidence of a validated payload. A successful response with malformed JSON throws a decoding error; a success labeled only `application/problem+json` is treated as text by this check.

Non-2xx HTTP responses become `APIError`. The client starts with `about:blank`, `Request failed`, HTTP status, and the response/generated request ID, then attempts to merge the response's parsed JSON problem fields. Falsy body status or request ID falls back to the HTTP/header value. If JSON parsing fails, detail uses HTTP status text or a generic message. Parsed fields are not runtime-validated against `ProblemDetails`; malformed-but-parseable bodies should not be assumed to produce trustworthy diagnostics.

Network failures, URL construction errors, JSON serialization errors, successful-response decoding failures, and aborts are not converted by this method into `APIError`; they can propagate as native errors. Query-level retry behavior comes from TanStack Query configuration, not the API client. Query helpers pass their supplied abort signal through fetch, but there is no automatic operation deadline or shared cancellation of every mutation. Treat a failed client response as potentially ambiguous for writes and reconcile server state before retrying.

### Dashboard freshness and placeholder semantics

The health query is enabled only when configuration is available and is keyed by the selected workspace, even though its URL is the node-level `/api/v1/status`. The route supplies no periodic polling interval; the provider's stale-time/retry defaults and normal query lifecycle govern refetching. A WebSocket connection does not refresh this query from event payloads. The displayed component count is the number of keys in the returned `components` object, not the number of healthy components or a readiness verdict.

The proposals and approvals metrics are placeholders rather than zero counts. The Reconnection card's `ready` badge is static copy, not the measured toolbar stream state. Existing health data can remain visible alongside a failed refetch state. A rendered dashboard, successful initial health request, or connected socket does not demonstrate completed proposal/approval flows or continuous reconciliation.

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

### Script and configuration reference

| Script | Command | Evidence and prerequisites |
| --- | --- | --- |
| `dev` | `next dev` | Development server; not a production artifact. The browser test config expects port 3000. |
| `build` | `next build` | Production compilation with public environment values available at build time. Does not start the API or prove runtime integration. |
| `start` | `next start` | Serves a previously built production artifact; changing public variables now does not rewrite its embedded client configuration. |
| `typecheck` | `tsc --noEmit` | Type checking, not browser execution or response-schema validation. Incremental metadata can still be generated. |
| `lint` | `eslint .` | Next core-web-vitals lint rules; not exhaustive accessibility or security acceptance. |
| `test` | `vitest run` | jsdom tests, separate from Playwright browser tests. |
| `test:e2e` | `playwright test` | Configured Chromium desktop browser suite; requires an available browser installation and server startup. |
| `test:e2e:list` | `playwright test --list` | Discovery only; no page interaction or acceptance evidence. |

Next configuration enables `reactStrictMode` and defines no API rewrite/proxy, authentication middleware, or alternate output mode in `next.config.ts`. TypeScript uses strict checking, bundler module resolution, ES2017 target, DOM/esnext libraries, no JavaScript source inclusion, and the `@/*` alias to `src/*`. Its include set contains source files, `next-env.d.ts`, and generated `.next/types`; `skipLibCheck` skips checking declaration-library internals. `noEmit` does not mean every tool leaves the worktree untouched: incremental type metadata and Next-generated files are distinct outputs.

ESLint ignores `.next`, `node_modules`, `dist`, and `coverage` directories and extends `next/core-web-vitals` through FlatCompat. Vitest resolves `@` to `src`, runs in jsdom, loads `src/test/setup.ts` for jest-dom matchers, and excludes Playwright's `src/test/e2e` tree plus `node_modules` and `.next`. A jsdom pass does not verify browser networking, WebSocket handshakes, native dialog focus behavior, or responsive layout.

### Browser test isolation and scope

Playwright discovers tests under `src/test/e2e`, uses base URL `http://127.0.0.1:3000`, and defines one `chromium` project with the Desktop Chrome device preset. Its web-server command is `npm run dev` even when Playwright itself is invoked through pnpm. `reuseExistingServer: true` allows an already-running server on that URL to be used; the config does not verify that server's revision or environment. Ensure port 3000 belongs to the intended checkout and that its required public variables are current before treating a result as evidence for a specific revision. There is no configured mobile, Firefox, WebKit, production-server, or live-API test matrix.

The current bootstrap suite checks explicit configuration/dashboard rendering, a retry affordance with a mocked 503 health response, and workspace/repository context with intercepted REST responses. The latter tests require a configured dashboard to reach their expected controls; the first permits either configuration-error or dashboard copy and does not by itself prove a usable configuration. Fixtures do not demonstrate real token authentication, branch switching, WebSocket payload reconciliation, data persistence, or complete feature-page behavior. Record mocked-browser, live-service, production-build, and manual accessibility results separately.

Local `.env.local`, dependency installs, `.next`, test reports/results, and TypeScript build metadata are environment artifacts, not public documentation inputs. Avoid printing local environment contents when diagnosing configuration and do not commit secrets or generated outputs simply because a test created them.

## Dependency Manifest and Lockfile

The tracked `ui/package-lock.json` is the npm resolution record; the package manifest uses version ranges rather than exact pins. In the current checkout it locks Next 16.3.8, React 19.3.0, Vitest 5.0.1, and `eslint-config-next` 15.5.25. The lint package therefore remains on a different major from Next. This records source state, not a claim of compatible tooling or successful upgrade acceptance.

For installation consistent with the tracked npm lock, use `npm ci` from `ui/`; it replaces the dependency installation and should be run only when that local replacement is intended. The documented scripts can be invoked with `npm run <script>`. A pnpm invocation uses its own resolution/lock behavior and is not proof that the npm-locked dependency graph was installed. Do not automatically stage local pnpm lock/workspace files or update both managers' records as a side effect of a documentation check.

After pulling a manifest/lockfile change, an existing `node_modules` or `.next` tree can still represent older dependencies/build output. Record installed versions and the revision/environment used before attributing test results to the upgrade. Documentation builds do not install UI dependencies or run typecheck, lint, Vitest, Next production build, or Playwright. A dependency version bump alone is not evidence that these gates passed or that the browser/API integration works.
