# Tree-sitter Symbol Index Implementation Plan

> **For Codex:** Execute this plan inline, in order, using `superpowers:executing-plans` and strict TDD. The approved specification is `docs/superpowers/specs/2026-10-10-tree-sitter-symbol-index-design.md`. The user explicitly authorized implementation without another approval gate.

**Goal:** Replace Go, TypeScript, TSX, and Python symbol scanning with official pinned Tree-sitter grammars while preserving existing public contracts, symbol identities, claim behavior, and MCP behavior.

**Architecture:** `Indexer.IndexContent` dispatches by the existing extension detector to a fresh Tree-sitter parser configured with a compiled official grammar. Language extractors traverse grammar nodes, reject any tree containing ERROR/MISSING, derive one-based inclusive spans, and produce the existing `Symbol` values. Parser and tree ownership is local to one operation and closed on all paths. Consumers and resource-claim logic remain unchanged.

**Tech Stack:** Go 1.26, `github.com/tree-sitter/go-tree-sitter`, official Go/TypeScript+TSX/Python grammar bindings, CGO.

## Scope Guard

- Do not alter `Symbol`, `Indexer.IndexPath`, `Indexer.IndexContent`, `ResourceID`, `DetectLanguage`, MCP schemas/results, claim semantics, or sorting/identity conventions.
- Keep pre-existing `.gitignore` modification and task-pack ZIP deletions unstaged and untouched.
- Do not use `ParseWithOptions`; use the simple parse API and close every native parser/tree.
- Do not add fallback scanners, partial results, dynamic grammar loading, or broader supported extensions.
- Add upstream attribution/license texts in `THIRD_PARTY_NOTICES.md`; do not declare a project license.

## Tasks

### Task 1: Pin and prove the binding/grammar set

**Files:** `go.mod`, `go.sum`, new `THIRD_PARTY_NOTICES.md`, `internal/symbols/index_test.go`

1. Resolve explicit compatible versions of the binding and three official grammar modules; inspect their module metadata/licenses and verify each grammar can be assigned and parse representative source in the current CGO toolchain.
2. Add a focused grammar-load/parse test covering Go, TS, TSX, and Python and run it before extractor changes. If compatibility fails, select a mutually compatible tagged set and record the evidence in the implementation ledger.
3. Add notice texts for each direct new module and its upstream license/attribution.

**Expected:** dependencies are pinned, all four grammar configurations work, and notices contain no project-license assertion.

### Task 2: Define grammar-driven behavior with regression tests

**Files:** `internal/symbols/index_test.go`

1. Add exact-output tests for kinds/names/spans/IDs, Go receiver and generic types/grouped exported constants, TS generics/enum/const/function-valued const/interface signatures, TSX, decorators, async Python, nested declarations, direct class/interface methods versus nested local functions.
2. Add lexical-trap/multiline tests (comments, strings, templates, braces/indentation), unsupported extensions, deterministic sort, and malformed Go/TS/TSX/Python returning error and zero symbols.
3. Run focused tests and confirm they fail against the old scanner for the intended reasons.

**Expected:** tests pin all compatibility and syntax-tree acceptance criteria before implementation.

### Task 3: Implement native parser lifecycle and syntax validation

**Files:** `internal/symbols/index.go`, `internal/symbols/index_test.go`

1. Replace parser imports and language dispatch with a fresh configured parser for each supported source operation; retain existing public APIs and unsupported-extension result.
2. Parse the provided byte slice without recovery options; validate the complete root for ERROR/MISSING and return no partial symbols on any syntax error.
3. Close tree and parser deterministically on every success/error path; keep errors contextual to the file/language.
4. Add repeated-parse coverage and grammar setup error coverage where practical.

**Expected:** parsing is deterministic, parse failures yield nil/empty results plus error, and native resources have explicit ownership.

### Task 4: Extract compatible symbols from grammar nodes

**Files:** `internal/symbols/index.go`, `internal/symbols/index_test.go`

1. Implement Go declarations, including methods, receiver naming, named struct/interface types, other types, and exported const identifiers.
2. Implement TypeScript and TSX declarations, preserving TS language label, qualifying only direct class/interface methods/signatures, classifying function-valued consts, and retaining nested named function/class declarations without new name qualification.
3. Implement Python class/function/async-function declarations, nearest-class qualification for methods, and def/class-line starts despite decorators.
4. Convert Tree-sitter byte/point spans to one-based inclusive lines correctly at exclusive next-line column zero; preserve sorting and resource IDs, including duplicate names.

**Expected:** exact tests from Task 2 pass; no claim or MCP contract changes are needed.

### Task 5: Integration regressions and docs

**Files:** `internal/claims/*_test.go`, `internal/mcp/*_test.go`, `docs/SYMBOLS.md`

1. Add or refine claim symbol/file and symbol/range conflict regression tests without changing implementation semantics.
2. Assert `repo.symbols` results for Go, TypeScript (including TSX), and Python and preserve error propagation/result shape.
3. Rewrite parser/extraction documentation for Tree-sitter grammars, kinds, spans, malformed-input behavior, compatibility limits, and CGO prerequisites.

**Expected:** consumer contracts remain unchanged and docs match actual tested behavior.

### Task 6: Verify, review, remediate, and record completion

**Files:** task record, progress/completion tracking, any review-driven fixes

1. Run focused tests, required package tests, `go test ./...`, `go test -race ./...`, `go build ./...`, formatting, and diff checks; investigate and resolve all failures.
2. Request a fresh independent read-only review of the complete task diff; address each actionable finding with regression coverage and repeat review until blocking findings are resolved. Report reviewer availability honestly.
3. Re-run applicable complete verification after fixes.
4. Update the task record and progress tracker only with evidence-backed acceptance status; move the original task filename to `TASKS.ROUNDTABLE/completed/` only when all criteria pass.
5. Commit only Tree-sitter task files/code/docs/notices/task record; preserve unrelated dirty paths. Push normally without rewriting published history. Verify commit is on origin and task file is present under `completed/`.
6. Inspect the next eligible TODO item and its spec/dependencies, then continue the authorized backlog without conflating it with this task's completion.

**Expected:** acceptance criteria are evidenced; task completion is accurately recorded; scoped commit is pushed; unrelated worktree changes remain untouched.

## Review Focus

- Parser/tree closure on every return path; no shared parser races or accidental recovery-option leaks.
- Grammar node traversal does not miss nested named declarations or incorrectly turn local functions/properties into methods.
- Inclusive line conversion for nodes ending at column zero on the next line and for single-line nodes.
- Stable names/IDs and duplicate behavior, especially Go generic receivers and Python nested classes.
- No syntax-error path returns partial symbols; no consumer starts swallowing/transforming errors differently.
- Notices reflect actual pinned modules and their license text; no project license is invented.

## Verification Commands

```sh
gofmt -w internal/symbols/index.go internal/symbols/index_test.go
GOCACHE=/tmp/roundtable-go-cache go test -count=1 ./internal/symbols ./internal/claims ./internal/proposals ./internal/mcp ./internal/app
GOCACHE=/tmp/roundtable-go-cache go test ./...
GOCACHE=/tmp/roundtable-go-cache go test -race ./...
GOCACHE=/tmp/roundtable-go-cache go build ./...
git diff --check
```

Use the configured Go module proxy if required to fetch the approved pinned modules; do not weaken tests or substitute non-official parsers to avoid dependency resolution.
