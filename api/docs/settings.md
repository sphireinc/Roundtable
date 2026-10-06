# Workspace Settings API

Workspace settings are API-managed database revisions, separate from `.roundtable/config.yaml`, process flags, and browser build variables. Persisting a setting does not itself restart a component, change the Go coordinator's loaded policy/configuration, or configure an installed provider. The implementation is `api/internal/httpapi/settings.go`.

## Routes and Defaults

All routes require a resolvable workspace under `/api/v1/workspaces/{id}`:

| Route | Behavior |
| --- | --- |
| `GET /settings/schema` | Returns section fields with default, inferred type, secret flag, and restart metadata. |
| `GET /settings` | Returns workspace ID, current global revision version, and effective section projections. |
| `POST /settings/preflight` | Validates section/values and returns version, impact, and confirmation token; does not save a revision. |
| `PATCH /settings/{section}` | Checks expected version and governance confirmation, then saves a revision and audit row. |

| Section | Default Values | Restart Metadata |
| --- | --- | --- |
| `general` | `locale: "en-US"`, `timezone: "UTC"` | false |
| `governance` | `approval_required: true`, `quorum: 1`, `require_security_review: true` | false |
| `adapters` | `default: "codex"`, `allow_shell: false` | false |
| `mcp` | `transport: "unix"`, `readonly_repository: true` | true |
| `storage` | `wal: true`, `retention_days: 30` | true |
| `notifications` | `enabled: true`, `default_recipient: "human"` | false |

The schema labels booleans `boolean`, numeric defaults `number`, and other defaults `string`. It is descriptive output, not JSON Schema enforcement. Section names must match the six listed names exactly. Unknown field keys and arbitrary field types are otherwise accepted. Only a JSON-number `governance.quorum` receives the integer/range check (1-100); a string or another type bypasses that particular check. Locale/timezone validity, adapter availability, transport support, retention bounds, and boolean types are not validated here.

## Effective Projection

The loader starts from defaults and reads only the highest-version revision for the workspace. It overlays that revision's values into its one section, marks that section source `workspace`, and sets its section version to the global version. Other sections remain source `default` and section version zero. It does **not** replay all previous revisions. Updating another section can therefore make a previously customized section appear reverted to defaults; updating a subset of keys in the same section also does not inherit omitted keys from older revisions.

The top-level version is the maximum stored version, initially zero. It is not the revision of each section independently. Invalid JSON in the latest stored revision can leave the default projection while the version still advances. The standalone runtime YAML loader does not consume these revision rows, and this handler does not write a YAML file.

## Preflight and Confirmation

Preflight requires the shared generic `authorized` check, not `humanAuthorized` or a human-role check, and accepts body `{"section":"governance","values":{"quorum":2}}`. Its forbidden error text mentions a human actor but does not impose additional role validation. Common authentication still applies. Missing/null values can pass validation because a nonempty values object is not required. It returns HTTP 200 with `allowed: true` after the limited validation above, current version, confirmation token, affected component equal to the section name, and restart flag true only for `mcp`/`storage`. It does not test live restart readiness or dependency compatibility, persist an audit/event, or reserve the version. Malformed JSON, an unknown section, and rejected numeric quorum use HTTP 400 `invalid_settings`. A failed effective-settings read is ignored on this path and can yield version zero.

The confirmation token is the hexadecimal SHA-256 of `workspaceId|section|version`. It is deterministic, not a secret, expiring capability, or signed proof of the proposed values. Different values for the same workspace/section/version produce the same token. Only governance updates require it, in `X-Confirmation-Token`; other sections do not. The current CORS allowed-header list does not include that header, so a cross-origin browser preflight may prevent the governance update even when the API endpoint supports it.

## Update Request and Concurrency

Updates accept either a top-level values map or a nested `values` object. When a nested object is present, it is used as the values map; otherwise the entire request is treated as values, including metadata-like keys. Expected version comes from `If-Match` with surrounding quote characters stripped, or numeric `expected_version` in the body, which takes precedence and is converted to an integer. Fractional body numbers truncate. A malformed nonempty `If-Match` conversion currently produces zero because its parse error is ignored. A missing/negative expected version returns `invalid_settings_version`.

Update also uses generic `authorized`, without a human-role check or handler-level idempotency-header requirement. Unlike the body version, `If-Match` is not a general HTTP entity-tag parser: weak tags, wildcard tags, and comma-separated alternatives are not supported, and malformed nonempty input can be treated as expected version zero. No ETag is emitted by these settings handlers. A non-object nested `values` value falls back to treating the entire request as settings; `expected_version` can consequently be stored as an ordinary key in that form. A valid empty values object still creates a revision and advances version. For governance it still requires confirmation. Nothing in this endpoint interprets empty values as deleting the section or restoring all historical overrides.

Mismatch returns `409 settings_version_conflict`. Missing/wrong governance token returns `409 settings_confirmation_required`. An update uses version `current + 1` and ID `CFG-{workspaceId}-{version}`. The current-version read occurs before the write transaction, so concurrent attempts can both pass that check; a revision-key insertion conflict is reported as `settings_update_conflict`, not serialized through a section lock. Reinspect settings before retrying.

The revision and audit insert share a database transaction. Begin failure returns HTTP 500 `settings_update_failed`; insert/audit/commit failures return HTTP 409 `settings_update_conflict`. Revision creator and audit actor are the supplied `X-Actor-ID`. The domain `settings.updated` event is appended after commit under run ID equal to the workspace ID and its error is ignored. Success returns HTTP 200 with workspace ID, version, and sections; it proves neither event delivery nor a component restart. The final projection reload error is also ignored, so a committed update can return null sections and version zero. Current-version read errors before the transaction are likewise ignored and can distort version/conflict handling. By contrast, ordinary GET effective-settings read errors return HTTP 500 `settings_read_failed`. General [idempotency limitations](error-semantics.md) apply to retries.

## Secret Redaction and Storage

Keys containing case-insensitive `secret`, `token`, `password`, or `api_key` are replaced in effective output by `{"configured":...}`. This is a key-name heuristic, not content scanning or recursive redaction. Secret-looking values under other keys remain visible; nested objects under ordinary keys are not traversed. Submitted values are stored in revision JSON without encryption or removal. The configured test is non-null/nonempty-string, not proof that a credential is valid; false/zero values can still count as configured. Do not use these settings as a general secret vault.

The audit payload records section/version, while the persisted revision retains submitted values. Protect database backups and raw revision access accordingly. Restart-required labels and security-looking settings are metadata, not evidence that sandboxing, WAL changes, shell restrictions, approval gates, or notification delivery were applied to running components.
