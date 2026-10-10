# 0003 Tree-sitter Symbol Index and Symbol-Level Claims

## Goal

Replace line-oriented and Go AST symbol parsing with pinned official Tree-sitter grammars for Go, TypeScript, TSX, and Python. Preserve current symbol IDs, public API, claim conflicts, and MCP behavior. Treat symbol ranges as coordination metadata, not an access-control boundary.

## Scope

- Official pinned Go bindings/runtime and Go, TypeScript/TSX, and Python grammars, with compatible parser setup tested.
- Grammar-node extraction for the declaration kinds and established names documented in `docs/SYMBOLS.md`.
- Full-file syntax validation: any Tree-sitter `ERROR` or `MISSING` node returns an error and zero symbols; no fallback or partial index.
- Deterministic parser/tree resource closure on success and error paths.
- Preserve `Symbol`, `IndexPath`, `IndexContent`, `ResourceID`, `DetectLanguage`, language labels, sorting, duplicate behavior, and one-based inclusive ranges.
- Preserve symbol/file/directory/range conflict and `repo.symbols` contracts.
- Update index documentation and include third-party license/attribution texts.

## Acceptance Criteria

- Go tests cover functions, methods (including pointer/generic receivers), named types/structs/interfaces, grouped exported constants, exact spans, and malformed syntax.
- TypeScript/TSX tests cover functions, classes, direct methods and interface signatures, aliases, enums, function-valued and ordinary consts, decorators, nested declarations, lexical traps, exact spans, and malformed syntax.
- Python tests cover classes, regular/async functions, nested declarations, decorator range convention, exact spans, and malformed syntax.
- Unsupported extensions, stable IDs/sorting, and repeated concurrent parsing are tested.
- Existing claim conflict regressions pass; MCP `repo.symbols` returns the established language labels for Go, TypeScript, TSX, and Python.
- Existing positive in-symbol and negative out-of-symbol patch-hunk range coverage tests pass; no new claim resource type or conflict behavior is introduced.
- Focused packages, `go test ./...`, `go test -race ./...`, and `go build ./...` pass.
- `docs/SYMBOLS.md` describes grammars, behavior, limits, parse errors, identities, and CGO prerequisites; `THIRD_PARTY_NOTICES.md` includes new dependencies' license and attribution.

## Risk

Normal
