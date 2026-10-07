require "minitest/autorun"
require "open3"
require "rbconfig"

ROOT = File.expand_path("../..", __dir__)
GENERATOR = File.join(__dir__, "generate-admin-api-guide.rb")
SCHEMA_REFERENCE = File.join(__dir__, "..", "docs", "schema-reference.md")

class GenerateAdminAPIGuideTest < Minitest::Test
  def test_schema_reference_lists_nested_inline_fields
    _stdout, stderr, status = Open3.capture3(RbConfig.ruby, GENERATOR, chdir: ROOT)
    assert status.success?, stderr

    reference = File.read(SCHEMA_REFERENCE)
    assert_includes reference, "| `window.from` | `string` | yes | format=\"date-time\" |  |"
    assert_includes reference, "| `properties.name.type` | `string` | yes | const=\"string\" |  |"
  end
end
