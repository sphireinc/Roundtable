package symbols

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Symbol struct {
	ResourceID string
	Path       string
	Language   string
	Kind       string
	Name       string
	StartLine  int
	EndLine    int
}

type Indexer struct{}

type tsBlock struct {
	index      int
	closeDepth int
	kind       string
	name       string
}

type pyBlock struct {
	index  int
	indent int
	kind   string
	name   string
}

func NewIndexer() *Indexer {
	return &Indexer{}
}

func (i *Indexer) IndexPath(path string) ([]Symbol, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	return i.IndexContent(path, data)
}

func (i *Indexer) IndexContent(path string, data []byte) ([]Symbol, error) {
	switch detectLanguage(path) {
	case "go":
		return extractGoSymbols(path, data)
	case "typescript":
		return extractTypeScriptSymbols(path, data), nil
	case "python":
		return extractPythonSymbols(path, data), nil
	default:
		return nil, nil
	}
}

func ResourceID(path, name string) string {
	return "symbol:" + filepath.ToSlash(path) + "#" + name
}

func DetectLanguage(path string) string {
	return detectLanguage(path)
}

func detectLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	default:
		return ""
	}
}

func extractGoSymbols(path string, data []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	var out []Symbol
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			kind := "function"
			name := decl.Name.Name
			if decl.Recv != nil && len(decl.Recv.List) > 0 {
				kind = "method"
				recvName := receiverName(decl.Recv.List[0].Type)
				if recvName != "" {
					name = recvName + "." + name
				}
			}
			out = append(out, newSymbol(path, "go", kind, name, fset.Position(decl.Pos()).Line, fset.Position(decl.End()).Line))
		case *ast.GenDecl:
			switch decl.Tok {
			case token.TYPE:
				for _, spec := range decl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					kind := "type"
					switch typeSpec.Type.(type) {
					case *ast.InterfaceType:
						kind = "interface"
					case *ast.StructType:
						kind = "struct"
					}
					out = append(out, newSymbol(path, "go", kind, typeSpec.Name.Name, fset.Position(typeSpec.Pos()).Line, fset.Position(typeSpec.End()).Line))
				}
			case token.CONST:
				for _, spec := range decl.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range valueSpec.Names {
						if ast.IsExported(name.Name) {
							out = append(out, newSymbol(path, "go", "const", name.Name, fset.Position(name.Pos()).Line, fset.Position(name.End()).Line))
						}
					}
				}
			}
		}
	}
	sortSymbols(out)
	return out, nil
}

func newSymbol(path, language, kind, name string, startLine, endLine int) Symbol {
	return Symbol{
		ResourceID: ResourceID(path, name),
		Path:       filepath.ToSlash(path),
		Language:   language,
		Kind:       kind,
		Name:       name,
		StartLine:  startLine,
		EndLine:    endLine,
	}
}

func receiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return receiverName(expr.X)
	case *ast.IndexExpr:
		return receiverName(expr.X)
	case *ast.IndexListExpr:
		return receiverName(expr.X)
	default:
		return ""
	}
}

var (
	tsFuncRe         = regexp.MustCompile(`^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	tsClassRe        = regexp.MustCompile(`^\s*(?:export\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)`)
	tsInterfaceRe    = regexp.MustCompile(`^\s*(?:export\s+)?interface\s+([A-Za-z_][A-Za-z0-9_]*)`)
	tsConstRe        = regexp.MustCompile(`^\s*(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_]*)`)
	tsTypeRe         = regexp.MustCompile(`^\s*(?:export\s+)?type\s+([A-Za-z_][A-Za-z0-9_]*)\s*=`)
	tsEnumRe         = regexp.MustCompile(`^\s*(?:export\s+)?enum\s+([A-Za-z_][A-Za-z0-9_]*)`)
	tsMethodRe       = regexp.MustCompile(`^\s*(?:public\s+|private\s+|protected\s+|static\s+|readonly\s+|async\s+)*(?:get\s+|set\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	tsArrowConstRe   = regexp.MustCompile(`^\s*(?:export\s+)?const\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(?:async\s*)?\(`)
	pyFuncRe         = regexp.MustCompile(`^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pyClassRe        = regexp.MustCompile(`^\s*class\s+([A-Za-z_][A-Za-z0-9_]*)`)
	tsControlNames   = map[string]struct{}{"if": {}, "for": {}, "while": {}, "switch": {}, "catch": {}}
	tsContainerKinds = map[string]struct{}{"class": {}, "interface": {}}
)

func extractTypeScriptSymbols(path string, data []byte) []Symbol {
	lines := strings.Split(string(data), "\n")
	symbols := make([]Symbol, 0)
	var blocks []tsBlock
	braceDepth := 0
	for lineNo, line := range lines {
		currentDepth := braceDepth
		trimmed := strings.TrimSpace(line)
		containerName := nearestContainer(blocks)
		if matches := tsClassRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "class", matches[1], lineNo+1, lineNo+1))
			if strings.Contains(line, "{") {
				blocks = append(blocks, tsBlock{index: len(symbols) - 1, closeDepth: currentDepth, kind: "class", name: matches[1]})
			}
		} else if matches := tsInterfaceRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "interface", matches[1], lineNo+1, lineNo+1))
			if strings.Contains(line, "{") {
				blocks = append(blocks, tsBlock{index: len(symbols) - 1, closeDepth: currentDepth, kind: "interface", name: matches[1]})
			}
		} else if matches := tsEnumRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "enum", matches[1], lineNo+1, lineNo+1))
			if strings.Contains(line, "{") {
				blocks = append(blocks, tsBlock{index: len(symbols) - 1, closeDepth: currentDepth, kind: "enum", name: matches[1]})
			}
		} else if matches := tsFuncRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "function", matches[1], lineNo+1, lineNo+1))
			if strings.Contains(line, "{") {
				blocks = append(blocks, tsBlock{index: len(symbols) - 1, closeDepth: currentDepth, kind: "function", name: matches[1]})
			}
		} else if matches := tsArrowConstRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "function", matches[1], lineNo+1, lineNo+1))
			if strings.Contains(line, "{") {
				blocks = append(blocks, tsBlock{index: len(symbols) - 1, closeDepth: currentDepth, kind: "function", name: matches[1]})
			}
		} else if matches := tsTypeRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "type", matches[1], lineNo+1, lineNo+1))
		} else if matches := tsConstRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "typescript", "const", matches[1], lineNo+1, lineNo+1))
		} else if containerName != "" && currentDepth > 0 {
			if matches := tsMethodRe.FindStringSubmatch(line); len(matches) == 2 && strings.Contains(line, "{") {
				if _, blocked := tsControlNames[matches[1]]; !blocked && !strings.HasPrefix(trimmed, "function ") {
					name := containerName + "." + matches[1]
					symbols = append(symbols, newSymbol(path, "typescript", "method", name, lineNo+1, lineNo+1))
					blocks = append(blocks, tsBlock{index: len(symbols) - 1, closeDepth: currentDepth, kind: "method", name: name})
				}
			}
		}

		braceDepth += braceDelta(line)
		for len(blocks) > 0 && braceDepth <= blocks[len(blocks)-1].closeDepth {
			top := blocks[len(blocks)-1]
			if symbols[top.index].EndLine < lineNo+1 {
				symbols[top.index].EndLine = lineNo + 1
			}
			blocks = blocks[:len(blocks)-1]
		}
	}
	for i := range blocks {
		symbols[blocks[i].index].EndLine = len(lines)
	}
	sortSymbols(symbols)
	return symbols
}

func extractPythonSymbols(path string, data []byte) []Symbol {
	lines := strings.Split(string(data), "\n")
	symbols := make([]Symbol, 0)
	var blocks []pyBlock
	lastCodeLine := 1
	for lineNo, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := leadingIndent(line)
		for len(blocks) > 0 && indent <= blocks[len(blocks)-1].indent {
			top := blocks[len(blocks)-1]
			symbols[top.index].EndLine = lastCodeLine
			blocks = blocks[:len(blocks)-1]
		}
		if matches := pyClassRe.FindStringSubmatch(line); len(matches) == 2 {
			symbols = append(symbols, newSymbol(path, "python", "class", matches[1], lineNo+1, lineNo+1))
			blocks = append(blocks, pyBlock{index: len(symbols) - 1, indent: indent, kind: "class", name: matches[1]})
		} else if matches := pyFuncRe.FindStringSubmatch(line); len(matches) == 2 {
			name := matches[1]
			if container := nearestPythonClass(blocks); container != "" {
				name = container + "." + name
			}
			symbols = append(symbols, newSymbol(path, "python", "function", name, lineNo+1, lineNo+1))
			blocks = append(blocks, pyBlock{index: len(symbols) - 1, indent: indent, kind: "function", name: name})
		}
		lastCodeLine = lineNo + 1
	}
	for len(blocks) > 0 {
		top := blocks[len(blocks)-1]
		symbols[top.index].EndLine = lastCodeLine
		blocks = blocks[:len(blocks)-1]
	}
	sortSymbols(symbols)
	return symbols
}

func sortSymbols(symbols []Symbol) {
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Path != symbols[j].Path {
			return symbols[i].Path < symbols[j].Path
		}
		if symbols[i].StartLine != symbols[j].StartLine {
			return symbols[i].StartLine < symbols[j].StartLine
		}
		return symbols[i].Name < symbols[j].Name
	})
}

func braceDelta(line string) int {
	delta := 0
	inSingle := false
	inDouble := false
	inBacktick := false
	escaped := false
	for _, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && (inSingle || inDouble || inBacktick) {
			escaped = true
			continue
		}
		switch r {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '{':
			if !inSingle && !inDouble && !inBacktick {
				delta++
			}
		case '}':
			if !inSingle && !inDouble && !inBacktick {
				delta--
			}
		}
	}
	return delta
}

func nearestContainer(blocks []tsBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if _, ok := tsContainerKinds[blocks[i].kind]; ok {
			return blocks[i].name
		}
	}
	return ""
}

func nearestPythonClass(blocks []pyBlock) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].kind == "class" {
			return blocks[i].name
		}
	}
	return ""
}

func leadingIndent(line string) int {
	width := 0
	for _, r := range line {
		switch r {
		case ' ':
			width++
		case '\t':
			width += 4
		default:
			return width
		}
	}
	return width
}
