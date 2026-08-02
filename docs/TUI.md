# TUI Design

Roundtable uses Bubble Tea and Bubbles.

## Main layout

```text
┌───────────────────────────────────────────────┬───────────────────────────────────────────────┐
│ LIVE TABLE                                    │ WATCH FEED                                    │
│ - active claims                               │ - messages                                    │
│ - proposals                                   │ - votes                                       │
│ - consensus state                             │ - tool calls                                  │
│ - blocked tasks                               │ - test results                                │
├───────────────────────────────────────────────┼───────────────────────────────────────────────┤
│ COMMAND / HUMAN TERMINAL                      │ INSPECTOR                                     │
│ > ask chair ...                               │ selected task/proposal/claim/agent/memory     │
│ > approve P-12                                │ diff preview / vote detail / test log         │
│ > veto P-13                                   │                                               │
└───────────────────────────────────────────────┴───────────────────────────────────────────────┘
```

## Pane 1: Live Table

Displays:

- current run
- current goal
- active tasks
- active agents
- active/suspended claims
- pending proposals
- consensus state
- blocked tasks
- required human approvals
- latest transaction ids

## Pane 2: Watch Feed

Streaming chronological feed of:

- agent messages
- MCP calls
- claims
- releases
- proposals
- votes
- decisions
- security warnings
- human approvals
- test results
- transactions
- memory updates

## Pane 3: Human Command Terminal

Accepts commands:

```text
ask chair <message>
approve P-0001
reject P-0001 --reason "..."
veto P-0001 --reason "..."
pause
resume
claim revoke C-0001
memory query <query>
show proposal P-0001
show tx TX-0001
```

## Pane 4: Inspector

Shows details for the selected item.

Inspector modes:

- Diff
- Task
- Agent
- Claim
- Vote
- Test Run
- Memory
- Security
- Transaction

## Bubble Tea guidance

- Use a root Elm model.
- Use child models for panes.
- Use Bubbles list/table/textinput/viewport/spinner/help/key components where appropriate.
- Avoid raw terminal output management.
- Keep rendering deterministic and testable.
