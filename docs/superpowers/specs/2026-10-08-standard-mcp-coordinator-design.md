# Standard MCP Coordinator Design

## Purpose

Make Roundtable available to Codex and other MCP-capable harnesses through standard MCP transports while keeping one continuously running coordinator as the owner of runtime state and orchestration.

Roundtable's governing invariant remains unchanged: multiple agents deliberate around one authoritative shared project/repository. Agents use governed tools to inspect state, claim resources, submit patch-only proposals, and pass review, policy, and human-approval gates. Repository mutation continues through the transaction manager; an MCP transport must not add an alternate write path.

## Current State

- The local Unix socket uses a Roundtable-specific newline-delimited JSON envelope (`tool` plus `args`), not MCP JSON-RPC.
- `roundtable mcp serve` opens a runtime and serves that socket, but does not run orchestration.
- `roundtable run` starts the socket and TUI while running the orchestrator for one run; it exits when that run converges or the process is stopped.
- The orchestrator exposes a one-run `Run` loop and a single-cycle `Tick`; there is no project-wide foreground service that ticks all active runs.
- The registry advertises one tool without a dispatch handler (`repo.dependency_context`); protocol discovery must not advertise unimplemented tools.
- Existing runtime handlers already enforce Roundtable's resource, proposal, policy, approval, and transaction behavior and should remain the authoritative tool implementation.

## Approved Architecture

### Process ownership and lifecycle

`roundtable start --root DIR` is a foreground service. It resolves the project root to one canonical absolute path, then opens and owns one database store/runtime, coordinator service, and MCP server set for the lifetime of the process. It acquires a per-project operating-system process lock before opening listeners. The lock is shared by every command that owns the project coordinator or binds the compatibility Unix socket, including `run` and standalone `mcp serve`, so competing commands fail without unlinking or replacing live endpoints. MCP stdio bridge processes do not acquire this lock because they only connect to the running HTTP endpoint.

At startup it loads and validates project configuration and policy, synchronizes configured agent capabilities, starts the listeners, and reports readiness. It does not create an implicit run or goal. The process stays alive when there are no active runs and periodically discovers and ticks every active run. Ticks are scoped and isolated per run; one run's transient failure is logged and does not prevent other active runs from being ticked. The coordinator does not exit or mark a run terminal merely because a single run converged. Existing finite `roundtable run` semantics remain available and unchanged.

The default scheduler interval is one second and is configurable by a `--interval` flag. Startup/configuration/bind/lock failures are fatal and return nonzero. Transient tick and storage errors after startup are logged and retried with capped backoff while service health reports degraded. A healthy idle service is distinct from a failed/degraded service.

SIGINT and SIGTERM initiate graceful shutdown: stop scheduling and accepting new requests, allow in-flight MCP calls a bounded drain interval, close listeners, then close the single shared runtime/database. Startup and shutdown messages and coordinator logs go to terminal stdout. Logs must not include credentials, patch contents, or unrestricted tool arguments.

### MCP transports

Use the official Go MCP SDK, pinned to a release compatible with the repository's Go version. The SDK owns protocol framing, lifecycle negotiation, JSON-RPC validation, and transport behavior; Roundtable owns tool definitions and execution.

`roundtable start` exposes Streamable HTTP at `/mcp`, by default on `127.0.0.1:7117`. The HTTP address is configurable. Loopback is the default trust boundary. A non-loopback bind is disabled unless the operator explicitly configures a bearer token and TLS certificate/key; secrets are supplied through environment/config-file references, never command-line arguments or logs. Apply Host/Origin protections for loopback HTTP requests. A minimal `/healthz` endpoint reports readiness/degraded state without exposing project records or secrets.

`roundtable mcp stdio --root DIR` is a standard MCP stdio server process for harnesses that launch child processes. It is a transport bridge, not a second coordinator: it connects to the configured Streamable HTTP endpoint and forwards MCP operations without opening SQLite or starting orchestration. It uses stdin/stdout only for MCP protocol frames and writes diagnostics to stderr. It exits with a clear error if the coordinator endpoint is unavailable; it does not silently start another coordinator.

All transports reach the same coordinator-owned runtime. Existing handlers are mapped to standard MCP `tools/list` and `tools/call` behavior, preserving input schemas and returning useful text plus structured results where supported. Only tools with runtime dispatch handlers are listed. Tool calls are routed through the existing runtime dispatch; the transport layer adds no direct filesystem/repository write capability.

The existing Unix socket protocol remains available for compatibility and is hosted by `roundtable start` against the same runtime. It remains documented as legacy Roundtable IPC, not standard MCP. The standalone `roundtable mcp serve` command remains compatibility-only and does not claim to provide continuous coordination. On startup, socket recovery must inspect the path type, refuse to remove a non-socket path, detect/refuse a live listener, and remove only a confirmed stale socket; recursive deletion is forbidden.

### Configuration and harness integration

Add an MCP HTTP address setting with a loopback default while preserving the existing Unix socket setting. `roundtable mcp inspect` and generated MCP assets must distinguish standard MCP endpoints from the legacy socket. Documentation will show:

- starting the foreground service and stopping it with the normal terminal interrupt;
- configuring Codex and other local harnesses to launch `roundtable mcp stdio`;
- configuring clients that support Streamable HTTP to connect to `http://127.0.0.1:7117/mcp`;
- supplying authentication safely when TLS/non-loopback access is intentionally configured;
- distinguishing service liveness, MCP endpoint readiness, and per-run orchestration health.

The stdio bridge uses the same client-visible MCP server identity and schemas as the HTTP endpoint. It must not print banners or status messages to stdout.

## Security and Compatibility

- Preserve the one-authoritative-database model and transaction-manager repository mutation invariant.
- Keep remote listening opt-in; require TLS and bearer authentication for non-loopback exposure.
- Validate Host/Origin on HTTP and do not trust agent IDs in tool arguments as cryptographic identity.
- Do not log bearer tokens, authorization headers, patch bodies, or raw tool argument maps.
- Retain the existing socket protocol for current local clients during this change; document its custom framing and weaker identity boundary. Protect all commands binding the socket with the shared per-project process lock, and remove only a confirmed stale socket path.
- Do not expose tools that are advertised but unimplemented by the runtime.
- Do not add Docker, launchd/systemd installers, auto-start-on-boot behavior, external agent process launch, or provider session capture in this task.

## Acceptance Criteria

1. `roundtable start --root DIR` stays in the foreground until cancellation, prints operational logs to stdout, and runs with no active runs without exiting or creating a run.
2. While `start` is live, each active run is periodically ticked; one run's transient error does not starve other runs or terminate the service.
3. A second coordinator/socket-owning command (`start`, interactive `run`, or `mcp serve`) for the same project fails safely and does not unlink or disrupt the first process's socket/listeners.
4. The Streamable HTTP endpoint negotiates MCP and supports tool listing/calls using standard schemas and JSON-RPC results/errors.
5. `roundtable mcp stdio` interoperates with a conforming MCP client, forwards calls to the live coordinator, and never writes non-protocol content to stdout.
6. Calls made through HTTP, stdio, and the legacy Unix socket operate on the same coordinator-owned runtime and shared state.
7. MCP discovery contains every implemented tool and excludes `repo.dependency_context` until a dispatch handler exists.
8. Existing governed tool behavior remains intact: no new path bypasses claims, proposal review, policy, human approval, or transaction management.
9. Loopback is the default HTTP bind. Non-loopback binding is rejected unless TLS and bearer authentication are configured; credentials do not appear in logs.
10. SIGINT/SIGTERM stop scheduling, drain in-flight requests within a bounded timeout, close listeners/database cleanly, and leave no active listener behind.
11. Health output distinguishes ready, healthy-idle, and degraded states without exposing project data.
12. Tests cover startup failures, no-run idle, active-run ticking, retry isolation, cross-command duplicate ownership, live/stale/non-socket socket paths, HTTP protocol flow, stdio framing/forwarding, shared state, security boundaries, and graceful shutdown. A manual MCP Inspector/client smoke test verifies real harness interoperability.
13. Documentation describes `roundtable start`, stdio and HTTP harness configuration, legacy socket status, security requirements, and the fact that foreground operation does not auto-start after reboot.

## Verification Scope

Automated evidence includes focused package tests, `go test ./...`, `go test -race ./...`, and `go build ./...`. Protocol tests must exercise the pinned official Go SDK client against both transports, not only call `Runtime.Call` directly. Manual interoperability is recorded separately from automated test results. Existing dirty task-pack deletions and unrelated generated-file ignore/index changes are not part of this feature.

## Operational Non-Goals

This feature provides a continuously running foreground coordinator while its terminal process is alive. It does not install an OS service, restart after host reboot, supervise itself after a crash, run in Docker, or launch/configure external agent CLIs. Those remain separate follow-up tasks.
