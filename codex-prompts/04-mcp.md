# Prompt 04: MCP Server

Implement the MCP-compatible local server and tool registry.

Requirements:

- local socket transport
- tool registry
- generated manifest
- generated schema JSON
- table.get_state
- task.list/get
- resource.search/get/claim/release/claim_status
- repo.read_file/search/symbols
- proposal.create/get/list
- vote.cast/list
- decision.record
- test.run/get_result
- memory.query/record/summarize/mark_stale
- security.review
- human.request_approval

Acceptance:

- `roundtable mcp serve` starts server
- `roundtable mcp inspect` prints tools
- `roundtable mcp inspect --write` regenerates manifest/schema/server files
