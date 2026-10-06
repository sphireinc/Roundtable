#!/usr/bin/env ruby
require "yaml"
require "json"

root = File.expand_path("..", __dir__)
spec = YAML.load_file(File.join(root, "openapi.yaml"))
out = []
out << "# Roundtable Admin API guide"
out << ""
out << "This guide is generated from [`api/openapi.yaml`](../openapi.yaml). Regenerate it with `ruby api/scripts/generate-admin-api-guide.rb` after changing the contract. All routes use `/api/v1` except the Prometheus `/metrics` endpoint."
out << ""
out << "## Shared contract"
out << ""
out << "- Ordinary handler responses are JSON; structured handler errors use RFC 7807 `application/problem+json` with `request_id` and `code`. Audit exports also support CSV/NDJSON, metrics use text, and WebSocket upgrades use their own protocol. Router, middleware, and transport failures need not share the handler error shape."
out << "- Request middleware assigns `X-Request-ID` to ordinary HTTP responses; pass `X-Correlation-ID` for request-log correlation. Hijacked WebSocket handshakes and downstream persisted events have separate propagation boundaries; do not assume every protocol message or event carries these IDs."
out << "- Mutations commonly require `X-Actor-ID` and, for human-only actions, `X-Actor-Role`. When bearer tokens are configured, requests must also use the matching human or agent token; header-only mode is for loopback development, not remote authentication."
out << "- Mutations that can be retried declare `Idempotency-Key`; body-bearing mutations replay the original response, while empty-body lifecycle transitions are re-evaluated."
out << "- Pagination is endpoint-specific: offset, keyset, and decimal-ID cursors coexist, and some collections are unpaginated. Consult [Pagination](pagination.md), preserve returned cursors, and check documented implementation mismatches before assuming a universal protocol."
out << "- Permission and idempotency columns summarize OpenAPI metadata; check each handler for runtime authorization details. Many governance writes group state and audit/outbox records in a SQLite transaction, but this is not guaranteed for every route or filesystem mutation."
out << "- Browser state-changing requests with an Origin header must match an allowed CORS origin. Default origins and loopback binding are described in `api/README.md`."
out << ""
out << "## Endpoint inventory"
out << ""
out << "| Method | Path | Operation | Permission / boundary | Idempotency | Request schema | Response schemas |"
out << "|---|---|---|---|---|---|---|"

describe_schema = nil
describe_schema = lambda do |shape|
  if shape["$ref"]&.start_with?("#/components/schemas/")
    "`#{shape["$ref"].delete_prefix("#/components/schemas/")}`"
  elsif shape["type"] == "array"
    "array of #{describe_schema.call(shape.fetch("items", {}))}"
  elsif shape["properties"].is_a?(Hash)
    required = Array(shape["required"])
    fields = shape["properties"].map do |name, property|
      required_suffix = required.include?(name) ? "!" : ""
      "`#{name}`: #{describe_schema.call(property)}#{required_suffix}"
    end
    "inline object {#{fields.join(", ")}}"
  elsif shape["type"] == "object" && shape["additionalProperties"]
    "object map of #{describe_schema.call(shape["additionalProperties"])}"
  elsif shape["type"].is_a?(Array)
    shape["type"].join(" or ")
  else
    shape["type"] || "object"
  end
end

content_schema_labels = lambda do |content|
  next [] unless content.is_a?(Hash)

  content.values.map do |media|
    shape = media.is_a?(Hash) ? media["schema"] : nil
    next unless shape.is_a?(Hash)

    if shape["$ref"]&.start_with?("#/components/schemas/")
      name = shape["$ref"].delete_prefix("#/components/schemas/")
      "[`#{name}`](schema-reference.md#schema-#{name.downcase})"
    else
      describe_schema.call(shape)
    end
  end.compact.uniq
end

spec.fetch("paths").each do |path, item|
  item.each do |method, operation|
    next unless %w[get post patch put delete].include?(method)
    parameters = (item.fetch("parameters", []) + operation.fetch("parameters", [])).map { |p| p["$ref"] || p["name"] }.compact
    permission = method == "get" ? "view" : (parameters.include?("#/components/parameters/ActorRole") ? "role-gated human" : "orchestrator or human")
    permission = "Prometheus scrape" if path == "/metrics"
    idempotency = parameters.include?("#/components/parameters/IdempotencyKey") ? "required" : "not required"
    request_schema = content_schema_labels.call(operation.dig("requestBody", "content"))
    request_schema = ["no schema declared"] if request_schema.empty? && operation.key?("requestBody")
    request_schema = ["none"] if request_schema.empty?
    response_schemas = operation.fetch("responses").sort.flat_map do |status, response|
      labels = content_schema_labels.call(response.dig("content"))
      labels = ["no schema declared"] if labels.empty? && response.key?("content")
      labels.map do |label|
        "#{status}: #{label}"
      end
    end
    response_schemas = ["none declared"] if response_schemas.empty?
    out << "| `#{method.upcase}` | `#{path}` | `#{operation.fetch("operationId")}` | #{permission} | #{idempotency} | #{request_schema.join(", ")} | #{response_schemas.join(", ")} |"
  end
end
out << ""
out << "## Lifecycle operations"
out << ""
out << "The API exposes explicit action endpoints for deliberations, proposals, policies, approvals, sessions, claims, runs, and transactions. Each endpoint's accepted action, current-state preconditions, and conflict response are defined by its OpenAPI operation and handler. The API control-plane state model is distinct from the local Go proposal/transaction service; do not apply one component's state diagram to the other."
out << ""
out << "For proposal apply, transaction recovery, and compensation semantics, consult the corresponding transaction operation schema and handler. Compensation creates a separate proposal from the rollback artifact; it does not silently rewrite history."
out << ""
out << "## Local fixtures"
out << ""
out << "The realistic dashboard payload pack is in [`api/examples/dashboard-fixtures.json`](../examples/dashboard-fixtures.json). Start the API with `go run ./api/cmd/server -addr 127.0.0.1:8080 -workspace-root .`, then use the examples as response fixtures; no fixture contains secrets or absolute repository paths."
File.write(File.join(root, "docs", "admin-api-guide.md"), out.join("\n") + "\n")

def schema_type(shape)
  return "`#{shape.fetch("$ref").delete_prefix("#/components/schemas/")}`" if shape["$ref"]

  type = shape["type"]
  if type.is_a?(Array)
    type = type.map { |entry| entry == "null" ? "null" : entry }.join(" or ")
  end
  type ||= "object"
  if type == "array"
    item = shape.fetch("items", {})
    item_constraints = schema_constraints(item)
    suffix = item_constraints.empty? ? "" : " (item constraints: #{item_constraints})"
    return "array of #{schema_type(item)}#{suffix}"
  end
  if type == "object" && shape.key?("additionalProperties")
    additional = shape["additionalProperties"]
    value_type = if additional == false
      "no additional properties"
    elsif additional == true
      "any value"
    else
      "values of #{schema_type(additional)}"
    end
    return "object (#{value_type})"
  end
  "`#{type}`"
end

def schema_constraints(shape)
  keys = %w[format enum minimum maximum exclusiveMinimum exclusiveMaximum minLength maxLength minItems maxItems minProperties maxProperties multipleOf pattern default example uniqueItems]
  keys.map do |key|
    next unless shape.key?(key)
    value = shape[key]
    "#{key}=#{value.is_a?(String) ? value.inspect : value.to_json}"
  end.compact.join("; ").gsub("|", "\\|")
end

schemas = spec.dig("components", "schemas") || {}
schema_doc = []
schema_doc << "# API Schema Reference"
schema_doc << ""
schema_doc << "Generated from [`api/openapi.yaml`](../openapi.yaml) by `ruby api/scripts/generate-admin-api-guide.rb`. This page lists every component schema and its declared fields; runtime validation may impose additional rules documented by endpoint handlers and [Error Semantics](error-semantics.md)."
schema_doc << ""
schema_doc << "The endpoint guide links operations to request and response schemas. `required` reflects the OpenAPI contract, not whether a response field may be omitted by every runtime branch. `additionalProperties` is shown when declared."
schema_doc << ""

schemas.each do |name, shape|
  schema_doc << "### Schema: #{name} {#schema-#{name.downcase}}"
  schema_doc << ""
  schema_doc << "Type: #{schema_type(shape)}"
  schema_doc << ""
  schema_doc << "Description: #{shape["description"].to_s.gsub("|", "\\|").gsub("\n", "<br>")}" if shape["description"]
  root_constraints = schema_constraints(shape)
  schema_doc << "Schema constraints: #{root_constraints}" unless root_constraints.empty?
  schema_doc << "Required fields: #{Array(shape["required"]).map { |field| "`#{field}`" }.join(", ")}" unless Array(shape["required"]).empty?
  if shape.key?("additionalProperties")
    value = shape["additionalProperties"]
    schema_doc << "Additional properties: #{value == false ? "forbidden" : (value == true ? "allowed with any value" : "values of #{schema_type(value)}")}."
  end
  schema_doc << ""
  properties = shape["properties"] || {}
  if properties.empty?
    item = shape["items"]
    schema_doc << "Array item schema: #{schema_type(item)}." if item
    schema_doc << "No named properties are declared." unless item
  else
    required = Array(shape["required"])
    schema_doc << "| Property | Type | Required | Constraints | Description |"
    schema_doc << "|---|---|---|---|---|"
    properties.each do |property, property_shape|
      description = property_shape["description"].to_s.gsub("|", "\\|").gsub("\n", "<br>")
      constraints = schema_constraints(property_shape)
      schema_doc << "| `#{property}` | #{schema_type(property_shape)} | #{required.include?(property) ? "yes" : "no"} | #{constraints} | #{description} |"
    end
  end
  schema_doc << ""
end

schema_doc.pop if schema_doc.last == ""
File.write(File.join(root, "docs", "schema-reference.md"), schema_doc.join("\n") + "\n")
puts "generated #{out.length} endpoint-guide lines and #{schema_doc.length} schema-reference lines"
