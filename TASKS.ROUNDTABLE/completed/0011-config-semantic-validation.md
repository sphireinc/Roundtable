# 0011: Configuration Semantic Validation

## Goal

Fail startup configuration loading with actionable errors when agent pool counts are negative or a configured role references an unavailable adapter command.

## Acceptance criteria

- `config.Load` rejects negative implementer and reviewer counts.
- `config.Load` rejects role adapter names absent from `adapters`.
- `config.Load` rejects role adapters whose configured command is blank.
- Zero-sized pools and valid custom adapter definitions remain accepted.
- Configuration documentation describes these checks.
- Focused and repository Go tests pass; reviewed findings are resolved before commit.

## Scope

This task does not implement broad schema validation, impose a maximum pool size, change transport behavior, or launch external processes.
