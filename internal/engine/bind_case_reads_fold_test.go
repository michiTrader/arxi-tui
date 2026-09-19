package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// The same defect class, asked of the third level: not whether a bind has a
// case, but whether that case reads anything.
//
// The guard has now been outrun twice in the same way. The first audit
// enumerated the fields of Node, and the next instance was a bind — a name,
// not a struct member — so the guard was looking one level below the defect.
// The bind audit was written to close that, and it asks whether a signed bind
// appears as a case label anywhere in the engine. The fifth instance had the
// label: `session.tokens_used` returned the literal "0", with a comment
// explaining that the budget was not yet wired from run.started. It had been
// wired since; the comment outlived the condition it described.
//
// So the label was present, the structural audit was satisfied, and the
// projection ignored fold.State completely. Each guard checks the level the
// last defect lived at, and the defect keeps moving one level in: field ->
// name -> body.
//
// Why this instance is worse than a missing case, and worth its own test.
// A bind with no case draws the placeholder, which is visibly wrong and
// reaches the screen as "[…]". A bind whose case returns a constant draws that
// constant — and here the constant was "0", which is this bind's own signed
// empty state in BINDS.md §4.1. There is no frame, at any budget, where the
// output looks like an error. The two signals that normally survive this class
// were both inverted: the screen looked right, and the guard read as green.
//
// Measured, not assumed: with the constant restored, the label audit
// (TestEverySignedBindIsHandledOrJustified) passes, and every test under
// internal/eval passes. This test is the only thing in the tree that fails.
//
// The check is structural for the same reason the other two are. A behavioural
// test would need a hand-written state per bind, and hand-enumeration is
// exactly what missed the first five instances. This one parses the engine's
// non-test source and requires every bind case in a switch to mention the fold
// state parameter somewhere in its body. That is a weak property on purpose:
// it cannot tell a correct projection from a wrong field — the behavioural
// tests do that — but it is the one property a constant cannot satisfy, and a
// constant is what the label-level audit cannot see.
func TestEveryBindCaseBodyReadsTheFoldState(t *testing.T) {
	cases := bindCaseBodiesInEngine(t)
	// The floor stops a vacuous pass. If the scan ever stops finding case
	// bodies — a renamed parameter, a restructured switch, a parse failure
	// swallowed upstream — then "every case reads the fold" would be true
	// of the empty set and the audit would report success having checked
	// nothing. That is the failure mode this whole family of tests exists
	// to prevent, so it is asserted rather than assumed.
	if len(cases) < 20 {
		t.Fatalf("found only %d bind cases in the engine source; the audit is reading the wrong switches and would pass vacuously", len(cases))
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			if c.readsState {
				return
			}
			t.Errorf("the case for bind %q never reads the fold state; it answers with a constant\n"+
				"consequence: this is the checked-but-never-drawn class at the level the label audit\n"+
				"cannot see. TestEverySignedBindIsHandledOrJustified asks whether a bind has a case, and\n"+
				"this one does — so the guard reads green while the projection ignores fold.State. It is\n"+
				"worse than a missing case, not better: a missing bind draws \"[…]\", which looks wrong on\n"+
				"screen, while a constant draws a plausible value at every state. session.tokens_used\n"+
				"returned the literal \"0\", which is its own signed empty state in BINDS.md §4.1, so no\n"+
				"frame at any budget ever looked like an error.\n"+
				"remedy: read the fold field the bind is signed against, so the projection follows the\n"+
				"state instead of restating a claim about it. If the value genuinely cannot be computed\n"+
				"yet, do not answer with a plausible constant — leave the bind unhandled so it draws the\n"+
				"placeholder, and record it in acceptedUnprojectedBinds with the reason. A gap that is\n"+
				"visible is recoverable; a gap disguised as a value is not.", c.label)
		})
	}
}

type bindCase struct {
	label      string
	readsState bool
}

// bindCaseBodiesInEngine collects every case clause that switches on a bind
// name, paired with whether its body mentions the fold state.
//
// It scans the whole package rather than resolveBind alone for the reason the
// label audit already established: collection binds are dispatched in
// renderList and elsewhere, so a single-function scan would miss them — and a
// bind this audit does not see is a bind it cannot report on.
func bindCaseBodiesInEngine(t *testing.T) []bindCase {
	t.Helper()

	var out []bindCase
	seen := make(map[string]bool)

	// ParseDir over the package's non-test sources, matching how
	// bindCaseLabelsInEngine scans. The two audits must read the same set of
	// files or they would disagree about which binds exist, and a bind
	// visible to one and not the other is a gap neither would report.
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse engine package: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fd, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}
				// Only functions that actually take a fold state can
				// be expected to read one. A switch on bind names
				// inside a helper with no state parameter is not this
				// defect.
				stateParam := foldStateParamName(fd)
				if stateParam == "" {
					return true
				}
				ast.Inspect(fd, func(m ast.Node) bool {
					cc, ok := m.(*ast.CaseClause)
					if !ok {
						return true
					}
					for _, expr := range cc.List {
						lit, ok := expr.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						label := strings.Trim(lit.Value, "`\"")
						// Bind names are dotted paths. Restricting
						// to them keeps the audit off the shape and
						// anchor switches ("double", "ascii",
						// "top-right"), which are not binds and have
						// no fold field to read.
						if !strings.Contains(label, ".") {
							continue
						}
						if seen[label] {
							continue
						}
						seen[label] = true
						out = append(out, bindCase{
							label:      label,
							readsState: identUsed(cc, stateParam),
						})
					}
					return true
				})
				return false
			})
		}
	}
	return out
}

// foldStateParamName returns the name of the fold.State parameter, or "" if
// the function takes none.
func foldStateParamName(fd *ast.FuncDecl) string {
	if fd.Type.Params == nil {
		return ""
	}
	for _, field := range fd.Type.Params.List {
		sel, ok := field.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "State" {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "fold" {
			continue
		}
		if len(field.Names) > 0 {
			return field.Names[0].Name
		}
	}
	return ""
}

// identUsed reports whether an identifier appears anywhere under a node.
func identUsed(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(k ast.Node) bool {
		if id, ok := k.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}
