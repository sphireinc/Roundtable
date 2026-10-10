# Symbol Index

The symbol index provides source-level resource identifiers for claim conflict checks and repository inspection. The implementation is `internal/symbols`; it is a focused parser, not a language server or a complete compiler frontend.

## Supported languages and extracted declarations

The index uses pinned official Tree-sitter grammars compiled through the Go bindings: Go `.go`, TypeScript `.ts`, TypeScript-grammar TSX `.tsx`, and Python `.py`. `.tsx` retains the existing `typescript` language label. Extension detection is case-insensitive; JavaScript extensions are not supported.

Go indexes functions, methods, named types, structs, interfaces, and exported constants. TypeScript and TSX index named functions (including nested named functions), classes, direct class/interface methods and signatures, interfaces, type aliases, enums, and `const` declarations. A const initialized by an arrow/function expression has kind `function`; other consts have kind `const`. Class properties and `let`/`var` declarations are not indexed. Python indexes classes and regular/async functions; functions lexically inside a class retain kind `function` and use the existing `Class.function` name convention. Nested declaration names otherwise remain unqualified. Go methods retain `Receiver.Method`; TS methods retain `Container.method`.

Results preserve the existing fields and `symbol:<path>#<name>` resource ID. Lines are one-based and inclusive; ranges use the grammar declaration node, except Python decorator lines are deliberately excluded and the range begins at `def`/`class`. Sorting remains path, start line, then name. Duplicate names are not deduplicated, even when they yield identical resource IDs.

The entire syntax tree is checked for Tree-sitter `ERROR` and `MISSING` nodes. Any such node makes the operation fail with an error and no symbols; there is no partial-result or legacy-parser fallback. A valid syntax tree is not semantic/type checking: imports, bindings, references, overload resolution, and type correctness are out of scope. Parsing uses only the supplied source bytes. Native parser and tree objects are scoped to each indexing operation and closed deterministically.

## CLI

```sh
roundtable symbols --path path/to/source.go
```

`--path` is required and is read from the process working directory. Output is one record per symbol: kind, name, path, start line, end line. This command indexes the named file; it does not create a persistent whole-repository index by itself.

## MCP and claims

`repo.symbols` exposes symbol extraction to connected agents. Symbol resource IDs can be used with the claim system. Claim conflict rules also consider containment/overlap between file, directory, and symbol resources; a symbol claim should therefore be treated as coordination metadata, not as a parser-enforced access-control boundary.

## Limits and extension points

### Paths, ordering, and identifier stability

Extension detection is case-insensitive. `.js`, `.jsx`, `.mjs`, and `.cjs` are not supported by this indexer, even when their contents resemble TypeScript. `IndexPath` reads the file before checking its language, so an unreadable unsupported file still produces a read error. Unsupported content otherwise returns no symbols.

The indexer converts platform path separators to forward slashes; it does not itself make paths repository-relative, resolve symlinks, or normalize `.` and `..`. The resource ID uses this path string and the declaration name only, not language, kind, line range, signature, or content hash. Moving or renaming a declaration changes its ID; editing its body without renaming it does not. Duplicate names can produce duplicate IDs because the result is not deduplicated. Sort order is path, start line, then name. Line numbers are one-based.

The MCP `repo.symbols` handler requires a nonempty `path` even though its advertised schema marks that property optional. It resolves the path through the repository-bound path guard and passes the resulting absolute path to the indexer. Consequently, returned paths and IDs can contain the local absolute checkout path; do not assume they are portable between machines or checkouts. The optional `language` filter is an exact, case-sensitive match against `go`, `typescript`, or `python`. It filters after parsing, so it neither selects a parser nor avoids a syntax error in a mismatched Go file. The handler does not persist extracted symbols as resource rows or refresh a repository-wide index.

### Declaration and range details

- Go methods use the receiver's type name, dropping pointer and generic arguments. Named types use kind `struct`/`interface` when their underlying declaration has that form, otherwise `type`. Exported constant names are individually indexed at their identifier spans; unexported constants and variables are omitted.
- TS/TSX class/interface method qualification applies only to direct members. A local function nested in a method is not reclassified as a method. The parser includes comments, strings, templates, multiline signatures, and decorators in syntax structure rather than interpreting declaration-like text in those regions as source declarations.
- Python async functions have the same `function` kind as regular functions. Functions under a class use the nearest class name prefix. Decorators are not included in Python declaration start lines.
- Grammar versions are pinned in `go.mod`/`go.sum`; `THIRD_PARTY_NOTICES.md` contains the upstream MIT attributions and license texts. Building retains the project's existing CGO/C compiler toolchain prerequisite.

Use these ranges for navigation and coordination, not as a guarantee that a proposed patch affects only one semantic declaration. File/directory claims and patch review remain necessary when extraction is incomplete or IDs overlap.

Supporting another language requires adding an explicitly pinned grammar, extension dispatch, extraction semantics, license notice, and parser/edge-case tests. Tree-sitter symbol ranges remain navigation and coordination aids, not a sandbox or proof that patches are semantically confined.
## Proposal-Time Symbol Resolution

The proposal service's `resolveSymbolResource` is not a general index refresh. Non-symbol resources, empty paths, and empty symbol names are returned unchanged. A stored span with positive start and end at least start is also returned immediately, **without reindexing**. A declaration can move, disappear, or change identity while proposal validation continues using that previously stored numeric range.

Only an eligible symbol resource lacking valid bounds is indexed from its path joined to the project root. Indexing errors are suppressed and leave the resource unchanged. Successful extraction selects the first exact, case-sensitive name match and copies its start/end lines; it does not disambiguate by kind, signature, scope, or a caller-selected occurrence. No match leaves the original bounds unchanged. The resolver returns this in-memory record without persisting discovered bounds, so repeated calls can index again.

Coverage uses the resolved numeric span for hunk overlap. Claim freshness hashing uses the same resolver and hashes the resolved line range, but persists the current hash on the original resource record rather than saving newly discovered bounds. Invalid/unavailable ranges can produce an empty hash, which does not suspend the claim; this is not evidence of unchanged symbol content. Valid stale bounds can instead hash unrelated current lines. Resume reconciliation has its own rediscovery behavior and must not be assumed identical to proposal resolution. See [Claims](CLAIMS.md#reconciliation-selection-details) and [Transaction Coverage](TRANSACTIONS.md#resource-coverage-algorithm).

Neither successful resolution nor a valid stored span proves semantic ownership, freshness of the declaration, or full containment of changed lines. Review current file contents and ranges after refactors, especially when names repeat or line positions shift. The local helper has no independent path-containment check, timeout, or persistent repository-wide index.

## HTTP Repository Entity Discovery

The HTTP entity list/detail handlers rebuild a filesystem inventory on every request, then enrich it with database governance records. This is not a persistent database catalog, cached semantic index, or dependency graph. A full walk occurs even for a single entity lookup or small page. Walk failures return HTTP 409 `repository_index_failed`; governance query failures return HTTP 500 `repository_entity_context_failed`.

Inventory excludes the root entry itself and skips root-level `.git`, `.roundtable`, and `node_modules` directories. Those exclusions are relative-path-specific: nested dependency/state directories are not generally skipped. It does not consult `.gitignore`. Non-directory paths containing case-insensitive `.env`, `secret`, or `credential`, or ending in `.pem`/`.key`, are omitted. This filename heuristic can hide benign files and does not detect arbitrary secrets. Directory names themselves are not filtered by that secret-path check.

Directories use `directory:<relative-path>` IDs and all advertise `directory:.` as a related entity, even for nested directories; the root itself is not returned. Files use `file:<relative-path>` IDs and point to their immediate directory. File type is classified in this precedence: recognized test suffixes (`_test.go`, `.test.ts`, `.spec.ts`, `.spec.tsx`) become `test_suite`; names containing `openapi` or schema JSON/YAML suffixes become `schema`; root `cmd/` or `scripts/` paths become `command`; other JSON/YAML files become `schema`; remaining paths become `file`. These are filename heuristics, not parsed test, schema, or executable validation.

Recognized languages are passed to the existing symbol indexer; indexing errors silently omit symbols while retaining the file entity. Symbols point to their file and expose name, language, kind, and line range. The indexer receives an absolute filesystem path, so symbol IDs can contain absolute paths while file/directory IDs use relative paths. Do not assume one interchangeable canonical ID scheme across these records or local MCP tools. WalkDir does not recurse through symlinked directories, but a recognized-language symlink passed to the indexer can be read through its target; the filename filter is not a content-confinement guarantee.

Governance enrichment scans global active claims and pending/in-review proposals, without workspace filtering or expiry checks. Relationships match exact resource ID only: ancestor-directory or symbol-overlap ownership is not inferred. An unmatched `claim_status` is omitted, not proof that a conflicting broader lease is absent. Related proposal IDs come from proposal-resource rows, not patch-content analysis. Cross-workspace records with matching IDs can contribute context. There is no atomic snapshot spanning the filesystem walk and database queries.

List `q` is a trimmed case-insensitive substring search over ID, path, name, and kind, not content. `type` is trimmed and case-sensitive. `path` is cleaned as a repository-relative path and matches itself or descendants; absolute paths, explicit root `.`, parent traversal patterns, and CR/LF are rejected as `invalid_repository_path`. This filter runs after the full inventory, not as a restricted subtree walk. Pagination uses base64url-encoded decimal offsets, default limit 50, range 1 through 200. Pages are not stable under concurrent file changes.

Detail trims its requested ID, rejects empty or CR/LF-containing values, and accepts either exact IDs or IDs stripped of `file:`, `directory:`, or `symbol:` prefixes. Unprefixed collisions select the first inventory match; prefer exact prefixed IDs. Missing entities return `entity_not_found`. No endpoint here executes commands, acquires a lease, resolves references, or guarantees semantic coverage; use the parser limitations in this reference when interpreting symbol ranges.
