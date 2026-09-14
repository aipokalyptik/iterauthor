package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Keep this executable boundary small and explicit. New interfaces may depend
// on application and project value types; they must not become worker owners.
func TestPackageBoundaries(t *testing.T) {
	allowed := map[string]map[string]bool{
		"project":     {},
		"model":       {"project": true},
		"engine":      {"project": true, "model": true},
		"application": {"project": true, "model": true, "engine": true},
		"tui":         {"application": true, "project": true},
	}
	for pkg, deps := range allowed {
		files, err := filepath.Glob(filepath.Join("..", pkg, "*.go"))
		check(t, err)
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			tree, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			check(t, err)
			projectAlias := "project"
			for _, imp := range tree.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				check(t, err)
				const internal = "github.com/aipokalyptik/iterauthor/internal/"
				if strings.HasPrefix(path, internal) && !deps[strings.TrimPrefix(path, internal)] {
					t.Errorf("%s: forbidden dependency %s", file, path)
				}
				if pkg != "tui" && (strings.Contains(path, "tview") || strings.Contains(path, "tcell")) {
					t.Errorf("%s depends on terminal UI", file)
				}
				if path == internal+"project" && imp.Name != nil {
					projectAlias = imp.Name.Name
				}
			}
			if pkg == "tui" {
				ast.Inspect(tree, func(n ast.Node) bool {
					sel, ok := n.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					ident, ok := sel.X.(*ast.Ident)
					if !ok || ident.Name != projectAlias {
						return true
					}
					switch sel.Sel.Name {
					case "Store", "Create", "Open", "Atomic", "WriteJSON":
						t.Errorf("%s accesses project storage directly through %s", file, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
}
