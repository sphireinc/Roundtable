# Roundtable API

The API is a standalone Go project boundary inside the repository. Its authoritative contract is [`openapi.yaml`](./openapi.yaml), and its container can be run independently of the UI:

```bash
docker compose -f api/docker-compose.yml up --build
```

The service listens on `http://127.0.0.1:8080` by default. Override the host port with `ROUNDTABLE_API_PORT`, and keep the repository mounted read-only at `/workspace`; only `.roundtable/` is writable for SQLite runtime state.

For a host-native run from the repository root:

```bash
GOCACHE=/tmp/roundtable-go-cache go run ./api/cmd/server -addr 127.0.0.1:8080 -workspace-root .
```

The native server refuses non-loopback binds unless `-allow-remote` is explicitly
provided. The container uses that opt-in because its process listens on the
container interface; publish it only to a trusted local interface or add a
network policy in front of it.

For a human/admin and agent separation, configure distinct secret bearer tokens
outside source control:

```bash
export ROUNDTABLE_HUMAN_TOKEN="$(openssl rand -hex 32)"
export ROUNDTABLE_AGENT_TOKEN="$(openssl rand -hex 32)"
docker compose -f api/docker-compose.yml up --build
```

Keep the human token in the local admin runtime only, never in browser source
or a checked-in `.env` file. Keep the agent token in the MCP/orchestrator
runtime only, rotate both by replacing the environment values and restarting
the API, and use `Authorization: Bearer ...` for the matching control surface.
When no tokens are configured, the API retains its local development header
mode; this mode is intended for loopback development and is not an
authentication boundary for remote exposure.
