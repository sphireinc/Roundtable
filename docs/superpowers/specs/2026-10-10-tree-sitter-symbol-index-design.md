# Tree-sitter Symbol Index Design

## Purpose

Replace TypeScript and Python line-oriented declaration scanning with syntax-tree-backed extraction and move Go extraction to Tree-sitter as well. Preserve the symbol index as a coordination aid for resource claims, proposal coverage, MCP inspection, and repository entity discovery; do not present symbol ranges as a sandbox or proof that a patch is semantically confined.

## Current State

- `internal/symbols.Indexer` dispatches by `.go`, `.ts`/`.tsx`, and `.py` extensions.
- Go uses `go/parser` and `go/ast`, returning an error on invalid Go syntax.
- TypeScript uses regular expressions and line-local brace counting; Python uses declaration regular expressions and indentation tracking.
- The public `Symbol` data shape is consumed by claims, proposals, MCP, CLI, and HTTP inventory. Resource IDs are formed from path and declaration name.
- A failed index is already treated as unavailable by some repository-inventory callers; claim/proposal flows use extracted names/ranges to coordinate and validate edits.
- Existing symbol/file/directory conflict logic and the `repo.symbols` tool are separate from parsing and must remain behaviorally compatible.

## Goals

- Use pinned Tree-sitter Go bindings and pinned official grammars for Go, TypeScript, TSX, and Python.
- Parse `.go`, `.ts`, `.tsx`, and `.py` using concrete syntax trees, extracting declarations from grammar nodes rather than line scanning.
- Preserve existing public symbol fields, resource ID format, language labels, naming conventions, and one-based inclusive line ranges.
- Surface malformed syntax as an index error and never present partial extraction as a complete symbol index.
- Preserve symbol/file/directory and symbol/range conflict behavior and keep `repo.symbols` functional.
- Explicitly close native parser/tree resources on every success and error path.

## Non-Goals

- No language server, type checking, name binding, import resolution, reference index, dependency graph, or persistent whole-repository symbol cache.
- No new language or JavaScript extensions beyond Go, TypeScript (`.ts`, `.tsx`), and Python (`.py`).
- No database schema, MCP schema, HTTP response, CLI output, or claim identity migration.
- No change to the transaction manager, claim policy, or repository write boundary.
- No automatic partial-result mode or syntax-diagnostic response schema.

## Approved Architecture

### Parser and grammar dependencies

Use `github.com/tree-sitter/go-tree-sitter` with the official Go, TypeScript, and Python grammar modules. TypeScript and TSX use their distinct grammars while retaining the existing `typescript` value in `Symbol.Language` and MCP language filtering. Pin each module to an explicit compatible release in `go.mod`/`go.sum`; do not fetch grammars dynamically or load user-provided shared libraries.

Grammar modules are compiled into the application using their Go bindings. Roundtable already uses the CGO-backed `go-sqlite3` dependency; this change does not introduce a new CGO class of build requirement, but documentation must retain the existing compiler/build-tool prerequisite. Do not vendor generated grammar code manually. No project license or third-party notices file currently exists. Add `THIRD_PARTY_NOTICES.md` containing the upstream attribution and license text for each newly included Tree-sitter binding/runtime and grammar module. Do not invent or change the project's own license as part of this task.

Use a fresh parser per index operation (or otherwise prove safe ownership and synchronization); set its language before parsing. Close parser and tree objects deterministically, including when setup, parse, or extraction fails. Parsing operates only on the supplied byte slice and does not read imports or execute source code.

### Language and declaration extraction

Keep extension detection case-insensitive and preserve these dispatch values:

| Extension | Grammar | `Symbol.Language` | Declarations |
| --- | --- | --- | --- |
| `.go` | Go | `go` | Functions, methods, named types, structs, interfaces, and exported constants |
| `.ts` | TypeScript | `typescript` | Functions, classes, methods/signatures, interfaces, type aliases, enums, and constants |
| `.tsx` | TSX | `typescript` | The same declaration kinds as TypeScript, using the TSX grammar |
| `.py` | Python | `python` | Classes and functions, including `async def`; methods retain kind `function` for compatibility |

Preserve current kind and name conventions where already established: Go methods use `Receiver.Method`; TypeScript direct class/interface methods and signatures use `Container.method`; Python functions use their declared name, and class-contained functions use `Class.function`. Go named structs and interfaces retain `struct` and `interface` kinds; other named types use `type`. Go constants remain limited to exported names. TypeScript const declarators initialized by an arrow or function expression use kind `function`, while other indexed const declarations use `const`. Do not index `let`/`var` as constants. Do not add class properties as symbols.

Collect named function and class declarations at nested lexical scopes as well as module scope, matching the current scanners' coverage. Keep nested function names unqualified except for Python functions contained by a class, which retain the existing nearest-class prefix. Only direct TypeScript class/interface members are methods; do not turn nested local functions into methods. Do not introduce new scope qualification or identity components.

Include declaration decorators/annotations in the grammar node's reported range when they are part of the declaration node; for Python, retain the current convention of starting at the `def`/`class` line rather than its decorator. In all cases, expose line ranges as one-based inclusive lines. Convert Tree-sitter's zero-based, end-exclusive points carefully, including nodes whose end point is column zero on the following line and single-line declarations.

Resource IDs remain `symbol:<path>#<name>` using the same path representation passed to the indexer. This task does not deduplicate declarations or change path normalization. If a grammar exposes multiple same-name declarations in a file, keep the current behavior of returning each occurrence even though IDs may match; changing claim identity needs a separate migration design.

### Parse errors and recovery

Tree-sitter can recover a syntax tree containing `ERROR` or `MISSING` nodes. For this symbol index, if the root tree contains either kind of syntax error, return an error and no symbols. Do not return partial symbols as if they were complete, and do not silently fall back to the old scanners or Go standard-library parser. A syntactically valid tree may still contain type/name-resolution errors because semantic analysis is explicitly out of scope.

Unsupported file extensions continue to return no symbols and no parse error. Read failures continue to be reported by `IndexPath`. `IndexContent` remains deterministic for equal path/content inputs.

## Compatibility and Integration

- Do not change `Symbol`, `Indexer.IndexPath`, `Indexer.IndexContent`, `ResourceID`, or `DetectLanguage` signatures.
- Keep symbol sorting deterministic by path, start line, then name.
- Preserve claim conflict rules for symbol/file, symbol/directory, symbol/symbol, and symbol/range interactions; add regression coverage if parser spans expose a missed case.
- Preserve the `repo.symbols` arguments and result shape. Existing handlers continue to use the shared indexer and propagate parse errors according to current behavior.
- Update `docs/SYMBOLS.md` to describe grammar-backed extraction, supported kinds, exact malformed-source behavior, and remaining limitations. Remove statements that TypeScript/Python are line-oriented scanners.

## Acceptance Criteria

1. All supported extensions are parsed by their assigned Tree-sitter grammar; Go no longer dispatches through `go/parser` in the symbol index.
2. Tests cover each declared kind and naming convention in the table, including Go receivers/generic types/grouped constants, TypeScript generics and TSX, interface method signatures, decorators, async Python declarations, nested functions/classes, and direct versus nested class members.
3. Tests cover comments/string/template text containing declaration-like tokens, multiline declarations, braces/indentation that defeated the old scanners, and malformed syntax in each language. Malformed input returns an error and no symbols.
4. Tests assert exact one-based inclusive spans, stable resource IDs, extension detection, sorting, and unsupported-extension behavior.
5. Regression tests prove symbol/file and symbol/range conflict behavior is unchanged and `repo.symbols` reports expected symbols for all three language labels.
6. Parser/tree native resources are closed on success and every error path; repeated parsing under `go test -race` has no races or resource-growth failure.
7. `go test ./internal/symbols ./internal/claims ./internal/proposals ./internal/mcp ./internal/app`, `go test ./...`, `go test -race ./...`, and `go build ./...` pass with required CGO tooling.
8. `docs/SYMBOLS.md` accurately documents supported grammars, declaration semantics, error behavior, compatibility boundaries, and build prerequisites.
9. `THIRD_PARTY_NOTICES.md` includes license and attribution notices for each newly introduced Tree-sitter dependency without asserting a project-wide license.

## Risks and Mitigations

- **Grammar node shape drift:** Pin grammar releases and keep representative fixtures for the declaration nodes the extractor depends on.
- **Line-boundary off-by-one errors:** Assert exact inclusive ranges for single-line and multiline declarations, including exclusive end points at the next line.
- **Native allocation leaks:** Use deterministic close calls and repeat-index/race tests.
- **Claims based on incomplete syntax:** Reject the whole file on `ERROR` or `MISSING` nodes rather than returning a deceptively complete subset.
- **Identifier compatibility:** Preserve path/name resource IDs and naming conventions; treat any identity change as out of scope.
- **Platform assumptions:** The repository has no declared Go cross-platform CI matrix. Verify the host build and document the existing CGO toolchain requirement; adding a platform matrix is a separate task unless the implementation reveals a supported target that cannot build.

## Rollback

The parser dispatch and extraction layer can be reverted without changing stored resource IDs or database schema. Keep the old scanner implementation only in version control history; do not ship dual-parser fallback behavior, which could produce different claim ranges depending on syntax validity.

## References

- [Tree-sitter Go bindings](https://github.com/tree-sitter/go-tree-sitter)
- [Official Go grammar](https://github.com/tree-sitter/tree-sitter-go)
- [Official TypeScript and TSX grammars](https://github.com/tree-sitter/tree-sitter-typescript)
- [Official Python grammar](https://github.com/tree-sitter/tree-sitter-python)
- Project requirements: `TASKS.ROUNDTABLE/0003-symbol-index.md`
