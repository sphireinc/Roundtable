# 0002 Resource Claims

## Goal

Implement resource claim storage, conflict detection, and claim lifecycle.

## Scope

- resources table
- claims table
- file/directory claim conflict detection
- status transitions: active, suspended, expired, released, transferred, revoked
- TTL handling
- CLI commands for claims
- MCP tools: resource.claim, resource.release, resource.claim_status

## Acceptance criteria

- Claims can be created, listed, released, expired, suspended, and revoked.
- Directory/file conflicts are detected.
- Claim attempts are denied when conflicting active claims exist.
- All claim actions are recorded in events.

## Risk

Normal
