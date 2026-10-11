# 0008 CLI Agent Adapters

## Goal

Implement controlled CLI adapters for existing coding agents.

## Implementation Status

Verified; strict integration, full race suite, and independent review pass. Container isolation, scoped MCP authorization, provider egress controls, supervised lifecycle, coordinator dispatch, and governed proposal handoff are implemented. External reviewer/tester/security capabilities are proposal-bound; role-limited claims are constrained to operator-authored readable paths. Proposal handoff reuses only an exact patch already submitted by the same execution, and terminal/recovered executions release only claims bound to that exact session unless its proposal remains open. A completed no-proposal session suppresses repeat launches only within its run and can be retried by a later run. Tester agents can submit proposal-bound evidence with authenticated provenance, stored as `reported` rather than trusted test success; host test execution remains unavailable to agent capabilities. Implementers may revise/request review only on proposals created by their own session. Configured provider credentials are redacted from captured output before session persistence. A compatible SQLite migration records test-evidence provenance while preserving existing records. With `DOCKER_CONTEXT=desktop-linux ROUNDTABLE_REQUIRE_CONTAINER_TESTS=1 ROUNDTABLE_TEST_AGENT_IMAGE=roundtable-agent-egress-test:local`, the complete strict Docker matrix passes, including read-only/pinned source and host-filesystem isolation, assigned MCP socket mounting, provider egress denial, storage quotas, child-process freeze before patch export, cancellation, timeout, cleanup, idempotent stop, and concurrent workspace isolation. The full strict race suite, build, vet, and diff checks pass in this Docker Desktop environment. Colima's VirtioFS socket-mount limitation is documented as a Colima-specific runtime constraint; Docker Desktop is the supported runtime used for acceptance evidence. Independent full review found three actionable issues; all were fixed, and follow-up review found no remaining findings in those changes. Ready for scoped commit/push; after successful push, move this task file to `completed/` and update tracking. Do not weaken isolation or treat skipped tests as passes.

## Scope

- adapter interface
- codex adapter
- claude adapter
- gemini adapter
- opencode adapter
- generic adapter
- read-only workspace handling
- prompt composition
- MCP manifest injection
- session id capture where possible

## Acceptance criteria

- Adapters can launch agent processes with role-specific context.
- Agents receive generated MCP manifest and task context.
- Adapters store session metadata.
- Agents are restricted to read-only repo access and patch-only mutation contract.
- External execution requires Docker or Podman by default and fails closed without a usable runtime; no host-execution fallback is permitted.
- Authoritative repository mounts are read-only; writable agent workspaces are disposable, task-path-filtered, and cannot mutate authoritative state outside approved transaction application.
- Capabilities are opaque, short-lived, session/task/role-bound, checked server-side for every operation, and proposal-bound for reviewer/tester/security work; agents cannot impersonate Human Operator or invoke direct apply/admin functions.
- Container security controls include non-root identity, dropped capabilities, no-new-privileges, seccomp, read-only root, resource/process/time bounds, no daemon socket, no host networking, restricted mounts, deterministic termination, and cleanup.
- Provider egress uses exact endpoint policy and scoped credentials; arbitrary network, internal services, and unrelated credentials remain unavailable. Document residual encrypted-tunnel/exfiltration and runtime-escape limitations.
- Adversarial tests prove filesystem/path/symlink isolation, authorization limits, no direct apply, failed-execution immutability, cancellation/timeouts, concurrent isolation, and successful governed transaction flow, including strict supported-runtime integration.
- Persist and reconcile enough execution metadata to inspect/recover interrupted sessions. A successful process exit is not task acceptance or repository application.

## Risk

High
