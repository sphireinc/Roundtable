package symbols

import (
	"strings"
	"testing"
)

func TestGoSymbols(t *testing.T) {
	indexer := NewIndexer()
	content := []byte(`package sample

type Service struct{}
type Runner interface { Run() }

const Version = "1"

func Build() {}

func (s *Service) Execute() {}
`)

	got, err := indexer.IndexContent("sample.go", content)
	if err != nil {
		t.Fatalf("IndexContent failed: %v", err)
	}

	names := joinNames(got)
	for _, want := range []string{"Service", "Runner", "Version", "Build", "Service.Execute"} {
		if !strings.Contains(names, want) {
			t.Fatalf("expected %q in %q", want, names)
		}
	}
}

func TestTypeScriptSymbols(t *testing.T) {
	indexer := NewIndexer()
	content := []byte(`export function buildThing() {
  return 1
}

export class Widget {
  async render() {
    return buildThing()
  }
}

export interface Runner {
  run(): void
}

export const DEFAULT_LIMIT = 10
export const makeThing = () => {
  return buildThing()
}
`)

	got, err := indexer.IndexContent("sample.ts", content)
	if err != nil {
		t.Fatalf("IndexContent failed: %v", err)
	}
	names := joinNames(got)
	for _, want := range []string{"buildThing", "Widget", "Widget.render", "Runner", "DEFAULT_LIMIT", "makeThing"} {
		if !strings.Contains(names, want) {
			t.Fatalf("expected %q in %q", want, names)
		}
	}
	assertSymbolSpan(t, got, "buildThing", 1, 3)
	assertSymbolSpan(t, got, "Widget", 5, 9)
	assertSymbolSpan(t, got, "Widget.render", 6, 8)
}

func TestPythonSymbols(t *testing.T) {
	indexer := NewIndexer()
	content := []byte(`class MemoryOracle:
    def query_memory(self):
        return "ok"

def run():
    return MemoryOracle()
`)

	got, err := indexer.IndexContent("oracle.py", content)
	if err != nil {
		t.Fatalf("IndexContent failed: %v", err)
	}
	names := joinNames(got)
	for _, want := range []string{"MemoryOracle", "MemoryOracle.query_memory", "run"} {
		if !strings.Contains(names, want) {
			t.Fatalf("expected %q in %q", want, names)
		}
	}
	assertSymbolSpan(t, got, "MemoryOracle", 1, 3)
	assertSymbolSpan(t, got, "MemoryOracle.query_memory", 2, 3)
	assertSymbolSpan(t, got, "run", 5, 6)
}

func joinNames(symbols []Symbol) string {
	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.Name)
	}
	return strings.Join(names, ",")
}

func assertSymbolSpan(t *testing.T, symbols []Symbol, name string, start, end int) {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.Name != name {
			continue
		}
		if symbol.StartLine != start || symbol.EndLine != end {
			t.Fatalf("unexpected span for %s: %+v", name, symbol)
		}
		return
	}
	t.Fatalf("missing symbol %s in %+v", name, symbols)
}
