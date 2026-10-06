# Symbol Index

The symbol index provides source-level resource identifiers for claim conflict checks and repository inspection. The implementation is `internal/symbols`; it is a focused parser, not a language server or a complete compiler frontend.

## Supported languages and extracted declarations

| Language | File extensions | Extraction approach | Current symbol kinds |
| --- | --- | --- | --- |
| Go | `.go` | Go standard-library `go/parser` and AST | functions, methods, types, interfaces, structs, exported constants |
| TypeScript | `.ts`, `.tsx` | line-oriented regular expressions and brace-depth tracking | functions, arrow-function constants, classes, interfaces, methods, types, constants, enums |
| Python | `.py` | indentation-aware declaration scanning | functions and classes |

Results include a normalized path, detected language, kind, name, start/end line, and resource ID of the form `symbol:<path>#<name>`. Go parsing reports syntax errors; unsupported file extensions return no symbols. TypeScript and Python extraction is structural and can miss declarations or misinterpret unusual formatting, decorators, comments, multiline signatures, or syntax extensions. It does not resolve imports, types, overloads, scopes, or references.

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

- Go functions and methods include unexported declarations. Method names use `Receiver.Method`; pointer and generic receiver forms are reduced to their receiver type name. Type aliases and other named types are emitted as `type`, except interface and struct AST nodes, which use their respective kinds. Top-level variables and unexported constants are omitted. Exported constant ranges cover the identifier itself rather than its initializer or complete declaration. A Go syntax error returns an error rather than partial symbols.
- TypeScript recognizes a limited set of line-start declarations, optionally prefixed by `export`. Function declarations may also use `async`. Forms such as `export default function`, abstract classes, `let`/`var` declarations, and many generic or multiline signatures are not covered by these expressions. A constant beginning with a parenthesized parameter expression is classified as a function without requiring an actual arrow token; single unparenthesized arrow parameters are not recognized as functions.
- TypeScript class/interface method detection requires an opening brace on the method's declaration line. Signature-only interface methods are therefore omitted. Names are qualified with the nearest tracked class/interface. Control names `if`, `for`, `while`, `switch`, and `catch` are excluded, but this is not complete syntax validation. Constants and type aliases remain single-line ranges; tracked blocks extend to the closing brace or end of input.
- TypeScript brace counting ignores braces in single-, double-, and backtick-quoted text on the current line. Quote state does not persist between lines, and comments, regular-expression literals, and template interpolation are not fully parsed. Braces in comments or multiline constructs can distort nesting and end lines.
- Python recognizes line-start `def` and `class`, not `async def`. A function inside a tracked class is named `Class.function` but retains kind `function`. Nested function names are not qualified with their enclosing function. Decorator lines are not included in declaration start lines. Tabs count as four indentation columns rather than Python's general tab-expansion rules.
- Python blank lines and comment-only lines do not update the last-code-line marker. A block ends at the last nonblank, non-comment line before dedentation, or at the final such line in the file. Multiline strings and signatures are not lexically parsed, so declaration-like text within them can be mistaken for code.

Use these ranges for navigation and coordination, not as a guarantee that a proposed patch affects only one semantic declaration. File/directory claims and patch review remain necessary when extraction is incomplete or IDs overlap.

There is no tree-sitter dependency or language grammar registry in the current implementation. Supporting another language requires updating extension detection, implementing extraction, preserving stable resource ID behavior, and adding parser/edge-case tests. A future tree-sitter implementation would need explicit grammar/version distribution and error-recovery semantics; it is not an installed feature today.
## HTTP Repository Entity Discovery

The HTTP entity list/detail handlers rebuild a filesystem inventory on every request, then enrich it with database governance records. This is not a persistent database catalog, cached semantic index, or dependency graph. A full walk occurs even for a single entity lookup or small page. Walk failures return HTTP 409 `repository_index_failed`; governance query failures return HTTP 500 `repository_entity_context_failed`.

Inventory excludes the root entry itself and skips root-level `.git`, `.roundtable`, and `node_modules` directories. Those exclusions are relative-path-specific: nested dependency/state directories are not generally skipped. It does not consult `.gitignore`. Non-directory paths containing case-insensitive `.env`, `secret`, or `credential`, or ending in `.pem`/`.key`, are omitted. This filename heuristic can hide benign files and does not detect arbitrary secrets. Directory names themselves are not filtered by that secret-path check.

Directories use `directory:<relative-path>` IDs and all advertise `directory:.` as a related entity, even for nested directories; the root itself is not returned. Files use `file:<relative-path>` IDs and point to their immediate directory. File type is classified in this precedence: recognized test suffixes (`_test.go`, `.test.ts`, `.spec.ts`, `.spec.tsx`) become `test_suite`; names containing `openapi` or schema JSON/YAML suffixes become `schema`; root `cmd/` or `scripts/` paths become `command`; other JSON/YAML files become `schema`; remaining paths become `file`. These are filename heuristics, not parsed test, schema, or executable validation.

Recognized languages are passed to the existing symbol indexer; indexing errors silently omit symbols while retaining the file entity. Symbols point to their file and expose name, language, kind, and line range. The indexer receives an absolute filesystem path, so symbol IDs can contain absolute paths while file/directory IDs use relative paths. Do not assume one interchangeable canonical ID scheme across these records or local MCP tools. WalkDir does not recurse through symlinked directories, but a recognized-language symlink passed to the indexer can be read through its target; the filename filter is not a content-confinement guarantee.

Governance enrichment scans global active claims and pending/in-review proposals, without workspace filtering or expiry checks. Relationships match exact resource ID only: ancestor-directory or symbol-overlap ownership is not inferred. An unmatched `claim_status` is omitted, not proof that a conflicting broader lease is absent. Related proposal IDs come from proposal-resource rows, not patch-content analysis. Cross-workspace records with matching IDs can contribute context. There is no atomic snapshot spanning the filesystem walk and database queries.

List `q` is a trimmed case-insensitive substring search over ID, path, name, and kind, not content. `type` is trimmed and case-sensitive. `path` is cleaned as a repository-relative path and matches itself or descendants; absolute paths, explicit root `.`, parent traversal patterns, and CR/LF are rejected as `invalid_repository_path`. This filter runs after the full inventory, not as a restricted subtree walk. Pagination uses base64url-encoded decimal offsets, default limit 50, range 1 through 200. Pages are not stable under concurrent file changes.

Detail trims its requested ID, rejects empty or CR/LF-containing values, and accepts either exact IDs or IDs stripped of `file:`, `directory:`, or `symbol:` prefixes. Unprefixed collisions select the first inventory match; prefer exact prefixed IDs. Missing entities return `entity_not_found`. No endpoint here executes commands, acquires a lease, resolves references, or guarantees semantic coverage; use the parser limitations in this reference when interpreting symbol ranges.
