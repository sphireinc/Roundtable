# Test Execution and Evidence

The local runtime exposes `test.suggest`, `test.run`, and `test.get_result`. Suggestions are heuristics; execution stores a result and log; proposal patch application evaluates its other gates separately. The runtime does not automatically execute a proposal's `expected_tests` or enforce the parsed `tests_required` policy field during apply.

## Suggestions

`test.suggest` accepts optional `proposal_id` and `task_id`. A proposal ID loads that proposal, reads/parses its stored patch, inherits its risk, and supplies its task ID when no task ID was given. Missing proposals or unreadable/malformed patches return errors. With only a task ID, the handler tries to read its risk; a missing task is ignored and suggestions still return.

The returned `commands` list is deduplicated and sorted alphabetically:

| Input | Added suggestion |
| --- | --- |
| Every call, including no IDs | `go test ./...` |
| `.py` touched path | `pytest` |
| `.ts`, `.tsx`, `.js`, or `.jsx` touched path | `npm test` |
| Path containing `Dockerfile` or `.github/workflows/` | `npm test` and `go test ./...` |
| Exact risk `high` or `critical` | `npm test` and `go test ./...` |

The response also contains `proposal_id` and `task_id`. The handler does not inspect installed tools, package scripts, language configuration, or working subdirectories. Review suggestions before executing them; they may not suit the project.

## Command execution

`test.run` requires a nonempty string `command`, with optional `proposal_id` and `task_id` associations. The handler additionally accepts `test_run_id`, although it is not advertised in the registry. Omitted/empty ID generates a `TR-` timestamp ID; reusing an ID overwrites its result fields and log file.

```json
{"command":"go test ./...","proposal_id":"P-123","task_id":"T-123"}
```

Before persisting a run, the guard trims the command, rejects `;`, `|`, `&`, `>`, `<`, backticks, `$`, newline, and carriage return anywhere in it, and splits on whitespace to inspect the first word. Its basename must be one of `cargo`, `git`, `go`, `make`, `node`, `npm`, `pnpm`, `printf`, `pytest`, `ruby`, `swift`, `vitest`, or `yarn`. Full executable paths with those basenames pass the same check. The guard does not parse shell quotes or validate every subcommand/argument; an allowed executable can still change files or use the network. Use commands whose effects have been reviewed.

The service persists status `running`, launches `/bin/sh -lc` with the repository root as working directory, and captures combined stdout/stderr in memory. Environment is inherited by the command; there is no tool argument for environment, working directory, timeout, or output limit. The process is attached to the call context. There is no live output stream or test-cancel tool in this local interface.

For socket calls, that context comes from the server lifetime rather than a deadline supplied by the client. Closing the client connection does not itself cancel the command. Do not treat a lost response as evidence that execution stopped; inspect the result/artifact and coordinator state before retrying a command.

After execution it writes `.roundtable/testlogs/<test_run_id>.log`, saves that relative path and a summary, and changes status to `passed` if the command returned no error or `failed` otherwise. The summary is the last six lines of trimmed output, or `No test output captured.` for empty output. The response is `{"test_run": ..., "log": ...}`, with the full captured output in `log`.

A command failure is represented by the saved `failed` status; it is not itself a tool-call error when result/log persistence succeeds. Clients must inspect `test_run.Status` as well as the MCP envelope's `ok`. The handler does not store a separate numeric process exit code. Directory/log-write/database errors return tool errors and can leave the initial `running` record behind, even after the command finished. Inspect the record and filesystem before treating such a row as proof of a live process.

## Reading and interpreting results

`test.get_result` requires `test_run_id` and returns `test_run` plus `log`. Missing result rows return an error. If the stored log cannot be read, the handler returns an empty log without surfacing the file-read error. Empty output therefore has several possible causes; inspect the saved status, summary, path, and actual artifact.

Associations label the result but do not establish that the executed command tested the current proposal revision. The handler stores no patch hash or repository revision binding. Record the command, environment, tested revision, scope, and timing in review evidence, and repeat relevant tests after changes. A successful shell command establishes only that command's result; it does not establish browser, external-provider, or whole-project acceptance.

See [MCP Tools](MCP_TOOLS.md), [Proposals and Transactions](TRANSACTIONS.md), and [Database](DATABASE.md).
