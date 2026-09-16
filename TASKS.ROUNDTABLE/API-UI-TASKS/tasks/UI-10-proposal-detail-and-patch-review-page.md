# UI-10: Proposal Detail and Patch Review Page

## Objective

Implement **Proposal Detail and Patch Review Page** as a production-ready part of the Roundtable admin control plane.

## Product constraints

- Agents must never receive direct repository write controls through this UI.
- Patch application is always mediated by Roundtable's transaction manager.
- Human UI state must reflect the server's authoritative governance state.
- All workspace-scoped requests include the selected workspace context.
- Every live view supports reconnect/resync behavior.
- Every visible control must have a defined action, disabled condition, and failure state.

## Detailed requirements

- Build `/proposals/:proposalId` as the full human review surface.
- Header includes proposal state machine, proposer, deliberation, claims, created/updated time, base revision, current HEAD relationship, and stale/rebase warning.
- Files panel provides tree/list switch, per-file diff, symbol anchors, additions/removals, binary-file metadata, and large-diff truncation with explicit load-more.
- Side-by-side and unified diff modes are required. Preserve whitespace and do not syntax-highlight removed/added line backgrounds in a way that hides characters.
- Policy tab displays each rule, version, severity, result, evidence, and remediation. Consensus tab displays every vote, policy weight, rationale, abstention/rejection reason, and computed threshold math.
- Human actions: approve, reject, request changes, rerun policy evaluation, rerun tests/checks, stage for transaction where allowed, cancel uncommitted proposal if permitted.
- Never expose a direct 'write file' control.

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
