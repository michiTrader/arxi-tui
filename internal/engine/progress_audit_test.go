package engine

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The progress audit: the completion number, measured instead of asserted.
//
// This exists because of a defect in the reporting rather than in the code,
// and it is the same class the last thirteen turns spent closing in the
// renderer. AGENTS.md signs "Verify, do not assume. Before reporting a number,
// measure it." Thirteen consecutive turns reported this project as "45-50%
// complete" and the string "45" appears in no file in this repository. The
// number was never instrumented, so nothing could move it and nothing could
// contradict it — the five-point band is the tell, because a measurement does
// not come with a band of convenience.
//
// That is precisely the shape of the silent drop this suite hunts elsewhere. A
// scene property that parsed, validated and drew nothing reported success with
// no diagnostic anywhere; a completion figure with no denominator reports
// progress the same way. In both cases the author's only evidence is that the
// screen looks wrong, and in both cases the remedy is the same: make the thing
// observable, and fail when the claim and the artifact disagree.
//
// What it deliberately does NOT do is print a percentage. A single number
// invites exactly the averaging that produced "45-50%": eleven scenes and five
// animation properties are not commensurable units, and weighting them would
// be a new invention nobody signed. It reports the axes separately, each with
// its own denominator read out of the documents, and it fails only on
// *divergence* — a document promising something the code does not have, or
// code the documents do not describe. The number is then whatever the reader
// computes from honest components.
//
// Every denominator is read from docs/ or derived from the source by AST, on
// the precedent this package has already paid for four times: a hand-copied
// inventory drifts, and its failure mode is the expensive one — the audit
// agrees with the code and the contract is the thing nobody checked. The
// signed-bind map drifted in both directions, the unrendered-field map drifted
// from the refusal it advertised, the universals audit drifted from the engine
// it claimed to check, and a reflected copy of two anonymous structs made a
// guard warn about a document the renderer honoured. A progress report is the
// most tempting place of all to hand-maintain a list, because nothing fails
// when it goes stale.

// TestProgressAxesAreMeasuredNotAsserted reports each completion axis with the
// denominator read from the documents, and fails when a document and the code
// disagree about what exists.
//
// It is one test with subtests rather than several, because the axes are a
// single report: read separately they invite the reader to quote the
// flattering one. `go test -run TestProgressAxes -v ./internal/engine/` prints
// the ledger.
func TestProgressAxesAreMeasuredNotAsserted(t *testing.T) {
	t.Run("scenes", func(t *testing.T) {
		documented := documentedScenes(t)
		pinned := pinnedScenes(t)

		// The floor is what stops a vacuous pass: a parser that silently
		// stops matching would report zero documented scenes and every
		// pinned one as a surplus, which reads as a passing audit.
		//
		// This axis has a floor and no ceiling, and unlike the other three
		// that is deliberate. Three of the four denominators here are
		// backticked words scraped out of a prose paragraph, so a sentence
		// moves them and each needed a ceiling. This one is not: it is
		// anchored to the heading form `^## Scene \d+ — `, and that was
		// measured rather than assumed — a `###` subheading naming four
		// scenes, plus body prose reading "Scene 4 and Scene 11 — ANIMATED
		// share a cadence", left the count at 11.
		//
		// The only edit that moves it is a real `## Scene 12 —` heading,
		// which is a vocabulary change that announces itself in the diff.
		// A ceiling here could therefore only fire in the one case where
		// the count is correct, and last turn's rule says a guard that
		// cannot be the one that fires teaches nothing when it does.
		if len(documented) < 8 {
			t.Fatalf("parsed only %d scene headings from docs/SCENES.md (%v); the audit is reading\n"+
				"the wrong section and would measure nothing", len(documented), documented)
		}

		var missing []string
		for _, name := range documented {
			if !pinned[strings.ToUpper(name)] {
				missing = append(missing, name)
			}
		}
		t.Logf("scenes: %d documented, %d pinned as goldens, %d unpinned (%s)",
			len(documented), len(documented)-len(missing), len(missing), strings.Join(missing, ", "))

		// An unpinned scene is the expected state of a phased project, so
		// it is reported and not failed. What fails is the opposite
		// direction: a golden fixture for a scene no document describes,
		// because that is a frame pinned byte-for-byte against a contract
		// nobody wrote down, and the golden discipline says a pixel change
		// is a review event — a review against nothing is not one.
		for name := range pinned {
			if !containsFold(documented, name) {
				t.Errorf("testdata holds a golden for scene %q, and docs/SCENES.md describes no such scene.\n"+
					"consequence: the fixture pins a frame byte-for-byte against a contract that does not\n"+
					"exist, so a diff in it is unreviewable — the reviewer has nothing to check it against.\n"+
					"remedy: describe the scene in docs/SCENES.md, or delete the fixture.", name)
			}
		}
	})

	t.Run("node_types", func(t *testing.T) {
		documented := documentedNodeTypes(t)
		implemented := renderedNodeTypes(t)

		if len(documented) < 10 {
			t.Fatalf("parsed only %d node types from docs/SCENES.md (%v); the audit is reading the\n"+
				"wrong paragraph and would measure nothing", len(documented), documented)
		}
		// A ceiling as well as a floor, for the reason the animation axis
		// already proved and this one inherited without the defence: both
		// denominators are backticked words scraped out of a prose
		// paragraph, so both move when someone writes a sentence. That is
		// not a hypothetical here — measured by adding "`sticky` header"
		// to the list's parenthetical, which is ordinary documentation
		// prose describing a property of `list`:
		//
		//	node types: 16 documented, 11 rendered, 5 drawing
		//	[[UNKNOWN NODE TYPE]] (button, slider, sparkline, sticky, switch)
		//
		// The floor passed, the audit passed, and the reported gap grew by
		// a type that does not exist. The direction is the mild one — the
		// progress number understates itself — but a denominator that
		// moves when someone writes a sentence is not a measurement, and
		// this file exists because the previous answer to "how far along
		// is this" was an assertion nobody could check.
		//
		// The exclusion lists in the two readers of this paragraph are
		// what hold the count down, and they are hand-maintained: every
		// property named in backticks here has to be listed in both, in
		// two packages, by someone who remembers. This ceiling is the
		// cheap guard that fires when the next one is forgotten.
		//
		// Fifteen is what SCENES.md settles. Raising this number is a
		// review event, which is the point: a real new primitive is a
		// vocabulary change and should be argued in the commit that makes
		// it, not absorbed silently by an audit.
		if len(documented) > 15 {
			t.Errorf("parsed %d node types from docs/SCENES.md (%v), and the settled vocabulary is 15.\n"+
				"consequence: the denominator moved without a vocabulary change, so the progress this\n"+
				"file reports is measured against a number that grew on its own. The likely cause is\n"+
				"prose: a property or value written in backticks inside the primitive paragraph reads\n"+
				"as a type to both parsers of it, and both exclusion lists have to be updated by hand,\n"+
				"in two packages. That is how this axis's sibling drifted from 5 to 8.\n"+
				"remedy: add the word to the exclusion lists in documentedNodeTypes (here) and\n"+
				"documentedPrimitives (internal/scene), or -- if the vocabulary really did gain a\n"+
				"primitive -- sign it in signedNodeTypes and raise this ceiling in the same commit.",
				len(documented), documented)
		}
		if len(implemented) == 0 {
			t.Fatal("found no node type case labels in renderNode; the audit cannot tell an\n" +
				"unimplemented type from a parse failure, and would report the whole vocabulary\n" +
				"as missing — a wall of false alarms.")
		}

		var gaps []string
		for _, typ := range documented {
			if !implemented[typ] {
				gaps = append(gaps, typ)
			}
		}
		t.Logf("node types: %d documented, %d rendered, %d drawing [[UNKNOWN NODE TYPE]] (%s)",
			len(documented), len(documented)-len(gaps), len(gaps), strings.Join(gaps, ", "))

		// A documented-but-unrendered type is legal: PLAN.md's
		// forward-compatibility rule makes it a placeholder, and the screen
		// says what it could not do. The failure is again the reverse — a
		// type the engine draws and no document defines, which is format
		// invented in the renderer, exactly what the row_template and
		// on_press refusals declined to do.
		for typ := range implemented {
			if !containsFold(documented, typ) {
				t.Errorf("renderNode has a case for node type %q, and docs/SCENES.md's primitive\n"+
					"vocabulary does not list it.\n"+
					"consequence: the engine renders a construction the format does not define, so a\n"+
					"scene relying on it is valid against the code and invalid against the contract —\n"+
					"the divergence every hand-maintained inventory in this package produced.\n"+
					"remedy: add %q to the vocabulary paragraph in docs/SCENES.md, or remove the case.", typ, typ)
			}
		}
	})

	t.Run("universal_properties", func(t *testing.T) {
		// This axis reuses universalsFromDocument rather than parsing the
		// paragraph again, because two parsers of one paragraph is the
		// second-inventory mistake in miniature.
		universals := universalsFromDocument(t)
		vocab := scene.NodeVocabularyForAudit()

		if len(vocab) == 0 {
			t.Fatal("scene.NodeVocabularyForAudit() is empty; every property would read as absent\n" +
				"and the axis would report total failure without measuring anything.")
		}

		// A ceiling, for the third axis to need one and the first where the
		// drift direction is dangerous rather than merely untrue.
		//
		// The other two ceilings guard denominators that grow: the animation
		// axis went 5 to 8 and reported 1/8 instead of 1/5, the node-type
		// axis gained a type that does not exist. Both understate progress,
		// which is a false alarm. This axis fails the other way, and it was
		// measured — the Universal paragraph was given one sentence of
		// ordinary documentation prose, naming two properties that are real
		// members of the parser vocabulary:
		//
		//	a bordered container may also carry a `title`, drawn into its `border`.
		//
		//	universal properties: 10 documented, 10 present in the parser
		//	vocabulary, 0 absent ()
		//
		// The axis passed. It did not merely mis-measure: the ratio this
		// subtest exists to report went from 8/8 to 10/10, and the second
		// number moved in lockstep with the first because a word already in
		// the vocabulary is, by construction, never absent. The one
		// quantity that can fail here is pinned to zero by the same
		// property that inflates the count. That is why the floor cannot
		// see it and why membership-checking cannot either.
		//
		// The sibling audit does catch this pair, naming both words, and
		// last turn's rule says a guard that can never be the one that
		// fires should not be added. It does not apply here, and the
		// difference is worth stating because it is the reason this
		// ceiling is not the duplicate it resembles. That audit needs a
		// probe per property, and it fails on the *missing probe*, not on
		// the count — so it catches an inflating word only while no probe
		// exists for it. More decisive: the header of this file documents
		// `go test -run TestProgressAxes -v ./internal/engine/` as the way
		// to print the ledger, and under exactly that invocation the
		// sibling does not run. The inflated 10/10 was printed, and PASS
		// was reported, by the command this file tells a reader to use.
		// A ledger that misreports when read the documented way is the
		// "45-50%" defect with a test around it.
		//
		// Eight is what the paragraph settles: id, bind, when, style, grow,
		// weight, on_press, scroll. Raising it is a vocabulary change and
		// belongs in the commit that argues for it.
		if len(universals) > 8 {
			t.Errorf("parsed %d universal properties from docs/SCENES.md (%v), and the paragraph names 8.\n"+
				"consequence: this is the inflating direction, and it is invisible here. A backticked\n"+
				"word that is already a Node field reads as a universal property and is never counted\n"+
				"absent, so the denominator and the numerator rise together and the axis reports a\n"+
				"clean ratio over a vocabulary the document does not actually settle -- measured at\n"+
				"10/10 with one sentence of prose naming `title` and `border`.\n"+
				"remedy: keep the prose out of the \"Universal:\" paragraph -- the document's own\n"+
				"convention puts vocabulary in one paragraph and commentary in the others -- or, if\n"+
				"the format really did gain a universal property, declare it on scene.Node, add its\n"+
				"probe pair to universalProbes, and raise this ceiling in the same commit.",
				len(universals), universals)
		}

		var absent []string
		for _, prop := range universals {
			if !vocab[prop] {
				absent = append(absent, prop)
			}
		}
		t.Logf("universal properties: %d documented, %d present in the parser vocabulary, %d absent (%s)",
			len(universals), len(universals)-len(absent), len(absent), strings.Join(absent, ", "))

		// Absence is a failure on this axis alone, and that is the
		// asymmetry worth stating. For scenes and node types the gap is a
		// scheduled phase with a placeholder on screen. A universal
		// property absent from the vocabulary has no placeholder: the key
		// is dropped by encoding/json and the document reports success.
		// That is the defect of PR #14 and #15, and it must not be able to
		// return silently.
		for _, prop := range absent {
			t.Errorf("docs/SCENES.md calls %q universal and it is not in the parser's vocabulary.\n"+
				"consequence: encoding/json discards the key, so a scene declaring it parses,\n"+
				"validates clean and renders byte-identically to one that omits it — success is\n"+
				"reported and the property never happened, with no diagnostic anywhere.\n"+
				"remedy: declare the field on scene.Node (which is what the vocabulary is derived\n"+
				"from), then honour it in the engine or refuse it with an address.", prop)
		}
	})

	t.Run("animation_properties", func(t *testing.T) {
		// Scene 4's properties are the standing debt this audit was asked
		// to make visible, so they get their own axis rather than being
		// averaged into the node-type count. They are read out of the
		// Scene 4 paragraph for the same reason as everything else here.
		props := animationPropertiesFromDocument(t)
		if len(props) < 4 {
			t.Fatalf("parsed only %d animation properties from docs/SCENES.md's Scene 4 paragraph (%v);\n"+
				"the audit is reading the wrong section and the standing debt would read as paid",
				len(props), props)
		}
		// A ceiling as well as a floor, because this axis has already drifted
		// upward once. Adding prose to the Scene 4 section that mentioned
		// `row_template`, `on_press` and `id` in backticks pushed the
		// denominator from 5 to 8 and dropped the honoured fraction from 1/5
		// to 1/8 with no code change. A floor alone cannot catch that: the
		// count was too *high*, and a denominator that grows quietly makes
		// progress look worse for free — the mirror of the vanity metric this
		// file replaced, and just as untrue. Scene 4 names five properties;
		// if that genuinely changes, this number changes with it in the same
		// commit.
		if len(props) > 5 {
			t.Fatalf("parsed %d animation properties from Scene 4's vocabulary paragraph (%v), and the\n"+
				"paragraph names five.\n"+
				"consequence: the parser has captured prose from elsewhere in the section, so the\n"+
				"denominator moves when someone writes a sentence and the axis measures the\n"+
				"documentation rather than the engine.\n"+
				"remedy: narrow animationPropertiesFromDocument to the vocabulary paragraph, or — if\n"+
				"Scene 4 really did gain a property — update this ceiling in the same commit.",
				len(props), props)
		}

		rendered := renderedAnimationProperties(t)

		var honoured, known, silent []string

		// The classification is behavioural — it loads a document that sets
		// the property and asks what the format said about it — and the
		// first version of this subtest is why that matters. It classified
		// by *vocabulary membership*: in-vocabulary meant "warned", absent
		// meant "silent". That is backwards. Absence from the vocabulary is
		// exactly the condition that produces the warning; membership means
		// the key is parsed into a field, which is silent unless something
		// then reads it. So the audit reported focus_glow, transition,
		// reveal and enter as silent drops when all four were warned about
		// with an address, and would have reported them as fixed the moment
		// a field was declared and nothing read it.
		//
		// Both errors point the same way: a false alarm about working code,
		// and a clean bill of health for the real defect. That is the
		// direction that gets a guard switched off, and it is the third time
		// this project has produced it by asking a structural question where
		// the contract is behavioural — R19h's reflected copy, the
		// unrenderedFields map before it. The lesson has a cost now: when a
		// guard can ask the artifact directly, inferring from a proxy is not
		// a shortcut, it is a different question.
		for _, p := range props {
			observed, err := observeAnimationProperty(p)
			if err != nil {
				t.Fatalf("premise broken: the %q probe must parse, or the audit cannot tell a\n"+
					"silent drop from a malformed probe; got %v", p, err)
			}
			switch {
			case rendered[p]:
				honoured = append(honoured, p)
			case observed:
				known = append(known, p)
			default:
				silent = append(silent, p)
			}
		}
		t.Logf("animation properties: %d documented, %d honoured by the engine, %d parsed-and-warned, %d silent",
			len(props), len(honoured), len(known), len(silent))
		if len(honoured) > 0 {
			t.Logf("  honoured: %s", strings.Join(honoured, ", "))
		}
		if len(known) > 0 {
			t.Logf("  warned:   %s", strings.Join(known, ", "))
		}

		// Only silence fails. A property the engine draws is done; a
		// property the format warns about is an addressed gap, which is the
		// floor PR #16 established for every key in the format. What may not
		// happen is the third state.
		for _, p := range silent {
			t.Errorf("Scene 4 documents the animation property %q; a document setting it loads with no\n"+
				"warning, no refusal, and no engine code reading it.\n"+
				"consequence: the key vanishes — the scene reports success and the property never\n"+
				"happened, so the author's only evidence is a screen that looks wrong. This is the\n"+
				"class the last four fixes closed. Reaching it from a *declared* field is the newer\n"+
				"way in: declaring the field silences the vocabulary warning, so a half-finished\n"+
				"implementation is quieter than no implementation at all.\n"+
				"remedy: read the field in the engine so it changes the frame, or drop the field so\n"+
				"the vocabulary warns about the key with an address again.", p)
		}
	})
}

// documentedScenes returns the scene names from docs/SCENES.md's headings.
var sceneHeadingPattern = regexp.MustCompile(`(?m)^## Scene \d+ — ([A-Z][A-Z0-9 ]*[A-Z0-9])`)

func documentedScenes(t *testing.T) []string {
	t.Helper()
	data := readDoc(t, "SCENES.md")
	var out []string
	for _, m := range sceneHeadingPattern.FindAllStringSubmatch(data, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// pinnedScenes returns the scene names that own a golden fixture, keyed by the
// fixture basename.
//
// It reads the directory rather than listing the three names known today,
// because a list of fixtures in a test is a second inventory of a directory —
// and the fifth instance of that mistake is what this file exists to prevent.
func pinnedScenes(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v — the golden fixtures are one of this audit's two denominators", dir, err)
	}
	out := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		out[strings.ToUpper(strings.TrimSuffix(e.Name(), ".json"))] = true
	}
	if len(out) == 0 {
		t.Fatalf("found no .json golden fixtures in %s; the scenes axis would report every\n"+
			"documented scene as unpinned and the audit would be measuring a missing directory", dir)
	}
	return out
}

// documentedNodeTypes reads the primitive vocabulary paragraph of SCENES.md.
//
// The paragraph names types in backticks across the "Containers:" and
// "Content:" lines; `switch`/`slider` is written as a pair, which the backtick
// pattern handles without a special case.
var vocabularyTypePattern = regexp.MustCompile("`([a-z_]+)`")

func documentedNodeTypes(t *testing.T) []string {
	t.Helper()
	data := readDoc(t, "SCENES.md")
	lines := strings.Split(data, "\n")

	var block strings.Builder
	for i, line := range lines {
		if !strings.HasPrefix(line, "Containers:") {
			continue
		}
		// The paragraph runs from "Containers:" to the first blank line;
		// "Universal:" begins the property list, which is a different
		// denominator and belongs to another subtest.
		for _, l := range lines[i:] {
			if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "Universal:") {
				break
			}
			block.WriteString(l)
			block.WriteString("\n")
		}
		break
	}
	if block.Len() == 0 {
		t.Fatalf("found no \"Containers:\" paragraph in docs/SCENES.md — the primitive vocabulary\n" +
			"is this axis's denominator and the audit would pass vacuously")
	}

	seen := make(map[string]bool)
	var out []string
	for _, m := range vocabularyTypePattern.FindAllStringSubmatch(block.String(), -1) {
		typ := m[1]
		// `anchor` and `row_template` appear in the paragraph as property
		// names of the types being listed, not as types. They are named
		// here rather than pattern-matched away because a silent filter is
		// how a denominator quietly shrinks.
		if typ == "anchor" || typ == "row_template" || typ == "full" {
			continue
		}
		if seen[typ] {
			continue
		}
		seen[typ] = true
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// renderedNodeTypes returns the node types renderNode dispatches on, read from
// the AST of that function specifically.
//
// Scoping it to renderNode matters: the engine's other switches are on bind
// names, border shapes and overlay anchors, and a package-wide sweep for
// string case labels would report "double", "ascii" and "top-right" as node
// types. The signed-bind audit's package-wide sweep filters on a dot in the
// label, which works for binds and would silently mis-scope here.
func renderedNodeTypes(t *testing.T) map[string]bool {
	t.Helper()
	out := make(map[string]bool)
	for _, file := range parseEngineSource(t) {
		ast.Inspect(file, func(n ast.Node) bool {
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "renderNode" {
				return true
			}
			ast.Inspect(fn, func(inner ast.Node) bool {
				cc, ok := inner.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, expr := range cc.List {
					if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						out[strings.Trim(lit.Value, "`\"")] = true
					}
				}
				return true
			})
			return false
		})
	}
	return out
}

// animationPropertiesFromDocument reads Scene 4's vocabulary paragraph — the
// first paragraph of the section, which lists the properties, and not the
// prose that follows it.
//
// The narrower scope is a defect this audit produced against itself, and it is
// worth the words because the cause is general. The first version read the
// whole Scene 4 section to the next heading. Then the feat commit added an
// "Implementation status" paragraph to that section explaining why four
// properties stay warnings, and that explanation mentions `row_template`,
// `on_press` and `id` in backticks as comparisons. The parser counted all
// three as animation properties: the denominator went from 5 to 8, and the
// honoured fraction fell from 1/5 to 1/8 without one line of engine code
// changing.
//
// The direction is the mild one — a denominator that grows understates
// progress, so this was a false alarm rather than a false pass. But a
// denominator that moves when someone writes a sentence is not a measurement,
// which is the entire charge this file levels against the "45-50%" figure it
// replaced. An instrument whose reading depends on the prose around its
// subject is measuring the prose.
//
// So the block is the property list specifically. A paragraph boundary is the
// honest delimiter because the document's own structure puts the vocabulary in
// one paragraph and the commentary in the others — the same convention
// documentedNodeTypes relies on for "Containers:".
//
// `scroll` is named here as well as in the Universal list and is not filtered
// out: it is genuinely both, and dropping it would make the denominator
// disagree with the document to make a number look better — the exact move
// this file exists to stop.
var animationPropertyPattern = regexp.MustCompile("`([a-z_]+)(?::|`)")

func animationPropertiesFromDocument(t *testing.T) []string {
	t.Helper()
	data := readDoc(t, "SCENES.md")
	lines := strings.Split(data, "\n")

	var block strings.Builder
	for i, line := range lines {
		if !strings.HasPrefix(line, "## Scene 4") {
			continue
		}
		// Skip the blank line after the heading, then take exactly the
		// paragraph that follows: it is the vocabulary list. Stopping at
		// the blank line is what keeps later commentary — which names
		// other properties in backticks to contrast with them — out of
		// this axis's denominator.
		started := false
		for _, l := range lines[i+1:] {
			if strings.HasPrefix(l, "## ") {
				break
			}
			if strings.TrimSpace(l) == "" {
				if started {
					break
				}
				continue
			}
			started = true
			block.WriteString(l)
			block.WriteString("\n")
		}
		break
	}
	if block.Len() == 0 {
		t.Fatal("found no \"## Scene 4\" section in docs/SCENES.md — the animation debt is this\n" +
			"axis's denominator and the audit would report it as paid")
	}

	seen := make(map[string]bool)
	var out []string
	for _, m := range animationPropertyPattern.FindAllStringSubmatch(block.String(), -1) {
		prop := m[1]
		// `anim` is the token namespace of Q8, not a node property, and
		// the sub-keys of the two compound properties belong to their
		// parent. Named explicitly for documentedNodeTypes's reason.
		switch prop {
		case "anim", "speed", "pause_when", "row", "stagger":
			continue
		}
		if seen[prop] {
			continue
		}
		seen[prop] = true
		out = append(out, prop)
	}
	sort.Strings(out)
	return out
}

// renderedAnimationProperties reports which animation properties the engine
// actually reads, by finding selector expressions that resolve to a field read
// on scene.Node in the engine source (n.FocusGlow, n.Reveal, …).
//
// It asks the source rather than rendering a probe document because the
// behavioural question is answered next door in
// TestEveryUniversalPropertyIsHonouredOrRefused and in the focus_glow frame
// tests; what this axis needs is the count, and a count derived from a probe
// table would be a hand-maintained list of the very properties it counts.
//
// It type-checks rather than comparing selector names, and that is the whole
// point of this helper. Every ceiling in this file guards a *denominator*; the
// numerators were never guarded, and this one had the same defect in the more
// dangerous direction. The old matcher asked `sel.Sel.Name == goName` — a bare
// identifier, with no receiver — so *any* selector ending in the field's name
// counted the property as honoured, including ones that are not field reads at
// all. Measured against today's engine, a Scene 4 property declared as a field
// named:
//
//	Repeat   -> honoured, from strings.Repeat  at render.go:78
//	Cut      -> honoured, from ansi.Cut        at render.go:41
//	Truncate -> honoured, from ansi.Truncate   at render.go:91
//	Span     -> honoured, from ui.Span         at render.go:40
//
// none of which read the node. The direction is what matters: this axis fails
// only on `silent`, and `rendered[p]` is tested first in the classification, so
// a false honoured entry does not merely inflate a count — it pre-empts the one
// state that can fail. A half-finished property whose field happens to be named
// after a stdlib helper reports as *done*, and the subtest that exists to catch
// silent drops goes green. That is the same shape as the universals ceiling
// added last turn: the quantity that can fail is pinned to zero by the very
// thing that is wrong.
//
// `strings.Repeat` is not a hypothetical collision either — `repeat` is an
// ordinary name for an animation property, and the engine already calls
// strings.Repeat ten times to build rules, padding and box edges.
//
// go/types answers the real question — is this selector a field read, and is
// its receiver scene.Node — for the cost of a typecheck of one package, 1.3s
// measured. The precedent is signed three times over in this package: R19h's
// reflected copy, the unrenderedFields map, and this axis's own first version
// all inferred a behavioural fact from a structural proxy, and all three
// produced a false clean bill of health. When a guard can ask the artifact
// directly, a proxy is not a shortcut, it is a different question.
//
// The counterfactual was run rather than argued, because "the old code would
// have missed this" is exactly the kind of claim this file exists to distrust.
// `transition` — a real documented property that nothing in the engine reads —
// was declared as a field named `Repeat`, and the same ledger was printed with
// each matcher:
//
//	old (name only):  5 documented, 2 honoured, 3 warned, 0 silent   PASS
//	                  honoured: focus_glow, transition
//	new (go/types):   5 documented, 1 honoured, 3 warned, 1 silent   FAIL
//	                  honoured: focus_glow
//
// The old ledger reported a property as implemented that no line of the engine
// reads, and reported it under the invocation the header of this file
// documents. That is the "45-50%" defect reappearing inside the instrument
// built to replace it — which is the argument for holding a measuring tool to
// the standard it measures by.
func renderedAnimationProperties(t *testing.T) map[string]bool {
	t.Helper()
	fields := scene.AnimationFieldsForAudit()
	if len(fields) == 0 {
		// Not a fatal: before any Scene 4 property lands there are no
		// fields, and the axis correctly reports zero honoured.
		return map[string]bool{}
	}

	byGoName := make(map[string]string, len(fields))
	for jsonName, goName := range fields {
		byGoName[goName] = jsonName
	}

	used := make(map[string]bool)
	for sel, selection := range engineSelections(t) {
		// FieldVal is the discriminator the name comparison lacked:
		// strings.Repeat and ansi.Cut are package members, ui.Span is a
		// type, and none of them is a field value.
		if selection.Kind() != types.FieldVal {
			continue
		}
		jsonName, ok := byGoName[sel.Sel.Name]
		if !ok {
			continue
		}
		// The receiver must be scene.Node itself. A field of the same
		// name on some other struct is a different property, and this
		// axis reports on the scene format.
		if !isSceneNode(selection.Recv()) {
			continue
		}
		used[jsonName] = true
	}
	return used
}

// isSceneNode reports whether a selector's receiver is scene.Node, through any
// number of pointers. Named rather than inlined because "which type is this
// really" is the question the old matcher skipped.
func isSceneNode(typ types.Type) bool {
	for {
		ptr, ok := typ.(*types.Pointer)
		if !ok {
			break
		}
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "github.com/michiTrader/arxi_tui/internal/scene" &&
		obj.Name() == "Node"
}

// engineSelections type-checks the engine package and returns every resolved
// selector in it.
//
// The typecheck is required to fail loudly. A types.Config with a swallowing
// Error hook returns partial information on a broken build, and partial
// information here means "no selector resolved to a field read", which is
// indistinguishable from "the engine honours nothing" — a silent zero in the
// numerator of a progress report. That is the precise failure this file was
// written to stop, so the premise is asserted instead of assumed.
func engineSelections(t *testing.T) map[*ast.SelectorExpr]*types.Selection {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read engine package directory: %v", err)
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("parsed no non-test files from the engine package; every source-derived axis\n" +
			"would read as empty and the audit would pass vacuously")
	}

	info := &types.Info{Selections: make(map[*ast.SelectorExpr]*types.Selection)}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("github.com/michiTrader/arxi_tui/internal/engine", fset, files, info); err != nil {
		t.Fatalf("type-check the engine package: %v\n"+
			"consequence: without type information no selector can be resolved to a field read,\n"+
			"so every animation property reads as unhonoured and the axis reports a numerator of\n"+
			"zero — a measurement failure that looks exactly like a project that implemented\n"+
			"nothing.\n"+
			"remedy: fix the build error; this audit cannot measure a package that does not\n"+
			"type-check.", err)
	}
	if len(info.Selections) == 0 {
		t.Fatal("the engine package type-checked and produced no selector expressions at all;\n" +
			"the numerator of the animation axis would be zero for a reason that has nothing to\n" +
			"do with the engine.")
	}
	return info.Selections
}

// observeAnimationProperty loads a document that sets prop on a text node and
// reports whether the format said anything about it — a warning or a refusal,
// either of which is an addressed answer to the author.
//
// It is the behavioural half of the animation axis, and it asks the production
// load path rather than inspecting a map, for the reason recorded in the
// subtest above: membership in a vocabulary is not the same question as
// whether a document is told anything, and substituting one for the other
// inverted the whole classification.
func observeAnimationProperty(prop string) (bool, error) {
	src := fmt.Sprintf(
		`{"root":{"id":"root","type":"stack","children":[{"id":"probe","type":"text","text":"x","%s":{"a":1}}]}}`,
		prop)
	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		// A parse error is itself an addressed answer, not a silent drop.
		return true, nil
	}
	if len(doc.Warnings()) > 0 {
		return true, nil
	}
	if doc.Validate() != nil {
		return true, nil
	}
	return false, nil
}

func parseEngineSource(t *testing.T) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse engine package: %v", err)
	}
	var out []*ast.File
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			out = append(out, file)
		}
	}
	if len(out) == 0 {
		t.Fatal("parsed no non-test files from the engine package; every source-derived axis\n" +
			"would read as empty and the audit would pass vacuously")
	}
	return out
}

func readDoc(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — it is the contract this audit measures against", path, err)
	}
	return string(data)
}

func containsFold(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}
