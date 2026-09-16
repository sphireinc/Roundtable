# Interaction Matrix

Every button/action must map to one of these categories.

| Category | Examples | UX requirement |
|---|---|---|
| Read/navigation | Open proposal, copy ID, open session | immediate |
| Safe mutation | acknowledge notification, pin memory | inline pending + server error |
| Governed mutation | approve proposal, create claim, pause run | structured confirmation when policy says so |
| High-impact | force-release claim, terminate session, disable policy, cancel staged transaction | preflight impact + mandatory confirmation + audit reason |
| Irreversible/commit | transaction application/commit | never initiated by a generic button; only when server says proposal is eligible and governance satisfied |
| Diagnostic | health check, simulate policy, rerun checks | show run ID and result |
