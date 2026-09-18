#!/usr/bin/env ruby
require "yaml"

root = File.expand_path("..", __dir__)
spec = YAML.load_file(File.join(root, "openapi.yaml"))
out = []
out << "# Roundtable Admin API guide"
out << ""
out << "This guide is generated from [`api/openapi.yaml`](../openapi.yaml). Regenerate it with `ruby api/scripts/generate-admin-api-guide.rb` after changing the contract. All routes use `/api/v1` except the Prometheus `/metrics` endpoint."
out << ""
out << "## Shared contract"
out << ""
out << "- Responses are JSON; errors are RFC 7807 `application/problem+json` with `request_id` and `code`."
out << "- Every response includes `X-Request-ID`; pass `X-Correlation-ID` to connect a human action to downstream events."
out << "- Human mutations use `X-Actor-ID` and an allowlisted `X-Actor-Role`; configured deployments use a human bearer token. Agent/MCP calls use a separate agent token."
out << "- Mutations that can be retried declare `Idempotency-Key`; body-bearing mutations replay the original response, while empty-body lifecycle transitions are re-evaluated."
out << "- Collection pages use `limit` (maximum 200) and opaque `cursor` values. Never construct a cursor."
out << "- State-changing handlers validate current state in the same transaction that writes state, audit the action, and publish an after-commit event where applicable."
out << ""
out << "## Endpoint inventory"
out << ""
out << "| Method | Path | Operation | Permission / boundary | Idempotency | Responses |"
out << "|---|---|---|---|---|---|"
spec.fetch("paths").each do |path, item|
  item.each do |method, operation|
    next unless %w[get post patch put delete].include?(method)
    parameters = (item.fetch("parameters", []) + operation.fetch("parameters", [])).map { |p| p["$ref"] || p["name"] }.compact
    permission = method == "get" ? "view" : (parameters.include?("#/components/parameters/ActorRole") ? "role-gated human" : "orchestrator or human")
    permission = "Prometheus scrape" if path == "/metrics"
    idempotency = parameters.include?("#/components/parameters/IdempotencyKey") ? "required" : "not required"
    responses = operation.fetch("responses").keys.sort.join(", ")
    out << "| `#{method.upcase}` | `#{path}` | `#{operation.fetch("operationId")}` | #{permission} | #{idempotency} | #{responses} |"
  end
end
out << ""
out << "## State machines"
out << ""
out << "```mermaid"
out << "stateDiagram-v2"
out << "  [*] --> pending"
out << "  pending --> in_review: request review"
out << "  in_review --> accepted: consensus + policy"
out << "  in_review --> rejected: reject / veto"
out << "  accepted --> applied: transaction manager applies"
out << "  applied --> [*]"
out << "  rejected --> [*]"
out << "```
"
out << "Claims use `active -> released|expired|suspended`; transactions use `staged -> validating -> applying -> applied|failed`, with compensation represented as a new governed proposal."
out << ""
out << "## Local fixtures"
out << ""
out << "The realistic dashboard payload pack is in [`api/examples/dashboard-fixtures.json`](../examples/dashboard-fixtures.json). Start the API with `go run ./api/cmd/server -addr 127.0.0.1:8080 -workspace-root .`, then use the examples as response fixtures; no fixture contains secrets or absolute repository paths."
File.write(File.join(root, "docs", "admin-api-guide.md"), out.join("\n") + "\n")
puts "generated #{out.length} guide lines"
