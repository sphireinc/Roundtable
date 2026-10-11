# Configuration Reference

The Go runtime reads `.roundtable/config.yaml` using `internal/config`. `roundtable init` writes a complete default file. Configuration is currently a deliberately small YAML-like format, not a general YAML implementation: use two-space nesting, scalar `key: value` entries, and quote values only when needed. Lists, aliases, anchors, inline maps, and arbitrary YAML features are unsupported. Unknown keys and unsupported nesting return an error.

## Top-level settings

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `version` | integer | `1` | Configuration format version; only `1` is supported. |
| `project_name` | string | `Roundtable` | Human-readable project name used by runtime metadata. |
| `storage.sqlite_path` | path | `.roundtable/roundtable.db` | SQLite database path, resolved relative to the project root by the CLI. |
| `storage.wal` | boolean | `true` | Parsed for compatibility, but currently does not control the SQLite journal mode. `internal/db` unconditionally executes `PRAGMA journal_mode = WAL`; setting this field to `false` does not disable WAL. |
| `mcp.transport` | string | `unix` | Legacy socket protocol label; only `unix` is supported, and it does not select the standard MCP transport. |
| `mcp.socket_path` | path | `.roundtable/mcp/roundtable.sock` | Compatibility socket path; relative paths resolve under the project root. |
| `mcp.http_address` | host:port | `127.0.0.1:7117` | Streamable HTTP bind address. Non-loopback binds require TLS files and `ROUNDTABLE_MCP_TOKEN`. |
| `mcp.tls_cert_file` | path | empty | TLS certificate path, relative to project root when not absolute. Required with the key for non-loopback binds. |
| `mcp.tls_key_file` | path | empty | TLS private-key path, relative to project root when not absolute. Keep the key outside version control. |

## Agent roles

Single-agent roles have `role`, `adapter`, and `model` scalar fields. Implementers and reviewers are pools and instead accept `count`, `adapter`, and `model`.

| Section | Fields | Defaults |
| --- | --- | --- |
| `agents.chair` | `role`, `adapter`, `model` | `Chair`, `claude`, `default` |
| `agents.architect` | `role`, `adapter`, `model` | `Architect`, `claude`, `default` |
| `agents.implementers` | `count`, `adapter`, `model` | `2`, `codex`, `default` |
| `agents.reviewers` | `count`, `adapter`, `model` | `1`, `gemini`, `default` |
| `agents.tester` | `role`, `adapter`, `model` | `Tester`, `codex`, `default` |
| `agents.security` | `role`, `adapter`, `model` | `Security`, `claude`, `default` |
| `agents.memory_oracle` | `role`, `adapter`, `model` | `MemoryOracle`, `claude`, `default` |

The adapter value selects a named entry under `adapters`; the model is persisted as configuration metadata and is not itself a guarantee that a corresponding CLI model exists. Pool counts control the configured number of agent records, not the number of external processes launched.

## Adapter fields

Each adapter entry accepts only the following fields:

| Key | Type | Meaning |
| --- | --- | --- |
| `image` | string | Trusted Docker/Podman image containing the adapter CLI. Required for container execution; use an immutable digest reference in production. |
| `command` | string | Executable name used by the adapter inside its configured container image. |
| `supports_resume` | boolean | Capability metadata indicating whether a resume command is configured. Defaults false for containerized agents because provider-home state is disposable. |
| `resume_pattern` | string | Optional argv template; built-in disposable container adapters default to empty. |
| `supports_mcp` | boolean | Capability metadata for MCP support. |
| `supports_readonly_workspace` | boolean | Capability metadata for restricted workspace access. |
| `captures_session_id` | boolean | Capability metadata indicating external session ID capture. |
| `provider_hosts` | comma-separated exact DNS names | HTTPS provider destinations permitted through the session CONNECT gateway. No wildcard or IP literal is accepted. An empty list fails closed for execution. |
| `credential_env` | comma-separated environment variable names | Explicit API-key variables eligible for injection into this adapter's container. Values are read by exact name; arbitrary host environment variables are never forwarded. |

Built-in defaults set `supports_resume: false` and an empty `resume_pattern` for Codex, Claude, and Gemini because the container's provider home is destroyed at cleanup; OpenCode and generic also have resume/session capture disabled. Codex, Claude, and Gemini session IDs are captured for audit via provider-specific structured output. Pin images by immutable digest and verify flags against the actual CLI version; Roundtable does not negotiate CLI versions. An explicitly configured resume pattern is split into argv fields, requires exactly one standalone ID placeholder, and does not use a shell. It does not provision persistent provider state.

Adapter names are map keys and can be extended in configuration using the supported scalar fields. A role's `adapter` field refers to one of those names. Adding a custom name does not install the CLI or provision its image. External execution fails closed unless the selected adapter has a trusted `image` and a usable Docker or Podman runtime; there is no host-process fallback.

## Example

```yaml
version: 1
project_name: Example
storage:
  sqlite_path: .roundtable/roundtable.db
  wal: true
mcp:
  transport: unix
  socket_path: .roundtable/mcp/roundtable.sock
  http_address: 127.0.0.1:7117
  tls_cert_file: ""
  tls_key_file: ""
agents:
  implementers:
    count: 3
    adapter: codex
    model: default
adapters:
  codex:
    image: registry.example/roundtable-codex@sha256:<immutable-digest>
    command: codex
    supports_resume: false
    resume_pattern: ""
    supports_mcp: true
    supports_readonly_workspace: true
    captures_session_id: true
    provider_hosts: api.openai.com
    credential_env: OPENAI_API_KEY
```

Unspecified values retain defaults. Boolean values must parse using Go's `strconv.ParseBool` forms. Integer values must parse as base-10 integers. The current parser trims surrounding double-quote characters from scalar values, does not interpret escape sequences, and treats `#` as a comment only when it begins the trimmed line; inline comments are not stripped. Keep literal values simple.

Run `roundtable start --root DIR` to serve coordinator/admin MCP at `/mcp`, capability-scoped agent MCP at `/agent/mcp`, and the legacy socket from one runtime. A harness may launch `roundtable mcp stdio --root DIR`; the bridge connects to the already-running owner and does not start a coordinator. Never place bearer tokens in this configuration file or command-line arguments; supply `ROUNDTABLE_MCP_TOKEN` in the process environment. The agent endpoint requires a short-lived capability issued by Roundtable and does not accept the coordinator token as an agent identity.

Containers run as UID/GID 1000 with all capabilities dropped, no-new-privileges, the runtime's default seccomp profile, read-only root, bounded memory/CPU/process count, no host networking, and scoped source plus tmpfs-backed workspace (up to 1 GiB) and temp (up to 512 MiB) volumes and per-session MCP/egress sockets. Source snapshots contain only operator-assigned `readable_paths`; empty scopes fail closed and sensitive descendants are excluded. Include required build/test manifests and support directories explicitly in the task's path scope. Provider access is fail-closed unless `provider_hosts` is configured. The HTTPS CONNECT gateway permits only exact configured hostnames on port 443, pins validated public DNS results, and rejects local/private/special-use destinations. `credential_env` is an explicit list of API-key variable names; tokens are never stored in this file, and the host environment is not forwarded wholesale. An allowlisted provider can receive task data sent by the agent, so this is controlled provider connectivity, not an anti-exfiltration guarantee. Runtime/kernel vulnerabilities remain possible.

## Validation and limitations

The loader rejects unknown scalar keys and empty sections, invalid nesting, odd indentation, invalid booleans, and invalid integers. A field that expects a scalar cannot be written as a parent section. After parsing, it validates that version is `1`, transport is `unix`, implementer and reviewer counts are nonnegative, and every configured role references a defined adapter with a nonblank command. Zero-sized pools are allowed. It does not perform comprehensive semantic validation. Review both [CLI behavior](CLI.md) and [adapter behavior](ADAPTERS.md) when changing configuration.

## Loading and effective values

`config.Load(root)` reads exactly `<root>/.roundtable/config.yaml`. It does not search parent directories, merge another file, expand environment variables, interpolate placeholders generally, or fall back to defaults when the file cannot be read. A missing or unreadable file returns a `read config` error. Defaults are applied only after the file has been successfully read; a readable empty file produces the default configuration. Use `roundtable init` to create the normal initial file.

Each load starts from a fresh built-in configuration and applies scalar assignments in file order. This differs from policy-profile replacement: overriding one built-in adapter field preserves that adapter's other default fields. A new custom adapter begins with empty strings and false booleans. An empty custom-adapter section without a scalar child does not add an entry to the map. There is no syntax for deleting a built-in adapter from the default map.

The parser has no automatic environment-variable override layer. Strings containing `$HOME`, `~`, or other shell syntax remain literal strings at this stage. Command-specific flags are separate controls; see [CLI Reference](CLI.md). A running orchestrator holds its loaded configuration and does not watch this file for changes. Editing the file does not update that service's in-memory role/pool configuration automatically; restart the relevant runtime when changing startup configuration.

### Scalar values and practical hazards

Accepted boolean spellings are exactly Go's `strconv.ParseBool` forms: `1`, `t`, `T`, `TRUE`, `true`, `True`, `0`, `f`, `F`, `FALSE`, `false`, and `False`. Values such as `yes`, `no`, `on`, and `off` fail. Integers use `strconv.Atoi`: signed decimal values are accepted within the platform's `int` range, but fractional values, hexadecimal notation, underscores, and overflow fail. Quoting a value does not prevent numeric/boolean conversion for a typed field.

The parser treats `null`, `~`, `[]`, and `{}` as literal strings rather than YAML null/list/map values when they appear in string fields. For integer/boolean fields, these values fail conversion. To assign an empty string, use `""`; a bare `key:` is a section header and does not overwrite its default. Single-quoted strings retain their quote characters. These distinctions matter for executable names, paths, and adapter references.

No loader checks enforce nonempty project names, paths, model names, or role names. Adapter commands are checked only for nonblank content, not executable validity or launchability. Pool counts have no configured upper bound; very large counts can cause excessive allocations and database work. Configure operationally bounded counts rather than treating parse success as comprehensive safety validation.

Single-agent IDs/names remain fixed (`chair-1`, `architect-1`, `tester-1`, `security-1`, `memory-oracle-1`) even when their configured role strings change. Pool agents use sequential IDs (`implementer-1`, `reviewer-1`, and so on) and hard-coded `Implementer`/`Reviewer` roles. Changing a role label can affect exact role-name checks elsewhere; it is not a general role-definition mechanism. Agent model fields are loaded configuration values but are not added to the orchestrator's agent-spec command string; adapter planning and session metadata are separate surfaces.

## Parser edge behavior

These are implementation details of `parseYAMLInto` and `splitKV`, not promises of full YAML compatibility:

- The first colon separates key from value. Additional colons remain part of the value, so paths/URLs can be scalar values, but colons in keys are not supported.
- A scalar value is trimmed, then every double-quote character at either edge is stripped. This is not a balanced-quote parser: escape sequences are not decoded, embedded quotes are retained, and single quotes have no quoting meaning.
- A line is a comment only when its first non-whitespace character is `#`. An inline `#` is literal value content; do not append YAML-style inline comments.
- Blank lines and full-line comments do not change the current indentation section.
- Section indentation is counted in spaces and must be an even number. Tabs are not indentation. Nesting may add only one two-space level at a time.
- An empty value (for example `storage:`) is treated as a section header. It does not clear or set a scalar field. Empty headers must name a supported section; unknown sections and scalar fields written as sections fail immediately, even when they have no child entries.
- Repeated scalar keys are accepted and the last parsed assignment wins. Duplicate sections are not rejected.
- Custom adapter names are stored as the segment between `adapters.` and the field. Keep names simple: the format has no escaping for `.` or `:` in a key, and adapter nesting must be exactly `adapters.<name>.<field>`.
- Values are parsed directly into the default configuration. Omitted fields keep defaults; specifying only some fields on a custom adapter leaves its other fields at zero values (`false` or empty string), not built-in capabilities.

For example, do not write `sqlite_path: .roundtable/db.sqlite # local database`: the inline comment would become part of the path. Prefer a comment on its own line.

## Loader Diagnostics

The loader returns the first encountered error, not an aggregate report. Parser diagnostics do not include a line number or a caret; structural errors can include the complete offending line. Preserve the source file and inspect indentation and section context when troubleshooting. Error text is not generally redacted by this parser, so avoid sharing diagnostics containing sensitive scalar values without review.

| Diagnostic | Interpretation and next check |
| --- | --- |
| `read config: ...` | The fixed configuration path could not be read. Check the selected project root, existence, and permissions; this does not trigger a default-only fallback. |
| `unsupported config indentation: ...` | Leading space count is odd. Use two-space levels, not tabs or a general YAML formatter that changes nesting. |
| `invalid config line` | A nonblank/noncomment line has no colon separator. A YAML document marker such as `---` is not accepted as a document delimiter. |
| `missing config key` | The text before the first colon is empty after trimming. |
| `invalid config nesting near ...` | The line skips beyond the next permitted section level. Inspect preceding nonblank/noncomment lines, since they establish the section path. |
| `unsupported config section: ...` | The empty-value header is not a supported section, or a scalar field was used as a parent. Check the section name and indentation. |
| `unsupported config key: ...` | The computed ordinary scalar path is not supported. Check spelling and indentation, not merely the final key name. |
| `invalid value for <path>: ...` | A recognized ordinary integer/boolean value failed Go conversion. Inline comments and single quotes remain in the value and can cause this. |
| `version must be 1, got ...` | The configuration format version is unsupported; use version `1`. |
| `mcp.transport must be unix, got ...` | The configured transport label is unsupported; use `unix`. |
| `unsupported adapter nesting: ...` | A scalar adapter path does not have exactly three segments. Check `adapters`, name, and field nesting. |
| `unsupported adapter key: ...` | The adapter field is not one of the configured capability/command fields. |

Adapter boolean conversion failures are returned directly from `strconv.ParseBool`, rather than wrapped with the ordinary `invalid value for <path>` prefix. The absence of a key-specific prefix does not imply a filesystem or adapter-process failure. `Load` returns an empty `Config` alongside a parse error rather than exposing the partially assigned default configuration; do not continue startup using that result.

A successful load is syntax/conversion evidence only. It does not prove that counts are operationally sensible, adapters exist beyond the role-command relationship, paths are contained or writable, commands can launch, or a resume pattern works. Diagnose these through their actual runtime surfaces and keep loader success separate from service readiness.
