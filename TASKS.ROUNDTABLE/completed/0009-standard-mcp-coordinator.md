# 0009 Standard MCP Coordinator

## Goal

Provide a continuously running foreground Roundtable coordinator with standard MCP Streamable HTTP and stdio transports, sharing one authoritative runtime and preserving governed repository mutation.

## Acceptance Criteria

- `roundtable start --root DIR` remains alive while idle, creates no implicit run, ticks each active run, and logs lifecycle/coordinator status to stdout.
- HTTP and stdio MCP calls, plus the compatibility Unix socket, dispatch through the same runtime; stdio stdout contains protocol frames only.
- All coordinator/socket-owning commands share a canonical-project process lock and cannot remove or disrupt a live owner.
- Socket recovery refuses non-socket and live paths and removes only a confirmed stale socket.
- MCP discovery exposes only implemented runtime tools and preserves claims, proposal/review, policy, approval, and transaction-manager governance.
- HTTP binds to loopback by default; non-loopback exposure requires TLS and bearer authentication; health reveals no project records or credentials.
- Shutdown stops scheduling/listeners, drains calls within a bound, and closes the single runtime cleanly.
- Tests cover ownership, socket safety, HTTP/stdio protocol behavior, shared state, auth boundaries, scheduler isolation, health, and shutdown.
- README and runtime/harness docs describe start, HTTP/stdio, the legacy socket, security, and foreground/no-autostart semantics.

## Scope and Risk

Use the approved design at `docs/superpowers/specs/2026-10-08-standard-mcp-coordinator-design.md`. No Docker, boot-time service installation, automatic external agent launch, or session capture. Risk: high, due to process ownership, protocol exposure, and shared-runtime lifecycle.

## Verification

Run focused tests, `GIT_CONFIG_GLOBAL=/dev/null go test ./...`, `GIT_CONFIG_GLOBAL=/dev/null go test -race ./...`, `go build ./...`, and diff/security review. Exercise both standard transports with the official Go MCP SDK client; record manual Inspector availability separately.
