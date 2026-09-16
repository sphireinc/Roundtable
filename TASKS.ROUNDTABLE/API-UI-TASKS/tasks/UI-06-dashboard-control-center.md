# UI-06: Dashboard / Control Center

## Objective

Implement **Dashboard / Control Center** as a production-ready part of the Roundtable admin control plane.

## Product constraints

- Agents must never receive direct repository write controls through this UI.
- Patch application is always mediated by Roundtable's transaction manager.
- Human UI state must reflect the server's authoritative governance state.
- All workspace-scoped requests include the selected workspace context.
- Every live view supports reconnect/resync behavior.
- Every visible control must have a defined action, disabled condition, and failure state.

## Detailed requirements

- Recreate the screenshot as the canonical `/dashboard` page.
- Header: 'Roundtable Control Center', subtitle 'Local-first shared-state agentic development orchestrator', and strapline 'One table. One repo. One governed stream of changes.'
- Metric cards: agents online, proposal queue, consensus success, policy-gate pass rate. Each card must show its exact measurement window in a tooltip and link to the corresponding page.
- Main live consensus table uses the shared dense table component. Default columns: Proposal ID, Agent, Resource Claim, Patch Summary, Vote Status, Policy Weight, Human Approval, Transaction State, Updated.
- Selecting a proposal row updates the inspector pane without navigating away. Persist selected proposal ID in the URL query string so refresh/deep-linking is deterministic.
- Recent Activity Feed, Resource Claims, Team Memory, Proposal Inspector, and Repository Governance cards are live-updating independent widgets with isolated loading/error states.
- Dashboard must remain usable if one widget endpoint fails; show per-panel retry.

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
