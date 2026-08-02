# Resource Claims

Claims are durable leases over project resources.

## Resource types

- repo
- directory
- file
- symbol
- range
- endpoint
- schema
- command
- test-suite
- document-section
- design-file

Roundtable is designed to expand beyond code, so resource types should not assume only source files.

## Claim types

- read
- write
- review
- exclusive

## Claim statuses

- active
- suspended
- expired
- released
- transferred
- revoked

## Claim fields

- id
- resource id
- agent id
- task id
- claim type
- base hash
- status
- expires at
- heartbeat at
- renewable
- resume policy
- rationale

## Conflict rules

A claim conflicts if it overlaps with another active claim in a way that may cause textual, semantic, or operational conflict.

Examples:

- file write conflicts with symbol write inside the file
- directory write conflicts with file write below it
- endpoint claim conflicts with route handler changes touching that endpoint
- schema claim conflicts with migrations touching that schema
- command claim conflicts with another mutating command

## Resume policies

- hold: keep claim during pause/resume
- expire: expire when agent disconnects or TTL passes
- chair_review: suspend and ask Chair whether to renew
- human_review: suspend and require Human approval

## Stale claim handling

On resume:

- if base hash equals current hash, claim may be renewed
- if base hash differs, claim becomes suspended
- if high-risk resource, require Human review
- if conflicting applied transaction exists, revoke or refresh context
