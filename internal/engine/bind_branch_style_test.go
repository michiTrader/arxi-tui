package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The unit of the style sweep was wrong, and the shipped scene was in the gap.
//
// TestEveryNodeDrawsItsOwnContentUnderItsOwnToken sweeps renderNode's dispatch
// and asks its question once per *node type*. To make each type draw real
// content it pins one bind per type — `list` → `slash.matches`, and nothing
// else. That is a hand-written bind→node-type map, which is the second
// inventory nodeTypesInRenderNode was written to abolish; it has simply moved
// into the probe's setup, in `switch ty` syntax instead of a map literal.
//
// It matters because a node type is not the unit at which this defect lives.
// The renderers resolve the declared token separately per bind branch:
//
//	renderList      agent.todos / slash.matches / (default)   3 content paths
//	renderMarkdown  chat.history / thinking.text / (default)  3 content paths
//	renderMarquee   thinking.text / (default)                 2 content paths
//
// One bind per type measures one arm and reports on all of them. Measured, by
// welding the todo row to a literal "text" so that the token declared on the
// node reaches `slash.matches` and not `agent.todos`:
//
//	TestEveryNodeDrawsItsOwnContentUnderItsOwnToken   PASS
//	TestStylingAShippedSceneChangesItsFrame           SKIP
//	go test ./...                                     every package ok
//
// The arm left unmeasured is not a hypothetical corner. MAXIMUM's Tasks panel
// is `{"type":"list","bind":"agent.todos"}`, so the shipped scene rides the
// branch nothing measured, and `sobria-dim-the-footer` is already a corpus case
// ordering exactly this kind of restyle. The failure is the silent one this
// package keeps rediscovering: both validators accept the document, the frame
// does not change, nothing refuses, so the repair loop has no `file:line` to
// work from and `converged` scores the unchanged screen as a win.
//
// So this file sweeps the bind branches themselves, parsed out of each
// renderer's `switch n.Bind` the same way the node types are parsed out of
// renderNode's `switch n.Type`. A branch added to a renderer is swept from the
// moment it exists, and no arm is represented by a sibling.
//
// # What this guard does not decide
//
// Nothing about which token is correct, and nothing about the selected slash
// row — TestADeclaredTokenDoesNotEraseTheSlashHighlight owns that boundary, and
// this sweep honours it by asking only that *some* span on the branch carries
// the declaration, never that every span does.

// bindBranchWitness is the declared token. As with ownStyleWitness it is a name
// no renderer can mint by accident: the first version of the sibling guard used
// `dim`, and `list` passed on a token it hardcodes for its empty state.
const bindBranchWitness = "BINDBRANCHWITNESS"

// bindSwitchArms reads the `switch n.Bind` case values out of a named renderer.
//
// Parsed, not listed, for the reason recorded above: a hand-written arm list is
// the same second inventory in a new place, and it would drift the first time a
// bind is added. The `default` arm is reported as "" — it is a real content
// path (markdown and marquee both fall back to n.Text there) and a node whose
// bind matches no case is the commonest node in the corpus.
func bindSwitchArms(t *testing.T, fnName string) []string {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse engine package: %v", err)
	}

	var arms []string
	seen := map[string]bool{}
	found := false

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fd, ok := n.(*ast.FuncDecl)
				if !ok || fd.Name.Name != fnName {
					return true
				}
				ast.Inspect(fd, func(m ast.Node) bool {
					sw, ok := m.(*ast.SwitchStmt)
					if !ok {
						return true
					}
					// Only the switch on n.Bind. A renderer may switch on
					// other things (border style, category) and those arms
					// are not bind branches.
					sel, ok := sw.Tag.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Bind" {
						return true
					}
					found = true
					for _, stmt := range sw.Body.List {
						cc, ok := stmt.(*ast.CaseClause)
						if !ok {
							continue
						}
						if cc.List == nil {
							// `default:` — the fallback content path.
							if !seen[""] {
								seen[""] = true
								arms = append(arms, "")
							}
							continue
						}
						for _, expr := range cc.List {
							lit, ok := expr.(*ast.BasicLit)
							if !ok || lit.Kind != token.STRING {
								continue
							}
							v := strings.Trim(lit.Value, "`\"")
							if !seen[v] {
								seen[v] = true
								arms = append(arms, v)
							}
						}
					}
					return true
				})
				return false
			})
		}
	}

	if !found {
		t.Fatalf("no `switch n.Bind` found in %s\n"+
			"consequence: this guard would sweep zero branches for that renderer and report success\n"+
			"having measured nothing — the exact vacuous pass the floors in this package exist to stop.\n"+
			"remedy: if %s no longer dispatches on the bind, point this guard at whatever replaced it.", fnName, fnName)
	}

	sort.Strings(arms)
	return arms
}

// bindBranchProbeState is populated on every axis a bind branch might read, so
// that a branch draws its real content rather than its empty state.
//
// The empty-state distinction is not cosmetic here. With `Todos` left nil the
// `agent.todos` arm draws "no tasks" through a *different* styleNameOr call
// than the todo rows, so a probe with an empty fold measures the placeholder
// and reports on the rows. That is the sibling guard's mistake one level down,
// and it is avoided by filling the fold rather than by trusting it is full.
func bindBranchProbeState() fold.State {
	return fold.State{
		UserInput:     "typed text",
		ThinkingText:  "thinking about it",
		SlashActive:   true,
		SlashSelected: 0,
		SlashMatches: []fold.SlashMatch{
			{Name: "/help", Description: "show help", Category: "core"},
			{Name: "/quit", Description: "leave the session", Category: "core"},
		},
		History: []fold.ChatLine{
			{Role: "user", Text: "a turn of the transcript"},
			{Role: "assistant", Text: "the turn after it"},
		},
		Todos: []fold.TodoItem{
			{Task: "first task"},
			{Task: "second task", BlockedOn: "review"},
		},
	}
}

// branchDrawsUnderDeclaredToken renders one node type on one bind branch and
// reports whether it drew anything, and whether any span it drew carries the
// declared token.
//
// "Any span", deliberately, and not "every span": renderList holds the selected
// slash row at "text" on purpose (BINDS.md §4.3), and a guard demanding
// uniformity here would fail on the one piece of this behaviour that is
// correct, then get deleted for crying wolf. The highlight's boundary is
// TestADeclaredTokenDoesNotEraseTheSlashHighlight's to defend, not this one's.
//
// It also does not hold the unresolved-bind placeholder to the rule. "[…]" is
// not the node's content but the engine's own admission that it has none, and
// render.go is explicit that letting a scene restyle it "would let a document
// dress up the engine's own admission of a gap". That exception is recognised
// by comparing against the `placeholderValue` constant rather than by listing
// the `default` arm: a branch excused by name would go on being excused after
// it started drawing real content, and the constant is the same one evalWhen
// reads for the same reason — the difference between "the value is this text"
// and "there is no value" has to stay legible to code, not just to readers.
//
// The third return value is the floor's input rather than the sweep's. A
// branch that emits *nothing at all* — not even the placeholder — is a branch
// whose fold axis is unpopulated, and it drops out of the sweep silently.
// Measured: with bindBranchProbeState emptied, the sweep fell from 7 branches
// to 3 and went green with the agent.todos weld still in place. "Nothing to
// measure" and "nothing wrong" are the same colour unless they are counted
// apart, which is the whole subject of this package.
func branchDrawsUnderDeclaredToken(t *testing.T, nodeType, bind string) (drew, honoured, emitted bool) {
	t.Helper()

	n := &scene.Node{
		Type:        nodeType,
		Bind:        bind,
		Text:        "own content",
		Placeholder: "own placeholder",
		Style:       map[string]string{"style": bindBranchWitness},
	}

	r := &Renderer{Width: 60, Height: 12}
	frame := r.renderNode(n, bindBranchProbeState(), 12)

	for _, line := range frame.Live {
		for _, span := range line {
			text := strings.TrimSpace(span.Text)
			if text == "" {
				continue
			}
			emitted = true
			if text == placeholderValue {
				// The engine reporting an absence, not the node drawing
				// content. Neither drawn nor an offence.
				continue
			}
			drew = true
			if span.Style == bindBranchWitness {
				honoured = true
			}
		}
	}
	return drew, honoured, emitted
}

// armName renders a bind arm for a message, naming the fallback rather than
// printing an empty string where a reader expects a bind.
func armName(arm string) string {
	if arm == "" {
		return "(default)"
	}
	return arm
}

// TestEveryBindBranchDrawsUnderItsDeclaredToken is the sibling sweep at the
// unit the defect actually lives at.
func TestEveryBindBranchDrawsUnderItsDeclaredToken(t *testing.T) {
	// The renderers that dispatch on the bind and draw content of their own.
	// Paired with the node type renderNode routes to them, because the probe
	// enters through renderNode — going in the front door keeps the `when`
	// gate and the border arm in the measurement instead of around it.
	renderers := []struct{ fn, nodeType string }{
		{"renderList", "list"},
		{"renderMarkdown", "markdown"},
		{"renderMarquee", "marquee"},
	}

	var dropped, silent []string
	branches := 0

	for _, rd := range renderers {
		arms := bindSwitchArms(t, rd.fn)

		// Per-renderer floor. A renderer whose switch parsed to a single arm
		// is the state this whole file was written about: one arm standing in
		// for its siblings.
		if len(arms) < 2 {
			t.Fatalf("%s parsed to %d bind branch(es) (%q); this sweep is reading the wrong switch\n"+
				"consequence: with one arm the guard measures one content path and reports on all of them,\n"+
				"which is the defect this file exists to correct.", rd.fn, len(arms), arms)
		}

		for _, arm := range arms {
			drew, honoured, emitted := branchDrawsUnderDeclaredToken(t, rd.nodeType, arm)
			if !emitted {
				// Not "this branch is fine" but "this branch was never
				// exercised": it put nothing in the frame, which on a fold
				// this test populates itself means the probe is starved.
				silent = append(silent, rd.nodeType+" / "+armName(arm))
				continue
			}
			if !drew {
				// Drew only the unresolved-bind placeholder: the engine
				// reporting an absence, which it is entitled to keep dim.
				continue
			}
			branches++
			if !honoured {
				dropped = append(dropped, rd.nodeType+" / "+armName(arm))
			}
		}
	}

	sort.Strings(dropped)
	sort.Strings(silent)

	// The real floor, and it is per branch rather than over the total. A
	// starved probe does not fail this guard loudly; it shrinks the sweep and
	// keeps printing ok, which is how an empty fold hid the agent.todos weld
	// while `branches == 0` was still comfortably satisfied by the other arms.
	if len(silent) > 0 {
		t.Errorf("%d bind branch(es) put nothing in the frame, so they were not measured:\n  %s\n\n"+
			"consequence: a branch that draws nothing is skipped by this sweep, and a skipped branch is\n"+
			"indistinguishable from a correct one in the result line. Measured: with bindBranchProbeState\n"+
			"emptied, the sweep fell from 7 branches to 3 and reported PASS with a welded agent.todos.\n"+
			"remedy: populate the fold axis this branch reads in bindBranchProbeState. If the branch\n"+
			"genuinely cannot draw — renderMarquee collapses to zero height on empty text by contract —\n"+
			"say so here explicitly rather than letting it fall out of the count in silence.",
			len(silent), strings.Join(silent, "\n  "))
	}

	if len(dropped) > 0 {
		t.Errorf("%d bind branch(es) draw content and discard the token the node declared:\n  %s\n\n"+
			"consequence: the document validates — `style` is universal in SCENES.md and ValidateTokens\n"+
			"refuses an undefined token on any node type — and the frame is unchanged. Nothing refuses,\n"+
			"so Phase 2's repair loop has no file:line to work from and `converged` scores the unchanged\n"+
			"screen as correct. Reachable from a shipped scene: MAXIMUM's Tasks panel binds agent.todos,\n"+
			"and sobria-dim-the-footer is a corpus case ordering exactly this restyle.\n"+
			"remedy: resolve the declared token once per renderer (styleNameOr(n.Style, minted)) and use\n"+
			"that value on every bind branch's content, as renderMarkdown's `token` already does. Where a\n"+
			"branch mints its own token for a reason — the list's `dim` empty state, the input's\n"+
			"placeholder — the declared token is what the author asked for and the minted one is the\n"+
			"default it replaces. The one exception is the selected slash row, which is a semantic and\n"+
			"not a default; TestADeclaredTokenDoesNotEraseTheSlashHighlight holds that line.",
			len(dropped), strings.Join(dropped, "\n  "))
	}

	// The floor that makes the pass mean something. Every arm declining to
	// draw would print a green result having measured nothing at all.
	if branches == 0 {
		t.Fatal("no bind branch drew content, so this guard's passing means nothing\n" +
			"consequence: a green result would certify a styling surface nobody measured.\n" +
			"remedy: confirm bindBranchProbeState still fills the fold axes the branches read —\n" +
			"an empty fold sends every branch to its empty state, which is a different code path.")
	}

	t.Logf("swept %d bind branch(es) across %d renderer(s)", branches, len(renderers))
}

// TestTheBindBranchSweepOutnumbersTheNodeTypeSweep is the guard on the gap
// itself, and it is separate because it measures a different thing.
//
// The sweep above is only worth its file if it asks more questions than the
// node-type sweep next door does. If a later edit collapsed the bind switches —
// or the parser silently started returning one arm per renderer — this file
// would keep passing while quietly becoming the thing it replaced. That is the
// failure mode the whole package is about: a guard that still runs, still
// prints ok, and no longer measures.
//
// It compares counts rather than asserting an exact number so that adding a
// bind branch does not fail a test about coverage breadth.
func TestTheBindBranchSweepOutnumbersTheNodeTypeSweep(t *testing.T) {
	renderers := []string{"renderList", "renderMarkdown", "renderMarquee"}

	total := 0
	for _, fn := range renderers {
		total += len(bindSwitchArms(t, fn))
	}

	// One arm per renderer is precisely what the node-type sweep already
	// covers by pinning one bind per type.
	if total <= len(renderers) {
		t.Errorf("the bind branches parsed to %d arm(s) across %d renderer(s)\n\n"+
			"consequence: this file is no longer measuring anything the node-type sweep next door does\n"+
			"not already measure, while continuing to report success — a guard that still runs and no\n"+
			"longer asks a question is the shape every finding in this package has taken.\n"+
			"remedy: if the renderers genuinely stopped dispatching on the bind, delete this file and say\n"+
			"so in the commit; if they did not, bindSwitchArms has stopped reading them.",
			total, len(renderers))
	}

	t.Logf("%d bind branch(es) across %d renderer(s), against %d node type(s) in renderNode",
		total, len(renderers), len(nodeTypesInRenderNode(t)))
}
