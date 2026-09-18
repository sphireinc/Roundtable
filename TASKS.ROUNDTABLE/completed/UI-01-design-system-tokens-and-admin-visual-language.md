# UI-01: Design System, Tokens, and Admin Visual Language

## Objective

Implement **Design System, Tokens, and Admin Visual Language** as a production-ready part of the Roundtable admin control plane.

## Product constraints

- Agents must never receive direct repository write controls through this UI.
- Patch application is always mediated by Roundtable's transaction manager.
- Human UI state must reflect the server's authoritative governance state.
- All workspace-scoped requests include the selected workspace context.
- Every live view supports reconnect/resync behavior.
- Every visible control must have a defined action, disabled condition, and failure state.

## Detailed requirements

- Reproduce the screenshot's visual language with semantic tokens, not hard-coded component colors. Define background, elevated surfaces, borders, text hierarchy, accent, success, warning, danger, info, focus ring, diff-add, diff-remove, and chart-series tokens.
- Define typography scales for page title, section title, card title, body, metadata, table header, table body, status labels, and monospace identifiers.
- Create reusable primitives: `Panel`, `MetricCard`, `StatusPill`, `EntityChip`, `IconButton`, `ToolbarButton`, `Tabs`, `DenseTable`, `KeyValueList`, `EmptyState`, `ErrorState`, `Skeleton`, `CodeSnippet`, `DiffStat`, `ProgressMeter`, and `HealthIndicator`.
- Ensure focus rings are clearly visible in dark mode and all icon-only controls expose tooltips and accessible labels.
- Document spacing, radius, elevation, and density rules in a Storybook-equivalent route or internal style reference page.

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
