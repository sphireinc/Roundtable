# Agent Adapter Capabilities

The adapter package currently represents adapter configuration, capability metadata, and command/resume plans. It is not yet a process supervisor: the orchestrator does not start, monitor, stream output from, stop, or automatically resume Codex, Claude, Gemini, OpenCode, or generic shell processes. Installations of those CLIs alone do not make agents execute.

## Configuration

Adapter entries live under `adapters` in `.roundtable/config.yaml`. Supported fields are `command`, `supports_resume`, `resume_pattern`, `supports_mcp`, `supports_readonly_workspace`, and `captures_session_id`. Defaults for built-in names are fully enumerated in [Configuration Reference](CONFIGURATION.md).

| Adapter | Default command | Resume pattern | MCP | Read-only workspace metadata | Captures session ID metadata |
| --- | --- | --- | --- | --- | --- |
| `codex` | `codex` | `codex resume {{external_session_id}}` | yes | yes | yes |
| `claude` | `claude` | `claude --resume {{external_session_id}}` | yes | yes | yes |
| `gemini` | `gemini` | `gemini resume {{external_session_id}}` | yes | yes | yes |
| `opencode` | `opencode` | none | yes | yes | no |
| `generic` | `sh` | none | no | yes | no |

Capability values are persisted in `adapter_capabilities` and exposed by API diagnostics/configuration surfaces. They are declarations, not runtime enforcement. In particular, `supports_readonly_workspace: true` does not itself create a sandbox or mount the project read-only.

## Sessions and command plans

Session records are created/updated through the sessions CLI/API/MCP-related workflows and store adapter name, provider/model, external session ID, resume command, working directory, MCP socket, state, timestamps, and metadata. If `sessions register` receives no explicit resume command and the configured adapter has a nonempty pattern plus external session ID, the CLI replaces `{{external_session_id}}` and persists the result.

`roundtable resume` produces a database-derived briefing and reconciles stale claims. It does not execute the saved resume command. External session identifiers, command lines, working directories, and metadata should be protected as operational data and scrubbed from public logs.

## Intended integration contract (not yet implemented)

A real process adapter will need to define executable resolution, argument construction without unsafe shell interpolation, environment allowlisting, working-directory and filesystem isolation, process start/stop/cancellation, stdout/stderr streaming and redaction, exit-state mapping, external session ID capture, heartbeat ownership, MCP endpoint injection, and idempotent resume behavior. It must export changes as proposal artifacts rather than write to the authoritative repository. Tests should cover missing executables, cancellation, output limits, secrets, stale sessions, and crash/restart recovery before claiming the adapter feature complete.

Current unfinished scope is tracked in `TODO.md`: actual external process execution and session capture.
