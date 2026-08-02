package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"roundtable/internal/db"
)

func TestEvaluateProposal(t *testing.T) {
	result := EvaluateProposal("normal", []string{"README.md"}, []db.Vote{
		{AgentID: "architect", Vote: "approve"},
		{AgentID: "reviewer", Vote: "approve"},
	})
	if !result.Approvable || result.NeedsHuman {
		t.Fatalf("expected approvable proposal, got %+v", result)
	}

	result = EvaluateProposal("normal", []string{"go.mod"}, []db.Vote{
		{AgentID: "architect", Vote: "approve"},
		{AgentID: "reviewer", Vote: "approve"},
	})
	if result.Approvable || !result.NeedsHuman || len(result.HighRiskPaths) != 1 {
		t.Fatalf("expected human gate for high-risk path, got %+v", result)
	}

	result = EvaluateProposal("normal", []string{"README.md"}, []db.Vote{
		{AgentID: "security", Vote: "veto"},
		{AgentID: "reviewer", Vote: "approve"},
	})
	if result.Approvable || result.Vetoes != 1 {
		t.Fatalf("expected veto to block, got %+v", result)
	}
}

func TestLoadUsesMarkdownPolicyOverrides(t *testing.T) {
	root := t.TempDir()
	doc := strings.Join([]string{
		"# Roundtable Policies",
		"",
		"```yaml",
		"consensus:",
		"  default:",
		"    approvals_required: 1",
		"    blockers_allowed: 0",
		"  security:",
		"    security_veto_blocks: false",
		"    human_approval_required: false",
		"risk:",
		"  high_paths:",
		"    - \"docs/**\"",
		"commands:",
		"  dangerous:",
		"    - \"rm\"",
		"```",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, DefaultPolicyFile), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	engine, err := Load(root)
	if err != nil {
		t.Fatalf("load policy failed: %v", err)
	}
	result := engine.EvaluateProposal("normal", []string{"docs/guide.md"}, []db.Vote{
		{AgentID: "reviewer", Vote: "approve"},
	})
	if !result.Approvable {
		t.Fatalf("expected single approval to satisfy markdown override, got %+v", result)
	}
	if result.NeedsHuman {
		t.Fatalf("expected docs path override not to require human, got %+v", result)
	}
	if len(result.HighRiskPaths) != 1 || result.HighRiskPaths[0] != "docs/guide.md" {
		t.Fatalf("expected custom high-risk path from markdown, got %+v", result.HighRiskPaths)
	}
}
