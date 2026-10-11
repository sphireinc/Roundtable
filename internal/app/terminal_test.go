package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTokenizeTerminalCommandPreservesQuotedArguments(t *testing.T) {
	got, err := tokenizeTerminalCommand(`mcp call --tool human.request_approval --reason "needs operator review"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mcp", "call", "--tool", "human.request_approval", "--reason", "needs operator review"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenized command = %#v, want %#v", got, want)
	}
}

func TestExecuteTerminalCommandUsesActiveProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := Run(context.Background(), []string{"init", "--root", root}, &strings.Builder{}, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	output, err := executeTerminalCommand(context.Background(), root, "table")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "RUNS 0") {
		t.Fatalf("table command did not run against the active initialized root: %s", output)
	}
	if _, err := os.Stat(filepath.Join(root, ".roundtable/roundtable.db")); err != nil {
		t.Fatalf("active project database missing: %v", err)
	}
}

func TestValidateTerminalCommandRestrictsEntrypointsAndRoot(t *testing.T) {
	for _, args := range [][]string{
		{"start"},
		{"run"},
		{"agent", "exec", "--", "sh"},
		{"mcp", "serve"},
		{"table", "--root", "/tmp"},
		{"claims", "list", "--root=/tmp"},
	} {
		if err := validateTerminalCommand(args); err == nil {
			t.Errorf("accepted disallowed terminal command: %#v", args)
		}
	}
	for _, args := range [][]string{{"table"}, {"watch"}, {"claims", "list"}, {"claims", "revoke"}, {"sessions"}, {"mcp", "call", "--tool", "patch.apply"}} {
		if err := validateTerminalCommand(args); err != nil {
			t.Errorf("rejected allowed Roundtable command %#v: %v", args, err)
		}
	}
}

func TestPinTerminalRootBeforeUserArguments(t *testing.T) {
	root := "/trusted/project"
	for _, test := range []struct {
		args []string
		want []string
	}{
		{[]string{"table", "unexpected"}, []string{"table", "--root", root, "unexpected"}},
		{[]string{"claims", "list", "unexpected"}, []string{"claims", "list", "--root", root, "unexpected"}},
		{[]string{"mcp", "call", "--tool", "task.create", "unexpected"}, []string{"mcp", "call", "--root", root, "--tool", "task.create", "unexpected"}},
		{[]string{"sessions", "end", "--session", "S-1"}, []string{"sessions", "end", "--root", root, "--session", "S-1"}},
	} {
		if got := pinTerminalRoot(test.args, root); !reflect.DeepEqual(got, test.want) {
			t.Errorf("pinTerminalRoot(%#v) = %#v, want %#v", test.args, got, test.want)
		}
	}
}

func TestTerminalOutputBufferCapsCapturedOutput(t *testing.T) {
	var output terminalOutputBuffer
	chunk := strings.Repeat("x", terminalOutputLimit)
	if n, err := output.Write([]byte(chunk)); err != nil || n != len(chunk) {
		t.Fatalf("first write = (%d, %v)", n, err)
	}
	if n, err := output.Write([]byte("more output")); err != nil || n != len("more output") {
		t.Fatalf("overflow write = (%d, %v)", n, err)
	}
	if len(output.data) > terminalOutputLimit {
		t.Fatalf("captured %d bytes, limit is %d", len(output.data), terminalOutputLimit)
	}
	if !strings.Contains(output.String(), "... terminal output truncated ...") {
		t.Fatal("overflow output did not include a truncation notice")
	}
}

func TestTokenizeTerminalCommandRejectsMalformedOrOversizedInput(t *testing.T) {
	for _, input := range []string{`mcp call --tool "unterminated`, strings.Repeat("x", 4097)} {
		if _, err := tokenizeTerminalCommand(input); err == nil {
			t.Errorf("accepted malformed terminal input %q", input)
		}
	}
	args, err := tokenizeTerminalCommand(`table; touch /tmp/pwned`)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTerminalCommand(args); err == nil {
		t.Fatal("accepted shell-like command chaining")
	}
}
