# Codex Master Build Prompt

You are building Roundtable from the specification in this repository.

Roundtable is a local-first shared-state agentic development orchestrator. Multiple AI agents deliberate together, claim resources, propose patches, vote under policy-weighted governance, and mutate one authoritative repository only through a transaction manager.

Read these files first:

1. README.md
2. PROJECT.ROUNDTABLE.md
3. AGENTS.ROUNDTABLE.md
4. POLICIES.ROUNDTABLE.md
5. docs/ARCHITECTURE.md
6. docs/DATABASE.md
7. docs/MCP_TOOLS.md
8. docs/CLI.md
9. docs/TUI.md
10. docs/ADAPTERS.md
11. docs/CLAIMS.md
12. docs/TRANSACTIONS.md
13. docs/MEMORY_ORACLE.md
14. TASKS.ROUNDTABLE/*.md

Build this as a production-quality Go application.

Non-negotiable requirements:

- Go language.
- SQLite WAL for local state.
- Bubble Tea + Bubbles for TUI.
- MCP-compatible local server/tool surface.
- Tree-sitter-backed symbol-level claims.
- CLI agent adapters.
- Read-only repo access for agents.
- Patch-only mutation contract.
- Orchestrator-applied patches.
- Policy-weighted consensus.
- Human approval gates.
- Security veto.
- Persistent Memory Oracle.
- Durable agent sessions and resume support.
- Transaction ids for applied patches.
- No web dashboard.

Implementation approach:

1. Create a Go module.
2. Implement `roundtable init`.
3. Implement SQLite migrations and repositories.
4. Implement event log and basic pub/sub.
5. Implement MCP tool registry and generated manifest files.
6. Implement Bubble Tea TUI skeleton with four panes.
7. Implement resource claims.
8. Implement patch proposal storage and validation.
9. Implement transaction manager.
10. Implement policy engine.
11. Implement agent sessions/resume.
12. Implement adapters.
13. Implement tree-sitter symbols.
14. Implement Memory Oracle.
15. Add tests throughout.

Do not skip tests. Keep code clean, idiomatic, and modular.
