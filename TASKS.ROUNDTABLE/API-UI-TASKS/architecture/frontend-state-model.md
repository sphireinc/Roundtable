# Frontend State Model

## Server state
Owned by TanStack Query:
- workspace metadata;
- repository state;
- agents;
- sessions;
- deliberations;
- proposals;
- votes/consensus;
- claims;
- policies/evaluations;
- approvals;
- transactions;
- memory;
- logs/audit;
- settings/effective config.

## Ephemeral UI state
Owned locally/Zustand:
- sidebar collapsed state;
- open inspector/drawer;
- currently focused entity;
- local table column visibility;
- diff display preference;
- unsaved form drafts;
- command palette visibility;
- dismissed non-authoritative tips.

## Never store as client authority
- current consensus result;
- policy pass/fail;
- claim ownership;
- approval completion;
- transaction phase;
- repository revision;
- agent/session lifecycle.

Those values may be mirrored in the query cache for rendering, but only server responses/events can change them.
