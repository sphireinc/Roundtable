# CLI Specification

## `roundtable init`

Creates project scaffolding.

Expected output:

```text
.roundtable/
  roundtable.db
  config.yaml
  mcp/
    AGENT_MCP_MANIFEST.md
    tools.schema.json
    server.json
  sessions/
    agents/
    runs/
  memory/
    README.md
  patches/
  logs/
  runs/

AGENTS.ROUNDTABLE.md
PROJECT.ROUNDTABLE.md
POLICIES.ROUNDTABLE.md
TASKS.ROUNDTABLE/
  0001-bootstrap.md
```

Behavior:

- refuse to overwrite existing files unless `--force`
- initialize SQLite database
- enable WAL
- run migrations
- generate MCP manifest/schema/server files
- create starter task file

## `roundtable run`

Starts a Roundtable session.

Responsibilities:

- load config
- initialize or resume run
- start MCP server
- generate/refresh MCP manifest
- start TUI
- launch/resume agents
- start orchestration loop

Options:

```bash
roundtable run --goal "..."
roundtable run --task TASKS.ROUNDTABLE/0001-bootstrap.md
roundtable run --implementers 3
roundtable run --no-agents
roundtable run --resume
```

## `roundtable resume`

Rehydrates the latest or specified run.

```bash
roundtable resume
roundtable resume --run RUN-...
roundtable resume --agent implementer-1
```

Resume steps:

1. load latest run
2. load snapshots and events
3. restart MCP server
4. refresh manifest
5. reconcile claims
6. restore/resume agent sessions
7. inject resume briefings
8. reopen TUI

## `roundtable mcp inspect`

Shows MCP server and tool registry.

```bash
roundtable mcp inspect
roundtable mcp inspect --json
roundtable mcp inspect --write
```

`--write` updates `.roundtable/mcp/*`.

## `roundtable table`

Prints current table state without opening the full TUI.

## `roundtable watch`

Streams event feed.

## `roundtable sessions`

Lists active/resumable agent sessions and external resume commands.
