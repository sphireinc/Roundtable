# UI-07: Live Consensus Table Component

## Objective

Implement **Live Consensus Table Component** as a production-ready part of the Roundtable admin control plane.

## Product constraints

- Agents must never receive direct repository write controls through this UI.
- Patch application is always mediated by Roundtable's transaction manager.
- Human UI state must reflect the server's authoritative governance state.
- All workspace-scoped requests include the selected workspace context.
- Every live view supports reconnect/resync behavior.
- Every visible control must have a defined action, disabled condition, and failure state.

## Detailed requirements

- Create a reusable live consensus table used by Dashboard and Consensus pages.
- Support server-driven filtering by vote state, transaction state, human-approval state, agent, claim type, and time range. Support client-side column visibility only.
- Sort direction and filter values are reflected in the URL.
- Live events must patch existing rows when safe; if an event changes query membership, invalidate and refetch the page rather than attempting brittle local logic.
- Rows expose status pills, monospace IDs, relative time with exact timestamp tooltip, policy weight, and compact claim label.
- Support keyboard row selection, Enter to open full proposal details, and context action menu.
- Do not make rejected/closed proposals disappear merely because their live state changed; respect current filters.

## Data/API wiring

- Use the typed API client from UI-00.
- Initial data is loaded over REST.
- Live changes arrive over the workspace event stream defined in UI-34.
- Query keys must include `workspaceId` and any server-side filter/sort inputs.
- Do not fabricate state transitions in the browser. After a mutation, use the mutation response and/or authoritative follow-up event.
- Surface `request_id` from API errors in the error details affordance for debugging.

## Required UI states

Implement all of the following when relevant:

1. loading skeleton;
2. empty/new workspace;
3. populated;
4. partial/degraded data;
5. stale data while reconnecting;
6. WebSocket disconnected;
7. permission denied;
8. validation failure;
9. optimistic mutation pending only where safe;
10. server conflict (`409`) with refresh/retry guidance;
11. entity deleted or no longer accessible;
12. generic server failure with retry.

## Accessibility and interaction

- Full keyboard access.
- Visible focus.
- Icon-only buttons have tooltips and accessible labels.
- Status is communicated with text/iconography, not color alone.
- Exact timestamps are available even when relative time is shown.
- Tables use semantic headers and preserve readable focus states.
- Dialogs restore focus to their launching control.

## Acceptance criteria

- The feature visually matches the Roundtable admin design system.
- No placeholder button is present.
- All server state is typed.
- Loading/error/empty/reconnect states are covered.
- Critical state-changing actions have confirmation/preflight where appropriate.
- Unit/component tests cover core interaction logic.
- Playwright covers at least one happy path and one failure/permission path.
- Typecheck, lint, unit tests, and E2E tests pass.
