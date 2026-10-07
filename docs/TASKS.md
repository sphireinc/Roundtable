# Tasks and Assignments

The local runtime stores tasks in the configured SQLite database. `task.create`, `task.get`, `task.list`, and `task.update_status` return persisted task records through MCP. Local task rows are shared across runs: these handlers do not filter by run or workspace. The HTTP API does not expose first-class task create/list/get/update routes; other API records can carry a `task_id` association, but that does not make the associated task workspace-scoped or provide task CRUD. Do not infer workspace isolation for local MCP calls.

The HTTP repository-entity inventory is also not a task catalog: it walks filesystem paths and derives file, directory, and symbol entities. For the actual HTTP route surface, see the [API Endpoint Guide](../api/docs/admin-api-guide.md).

## Creation and ordering

`task.create` requires nonempty string `title` and `body_md`. Its advertised optional fields are integer `priority`, string `risk`, and string `assigned_agent_id`. The handler also recognizes string `task_id` and `status`, although those fields are not advertised in the tool schema.

| Field | Stored behavior |
| --- | --- |
| `task_id` | Generated `T-` timestamp ID when missing/empty. Reusing an ID updates the existing row's title, body, status, priority, risk, and assignment. |
| `status` | Defaults to `open`. The local handler accepts arbitrary nonempty strings rather than validating a lifecycle enum. |
| `priority` | Defaults to `100`; the store also converts `0` to `100`. Lower numbers are returned first. Negative values are preserved. |
| `risk` | Defaults to `normal`. The handler stores arbitrary nonempty strings. Exact `high`/`critical` values have special downstream policy behavior. |
| `assigned_agent_id` | Empty means unassigned. The create handler does not check that the agent exists, is enabled, or has the intended role. |

```json
{"title":"Add pagination","body_md":"Implement and validate cursor pagination.","priority":20,"risk":"normal","assigned_agent_id":"implementer-1"}
```

The response is `{"task": ...}`. `task.get` requires `task_id` and fails for a missing row. `task.list` returns `{"tasks": ...}` for all local tasks, ordered by ascending priority, creation timestamp, then ID. It has no pagination or status/agent/run filters; an empty result can serialize as `null` rather than `[]`.

The local handlers use string values only when JSON values are actually strings; a number/object supplied for an optional string becomes empty and therefore selects its default or leaves an update field unchanged. A non-string required title/body fails the same required-field check as omission. Required checks test exact emptiness rather than trimming, so whitespace-only titles and bodies are accepted. Integer priority accepts integer/JSON-number values via the runtime integer helper; floating-point numbers are truncated and nonnumeric/string values fall back to the default. These permissive local MCP conversions are distinct from HTTP API JSON decoding.

## Status and assignment updates

`task.update_status` requires an existing `task_id`. A nonempty string `status` changes status; a nonempty string `assigned_agent_id` changes assignment. Empty or omitted values preserve their existing fields. Consequently this operation cannot clear an assignment by sending an empty string. A call with neither optional field still upserts the task and advances `updated_at`. It returns the saved `task`.

The MCP handler does not enforce transition order or role permissions. Coordinator behavior does depend on status: open work is assigned by orchestration, and convergence examines the statuses described in [Orchestration](ORCHESTRATION.md). Use consistent lowercase lifecycle values and verify the resulting table state. Completing a task does not itself approve or apply its proposal, run tests, or release its claims; perform those operations through their own tools.

The task's `created_at` is preserved on updates. The local create/update handlers persist the task directly and do not themselves append a task event. Read task/table state to verify an update instead of assuming it will appear as a new event in `table.watch`.

See [MCP Tools](MCP_TOOLS.md), [Claims](CLAIMS.md), and [Proposals and Transactions](TRANSACTIONS.md) for the associated workflow.
