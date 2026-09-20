package scene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// Every vocabulary in this package must be derived from a named type, never
// from a struct literal written at the point of reflection.
//
// This is the structural guard for a defect this package has now produced
// twice, and the second time was inside the fix for the first. R19h: the
// border vocabulary reflected over two anonymous structs copied from the
// accessors, so teaching an accessor one extra key left a document using that
// key honoured by the renderer and *warned about* by the guard — a false alarm
// on working code, with the whole suite green. The remedy was one named type,
// borderObject, read by both the accessors and the vocabulary.
//
// Injection R20f measured that the remedy was a convention rather than a
// constraint. Replacing `reflect.TypeOf(FocusGlow{})` with an inline struct
// carrying an extra `speed` key taught the vocabulary a property the engine
// does not read: a document declaring `focus_glow: {"speed": 3}` then parsed,
// validated and warned about nothing, and the key was gone. The entire suite
// stayed green — including the audits written specifically to catch silent
// drops — because every one of them asks about *keys the vocabulary knows*,
// and the injection worked by adding one.
//
// That is the shape this project keeps paying for: the guard and the code it
// describes are free to disagree, and the disagreement is invisible precisely
// because the guard is what would have reported it. A test asserting "speed is
// warned about" would close this one instance; it would not stop the next
// vocabulary from reflecting a copy. So the assertion is on the shape of the
// derivation rather than on any one key, which is the same move that made
// borderObject work: prefer making the mistake unrepresentable to testing for
// its symptoms.
//
// Reading the AST is the only way to ask this question. At runtime a reflected
// anonymous struct and a reflected named type are both just a reflect.Type
// with fields; the difference — whether the thing being reflected is the same
// declaration the production code reads — exists only in the source.
func TestEveryVocabularyReflectsANamedType(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse scene package: %v", err)
	}

	found := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "TypeOf" {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "reflect" {
					return true
				}
				if len(call.Args) != 1 {
					return true
				}
				found++

				// reflect.TypeOf(T{}) — the argument is a composite
				// literal, and its type must be a plain identifier (a
				// named type in this package). An *ast.StructType there
				// is a struct declared inline: the copy.
				lit, ok := call.Args[0].(*ast.CompositeLit)
				if !ok {
					return true
				}
				if _, isStruct := lit.Type.(*ast.StructType); !isStruct {
					return true
				}

				pos := fset.Position(call.Pos())
				t.Errorf("%s:%d reflects over a struct declared inline rather than a named type.\n"+
					"consequence: the vocabulary derived here describes a copy, so it can disagree with\n"+
					"the type the engine actually reads — and the disagreement is invisible, because\n"+
					"this derivation is what would have reported it. Teaching the copy an extra key\n"+
					"makes a document declaring that key parse, validate and warn about nothing: the\n"+
					"key is silently dropped and every silent-drop audit passes, since they all ask\n"+
					"about keys the vocabulary knows.\n"+
					"This is R19h (the border vocabulary's two anonymous structs) and injection R20f\n"+
					"(focus_glow's) — twice, the second time inside the fix for the first.\n"+
					"remedy: declare the shape as a named type in this package, have the production\n"+
					"code that reads the object use that same type, and reflect over it here.",
					name, pos.Line)
				return true
			})
		}
	}

	// The floor: this file is worthless if it finds no derivations at all,
	// which is what a renamed helper or a moved file would produce. Five
	// vocabularies exist today; fewer than three means the walk is not
	// reaching the source and every future copy would pass unseen.
	if found < 3 {
		t.Fatalf("found only %d reflect.TypeOf calls in the scene package; the guard is not reaching\n"+
			"the source it audits and would pass vacuously while any vocabulary reflected a copy", found)
	}
}
