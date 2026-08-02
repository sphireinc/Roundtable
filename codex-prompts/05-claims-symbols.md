# Prompt 05: Claims and Symbols

Implement resource claims and tree-sitter-backed symbol indexing.

Requirements:

- file/directory/resource claims
- claim statuses and TTL
- conflict detection
- tree-sitter integration
- Go, TypeScript, Python symbols
- symbol/file/range conflict detection
- claim reconciliation on resume

Acceptance:

- conflicting claims are denied
- non-conflicting claims are granted
- symbols can be listed through MCP
- symbol claims work
