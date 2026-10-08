# Standard MCP Coordinator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a foreground, continuously running project coordinator with standard MCP Streamable HTTP and stdio transports that share one runtime.

**Architecture:** `roundtable start` owns one canonical project runtime, the project-wide active-run scheduler, Streamable HTTP MCP, and the legacy Unix socket. Harnesses either connect directly over HTTP or launch `roundtable mcp stdio`, which bridges standard stdio MCP to that one owner. A shared OS process lock protects all existing commands that own orchestration or the legacy socket.

**Tech Stack:** Go 1.26.0, official `github.com/modelcontextprotocol/go-sdk` v1.7.0-compatible APIs, existing SQLite/runtime, standard `net/http`, existing Go test/race/build toolchain.

**Spec:** `docs/superpowers/specs/2026-10-08-standard-mcp-coordinator-design.md`

## Global Constraints

- Preserve Roundtable's one authoritative shared project/repository and transaction-manager-only repository mutation invariant.
- `roundtable start --root DIR` is foreground, remains alive with no active runs, ticks all active runs, and does not create an implicit run.
- Standard HTTP and stdio calls plus legacy socket calls use the coordinator-owned runtime.
- Stdio stdout contains protocol frames only; stdio diagnostics go to stderr; `roundtable start` operational logs go to terminal stdout.
- Bind HTTP to `127.0.0.1:7117` by default; reject non-loopback binds unless TLS and bearer authentication are configured.
- Do not advertise tools without runtime dispatch handlers; `repo.dependency_context` remains excluded.
- Do not remove paths recursively when recovering the legacy socket; reject non-socket paths and remove only a confirmed stale socket.
- Preserve unrelated staged and unstaged user changes; commit only the completed feature slice after review and all required checks pass.
- Docker, boot-time service installation, automatic provider process launch, and provider session capture are out of scope.

## Review Focus

- A non-socket file or symlink at the configured socket path must be rejected without deletion; cover path-type and stale/live listener cases in the socket server tests.
- Two aliases to the same project root must not permit two owners; cover canonical-root lock acquisition and release in process-lock tests.
- A malformed/unknown MCP request must return a protocol error without panicking or leaking internal details; cover malformed JSON-RPC, invalid tool arguments, and unknown tools in protocol tests.
- An MCP stdio server must never contaminate stdout, including startup failure and cancellation; assert all stdout bytes parse as protocol frames and diagnostics appear only on stderr.
- One active run's tick failure and a no-active-run interval must not stop the project service or starve other active runs; cover with deterministic scheduler tests.

## File Map

- Create `TASKS.ROUNDTABLE/0009-standard-mcp-coordinator.md` as the feature task record; move it to `TASKS.ROUNDTABLE/completed/` only after final review and green verification.
- Create `internal/processlock/lock.go` plus platform-specific lock implementations and tests for canonical-root single ownership.
- Modify `internal/mcp/runtime.go` to expose only the minimal shared-store/runtime constructor/access needed by application wiring; keep tool dispatch authoritative.
- Modify `internal/mcp/server.go` to accept a caller-owned runtime, preserve standalone compatibility, avoid recursive socket deletion, and drain handlers on close.
- Create `internal/mcp/standard.go` for translating implemented Roundtable registry tools into the official SDK tool registry and binding calls to `Runtime.Call`.
- Create `internal/mcp/http.go` for the Streamable HTTP handler and loopback/remote security middleware.
- Create `internal/mcp/stdio.go` for the stdio MCP server-to-HTTP client bridge with protocol-only stdout.
- Create `internal/coordinator/service.go` and tests for active-run discovery, per-run tick isolation/backoff, and health state.
- Modify `internal/config/config.go`, `internal/templates/templates.go`, and config tests for HTTP address/TLS settings while preserving existing socket settings.
- Create `internal/app/start.go` and `internal/app/start_test.go`; modify `internal/app/app.go` only to route new commands and apply the shared owner lock to existing socket/coordinator commands.
- Modify `go.mod`/`go.sum` to pin the official Go SDK version selected in this plan.
- Update `README.md`, `docs/CLI.md`, `docs/CONFIGURATION.md`, `docs/MCP_TOOLS.md`, `docs/ORCHESTRATION.md`, `docs/DEPLOYMENT.md`, `docs/QUICKSTART.md`, architecture notes, and generated MCP manifest/config templates as needed.

## Task 1: Shared Project Ownership and Safe Legacy Socket

**Interfaces:**
- Produces `processlock.Acquire(root string) (*Lock, error)` and `(*Lock).Close() error`.
- Produces an MCP server constructor that accepts a caller-owned `*Runtime`; it must not close that runtime when the server closes.
- Existing `run` and `mcp serve` hold the shared lock while owning orchestration/listeners.

- [ ] Create `TASKS.ROUNDTABLE/0009-standard-mcp-coordinator.md` from the approved spec acceptance criteria; keep the task open until the final push and task-file move.
- [ ] Write tests for lock acquisition, duplicate acquisition, release/reacquisition, and two symlink paths to the same root.
- [ ] Run `go test ./internal/processlock -run 'TestAcquire|TestCanonicalRoot' -count=1`; confirm new tests fail because the package/API does not exist yet.
- [ ] Implement cross-platform per-project OS locking on the canonical absolute root, storing the lock under `.roundtable/` without treating a stale lock file as a live lock.
- [ ] Write tests proving socket startup refuses a non-socket path, refuses a live socket, and removes only a confirmed stale socket.
- [ ] Run focused `internal/mcp` socket tests and confirm the non-socket test fails against the current `os.RemoveAll` behavior before implementing the safe path handling.
- [ ] Refactor the legacy server to support an injected shared runtime, track in-flight handlers, stop accepting on close, and drain handlers within a bounded context.
- [ ] Acquire/release the same lock in `run` and standalone `mcp serve`; add tests that one command cannot disrupt the other's listener.
- [ ] Run `go test ./internal/processlock ./internal/mcp ./internal/app -count=1`; expected: PASS.
- [ ] Perform a focused read-only review of lock identity, command coverage, socket path handling, and shutdown; reproduce and fix any finding test-first before Task 2.

## Task 2: Standard MCP Tool Mapping and Streamable HTTP

**Interfaces:**
- Produces `NewStandardServer(runtime *Runtime) *sdkmcp.Server` in `internal/mcp/standard.go` (import the SDK as `sdkmcp` to distinguish it from the Roundtable package).
- Produces `NewStreamableHTTPHandler(server *sdkmcp.Server, options HTTPOptions) http.Handler` in `internal/mcp/http.go`.
- Tool handlers call `runtime.Call(ctx, name, arguments)` and return MCP text plus structured JSON results.

- [ ] Add SDK-client integration tests for protocol negotiation, `tools/list`, successful `tools/call`, unknown-tool errors, and invalid argument errors against an in-memory or `httptest` Streamable HTTP endpoint.
- [ ] Run `go test ./internal/mcp -run 'TestStandardHTTP' -count=1`; confirm failures are due to the missing standard server/handler.
- [ ] Add the pinned official Go MCP SDK dependency and implement registry-to-SDK tool mapping without advertising tools that have no runtime dispatch.
- [ ] Verify `repo.dependency_context` is omitted while every other dispatched registry tool is listed with valid MCP `inputSchema`.
- [ ] Add tests for default loopback bind, Host/Origin validation, rejecting non-loopback without TLS+token, and accepting the configured authenticated TLS mode.
- [ ] Implement Streamable HTTP at `/mcp` and a minimal `/healthz` response with ready/healthy-idle/degraded states only.
- [ ] Run `go test ./internal/mcp -count=1`; expected: PASS.
- [ ] Perform a focused read-only review of MCP negotiation, schemas, JSON-RPC errors, and HTTP trust boundaries; reproduce and fix any finding test-first before Task 3.

## Task 3: MCP Stdio-to-HTTP Bridge

**Interfaces:**
- Produces `RunStdioBridge(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, endpoint string, auth HTTPAuth) error` in `internal/mcp/stdio.go`.
- The bridge is an official SDK MCP stdio server backed by an official SDK Streamable HTTP client; it never opens the database/runtime.

- [ ] Add integration tests using SDK stdio and HTTP clients to verify initialization, forwarded tool listing/calls, result/error forwarding, cancellation, and coordinator-unavailable behavior.
- [ ] Run `go test ./internal/mcp -run 'TestStdioBridge' -count=1`; confirm failures are due to the missing bridge.
- [ ] Implement bidirectional request/response forwarding while preserving MCP schemas and structured results.
- [ ] Assert protocol stdout contains no banner/log text on success, startup failure, or cancellation; assert diagnostics are emitted only to stderr.
- [ ] Add config/flag tests for the configured HTTP URL override and bearer-token environment handling without echoing credentials.
- [ ] Run focused stdio/HTTP tests; expected: PASS.
- [ ] Perform a focused read-only review of bridge forwarding, cancellation, and stdout/stderr separation; reproduce and fix any finding test-first before Task 4.

## Task 4: Continuous Project Coordinator and `roundtable start`

**Interfaces:**
- Produces `coordinator.Service.Run(ctx context.Context, interval time.Duration) error` and `coordinator.Service.Health() Health` in `internal/coordinator/service.go`.
- The service discovers active runs from the shared store and invokes one `orchestrator.Service.Tick(ctx, runID)` per active run per cycle.
- Produces `runStart(ctx, args, stdout, stderr) error`; `start` owns one runtime, lock, coordinator, HTTP MCP, legacy socket, and health endpoint until cancellation.

- [ ] Add deterministic scheduler tests for zero active runs, multiple active runs, one failed run not starving others, recovery on subsequent cycles, and degraded/healthy health transitions.
- [ ] Run `go test ./internal/coordinator -count=1`; confirm new tests fail because the service is missing.
- [ ] Implement active-run polling, per-run capped retry/backoff, cycle-level health reporting, and cancellation without using the finite `orchestrator.Run` convergence path.
- [ ] Add app integration tests that launch `start` on a temp project, observe it remains alive and healthy-idle, activate a run/request, observe coordinator tick behavior, then cancel and verify clean shutdown.
- [ ] Run `go test ./internal/app -run 'TestStart' -count=1`; confirm failures before wiring `start`.
- [ ] Implement CLI parsing for `roundtable start --root DIR [--interval DURATION]`, startup initialization/sync, logs to stdout, signal-aware shutdown, HTTP and socket listener ownership, and safe fatal startup errors.
- [ ] Add an integration test that starts two owner commands for the same canonical project; the second must fail without removing the first command's listeners.
- [ ] Run coordinator/app tests with `-count=1`; expected: PASS.
- [ ] Perform a focused read-only review of start/stop behavior, active-run fairness, retries, and degraded health; reproduce and fix any finding test-first before Task 5.

## Task 5: Configuration, Harness Setup, and Documentation

**Interfaces:**
- Config adds `MCP.HTTPAddress`, `MCP.TLSCertFile`, and `MCP.TLSKeyFile`, with default HTTP address `127.0.0.1:7117`.
- `roundtable mcp stdio --root DIR [--url URL]` launches the bridge; URL defaults to `http://<configured-loopback-address>/mcp`.
- `roundtable mcp inspect` describes standard endpoints and legacy IPC without claiming that the custom socket itself is MCP.

- [ ] Add config round-trip/default/invalid-remote-listener tests and stdio CLI dispatch tests; confirm failures before implementation.
- [ ] Wire config/template parsing and CLI command routing; require TLS cert/key and nonempty `ROUNDTABLE_MCP_TOKEN` for non-loopback HTTP listeners.
- [ ] Add generated Codex and generic harness setup snippets using the local stdio command and direct Streamable HTTP URL; never edit the user's global Codex configuration.
- [ ] Update `README.md` narrowly to document `roundtable start` and standard MCP HTTP/stdio setup, while preserving its trust-boundary and current-limitations guidance; update affected runtime docs, quickstart, deployment, and task index with foreground/no-autostart semantics.
- [ ] Run config, CLI, docs-navigation, and full protocol tests; expected: PASS.

## Task 6: Full Verification, Independent Review, and Closeout

- [ ] Run `go test ./...` and `go test -race ./...`; expected: PASS.
- [ ] Run `go build ./...`; expected: PASS.
- [ ] Run `git diff --check` and inspect the complete feature diff; expected: no whitespace errors, no secrets, no unrelated files staged.
- [ ] Run `npx @modelcontextprotocol/inspector` or an equivalent conforming MCP client against both HTTP and stdio transports if available; record this separately from automated tests.
- [ ] Request a fresh, read-only code review of the task diff focused on lock races, socket path safety, transport compliance, auth, shutdown, runtime ownership, and tool-schema correctness.
- [ ] Reproduce each review finding, implement justified fixes test-first, and rerun all affected tests plus the full suite.
- [ ] Once all checks and review findings are green, stage only the feature spec, plan, task, implementation, and docs; commit and push the feature commit.
- [ ] After the successful push, move `TASKS.ROUNDTABLE/0009-standard-mcp-coordinator.md` to `TASKS.ROUNDTABLE/completed/` in a follow-up commit and push it; verify the task file is present only in `completed/` and the remote contains both commits.

## Manual Review Questions

- Does the advisory lock behave correctly across all target operating systems and process crashes?
- Can the bridge preserve client cancellation and structured tool results across both MCP sessions?
- Does SDK tool discovery preserve current JSON schemas and accurately encode every map/string/number response?
- Can a listener bind failure or an unhealthy database leave an apparently healthy status behind?
- Are logs safe when errors originate in tool arguments, policy, patch validation, or HTTP headers?
