# Containerized Agent Execution Implementation Plan

> **For agentic workers:** Execute this plan inline with strict TDD. External agents are untrusted workers; Roundtable retains exclusive authority over repository mutation.

**Goal:** Execute configured CLI agents in disposable, resource-limited containers with authenticated role-scoped MCP access and proposal-only repository changes.

**Architecture:** Introduce a Docker/Podman runtime interface and a lifecycle supervisor that mounts a pinned repository snapshot read-only and an isolated writable workspace. Issue short-lived opaque capabilities backed by server-side session records, and authorize every MCP dispatch from the authenticated identity rather than caller arguments. Keep patch submission in the proposal path and require its recorded base revision to match before transaction application. Default networking to disabled; any provider egress must be enforced by an explicit network backend, never by prompt or proxy variables alone.

**Tech Stack:** Go, existing SQLite/session/proposal services, Docker or Podman CLI, MCP Streamable HTTP bearer transport.

**Spec:** User-approved container sandbox decision in the 2026-10-10 task instructions; existing task `TASKS.ROUNDTABLE/0008-cli-adapters.md`.

## Global Constraints

- No unsandboxed host-process fallback.
- Never mount the authoritative repository writable or expose a container-runtime socket.
- Agents cannot invoke administrative or direct-apply MCP operations.
- Runtime absence and unsupported isolation requirements fail with actionable errors.
- Credentials and environment variables are explicitly allowlisted; no host home, SSH, cloud, Git, or unrelated agent credentials.
- Container resource and execution limits are mandatory; networking defaults to deny.
- Preserve proposal, review, policy, and transaction gates; validate patches against their known base revision.
- Do not claim full security or provider egress containment from containerization alone.

## Review Focus

- Symlink replacement and traversal during snapshot, workspace, and patch collection must not escape their roots.
- Authentication must remain session-bound and expire/revoke after lifecycle termination, including reconnects.
- Cancellation, timeout, daemon failure, and coordinator restart must reconcile process and persisted session states.
- Docker/Podman argument construction and user-controlled prompt/task data must never invoke a shell.
- Network policy must be enforced by the selected backend; unsupported provider egress must fail closed.

---

### Task 1: Agent capability authorization

**Files:** `internal/mcp/runtime.go`, MCP transport/auth tests, capability service files.

- [ ] Add opaque expiring identities bound to persisted session, agent, task, role, and server-defined tool allowlist.
- [ ] Authorize tool dispatch server-side; reject missing, expired, revoked, cross-session, and caller-role-spoofed identities.
- [ ] Ensure implementers cannot call approval, decision, admin, or `patch.apply` tools; expose only role-appropriate proposal/review/test capabilities.
- [ ] Add adversarial tests for impersonation, direct apply, privileged tools, and unauthenticated legacy agent dispatch.

### Task 2: Runtime abstraction and hardened container construction

**Files:** `internal/adapters/`, container runtime tests, adapter configuration and docs.

- [ ] Define a runtime interface and Docker and Podman CLI implementations with explicit executable lookup and version/runtime diagnostics.
- [ ] Construct containers with non-root user, cap-drop all, no-new-privileges, default seccomp, read-only root, bounded resources/pids, no host network, no privileged flags, and no daemon socket.
- [ ] Mount only immutable base snapshot read-only, disposable agent workspace read-write, and bounded temp/cache locations; validate canonical paths and symlinks.
- [ ] Default to no network; reject provider execution if requested egress cannot be enforced. Do not treat HTTP proxy env as a security boundary.
- [ ] Test generated runtime arguments and paths, plus actionable failure when neither runtime is usable.

### Task 3: Snapshot, proposal base, and patch security

**Files:** `internal/repo/`, `internal/proposals/`, persistence/migrations, adversarial tests.

- [ ] Pin each execution to a commit/repository state hash and create an immutable read-only source snapshot plus separate writable worktree.
- [ ] Collect only workspace changes as patch artifacts; reject absolute paths, traversal, symlink escapes, malformed diffs, and paths outside claims.
- [ ] Persist patch base revision and reject transaction application when the authoritative revision no longer matches, without weakening existing review gates.
- [ ] Verify an approved proposal still reaches the normal transaction workflow and failed execution leaves authoritative bytes unchanged.

### Task 4: Supervised process/session lifecycle

**Files:** `internal/adapters/`, `internal/sessions/`, orchestrator integration, session persistence/tests.

- [ ] Persist execution identity metadata, runtime/container ID, pinned revision, workspace/artifact paths, timestamps, exit status, and bounded output references.
- [ ] Provision, launch, stream separately captured stdout/stderr, heartbeat, enforce deadline/output/resource ceilings, cancel/terminate, collect artifacts, revoke identity, and deterministically clean up.
- [ ] Reconcile interrupted sessions and containers at coordinator startup; never infer task acceptance from process exit success.
- [ ] Resume only when runtime/session state and adapter support are verified; never execute persisted shell command strings.

### Task 5: Runtime-backed adversarial verification, docs, and task tracking

**Files:** `TASKS.ROUNDTABLE/0008-cli-adapters.md`, `TASKS.ROUNDTABLE/completed/`, `TODO.md`, `DONE.md`, `docs/ADAPTERS.md`, deployment/security docs, integration tests.

- [ ] Exercise actual mount isolation, denied host paths, no network, privilege/resource limits, timeout/cancel, concurrent-container separation, and unchanged authoritative repository using Docker/Podman.
- [ ] Provide an explicit integration-test mode: skip with setup instructions by default when runtime is absent, or fail when strict mode is enabled; report skipped coverage as unverified.
- [ ] Document provider-egress limitations, credentials, runtime prerequisites, cleanup/recovery, and the distinction between execution success and governed acceptance.
- [ ] Run all applicable Go tests including race tests, runtime-backed tests, build, independent read-only review, and remediate findings.
- [ ] Commit/push only task-scoped files; preserve unrelated worktree changes; move the task file and update ledgers only after every acceptance criterion is evidenced.
