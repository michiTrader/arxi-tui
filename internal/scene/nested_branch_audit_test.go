package scene

import (
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// The audit for the axis the *inventory* holds fixed: which branches of Node
// are nested branches at all.
//
// # Why this is a different question again
//
// nestedFormReaders was the fifth fix for one defect, and it was written to be
// the structural one: the branch became a key (`<branch>.<shape>`) rather than
// a hardcoded pair, so a nested branch is either listed or caught. That claim
// is true of the *inventory*. It is not true of the repository, because
// nothing derives the set of branches from Node — five hand-written sites do,
// independently:
//
//	scene/validate.go   validateBinds      children, prefix, suffix, row_template
//	scene/validate.go   collectTokenErrors children, prefix, suffix, row_template
//	scene/vocabulary.go collectWarnings    children, prefix, suffix, row_template
//	eval/grade.go       walk               children, prefix, suffix, row_template
//	scene/signed_types.go nestedFormReaders  children, prefix, suffix
//
// Every one of those lists is correct today and every one is a separate
// declaration of the same fact. That is the shape this package has watched
// drift six times, and the rule written from the fifth recurrence names it
// exactly: **when a fix enumerates the instances of a defect, the enumeration
// itself is the next axis.** The fifth fix enumerated the branches; the branch
// set is what it holds fixed.
//
// # Measured, before this file existed
//
// A `Footer *Node` field added to Node, read by renderText and by nothing
// else — the realistic shape, since a branch nobody reads at all is already
// caught by TestEveryNodeFieldIsEitherRenderedRefusedOrJustified. The whole
// suite stayed green, including every audit written for this defect class, and
// two things were true of it:
//
//	{"type":"box", "footer":{"type":"text","text":"X"}}
//	  -> validates clean, 0 warnings, X never drawn
//	{"type":"text","footer":{"type":"text","text":"X","bind":"totally.invented"}}
//	  -> validates clean, 0 warnings, and the unsigned bind is never refused
//
// The first line is the silent drop, for the sixth time, arriving through the
// one axis the fifth fix could not see. The second is worse and is the reason
// this audit checks two things rather than one: an unsigned bind inside an
// unwalked branch is not merely undrawn, it is **unvalidated**. The document
// invariant this package enforces everywhere else — every bind resolves to a
// signed row in BINDS.md, or the load fails with an address — simply does not
// hold inside a branch no walker recurses into. That is a containment failure
// rather than a rendering gap, and no amount of warning inventory reaches it.
//
// # What it measures
//
// The node-bearing branches are read out of Node's declaration, and then:
//
//   - every one must be a key of nestedFormReaders, so the drop-warning path
//     has an opinion about which owners compose it;
//   - every one must be recursed into by every walker that claims to visit the
//     whole document, so the binds and tokens inside it are checked.
//
// Neither list is written here. Both are derived, which is the only form of
// this fix that the seventh recurrence cannot walk around.
//
// # Proved in both directions, by counterfactuals actually run
//
// A guard green on a clean tree has demonstrated nothing. Four defects were
// constructed and measured against this file, and each number below is from a
// run, not an argument:
//
//	the measured defect (Footer branch, read by renderText only)  -> 5 findings
//	  the missing inventory row, and all four walkers skipping it
//	a branch mentioned but not recursed into (`_ = n.Suffix`)     -> 1 finding
//	  so the check cannot be satisfied by the line that merely names a branch
//	an unclassified raw field (Caption json.RawMessage)           -> caught
//	  so a raw branch cannot be exempted by omission — how `children` hid
//	a raw branch misclassified as carrying no node (PrefixRaw "") -> caught
//	  by the floor, since a wrong classification shrinks the derived set
//
// Two of this audit's own bugs were found the same way and are recorded at
// their sites rather than here: documentWalkers checked this package under
// the wrong import path and saw one walker instead of four (the floor caught
// it), and branchesRecursedInto missed the `prefix := n.PrefixNode()` form and
// accused three walkers of skipping a branch all three visit — a false alarm
// on working code, found by reading the source the failure pointed at.
func TestEveryNestedBranchIsInventoriedAndWalked(t *testing.T) {
	branches := nodeBearingBranches(t)

	// The floor. If the derivation stops finding branches — a renamed type,
	// a moved file, a parse that silently yields nothing — every assertion
	// below would iterate an empty set and report success having asked
	// nothing. Four node-bearing branches exist today.
	if len(branches) < 4 {
		t.Fatalf("derived only %d node-bearing branch(es) from Node; expected at least 4.\n"+
			"consequence: with an empty derivation this audit checks no branch at all and passes\n"+
			"vacuously — the shape of a deleted test that still prints.\n"+
			"remedy: confirm nodeBearingBranches still reads node.go's Node declaration.", len(branches))
	}

	walkers := documentWalkers(t)
	if len(walkers) < 4 {
		t.Fatalf("found only %d whole-document walker(s); expected at least 4 "+
			"(validateBinds, collectTokenErrors, collectWarnings, eval's walk).\n"+
			"consequence: the containment half of this audit would check almost nothing, so a\n"+
			"branch no walker recurses into — where an unsigned bind is never refused — would\n"+
			"pass unseen.\n"+
			"remedy: confirm documentWalkers still resolves the recursive node walkers.", len(walkers))
	}

	for _, b := range branches {
		t.Run(b.jsonName, func(t *testing.T) {
			assertBranchIsInventoried(t, b)
			for _, w := range walkers {
				if !w.recursesInto[b.selector] {
					t.Errorf("walker %s does not recurse into the %q branch of Node.\n\n"+
						"consequence: the containment failure, not merely a drop. Everything this package\n"+
						"promises about a document — every bind resolves to a signed row in BINDS.md, every\n"+
						"token exists in the theme, every unknown key is reported with an address — holds\n"+
						"only where a walker goes. Measured on an injected branch: a node carrying\n"+
						"`bind: \"totally.invented\"` inside an unwalked branch validated clean and was never\n"+
						"refused. The author gets no address because no layer ever looked.\n"+
						"remedy: recurse into %s in %s, beside the other branches it already visits.",
						w.name, b.jsonName, b.selector, w.name)
				}
			}
		})
	}
}

// assertBranchIsInventoried holds a branch to the drop-warning inventory:
// nestedFormReaders must carry at least one `<branch>.<shape>` key for it.
//
// Being *in* the inventory is what gives warnDroppedNestedForm an opinion; a
// branch absent from it takes the `!known` early return, which is silence —
// the same silence, reached one level further out, that the inventory was
// written to end.
//
// A refused branch is exempt, and that exemption is measured rather than
// assumed. `row_template` is in unrenderedFields, so a document declaring one
// never loads at all:
//
//	{"type":"text","row_template":{…}}
//	  -> <scene>:1:9: node type "text" declares "row_template", … it is refused instead
//
// The author already gets the address and the reason, which is everything a
// drop warning could add — so demanding an inventory row here would be a
// warning about a construction that cannot reach a frame. That is the
// false-alarm direction, and this audit reported it on its first run before
// the refusal was checked; the entry above is what the probe found, not what
// the code looked like it did.
func assertBranchIsInventoried(t *testing.T, b nestedBranchField) {
	t.Helper()

	if _, refused := unrenderedFields[b.jsonName]; refused {
		return
	}

	for form := range nestedFormReaders {
		if strings.HasPrefix(form, b.jsonName+".") {
			return
		}
	}

	forms := make([]string, 0, len(nestedFormReaders))
	for form := range nestedFormReaders {
		forms = append(forms, form)
	}
	sort.Strings(forms)

	t.Errorf("Node declares %q as a nested branch, and nestedFormReaders has no entry for it.\n\n"+
		"consequence: warnDroppedNestedForm takes its `!known` early return for this branch, so an\n"+
		"owner with no reader for it drops the construction in silence — the defect this inventory\n"+
		"exists to end, arriving through the axis the inventory itself holds fixed. The fifth fix\n"+
		"made the branch a key so a new branch would be listed or caught; this is the check that\n"+
		"makes 'or caught' true.\n"+
		"remedy: add a %q row to nestedFormReaders naming the owners that compose it (and the\n"+
		"accessor each reads it with), or none if no owner does.\n\n"+
		"forms present: %v", b.jsonName, b.jsonName+".<shape>", forms)
}

// nestedBranchField is one branch of Node that carries another node.
type nestedBranchField struct {
	fieldName string // Go field name, e.g. "RowTemplate"
	jsonName  string // json key, e.g. "row_template"
	// selector is the name a walker uses to reach it. For a directly typed
	// branch that is the field; for a raw-encoded one it is the accessor
	// that decodes it, because `n.PrefixRaw` is not how anything recurses.
	selector string
}

// rawBranchAccessors classifies Node's json.RawMessage fields, which are the
// one case the type of the field cannot answer.
//
// A `json.RawMessage` is a deferred decision about shape, so whether it holds
// a node is a fact about the accessors rather than the declaration — and it is
// the fact this whole defect class turns on: `prefix` is polymorphic, and
// which shape an owner reads is why four owner/shape pairings drew nothing.
// An empty value means the branch carries no node.
//
// The map is not a second inventory of Node's fields: it is pinned below to be
// exactly the RawMessage fields the source declares, so a raw field added
// tomorrow fails here rather than being classified by omission — which is
// precisely how `children` stayed outside nestedFormReaders.
var rawBranchAccessors = map[string]string{
	// Decoded as a child node by PrefixNode(); the string shape is read by
	// PrefixText(). Both shapes are inventoried in nestedFormReaders.
	"PrefixRaw": "PrefixNode",
	// A border is { shape, style } — strings, no node. borderObject names
	// the whole of what it may contain.
	"BorderRaw": "",
	// scroll is { speed, pause_when }: a number and a bind path. It is left
	// raw because nothing reads it yet (unrenderedFields refuses it with an
	// address), and the shape belongs to the animation clock Phase 4 designs.
	"Scroll": "",
}

// nodeBearingBranches derives, from Node's declaration, the branches that
// carry another node.
//
// Read out of the source rather than by reflection, for
// nodeFieldsFromSource's reason: this audit exists to catch a *newly added*
// branch, so it is anchored to the declaration a contributor actually edits.
func nodeBearingBranches(t *testing.T) []nestedBranchField {
	t.Helper()

	fields := nodeFieldsFromSource(t)
	if len(fields) < 15 {
		t.Fatalf("parsed only %d fields off Node; the derivation is reading the wrong type and\n"+
			"every check below would run over the wrong set", len(fields))
	}

	fset := token.NewFileSet()
	files, err := parseDir(fset, ".")
	if err != nil {
		t.Fatalf("parse scene package: %v", err)
	}

	// The Go type of each field, by name, straight off the struct.
	types := make(map[string]string, len(fields))
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Node" {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return false
			}
			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					types[name.Name] = exprString(field.Type)
				}
			}
			return false
		})
	}
	if len(types) == 0 {
		t.Fatal("read no field types off Node; the derivation below would classify every branch\n" +
			"as carrying no node and the audit would check nothing")
	}

	// The raw fields the source declares must be exactly the ones
	// rawBranchAccessors classifies. Without this the map is a second
	// inventory that can fall behind by omission, and a raw branch added
	// tomorrow would be silently treated as carrying no node — the same
	// classification-by-omission that kept `children` out of
	// nestedFormReaders for five recurrences.
	declaredRaw := make(map[string]bool)
	for name, typ := range types {
		if typ == "json.RawMessage" {
			declaredRaw[name] = true
		}
	}
	for name := range declaredRaw {
		if _, classified := rawBranchAccessors[name]; !classified {
			t.Errorf("Node declares %s as json.RawMessage and rawBranchAccessors does not classify it.\n\n"+
				"consequence: a raw field is a deferred decision about shape, so nothing about its\n"+
				"declaration says whether it holds a node. Unclassified it defaults to 'carries no\n"+
				"node', which exempts it from both halves of this audit — the silent-drop inventory\n"+
				"and the walker containment check — by omission rather than by a decision anyone made.\n"+
				"remedy: add %q to rawBranchAccessors, mapped to the accessor that decodes it as a\n"+
				"*Node, or to \"\" if it carries no node (with the reason in writing).", name, name)
		}
	}
	for name := range rawBranchAccessors {
		if !declaredRaw[name] {
			t.Errorf("rawBranchAccessors classifies %s, which Node no longer declares as json.RawMessage.\n"+
				"consequence: the entry describes a field that is gone or retyped, so it is a comment\n"+
				"that is false — and if the field was retyped to *Node the classification is now\n"+
				"shadowing a real branch.\n"+
				"remedy: remove the stale entry.", name)
		}
	}

	var out []nestedBranchField
	for _, f := range fields {
		typ := types[f.name]
		selector := ""
		switch {
		case typ == "*Node" || typ == "[]*Node":
			selector = f.name
		case typ == "json.RawMessage":
			selector = rawBranchAccessors[f.name]
		}
		if selector == "" {
			continue
		}
		out = append(out, nestedBranchField{fieldName: f.name, jsonName: f.jsonName, selector: selector})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].jsonName < out[j].jsonName })
	return out
}

// documentWalker is one function that recurses over the node tree, and the set
// of branches it recurses into.
type documentWalker struct {
	name         string
	recursesInto map[string]bool
}

// documentWalkers finds the recursive node walkers and records which branches
// each one descends through.
//
// # Why the walkers are discovered rather than listed
//
// Listing them is the mistake this file is about, one level down: a hand-
// written set of walkers would be a sixth declaration of the same fact, and a
// walker added later would be exempt from the audit by not being on the list.
// A walker is recognised by its shape instead — a function that takes a
// *scene.Node and calls itself.
//
// # Why internal/engine is out of scope, and why that is not the same mistake
//
// The renderers also take a *Node and recurse, and they are *supposed* to be
// selective: renderText composing no children is not a bug, it is the fact
// nestedFormReaders records and the engine-side audit
// (TestEveryNestedShapeIsDrawnOrWarnedOnEveryOwner) measures against the real
// renderer in both directions. Holding them to "every walker visits every
// branch" would demand that every node type compose every branch, which is a
// false alarm on working code — the direction that gets a guard switched off.
//
// The distinction is not "which package" but which claim the function makes:
// these walkers are the ones that enforce a document-wide invariant, so a
// branch they skip is a branch where the invariant does not hold.
func documentWalkers(t *testing.T) []documentWalker {
	t.Helper()

	fset := token.NewFileSet()
	conf := types.Config{
		Importer: importer.ForCompiler(fset, "source", nil),
		Error:    func(error) {},
	}

	var out []documentWalker
	checked := 0

	// The import path is spelled out per directory rather than derived from
	// the relative path, and that detail cost this audit its first run.
	// packagePathFor(".") yields ".../arxi_tui/.", so this package's own
	// Node was checked under a path that does not end in "internal/scene",
	// isSceneNode rejected it, and the three walkers *in this file's own
	// package* were invisible — leaving only eval's. The floor below caught
	// it; a guard with no floor would have reported success while checking
	// one walker out of four.
	for _, pkg := range []struct{ dir, path string }{
		{".", "github.com/michiTrader/arxi_tui/internal/scene"},
		{"../eval", "github.com/michiTrader/arxi_tui/internal/eval"},
	} {
		files, err := parseDir(fset, pkg.dir)
		if err != nil || len(files) == 0 {
			continue
		}
		info := &types.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Uses:       make(map[*ast.Ident]types.Object),
			Defs:       make(map[*ast.Ident]types.Object),
		}
		_, _ = conf.Check(pkg.path, fset, files, info)
		if len(info.Selections) == 0 {
			continue
		}
		checked++

		for _, f := range files {
			out = append(out, walkersInFile(f, info)...)
		}
	}

	if checked == 0 {
		t.Fatal("type-checked no package holding a document walker.\n" +
			"consequence: the audit cannot tell an unwalked branch from a build problem, and\n" +
			"would report every walker as skipping every branch — a wall of false alarms.\n" +
			"remedy: fix the build before trusting this test.")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// walkersInFile collects the recursive node walkers declared in one file,
// whether written as a function or as the recursive closure eval uses.
func walkersInFile(f *ast.File, info *types.Info) []documentWalker {
	var out []documentWalker

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !takesNode(fn.Type, info) {
			continue
		}
		if branches := branchesRecursedInto(fn.Body, fn.Name.Name, info); branches != nil {
			out = append(out, documentWalker{name: fn.Name.Name, recursesInto: branches})
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range as.Rhs {
			lit, ok := rhs.(*ast.FuncLit)
			if !ok || !takesNode(lit.Type, info) || i >= len(as.Lhs) {
				continue
			}
			id, ok := as.Lhs[i].(*ast.Ident)
			if !ok {
				continue
			}
			if branches := branchesRecursedInto(lit.Body, id.Name, info); branches != nil {
				out = append(out, documentWalker{name: id.Name, recursesInto: branches})
			}
		}
		return true
	})

	return out
}

// takesNode reports whether a signature accepts a scene.Node, resolved through
// the type checker.
//
// The type checker rather than the spelling of the parameter, because AGENTS.md
// records five defects in this package from guards that matched an identifier
// name: a text match on "Node" accepts eval.Case's node-ish parameters and any
// local type someone names Node later, and the audit would then hold an
// unrelated function to a rule about scene documents.
func takesNode(sig *ast.FuncType, info *types.Info) bool {
	if sig.Params == nil {
		return false
	}
	for _, p := range sig.Params.List {
		if tv, ok := info.Types[p.Type]; ok && isSceneNode(tv.Type) {
			return true
		}
	}
	return false
}

// branchesRecursedInto returns the branch selectors a walker descends through,
// or nil if the function does not call itself at all.
//
// A branch counts as walked only when a *recursive call* is reached through
// it, never when the walker merely mentions it. The distinction is load-
// bearing: collectWarnings reads `len(n.Children)` to decide whether to warn
// about a dropped child array, and a check satisfied by that mention would
// call the branch walked on the strength of the line that reports it is not —
// a guard certifying the defect it exists to catch.
//
// Three ways a recursive call reaches a branch, which are the three the
// walkers use: the branch is in the call's arguments (`walk(n.Suffix)`), the
// call sits inside a range over it (`for _, c := range n.Children { walk(c) }`),
// or the branch was bound to a local first and the local is passed
// (`if prefix := n.PrefixNode(); prefix != nil { walk(prefix) }`).
//
// That third form is not a refinement, it is the one the first draft of this
// function got wrong — and it got it wrong in the direction that matters.
// Every walker reaches `prefix` exactly that way, so the audit reported three
// walkers skipping a branch all three do visit: a false alarm on working code,
// which borderVocabulary's comment records the cost of and which this package
// has now produced often enough to name. It was caught by reading the source
// the failure pointed at instead of believing the failure — the check a
// counterfactual makes routine.
//
// Locals are therefore resolved to the branch they were assigned from, which
// is the same choice the rest of this file makes: ask what a name refers to,
// never what it is spelled.
func branchesRecursedInto(body *ast.BlockStmt, self string, info *types.Info) map[string]bool {
	found := make(map[string]bool)
	recursive := false

	// Locals bound from a branch of the node: `prefix := n.PrefixNode()`.
	// Keyed by the object the type checker resolves the name to, so a
	// second local that merely shares a spelling is a different entry.
	aliases := make(map[types.Object][]string)
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			if i >= len(as.Rhs) {
				break
			}
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			obj := info.Defs[id]
			if obj == nil {
				obj = info.Uses[id]
			}
			if obj == nil {
				continue
			}
			if sels := nodeSelectorsIn(as.Rhs[i], info); len(sels) > 0 {
				aliases[obj] = append(aliases[obj], sels...)
			}
		}
		return true
	})

	// selectorsReached resolves an expression to the branches it stands
	// for: either selected on the node directly, or through one of the
	// locals above.
	selectorsReached := func(e ast.Expr) []string {
		out := nodeSelectorsIn(e, info)
		ast.Inspect(e, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if obj := info.Uses[id]; obj != nil {
				out = append(out, aliases[obj]...)
			}
			return true
		})
		return out
	}

	var ranges []ast.Expr
	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		if n == nil {
			return
		}
		if rs, ok := n.(*ast.RangeStmt); ok {
			ranges = append(ranges, rs.X)
			ast.Inspect(rs.Body, func(x ast.Node) bool { visit(x); return false })
			ranges = ranges[:len(ranges)-1]
			return
		}
		if call, ok := n.(*ast.CallExpr); ok && callsSelf(call, self) {
			recursive = true
			for _, arg := range call.Args {
				for _, sel := range selectorsReached(arg) {
					found[sel] = true
				}
			}
			for _, r := range ranges {
				for _, sel := range selectorsReached(r) {
					found[sel] = true
				}
			}
		}
		for _, child := range childrenOf(n) {
			visit(child)
		}
	}
	ast.Inspect(body, func(n ast.Node) bool { visit(n); return false })

	if !recursive {
		return nil
	}
	return found
}

// childrenOf enumerates a node's immediate children so the range stack above
// stays accurate. ast.Inspect cannot be used for the outer walk because it
// gives no hook for leaving a node.
func childrenOf(n ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(n, func(c ast.Node) bool {
		if c == nil || c == n {
			return c == n
		}
		out = append(out, c)
		return false
	})
	return out
}

// callsSelf reports whether a call is the walker calling itself, by either
// spelling the walkers use: a bare `walk(...)` or a method `d.validateBinds(...)`.
func callsSelf(call *ast.CallExpr, self string) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == self
	case *ast.SelectorExpr:
		return fn.Sel.Name == self
	}
	return false
}

// nodeSelectorsIn returns the names of every field or method selected on a
// scene.Node anywhere inside an expression, resolved by the type checker.
//
// Resolved, not matched: `c.Children` on some other type is not this branch,
// and the same rule AGENTS.md states — when a guard can ask the type checker,
// matching on an identifier name is a different question — is what keeps the
// containment check from being satisfied by a coincidence of spelling.
func nodeSelectorsIn(e ast.Expr, info *types.Info) []string {
	var out []string
	ast.Inspect(e, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selection, ok := info.Selections[sel]; ok && isSceneNode(selection.Recv()) {
			out = append(out, sel.Sel.Name)
		}
		return true
	})
	return out
}

// exprString renders a type expression in the spelling the struct uses.
func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.ArrayType:
		return "[]" + exprString(t.Elt)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.MapType:
		return "map[" + exprString(t.Key) + "]" + exprString(t.Value)
	}
	return ""
}
