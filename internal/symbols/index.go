package symbols

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	goGrammar "github.com/tree-sitter/tree-sitter-go/bindings/go"
	pythonGrammar "github.com/tree-sitter/tree-sitter-python/bindings/go"
	typescriptGrammar "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
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

type grammarSpec struct {
	language *tree_sitter.Language
	label    string
}

func NewIndexer() *Indexer { return &Indexer{} }

func (i *Indexer) IndexPath(path string) ([]Symbol, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	return i.IndexContent(path, data)
}

func (i *Indexer) IndexContent(path string, data []byte) ([]Symbol, error) {
	var spec grammarSpec
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		spec = grammarSpec{tree_sitter.NewLanguage(goGrammar.Language()), "go"}
	case ".ts":
		spec = grammarSpec{tree_sitter.NewLanguage(typescriptGrammar.LanguageTypescript()), "typescript"}
	case ".tsx":
		spec = grammarSpec{tree_sitter.NewLanguage(typescriptGrammar.LanguageTSX()), "typescript"}
	case ".py":
		spec = grammarSpec{tree_sitter.NewLanguage(pythonGrammar.Language()), "python"}
	default:
		return nil, nil
	}

	parser := tree_sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(spec.language); err != nil {
		return nil, fmt.Errorf("configure %s parser for %s: %w", spec.label, path, err)
	}
	tree := parser.Parse(data, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse %s file %s: parser returned no syntax tree", spec.label, path)
	}
	defer tree.Close()
	root := tree.RootNode()
	if hasSyntaxError(root) {
		return nil, fmt.Errorf("parse %s file %s: syntax tree contains ERROR or MISSING nodes", spec.label, path)
	}

	var out []Symbol
	switch spec.label {
	case "go":
		collectGo(path, data, root, &out)
	case "typescript":
		collectTypeScript(path, data, root, "", false, &out)
	case "python":
		collectPython(path, data, root, "", &out)
	}
	sortSymbols(out)
	return out, nil
}

func ResourceID(path, name string) string { return "symbol:" + filepath.ToSlash(path) + "#" + name }

func DetectLanguage(path string) string { return detectLanguage(path) }

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

func newSymbol(path, language, kind, name string, startLine, endLine int) Symbol {
	return Symbol{ResourceID: ResourceID(path, name), Path: filepath.ToSlash(path), Language: language, Kind: kind, Name: name, StartLine: startLine, EndLine: endLine}
}

func hasSyntaxError(node *tree_sitter.Node) bool {
	stack := []*tree_sitter.Node{node}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.IsError() || current.IsMissing() {
			return true
		}
		for i := uint(0); i < current.ChildCount(); i++ {
			stack = append(stack, current.Child(i))
		}
	}
	return false
}

func collectGo(path string, source []byte, node *tree_sitter.Node, out *[]Symbol) {
	switch node.Kind() {
	case "function_declaration":
		appendNodeSymbol(path, source, "go", "function", node.ChildByFieldName("name"), node, out)
	case "method_declaration":
		nameNode := node.ChildByFieldName("name")
		recv := node.ChildByFieldName("receiver")
		recvName := receiverTypeName(recv, source)
		name := nodeText(nameNode, source)
		if recvName != "" {
			name = recvName + "." + name
		}
		appendNodeSymbolNamed(path, "go", "method", name, node, out)
	case "type_spec":
		nameNode := node.ChildByFieldName("name")
		typeNode := node.ChildByFieldName("type")
		kind := "type"
		if typeNode != nil {
			switch typeNode.Kind() {
			case "struct_type":
				kind = "struct"
			case "interface_type":
				kind = "interface"
			}
		}
		appendNodeSymbol(path, source, "go", kind, nameNode, node, out)
	case "const_spec":
		for _, name := range goNames(node) {
			if isExported(nodeText(&name, source)) {
				appendNodeSymbol(path, source, "go", "const", &name, &name, out)
			}
		}
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		collectGo(path, source, node.NamedChild(i), out)
	}
}

func goNames(node *tree_sitter.Node) []tree_sitter.Node {
	cursor := node.Walk()
	defer cursor.Close()
	return node.ChildrenByFieldName("name", cursor)
}

func collectTypeScript(path string, source []byte, node *tree_sitter.Node, container string, directMember bool, out *[]Symbol) {
	kind, nameNode := "", (*tree_sitter.Node)(nil)
	switch node.Kind() {
	case "function_declaration", "generator_function_declaration":
		kind, nameNode = "function", node.ChildByFieldName("name")
	case "class_declaration", "abstract_class_declaration":
		kind, nameNode = "class", node.ChildByFieldName("name")
	case "interface_declaration":
		kind, nameNode = "interface", node.ChildByFieldName("name")
	case "type_alias_declaration":
		kind, nameNode = "type", node.ChildByFieldName("name")
	case "enum_declaration":
		kind, nameNode = "enum", node.ChildByFieldName("name")
	case "method_definition", "method_signature", "abstract_method_signature":
		if directMember {
			kind, nameNode = "method", node.ChildByFieldName("name")
		}
	case "lexical_declaration":
		if declarationIsConst(node, source) {
			for i := uint(0); i < node.NamedChildCount(); i++ {
				decl := node.NamedChild(i)
				if decl.Kind() != "variable_declarator" {
					continue
				}
				name := decl.ChildByFieldName("name")
				declKind := "const"
				value := unwrapTSExpression(decl.ChildByFieldName("value"))
				if value != nil && (value.Kind() == "arrow_function" || value.Kind() == "function_expression" || value.Kind() == "generator_function") {
					declKind = "function"
				}
				appendNodeSymbol(path, source, "typescript", declKind, name, decl, out)
			}
		}
	}
	name := nodeText(nameNode, source)
	if kind != "" && name != "" {
		if kind == "method" && container != "" {
			name = container + "." + name
		}
		appendNodeSymbolNamed(path, "typescript", kind, name, node, out)
	}

	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		childContainer, member := "", false
		if node.Kind() == "class_body" || node.Kind() == "interface_body" {
			childContainer, member = container, true
		}
		if node.Kind() == "class_declaration" || node.Kind() == "abstract_class_declaration" || node.Kind() == "interface_declaration" {
			childContainer = nodeText(node.ChildByFieldName("name"), source)
		}
		collectTypeScript(path, source, child, childContainer, member, out)
	}
}

func unwrapTSExpression(node *tree_sitter.Node) *tree_sitter.Node {
	for node != nil {
		switch node.Kind() {
		case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "type_assertion":
			inner := node.ChildByFieldName("expression")
			if inner == nil && node.NamedChildCount() > 0 {
				inner = node.NamedChild(0)
			}
			if inner == nil {
				return node
			}
			node = inner
		default:
			return node
		}
	}
	return nil
}

func declarationIsConst(node *tree_sitter.Node, source []byte) bool {
	text := strings.TrimSpace(nodeText(node, source))
	return strings.HasPrefix(text, "const ") || strings.HasPrefix(text, "export const ") || strings.HasPrefix(text, "declare const ") || strings.HasPrefix(text, "export declare const ")
}

func collectPython(path string, source []byte, node *tree_sitter.Node, className string, out *[]Symbol) {
	nextClass := className
	if node.Kind() == "class_definition" {
		nameNode := node.ChildByFieldName("name")
		nextClass = nodeText(nameNode, source)
		appendNodeSymbol(path, source, "python", "class", nameNode, node, out)
	} else if node.Kind() == "function_definition" {
		name := nodeText(node.ChildByFieldName("name"), source)
		if className != "" {
			name = className + "." + name
		}
		appendNodeSymbolNamed(path, "python", "function", name, node, out)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		collectPython(path, source, node.NamedChild(i), nextClass, out)
	}
}

func appendNodeSymbol(path string, source []byte, language, kind string, nameNode, rangeNode *tree_sitter.Node, out *[]Symbol) {
	if nameNode == nil {
		return
	}
	appendNodeSymbolNamed(path, language, kind, nodeText(nameNode, source), rangeNode, out)
}

func appendNodeSymbolNamed(path, language, kind, name string, node *tree_sitter.Node, out *[]Symbol) {
	if node == nil || name == "" {
		return
	}
	start, end := node.StartPosition(), node.EndPosition()
	endLine := int(end.Row) + 1
	if end.Column == 0 && end.Row > start.Row {
		endLine--
	}
	*out = append(*out, newSymbol(path, language, kind, name, int(start.Row)+1, endLine))
}

func nodeText(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if source == nil {
		return ""
	}
	start, end := node.ByteRange()
	if end > uint(len(source)) || start > end {
		return ""
	}
	return string(source[start:end])
}

func firstIdentifier(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "identifier" || node.Kind() == "type_identifier" || node.Kind() == "field_identifier" {
		return nodeText(node, source)
	}
	for i := uint(0); i < node.NamedChildCount(); i++ {
		if value := firstIdentifier(node.NamedChild(i), source); value != "" {
			return value
		}
	}
	return ""
}

func receiverTypeName(receiver *tree_sitter.Node, source []byte) string {
	if receiver == nil {
		return ""
	}
	var findType func(*tree_sitter.Node) *tree_sitter.Node
	findType = func(node *tree_sitter.Node) *tree_sitter.Node {
		if node.Kind() == "parameter_declaration" {
			return node.ChildByFieldName("type")
		}
		for i := uint(0); i < node.NamedChildCount(); i++ {
			if result := findType(node.NamedChild(i)); result != nil {
				return result
			}
		}
		return nil
	}
	return firstIdentifier(findType(receiver), source)
}

func isExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
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
