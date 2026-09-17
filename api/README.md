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
