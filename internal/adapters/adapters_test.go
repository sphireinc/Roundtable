package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"roundtable/internal/config"
	"roundtable/internal/sandbox"
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

func TestCLIAdapterExecutesOnlyInConfiguredContainerImage(t *testing.T) {
	runtime := &fakeRuntime{}
	request := StartAgentRequest{SessionID: "S-1", AgentID: "A-1", RunID: "R-1", TaskID: "T-1", Role: "implementer", Prompt: "task text", WorkingDirectory: "/host/work"}
	adapter := NewCLIAdapter("codex", config.Default().Adapters["codex"])
	if _, _, err := adapter.Execute(context.Background(), runtime, request, sandbox.Spec{}); err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("execution without a configured image was not rejected: %v", err)
	}
	if runtime.calls != 0 {
		t.Fatal("runtime was invoked without a configured trusted image")
	}

	adapter = NewCLIAdapter("codex", config.AdapterConfig{Image: "registry.example/roundtable-codex@sha256:0123456789abcdef", Command: "codex", SupportsMCP: true})
	request.AgentCapabilityToken = strings.Repeat("x", 48)
	request.AgentMCPURL = "https://roundtable.example/mcp/agent"
	request.BaseRevision = "rev-1"
	request.BaseStateHash = "tree-1"
	result, execution, err := adapter.Execute(context.Background(), runtime, request, sandbox.Spec{Source: "/source", Workspace: "/workspace", Temp: t.TempDir(), ExecutionRoot: "/exec", AgentEgressSocketPath: "/tmp/roundtable-egress-test/gateway.sock"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.ID != "S-1" || execution.ExitCode != 0 || runtime.spec.Image != adapter.cfg.Image {
		t.Fatalf("unexpected container execution result: session=%+v result=%+v spec=%+v", result.Session, execution, runtime.spec)
	}
	if got, want := runtime.spec.Command[:6], []string{"roundtable", "agent", "exec", "--egress-socket", "/run/roundtable-egress/g", "--"}; !slices.Equal(got, want) || !slices.Contains(runtime.spec.Command, "task text") {
		t.Fatalf("adapter command was not passed as argv: %+v", runtime.spec.Command)
	}
	if runtime.spec.Name == "" || runtime.spec.Timeout <= 0 {
		t.Fatalf("container identity/timeout were not assigned: %+v", runtime.spec)
	}
}

func TestCLIAdapterRedactsProviderCredentialsFromCapturedOutput(t *testing.T) {
	const secret = "provider-secret-value"
	t.Setenv("OPENAI_API_KEY", secret)
	runtime := &fakeRuntime{result: sandbox.ExecutionResult{
		ExitCode: 1, Stdout: "stdout: " + secret, Stderr: "stderr: " + secret,
		StartedAt: time.Now(), EndedAt: time.Now(),
	}, runErr: errors.New("runtime reported " + secret)}
	adapter := NewCLIAdapter("codex", config.AdapterConfig{
		Image: "agent:test", Command: "codex", SupportsMCP: true,
		CredentialEnv: []string{"OPENAI_API_KEY"},
	})
	request := StartAgentRequest{
		SessionID: "S-redact", AgentID: "A-redact", RunID: "R-redact", TaskID: "T-redact",
		Role: "implementer", Prompt: "task", WorkingDirectory: "/workspace",
		AgentCapabilityToken: strings.Repeat("c", 43), AgentMCPURL: "http://127.0.0.1:7117/agent/mcp",
		BaseRevision: "rev", BaseStateHash: "tree",
	}
	_, result, err := adapter.Execute(context.Background(), runtime, request, sandbox.Spec{
		Temp: t.TempDir(), Timeout: time.Minute, AgentEgressSocketPath: "/run/roundtable-egress/g",
	})
	if !strings.Contains(err.Error(), "[REDACTED_PROVIDER_CREDENTIAL]") || strings.Contains(err.Error(), secret) {
		t.Fatalf("runtime error retained provider credential: %v", err)
	}
	for label, output := range map[string]string{"stdout": result.Stdout, "stderr": result.Stderr} {
		if strings.Contains(output, secret) || !strings.Contains(output, "[REDACTED_PROVIDER_CREDENTIAL]") {
			t.Errorf("%s was not safely redacted: %q", label, output)
		}
	}
}

func TestCLIAdapterInjectsIsolatedSessionMCPConfiguration(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "gemini", "opencode"} {
		t.Run(provider, func(t *testing.T) {
			temp := t.TempDir()
			runtime := &fakeRuntime{}
			adapter := NewCLIAdapter(provider, config.AdapterConfig{Image: "agent:test", Command: provider, SupportsMCP: true})
			request := StartAgentRequest{
				SessionID: "S-config", AgentID: "A-config", RunID: "R-config", TaskID: "T-config", Role: "implementer",
				Prompt: "task prompt", WorkingDirectory: "/workspace", Provider: provider,
				AgentCapabilityToken: strings.Repeat("c", 43), AgentMCPURL: "http://127.0.0.1:7117/agent/mcp",
				BaseRevision: "revision", BaseStateHash: "tree-hash",
			}
			_, _, err := adapter.Execute(context.Background(), runtime, request, sandbox.Spec{Temp: temp, Timeout: time.Minute, AgentEgressSocketPath: "/tmp/roundtable-egress-test/gateway.sock"})
			if err != nil {
				t.Fatal(err)
			}
			var configPath string
			switch provider {
			case "codex":
				if got := runtime.spec.Env["CODEX_HOME"]; got != "/tmp/codex-home" {
					t.Fatalf("Codex home %q does not match the mounted configuration directory /tmp/codex-home", got)
				}
				configPath = filepath.Join(temp, "codex-home", "config.toml")
			case "claude":
				if index := slices.Index(runtime.spec.Command, "--mcp-config"); index >= 0 && index+1 < len(runtime.spec.Command) {
					configPath = filepath.Join(temp, filepath.Base(runtime.spec.Command[index+1]))
				}
			case "gemini":
				configPath = filepath.Join(temp, "roundtable-gemini-settings.json")
			case "opencode":
				configPath = filepath.Join(temp, "roundtable-opencode.json")
			}
			if configPath == "" {
				t.Fatalf("provider did not receive isolated MCP config: %+v", runtime.spec.Env)
			}
			configData, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read provider MCP config: %v", err)
			}
			if !strings.Contains(string(configData), "roundtable") || !strings.Contains(string(configData), "mcp") || !strings.Contains(string(configData), "stdio") || !strings.Contains(string(configData), "/run/roundtable-agent-mcp.sock") {
				t.Fatalf("provider config did not install the scoped Roundtable stdio bridge: %s", configData)
			}
			if strings.Contains(string(configData), request.AgentCapabilityToken) {
				t.Fatal("session capability leaked into the provider configuration file")
			}
			if provider == "opencode" {
				var decoded map[string]any
				if err := json.Unmarshal(configData, &decoded); err != nil {
					t.Fatal(err)
				}
				mcpConfig := decoded["mcp"].(map[string]any)
				server, ok := mcpConfig["roundtable"].(map[string]any)
				if !ok || server["type"] != "local" {
					t.Fatalf("OpenCode MCP server is not configured at the native mcp.<name> path: %#v", decoded)
				}
				command, ok := server["command"].([]any)
				if !ok || !slices.Equal(command, []any{"roundtable", "mcp", "stdio", "--unix-socket", "/run/roundtable-agent-mcp.sock"}) {
					t.Fatalf("OpenCode MCP command is not an argv array: %#v", server["command"])
				}
			}
		})
	}
}

func TestCLIAdapterRequiresSessionScopedProviderEgressSocket(t *testing.T) {
	runtime := &fakeRuntime{}
	adapter := NewCLIAdapter("codex", config.AdapterConfig{Image: "agent:test", Command: "codex", SupportsMCP: true})
	request := StartAgentRequest{SessionID: "S-egress", AgentID: "A-egress", RunID: "R-egress", TaskID: "T-egress", Role: "implementer", WorkingDirectory: "/workspace", AgentCapabilityToken: strings.Repeat("c", 48), AgentMCPURL: "http://127.0.0.1:7117/agent/mcp", BaseRevision: "rev", BaseStateHash: "tree"}
	_, _, err := adapter.Execute(context.Background(), runtime, request, sandbox.Spec{Temp: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "egress socket") || runtime.calls != 0 {
		t.Fatalf("adapter did not fail closed without an assigned egress socket: err=%v calls=%d", err, runtime.calls)
	}
}

func TestCLIAdapterInjectsOnlyExplicitProviderCredentialEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "scoped-provider-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "unrelated-cloud-secret")
	runtime := &fakeRuntime{}
	adapter := NewCLIAdapter("codex", config.AdapterConfig{Image: "agent:test", Command: "codex", SupportsMCP: true, CredentialEnv: []string{"OPENAI_API_KEY"}})
	request := StartAgentRequest{SessionID: "S-creds", AgentID: "A-creds", RunID: "R-creds", TaskID: "T-creds", Role: "implementer", WorkingDirectory: "/workspace", AgentCapabilityToken: strings.Repeat("c", 48), AgentMCPURL: "http://127.0.0.1:7117/agent/mcp", BaseRevision: "rev", BaseStateHash: "tree"}
	_, _, err := adapter.Execute(context.Background(), runtime, request, sandbox.Spec{Temp: t.TempDir(), AgentEgressSocketPath: "/tmp/roundtable-egress-test/gateway.sock"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.spec.Env["OPENAI_API_KEY"] != "scoped-provider-key" {
		t.Fatal("configured provider credential was not explicitly injected")
	}
	if _, ok := runtime.spec.Env["AWS_SECRET_ACCESS_KEY"]; ok {
		t.Fatal("unrelated cloud credential leaked into agent container environment")
	}
	if strings.Contains(strings.Join(runtime.spec.Command, " "), "scoped-provider-key") {
		t.Fatal("provider credential was included in command arguments")
	}
}

type fakeRuntime struct {
	calls  int
	spec   sandbox.Spec
	result sandbox.ExecutionResult
	runErr error
}

func (f *fakeRuntime) Name() string { return "fake" }
func (f *fakeRuntime) Run(_ context.Context, spec sandbox.Spec) (sandbox.ExecutionResult, error) {
	f.calls++
	f.spec = spec
	result := f.result
	if result.StartedAt.IsZero() {
		result.StartedAt = time.Now()
	}
	if result.EndedAt.IsZero() {
		result.EndedAt = time.Now()
	}
	if f.result == (sandbox.ExecutionResult{}) {
		result.ExitCode = 0
	}
	return result, f.runErr
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
	if got, want := launch.Command, []string{"codex", "--ask-for-approval", "never", "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "workspace-write", "--model", "gpt-5-codex", "Implement the task"}; !slices.Equal(got, want) {
		t.Fatalf("unexpected command: %+v", launch.Command)
	}
	if !strings.Contains(launch.Session.MetadataJSON, `"mode":"start"`) || !strings.Contains(launch.Session.MetadataJSON, `"task_id":"T-1"`) {
		t.Fatalf("unexpected metadata: %s", launch.Session.MetadataJSON)
	}
}

func TestCLIAdapterBuildsProviderSpecificNonInteractiveCommands(t *testing.T) {
	prompt := "inspect ; $(touch /host/pwned) --model hostile"
	tests := []struct {
		provider string
		model    string
		want     []string
	}{
		{"codex", "o4-codex", []string{"codex", "--ask-for-approval", "never", "exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "workspace-write", "--model", "o4-codex", prompt}},
		{"claude", "sonnet", []string{"claude", "-p", "--model", "sonnet", prompt}},
		{"gemini", "gemini-2.5-pro", []string{"gemini", "--model", "gemini-2.5-pro", "-p", prompt}},
		{"opencode", "anthropic/sonnet", []string{"opencode", "run", "--model", "anthropic/sonnet", prompt}},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			adapter := NewCLIAdapter(tt.provider, config.AdapterConfig{Command: tt.provider})
			launch, err := adapter.Start(context.Background(), StartAgentRequest{
				SessionID: "S-argv", AgentID: "A-argv", RunID: "R-argv", TaskID: "T-argv", WorkingDirectory: "/workspace",
				Model: tt.model, Prompt: prompt,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(launch.Command, tt.want) {
				t.Fatalf("argv mismatch:\n got: %#v\nwant: %#v", launch.Command, tt.want)
			}
		})
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
