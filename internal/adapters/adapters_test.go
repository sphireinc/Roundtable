package adapters

import (
	"context"
	"strings"
	"testing"

	"roundtable/internal/config"
)

func TestRegistryBuildsConfiguredAdapters(t *testing.T) {
	registry := NewRegistry(config.Default())
	names := registry.Names()
	for _, want := range []string{"claude", "codex", "gemini", "generic", "opencode"} {
		if !contains(names, want) {
			t.Fatalf("expected registry names to include %s, got %+v", want, names)
		}
	}
}

func TestCLIAdapterStartBuildsLaunchSession(t *testing.T) {
	adapter := NewCLIAdapter("codex", config.Default().Adapters["codex"])
	launch, err := adapter.Start(context.Background(), StartAgentRequest{
		SessionID:        "S-1",
		AgentID:          "implementer-1",
		RunID:            "RUN-1",
		TaskID:           "T-1",
		Role:             "Implementer",
		Prompt:           "Implement the task",
		WorkingDirectory: "/workspace",
		MCPSocket:        "/tmp/roundtable.sock",
		Model:            "gpt-5-codex",
	})
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if launch.Session.Adapter != "codex" || launch.Session.Status != "starting" {
		t.Fatalf("unexpected session: %+v", launch.Session)
	}
	if len(launch.Command) < 3 || launch.Command[0] != "codex" {
		t.Fatalf("unexpected command: %+v", launch.Command)
	}
	if !strings.Contains(launch.Session.MetadataJSON, `"mode":"start"`) || !strings.Contains(launch.Session.MetadataJSON, `"task_id":"T-1"`) {
		t.Fatalf("unexpected metadata: %s", launch.Session.MetadataJSON)
	}
}

func TestCLIAdapterResumeUsesConfiguredPattern(t *testing.T) {
	adapter := NewCLIAdapter("claude", config.Default().Adapters["claude"])
	launch, err := adapter.Resume(context.Background(), ResumeAgentRequest{
		SessionID:         "S-1",
		AgentID:           "architect-1",
		RunID:             "RUN-1",
		TaskID:            "T-1",
		Role:              "Architect",
		WorkingDirectory:  "/workspace",
		MCPSocket:         "/tmp/roundtable.sock",
		ExternalSessionID: "ext-123",
		ResumeBriefingMD:  "# Resume",
	})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	if got := strings.Join(launch.Command, " "); got != "claude --resume ext-123" {
		t.Fatalf("unexpected resume command: %s", got)
	}
	if launch.Session.ExternalResumeCommand != "claude --resume ext-123" || launch.Session.Status != "resuming" {
		t.Fatalf("unexpected resumed session: %+v", launch.Session)
	}
	if !strings.Contains(launch.Session.MetadataJSON, `"mode":"resume"`) {
		t.Fatalf("unexpected metadata: %s", launch.Session.MetadataJSON)
	}
}

func TestCLIAdapterResumeRejectsUnsupportedAdapter(t *testing.T) {
	adapter := NewCLIAdapter("generic", config.Default().Adapters["generic"])
	_, err := adapter.Resume(context.Background(), ResumeAgentRequest{
		SessionID:         "S-1",
		AgentID:           "tester-1",
		RunID:             "RUN-1",
		WorkingDirectory:  "/workspace",
		ExternalSessionID: "ext-123",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support resume") {
		t.Fatalf("expected unsupported resume error, got %v", err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
