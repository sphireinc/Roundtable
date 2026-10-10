package symbols

import (
	"strings"
	"sync"
	"testing"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	goGrammar "github.com/tree-sitter/tree-sitter-go/bindings/go"
	pythonGrammar "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescriptGrammar "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

func TestOfficialGrammarsLoadAndParse(t *testing.T) {
	grammars := []struct {
		name string
		lang *tree_sitter.Language
		src  []byte
	}{
		{"go", tree_sitter.NewLanguage(goGrammar.Language()), []byte("package p\nfunc f() {}\n")},
		{"typescript", tree_sitter.NewLanguage(typescriptGrammar.LanguageTypescript()), []byte("function f() {}\n")},
		{"tsx", tree_sitter.NewLanguage(typescriptGrammar.LanguageTSX()), []byte("const App = () => <div />\n")},
		{"python", tree_sitter.NewLanguage(pythonGrammar.Language()), []byte("def f():\n    pass\n")},
	}
	for _, tc := range grammars {
		t.Run(tc.name, func(t *testing.T) {
			parser := tree_sitter.NewParser()
			defer parser.Close()
			if err := parser.SetLanguage(tc.lang); err != nil {
				t.Fatalf("SetLanguage: %v", err)
			}
			tree := parser.Parse(tc.src, nil)
			if tree == nil {
				t.Fatal("Parse returned nil tree")
			}
			defer tree.Close()
			if tree.RootNode().HasError() {
				t.Fatalf("unexpected parse error: %s", tree.RootNode().ToSexp())
			}
		})
	}
}

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

func TestMalformedSourceReturnsNoPartialSymbols(t *testing.T) {
	for _, tc := range []struct{ path, source string }{
		{"broken.go", "package p\nfunc good() {}\nfunc {"},
		{"broken.ts", "function good() {}\nclass Broken {"},
		{"broken.tsx", "const Good = () => <div />\nconst Broken = <div>"},
		{"broken.py", "def good():\n    pass\nclass Broken(\n"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got, err := NewIndexer().IndexContent(tc.path, []byte(tc.source))
			if err == nil || len(got) != 0 {
				t.Fatalf("expected syntax error and no symbols, got symbols=%+v err=%v", got, err)
			}
		})
	}
}

func TestTreeSitterDeclarationsAndExactSpans(t *testing.T) {
	goSource := []byte("package p\n\ntype Box[T any] struct {\n Value T\n}\n\nconst (\n Exported, ExportedAlso = 1, 2\n hidden = 2\n)\n\nfunc (b *Box[T]) Get() T {\n return b.Value\n}\n")
	goSymbols, err := NewIndexer().IndexContent("x.go", goSource)
	if err != nil {
		t.Fatal(err)
	}
	assertSymbolSpan(t, goSymbols, "Box", 3, 5)
	assertSymbolSpan(t, goSymbols, "Exported", 8, 8)
	assertSymbolSpan(t, goSymbols, "ExportedAlso", 8, 8)
	assertSymbolSpan(t, goSymbols, "Box.Get", 12, 14)
	for _, s := range goSymbols {
		if s.Name == "hidden" {
			t.Fatal("unexported const indexed")
		}
	}

	tsSource := []byte("const text = `function fake() {}`; // class Fake {}\n@sealed\nexport class Box<T> {\n  method(\n    value: T\n  ): T {\n    function local() {}\n    class Inner {}\n    return value\n  }\n}\ninterface Runner {\n  run(value: string): void\n}\ntype Id<T> = T\nenum State { Ready }\nconst make = (x: number) => x\nconst wrapped = ((x: number) => x)\nconst wrappedFunction = (function () { return 1 })\nconst count = 1\nconst View = () => <div />\n")
	tsSymbols, err := NewIndexer().IndexContent("x.tsx", tsSource)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Box": "class", "Box.method": "method", "local": "function", "Inner": "class", "Runner": "interface", "Runner.run": "method", "Id": "type", "State": "enum", "make": "function", "wrapped": "function", "wrappedFunction": "function", "count": "const", "View": "function"}
	for _, sym := range tsSymbols {
		if kind, ok := want[sym.Name]; ok {
			if sym.Kind != kind {
				t.Errorf("%s kind=%s want %s", sym.Name, sym.Kind, kind)
			}
			delete(want, sym.Name)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing symbols: %v; got %+v", want, tsSymbols)
	}
	assertSymbolSpan(t, tsSymbols, "Box", 3, 11)
	assertSymbolSpan(t, tsSymbols, "Box.method", 4, 10)
	if ResourceID("x.tsx", "Box.method") != "symbol:x.tsx#Box.method" {
		t.Fatal("resource identity changed")
	}

	pySource := []byte("@decorator\nclass Box:\n    @property\n    async def get(\n        self,\n    ):\n        def local():\n            return 1\n        return local()\n\ndef run():\n    return 1\n    class LocalClass:\n        def work(self):\n            return 2\n\n# def fake_comment():\n\"\"\"def fake_string():\n    pass\"\"\"\n")
	pySymbols, err := NewIndexer().IndexContent("x.py", pySource)
	if err != nil {
		t.Fatal(err)
	}
	assertSymbolSpan(t, pySymbols, "Box", 2, 9)
	assertSymbolSpan(t, pySymbols, "Box.get", 4, 9)
	assertSymbolSpan(t, pySymbols, "Box.local", 7, 8)
	assertSymbolSpan(t, pySymbols, "run", 11, 15)
	assertSymbolSpan(t, pySymbols, "LocalClass", 13, 15)
	assertSymbolSpan(t, pySymbols, "LocalClass.work", 14, 15)
	for _, symbol := range pySymbols {
		if strings.Contains(symbol.Name, "fake") {
			t.Fatalf("indexed declaration-like text from comment/string: %+v", symbol)
		}
	}
}

func TestRepeatedConcurrentIndexing(t *testing.T) {
	indexer := NewIndexer()
	inputs := []struct{ path, source string }{
		{"race.go", "package p\nfunc F() {}\n"},
		{"race.ts", "export const f = (x: number) => x\n"},
		{"race.tsx", "const App = () => <main />\n"},
		{"race.py", "async def f():\n    return 1\n"},
	}
	var wg sync.WaitGroup
	for _, input := range inputs {
		for worker := 0; worker < 4; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 20; i++ {
					got, err := indexer.IndexContent(input.path, []byte(input.source))
					if err != nil || len(got) == 0 {
						t.Errorf("IndexContent(%s) got %d symbols, err=%v", input.path, len(got), err)
						return
					}
				}
			}()
		}
	}
	wg.Wait()
}

func TestUnsupportedExtensionRemainsEmpty(t *testing.T) {
	got, err := NewIndexer().IndexContent("source.js", []byte("function f() {}"))
	if err != nil || len(got) != 0 || DetectLanguage("source.js") != "" {
		t.Fatalf("unsupported extension changed behavior: symbols=%+v err=%v language=%q", got, err, DetectLanguage("source.js"))
	}
}

func TestStableIdentityLanguageAndSameLineSortOrder(t *testing.T) {
	got, err := NewIndexer().IndexContent("SAMPLE.TS", []byte("const zed = 1, alpha = 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if DetectLanguage("SAMPLE.TS") != "typescript" || len(got) != 2 {
		t.Fatalf("extension detection or extraction changed: %+v", got)
	}
	if got[0].Name != "alpha" || got[1].Name != "zed" {
		t.Fatalf("same-line symbols are not sorted by name: %+v", got)
	}
	for _, symbol := range got {
		if symbol.ResourceID != "symbol:SAMPLE.TS#"+symbol.Name || symbol.Path != "SAMPLE.TS" {
			t.Fatalf("symbol identity/path changed: %+v", symbol)
		}
	}
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
