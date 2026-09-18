#!/usr/bin/env ruby
# Lightweight contract gate used without adding a runtime dependency to the Go API.
require "yaml"

path = ARGV.fetch(0, File.expand_path("../openapi.yaml", __dir__))
doc = YAML.load_file(path)
abort "openapi must be 3.1.0" unless doc["openapi"] == "3.1.0"
components = doc.fetch("components")
schemas = components.fetch("schemas")
refs = []
walk = lambda do |value|
  case value
  when Hash
    refs << value["$ref"] if value["$ref"]
    value.each_value { |child| walk.call(child) }
  when Array
    value.each { |child| walk.call(child) }
  end
end
walk.call(doc.fetch("paths"))
doc.fetch("paths").each do |route, item|
  abort "path outside /api/v1: #{route}" unless route == "/metrics" || route.start_with?("/api/v1/")
  item.each do |method, operation|
    next unless %w[get post patch put delete].include?(method)
    abort "missing operationId: #{method} #{route}" unless operation["operationId"]
    abort "missing responses: #{method} #{route}" unless operation["responses"]&.any?
  end
end
refs.each do |ref|
  next unless ref.start_with?("#/components/schemas/")
  name = ref.delete_prefix("#/components/schemas/")
  abort "missing schema reference: #{name}" unless schemas.key?(name)
end
puts "openapi contract ok: #{doc.fetch("paths").length} paths, #{schemas.length} schemas"
