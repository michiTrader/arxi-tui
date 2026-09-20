package scene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The SCENES <-> signedNodeTypes audit, in three directions.
//
// signedNodeTypes is the one inventory in this package written by hand rather
// than derived by reflection, and its own comment says this test is the reason
// it is allowed to be. The list cannot be derived: `button`, `switch`,
// `slider` and `sparkline` exist in no Go construct at all — no field, no case
// clause, no struct tag — because the whole point is to name types the engine
// has *not* built. Reflecting over the renderer would produce "the types we
// built", which is the question the renderer already answers.
//
// So the drift risk is real and is answered the way signedBinds answers it.
// This package has watched four hand-maintained inventories and three drifted;
// the fourth did not, because binds_audit_test.go checks it against the
// document from both ends. This is that test for node types.

// primitiveTypePattern matches a backticked identifier in the primitive
// vocabulary paragraph of docs/SCENES.md.
var primitiveTypePattern = regexp.MustCompile("`([a-z_]+)`")

// documentedPrimitives reads the "Primitive vocabulary (v0, settled)" section
// of docs/SCENES.md — the Containers/Content lines only.
//
// The scope is narrow on purpose, and the reason is a defect the sibling audit
// in internal/engine produced against itself one commit ago: it read a whole
// section, a later prose paragraph mentioned three property names in
// backticks, and the denominator silently grew from 5 to 8 without one line of
// engine code changing. A parser that reads prose measures the prose.
//
// Here the same discipline matters twice over, because the `Universal:` line
// immediately following lists *properties* in identical backtick syntax, and
// swallowing it would sign `id`, `bind` and `when` as node types.
func documentedPrimitives(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "SCENES.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the primitive vocabulary is the contract this test audits;\n"+
			"without it signedNodeTypes is checked against nothing", path, err)
	}

	lines := strings.Split(string(data), "\n")
	var block strings.Builder
	for i, line := range lines {
		if !strings.HasPrefix(line, "Containers:") {
			continue
		}
		for _, l := range lines[i:] {
			// "Universal:" starts the property list, which is a different
			// vocabulary; a blank line ends the paragraph.
			if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "Universal:") {
				break
			}
			block.WriteString(l)
			block.WriteString("\n")
		}
		break
	}
	if block.Len() == 0 {
		t.Fatalf("found no \"Containers:\" paragraph in %s — this audit would pass vacuously,\n"+
			"reporting perfect agreement between a hand-written list and nothing at all", path)
	}

	// `anchor`, `row_template` and `full` appear in that paragraph as
	// property names and values of the types being listed, not as types.
	// They are named rather than pattern-matched away because a silent
	// filter is how a denominator quietly shrinks — the same three, named
	// the same way, as the engine's own reader of this paragraph.
	notATypeInThisParagraph := map[string]bool{
		"anchor": true, "row_template": true, "full": true,
	}

	seen := make(map[string]bool)
	var out []string
	for _, m := range primitiveTypePattern.FindAllStringSubmatch(block.String(), -1) {
		typ := m[1]
		if notATypeInThisParagraph[typ] || seen[typ] {
			continue
		}
		seen[typ] = true
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// Directions one and two: the document and the list must agree both ways.
//
// They fail for different reasons, and both are expensive:
//
//  1. A type SCENES.md settles and the list omits. The author writes a
//     documented primitive and is told it is unsigned — possibly with a
//     "did you mean" pointing at some unrelated word. The document becomes a
//     lie, and Phase 2's corpus would score the model down for using the
//     vocabulary the product itself documents.
//
//  2. A type the list signs and no document describes. That is vocabulary
//     entering through code instead of through a signature, the
//     closed-vocabulary-by-accident failure this project was built to avoid —
//     and here it is worse than usual, because a signed type is precisely the
//     one the engine promises to stop warning about, so the silence is
//     unearned.
func TestSignedNodeTypesMatchTheDocument(t *testing.T) {
	documented := documentedPrimitives(t)
	if len(documented) < 10 {
		t.Fatalf("parsed only %d primitives from docs/SCENES.md (%v); the audit is reading the\n"+
			"wrong paragraph and would report drift that is not there", len(documented), documented)
	}

	signed := make(map[string]bool)
	for _, typ := range SignedNodeTypes() {
		signed[typ] = true
	}

	for _, typ := range documented {
		if !signed[typ] {
			t.Errorf("docs/SCENES.md settles node type %q and signedNodeTypes omits it.\n"+
				"consequence: an author who uses a documented primitive is warned that it is\n"+
				"unsigned, and may be offered a \"did you mean\" for an unrelated type. The\n"+
				"document becomes a lie, and Phase 2's corpus scores the model down for using\n"+
				"the vocabulary the product itself documents.\n"+
				"remedy: add %q to signedNodeTypes, or stop documenting it.", typ, typ)
		}
	}

	for _, typ := range SignedNodeTypes() {
		if !containsString(documented, typ) {
			t.Errorf("signedNodeTypes signs %q and docs/SCENES.md's primitive vocabulary does not\n"+
				"describe it.\n"+
				"consequence: vocabulary entered through code instead of through a signature,\n"+
				"which is the closed-vocabulary-by-accident failure this project was built to\n"+
				"avoid — and a signed type is exactly the one the engine promises to stop\n"+
				"warning about, so the silence is now unearned.\n"+
				"remedy: document %q in the primitive vocabulary, or unsign it.", typ, typ)
		}
	}
}

// Direction three: every type the renderer has a case for must be signed.
//
// This is the one that would bite soonest. The engine's progress audit already
// fails a type the renderer draws and no document defines; this fails a type
// the renderer draws and the *format* has not signed, which is how the two
// lists come apart while each looks fine on its own.
func TestEveryRenderedTypeIsSigned(t *testing.T) {
	for _, typ := range renderedTypesFromDispatch(t) {
		if !IsSignedNodeType(typ) {
			t.Errorf("the renderer draws node type %q and the format has not signed it.\n"+
				"consequence: the engine renders a primitive no document settles, so the format\n"+
				"is defined by whatever the renderer happens to implement — and a scene using it\n"+
				"is warned as unsigned while drawing perfectly, which is the guard contradicting\n"+
				"the renderer in the false-alarm direction.\n"+
				"remedy: sign %q in signedNodeTypes and document it, or delete the case.", typ, typ)
		}
	}
}

// renderedTypesFromDispatch returns every type label in renderNode's `switch
// n.Type` — read from the AST, not from the text.
//
// The first version of this function matched `case "([a-z_]+)":` against the
// source, and an injection is why it does not any more. Teaching the renderer
// an unsigned type by widening an existing arm —
//
//	case "spinner":  ->  case "spinner", "gauge":
//
// — made the regex match *nothing on that line*, because it requires the
// closing quote to be followed immediately by a colon. The audit passed,
// reporting full agreement, while the engine drew a type the format had never
// signed. That is the false-negative direction, and it is worse than a plain
// miss: the guard did not merely fail to see the new label, it silently
// stopped seeing the arm it had been covering all along.
//
// The lesson is the one this repository keeps re-learning in new clothes — the
// audit that reflected a *copy* of the accessor structs, the parser whose
// denominator moved when prose was added nearby. A textual pattern encodes an
// assumption about formatting that nothing enforces, so the code drifts out
// from under it with no diff anywhere near the guard. The AST has the labels
// as data; there is no spelling for it to miss.
func renderedTypesFromDispatch(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("..", "engine", "render.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v — without the dispatch this audit cannot tell an unsigned\n"+
			"type from a parse failure, and would report perfect agreement with nothing", path, err)
	}

	var out []string
	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "renderNode" {
			return true
		}
		ast.Inspect(fn, func(inner ast.Node) bool {
			// Bounding the scan to renderNode matters: render.go holds a
			// dozen other switches (border shapes, anchors, bind paths),
			// and scooping their labels in would report `double`, `ascii`
			// and `chat.history` as unsigned node types — a wall of false
			// alarms, and a guard that cries wolf is a guard that gets
			// deleted.
			cc, ok := inner.(*ast.CaseClause)
			if !ok {
				return true
			}
			found = true
			for _, expr := range cc.List {
				if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					out = append(out, strings.Trim(lit.Value, "`\""))
				}
			}
			return true
		})
		return false
	})

	if !found {
		t.Fatal("found no case clauses in renderNode; this audit would pass vacuously while\n" +
			"the renderer could be drawing anything at all")
	}
	if len(out) < 8 {
		t.Fatalf("read only %d type labels from renderNode's dispatch (%v); the audit is\n"+
			"reading the wrong function", len(out), out)
	}
	sort.Strings(out)
	return out
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
