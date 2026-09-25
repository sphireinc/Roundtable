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

There is no tree-sitter dependency or language grammar registry in the current implementation. Supporting another language requires updating extension detection, implementing extraction, preserving stable resource ID behavior, and adding parser/edge-case tests. A future tree-sitter implementation would need explicit grammar/version distribution and error-recovery semantics; it is not an installed feature today.
