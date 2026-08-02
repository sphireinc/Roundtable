# Prompt 01: Bootstrap

Implement the initial Roundtable Go project skeleton.

Requirements:

- Initialize Go module.
- Add CLI entrypoint at `cmd/roundtable`.
- Implement `roundtable init`.
- Create expected files/directories.
- Initialize SQLite DB with WAL mode.
- Add migration infrastructure.
- Generate `.roundtable/mcp/AGENT_MCP_MANIFEST.md`, `.roundtable/mcp/tools.schema.json`, and `.roundtable/mcp/server.json`.
- Add placeholder `roundtable run` that starts the TUI and MCP server in stub mode.
- Add tests for init behavior.

Acceptance:

- `go test ./...` passes.
- `roundtable init` creates all expected scaffolding.
