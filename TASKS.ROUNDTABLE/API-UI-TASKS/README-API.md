# Roundtable Admin API — Implementation Task Pack

This pack defines the backend APIs that drive the Roundtable admin panel.

## Backend posture

Roundtable is local-first. The backend is the authoritative control plane for:

- workspace/repository state;
- agent/session lifecycle;
- deliberations;
- proposals and patch storage;
- claims;
- votes and policy-weighted consensus;
- policies and policy evaluations;
- human approvals;
- transaction manager execution;
- repository index;
- logs/audit;
- Memory Oracle state;
- settings;
- live events.

The browser is never allowed to write repository files directly or invoke local shell commands directly.

## Recommended backend stack

- Go 1.25+
- `net/http` or existing project router
- OpenAPI 3.1 source-of-truth
- SQLite with WAL mode
- migrations managed by the existing migration tool
- explicit repository/service/transport layers
- WebSocket event stream for live UI
- structured logging
- RFC 7807 Problem Details
- UUIDv7 or sortable IDs where appropriate
- repository-relative path canonicalization
- transaction manager as the sole mutation authority

## URL conventions

Base path: `/api/v1`

All workspace-scoped endpoints use either:
- path scope `/workspaces/{workspace_id}/...`, or
- a consistent validated workspace context header.

This task pack uses path scoping in examples because it is explicit.

## Response envelope

Resource endpoints return the resource directly unless pagination metadata is needed.

List example:

```json
{
  "items": [],
  "page": {
    "next_cursor": "opaque-or-null",
    "has_more": false
  }
}
```

Error responses use `application/problem+json` and include:
- `type`
- `title`
- `status`
- `detail`
- `instance`
- `request_id`
- optional `code`
- optional `field_errors`
- optional `conflict`

## Concurrency

Mutating endpoints that edit configuration/policy/memory or other revisioned state should use an explicit `version`/ETag precondition. Return `409` or `412` with current version metadata rather than silently overwriting concurrent edits.

## Idempotency

High-impact POST operations accept `Idempotency-Key`, especially:
- approval decision;
- transaction retry/stage/apply request;
- session resume;
- force-release claim;
- deliberation create/start.

## Audit invariant

Every state-changing human action emits an immutable audit event with:
- actor;
- action;
- target;
- workspace;
- request ID/correlation ID;
- reason where required;
- before/after state summary;
- timestamp.
