# Provider Session Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture supported provider session identifiers from containerized CLI output without falsely advertising resumability when provider-home state is disposable.

**Architecture:** Keep session parsing inside the CLI adapter, where the provider is known and output is still available. Enable machine-readable CLI output only for adapters that declare capture support; parse only documented session-start/result envelopes, and never treat arbitrary prose as an identifier. Persist IDs for audit, while built-in disposable adapters report resume unsupported.

**Tech Stack:** Go, existing `CLIAdapter`, `sandbox.ExecutionResult`, table-driven Go tests.

**Spec:** `TASKS.ROUNDTABLE/0006-agent-sessions-resume.md`, `TODO.md` provider compatibility item.

## Global Constraints

- External agents run only in the configured Docker/Podman sandbox; do not add host fallback.
- The repository remains read-only to agents; patches retain proposal/transaction governance.
- Do not claim provider compatibility without provider-image runtime evidence.
- Preserve adapter configuration interfaces and do not trust free-form output as session identity.

## Review Focus

- Malformed, unrelated, or adversarial JSON output must not create a session ID.
- Truncated output must not be reported as successful session capture.
- Resume IDs must remain a single argv value and cannot inject flags or commands.
- Captured identifiers must survive supervisor persistence; built-in disposable adapters must not emit resumable commands.
- CLI versions that lack the structured-output flags must fail visibly rather than silently claim capture support.

### Task 1: Provider output parsing and command construction

**Files:** `internal/adapters/adapters.go`, `internal/adapters/adapters_test.go`

- [x] Add failing tests for Codex `thread.started.thread_id`, Claude/Gemini `session_id`, malformed output, and adversarial identifiers.
- [x] Add failing argv tests for structured output when capture is enabled and unchanged argv when disabled.
- [x] Implement provider-specific parsing and argv construction; keep captured IDs in `LaunchSession.Session` and gate resume commands on explicit adapter capability.
- [x] Run adapter tests and then `go test ./internal/adapters`.

### Task 2: Supervisor persistence and documentation

**Files:** `internal/adapters/supervisor.go`, `internal/adapters/supervisor_test.go`, `docs/ADAPTERS.md`, `docs/SESSIONS.md`, `TODO.md`

- [x] Add a supervisor regression test that an output-captured ID is persisted; custom resume configuration is covered separately.
- [ ] Run targeted tests, full race suite, build, vet, and strict container integration tests when the configured runtime/image is available.
- [x] Document parser formats, provider-image compatibility limits, and the live-test requirement; retain any unsupported provider limitation in `TODO.md`.

**Verification evidence:** `go test -race ./...`, `go build ./...`, and `go vet ./...` pass using an isolated Go cache/Git config. Strict container tests and provider-authenticated smoke tests remain unverified: the configured Docker Desktop daemon is unreachable, Podman is not installed, and no provider-pinned agent images/credentials are configured. The backlog item remains open.
