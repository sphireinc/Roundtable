# 0010 Project Status Strip

## Goal

Make project context and shared-state governance immediately visible in the Roundtable UI without weakening the existing workspace and branch controls.

## Acceptance Criteria

- Workspace and branch controls, repository status, and live event-stream status are grouped in one prominent, accessible project-status region at the top of the application shell.
- Shared-state authority and the API transaction-manager mutation boundary are visible in the same region.
- Existing context-control behavior remains unchanged, including workspace scoping, governed branch-switch preflight, and recoverable error states.
- The strip remains legible and usable at narrow/mobile widths; status is communicated by text, not color alone.
- Component coverage verifies the region groups context controls and governance messaging.

## Verification

- `cd ui && npm test`
- `cd ui && npm run typecheck`
- `cd ui && npm run lint`
- `cd ui && PATH=/Users/JuanSanchez/.nvm/versions/node/v24.12.0/bin:$PATH npm run build`
