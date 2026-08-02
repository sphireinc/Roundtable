# 0003 Symbol-Level Claims

## Goal

Add tree-sitter-backed symbol indexing and symbol-level resource claims.

## Scope

- tree-sitter integration
- Go symbol extraction
- TypeScript symbol extraction
- Python symbol extraction
- symbol resources
- symbol/file/range conflict detection

## Acceptance criteria

- Symbol index can identify functions, methods, types, interfaces, classes, exported consts, and Python classes/functions.
- Symbol claims conflict with file claims.
- Symbol claims conflict with overlapping range claims.
- `repo.symbols` MCP tool works.

## Risk

Normal
