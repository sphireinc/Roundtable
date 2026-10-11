# 0013: Config Section and Core Value Validation

## Goal

Reject unsupported empty config sections and unsupported core format/transport values during startup loading.

## Acceptance criteria

- Unknown empty sections fail even when no child key follows.
- Known scalar fields cannot be used as empty parent sections.
- Supported top-level, agent-role, and custom-adapter sections remain accepted.
- Only configuration version `1` and the supported legacy MCP transport label `unix` are accepted.
- Diagnostics and the configuration reference explain these checks.
- Focused and repository-wide race tests, build, vet, independent review, and whitespace checks pass.

## Completion evidence

- Config tests reject unknown/scalar sections, version `2`, and transport `http` while retaining custom-adapter parsing coverage.
- `go test -race ./...`, `go build ./...`, `go vet ./...`, and `git diff --check` pass.
- Independent review found no actionable issues.
