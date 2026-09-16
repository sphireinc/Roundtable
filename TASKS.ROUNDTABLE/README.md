# Roundtable Admin UI — Implementation Task Pack

This pack defines the complete admin-panel UI for **Roundtable**, a local-first shared-state agentic development orchestrator.

The target experience is the dark, dense, engineering-focused control center shown in `reference/roundtable-control-center.png`.

## Product invariant

Roundtable is **not** a collection of isolated AI coding panes. The UI must continuously reinforce:

- one authoritative repository;
- agents are read-only against the repository;
- agents submit patch-only proposals;
- resources are explicitly claimed;
- consensus is visible and policy-weighted;
- policy gates are first-class;
- human approval appears only where governance requires it;
- repository mutation occurs only through the transaction manager;
- sessions are persistent and resumable;
- shared team memory is persistent and queryable.

## Recommended frontend stack

Use this stack unless the repository already standardizes on equivalent libraries:

- React 19 + TypeScript
- Vite
- TanStack Router
- TanStack Query
- Zustand for ephemeral UI-only state
- Radix UI primitives for accessible dialogs/menus/tooltips
- Tailwind CSS 4 or a token-driven CSS layer
- Lucide icons
- `@tanstack/react-table` for dense tabular views
- native WebSocket client for live event stream
- Vitest + Testing Library
- Playwright for E2E

Do **not** duplicate server state in Zustand. Server state belongs in TanStack Query and is updated by event invalidation/patching.

## Application route map

- `/` → redirect to `/dashboard`
- `/dashboard`
- `/deliberations`
- `/deliberations/:deliberationId`
- `/proposals`
- `/proposals/:proposalId`
- `/consensus`
- `/claims`
- `/sessions`
- `/sessions/:sessionId`
- `/memory`
- `/memory/:memoryId`
- `/policies`
- `/policies/:policyId`
- `/approvals`
- `/transactions`
- `/transactions/:transactionId`
- `/repository`
- `/repository/diff/:proposalId`
- `/agents`
- `/agents/:agentId`
- `/logs`
- `/settings`
- `/settings/general`
- `/settings/governance`
- `/settings/adapters`
- `/settings/mcp`
- `/settings/storage`
- `/settings/notifications`

## Live-data model

REST is used for initial page loads and mutations. A single workspace-scoped WebSocket carries live updates. Every live event contains:

- `event_id`
- `workspace_id`
- `sequence`
- `type`
- `occurred_at`
- `actor`
- `entity_type`
- `entity_id`
- `payload`

The frontend must detect sequence gaps and trigger a deterministic resync.

## Task execution rules

Each numbered file is intended to be independently assignable to a coding agent. Before marking a task complete:

1. implement the UI and state model;
2. wire to the stated API contract;
3. implement loading, empty, partial, stale, reconnecting, forbidden, and error states;
4. implement keyboard and screen-reader behavior;
5. add component/unit tests;
6. add Playwright coverage for the critical workflow;
7. avoid placeholder buttons—every visible control must perform the described action or be intentionally disabled with explanatory copy.

## Visual rules

- Dark charcoal/slate background.
- Dense but calm information hierarchy.
- Blue = primary / staged / selected.
- Green = approved / committed / healthy.
- Amber = pending / needs review / contested.
- Red = rejected / failed / destructive.
- Purple = locked / memory / special state.
- Status must never rely on color alone.
- Code identifiers use monospace typography.
- Tables remain readable at 1280px and become horizontally scrollable before columns collapse into illegibility.
- Avoid giant marketing-style whitespace inside authenticated admin pages.
