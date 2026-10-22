package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Phase D of docs/DESIGN-OPEN-TUI.md is done when no Go file draws a particular screen.
// Drawing is the engine's: it turns a scene document and a state into a ui.Frame. The host
// may hand a finished frame to the terminal (emitFrame) and nothing else. A file here that
// builds its own lines, spans or frames is a screen that a scene cannot rebuild.
//
// Consequence: a screen drawn in Go cannot be changed by asking arxi, and each one is a Go
// PR for an idea that a scene could carry.
// Remedy: put the screen in a scene document (a full overlay is a window, see
// shells_window_test.go) and feed it through binds; if a new primitive is missing, add it to
// the engine, not to a screen file.
func TestNoGoFileDrawsAScreen(t *testing.T) {
	// The only host function allowed to touch frame types: it writes a finished frame.
	allowed := map[string]bool{"emitFrame": true}
	drawing := map[string]bool{"Line": true, "Span": true, "Frame": true, "Cursor": true}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && allowed[fn.Name.Name] {
				continue
			}
			ast.Inspect(d, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "ui" && drawing[sel.Sel.Name] {
					found = append(found, fset.Position(sel.Pos()).String()+" ui."+sel.Sel.Name)
				}
				return true
			})
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("Go code in cmd/arxi-tui builds screen content. Consequence: that screen cannot be rebuilt by a scene, so changing it is a Go PR. Remedy: draw it with a scene document and binds.\n  %s", strings.Join(found, "\n  "))
	}
}
