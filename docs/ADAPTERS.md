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

## Adapter package planning semantics

The Go registry creates one `CLIAdapter` for every entry in the loaded configuration map, including custom names. Lookup is an exact, case-sensitive name match and returns a found/not-found result; it does not fall back to `generic`. Registry names are returned alphabetically. This package does not independently discover installed executables or probe their supported flags. Capability reporting simply copies the configured booleans; `captures_session_id` is exposed in the adapter capability JSON as `supports_session_capture`.

### Start plan

`Start` requires nonblank session ID, agent ID, run ID, and working directory. It checks these strings with whitespace trimming but retains the original values in the returned record. It does not validate database associations, filesystem existence, executable availability, provider compatibility, or MCP connectivity.

The returned `LaunchSession` contains a session record with status `starting`, a UTC RFC3339 last-seen timestamp, provider/model, external session ID, working directory, socket, and a resume command when a pattern and external ID are available. Metadata contains `mode: start`, role, and task ID. Neither this record nor an event is saved by the adapter itself.

The start command array uses the trimmed configured `command` as **one array element**, falling back to the adapter name when that string is empty. A value such as `codex --some-option` is not split into an executable and arguments. For every name except exact `generic`, a nonempty model adds `--model` and its value, and a nonempty prompt adds `--prompt` and its value. Provider, role, task, socket, working directory, and session ID are not added as command arguments or injected into an environment by this method. The `generic` adapter ignores model and prompt for command construction, although the model remains in the session record. These arrays are implementation plans, not verified provider-specific CLI invocations.

### Resume plan

`Resume` first rejects adapters whose `supports_resume` flag is false. It then requires a nonblank external session ID plus the same four required identifiers/directory as start. It returns status `resuming` and metadata containing `mode: resume`, role, task ID, and `briefing_md`. The briefing is stored in that metadata, not automatically passed to the planned executable. No persistence or process execution occurs.

Resume command construction replaces every literal `{{external_session_id}}` occurrence in `resume_pattern` with the supplied ID. There is no escaping, shell quoting, validation, or expansion of other placeholders. A nonempty generated command is split using Go `strings.Fields`: whitespace separates arguments, but quotes are not interpreted and cannot protect spaces in an ID or argument. If the pattern or external ID is empty, the command array falls back to one trimmed configured-command element; unlike start, this fallback does not substitute the adapter name for an empty command. A true resume capability with an empty pattern therefore does not prove a usable resume command exists.

`Stop` returns success without inspecting the session ID or stopping any process. The start/resume methods do not use their context to perform cancellable work. Statuses such as `starting` and `resuming` describe returned plans, not observed provider process state.

### Operational safeguards

Keep configured executable paths, external IDs, and resume patterns under trusted operator control. Do not execute a saved command by passing it to a shell without an explicit, separately reviewed trust boundary. The current whitespace splitting is not a safe general-purpose command-line parser. Capability declarations must not be used as proof of sandboxing, authenticated MCP access, session capture, process liveness, or compatibility with the installed CLI version.

## HTTP Agent Diagnostics

The API diagnostic action in `api/internal/httpapi/diagnostics.go` requires shared human authorization, a nonblank `Idempotency-Key`, a resolvable workspace, and an existing agent ID. The workspace is checked but not used to establish that the agent belongs to it or to set the probe's working directory. This is a synchronous executable probe with a persisted result, not a provider-session launch. It returns HTTP 202 only after probing and saving; there is no background diagnostic job merely implied by that status.

### Executable Probe

The configured agent command is trimmed, falling back to its adapter name if empty. It is split with `strings.Fields`, so quotes are not interpreted. The first field is executed directly, remaining fields are preserved, and `--version` is appended. Unlike start planning, this path actually calls `exec.CommandContext`. It uses a three-second context derived from the request, combined stdout/stderr, inherited process environment, and the API process's working directory. It does not set the workspace directory, strip credentials from the environment, resolve a provider-specific version flag, or install filesystem isolation.

An empty command produces failed check `executable`. Execution failure produces failed check `executable_version` with generic detail; successful exit produces passed detail containing the sanitized excerpt. A command that rejects `--version` can fail even when installed. Conversely, successful exit does not prove model availability, provider authentication, resume capability, session capture, safe command behavior, or compatibility with planned `--model`/`--prompt` flags. Configure commands as trusted executable configuration; calling something with `--version` does not guarantee it has no side effects.

### Reported Checks and Output

`mcp_surface` passes when the static registry contains at least one advertised tool. It does not connect to a socket, perform a handshake, execute a tool, or exclude unimplemented registry names. `read_only_boundary` is reported passed as descriptive text because the diagnostic does not create a session or explicitly grant repository write access; it is not the outcome of an attempted write or sandbox verification. Overall status is failed if any check has exact status `failed`, otherwise passed.

Combined output is placed in the response's `stdout`; `stderr` is not separately populated. Redaction removes entire lines containing case-insensitive `token=`, `password=`, or `secret=` and trims the remainder. Other secret formats, bearer values, JSON credential keys, paths, and sensitive text are not comprehensively scrubbed. The returned excerpt is capped at 2,000 bytes by slicing, which can split a UTF-8 character. Full subprocess output is collected before truncation, so that cap is not an execution-time memory limit. Remediation hints are fixed text, not a complete failure diagnosis.

### Persistence and Failure Boundaries

The report receives ID `diag-<start Unix nanoseconds>`, agent ID, and RFC3339Nano start/finish timestamps. It is stored as a `test_runs` row with command `agent-diagnostic`, empty task association, and the full report JSON in `summary_md`; it is not written to a separate diagnostic log artifact. Persistence failure returns `diagnostic_persist_failed` after the probe already executed.

After saving, the API appends `agent.diagnostic.completed` under literal run ID `diagnostics`, with agent ID, diagnostic run ID, and status in its payload. Event failure returns `diagnostic_audit_failed` even though the test row already exists. That event is not automatically associated with the requested workspace's run. A failed check can still produce a successful 202 response; inspect report status/checks rather than HTTP status alone. Empty-body requests are reevaluated on retries under the current idempotency middleware, so a repeated request can execute another probe and create another report. See [API Error and Retry Semantics](../api/docs/error-semantics.md).

## Intended integration contract (not yet implemented)

A real process adapter will need to define executable resolution, argument construction without unsafe shell interpolation, environment allowlisting, working-directory and filesystem isolation, process start/stop/cancellation, stdout/stderr streaming and redaction, exit-state mapping, external session ID capture, heartbeat ownership, MCP endpoint injection, and idempotent resume behavior. It must export changes as proposal artifacts rather than write to the authoritative repository. Tests should cover missing executables, cancellation, output limits, secrets, stale sessions, and crash/restart recovery before claiming the adapter feature complete.

Current unfinished scope is tracked in `TODO.md`: actual external process execution and session capture.
