package engine

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The SCENES <-> engine audit: every property SCENES.md calls universal must
// produce an observable effect — it is either honoured, or refused with an
// address. Silence is the one answer the format may not give.
//
// The question is behavioural, and the audit this replaces is why. That one
// lived in internal/scene and asked whether each universal was a json tag on
// Node, skipping the ones listed in a map of accepted gaps. Both halves could
// be satisfied without the defect being fixed, and measured together they
// were: deleting the `on_press`/`scroll` fields *and* their refusals returned
// the entire suite to green (injection R16a), because with the fields gone the
// skip-list answered on the engine's behalf. Nothing failed, so nothing held
// the fix in place.
//
// That is the project's recurring shape aimed at the instrument for the second
// turn running. Last turn the unrenderedFields map's advertised remedy was a
// no-op; this turn the remedy was a no-op *by construction*, because a map in
// a _test.go file cannot change what the parser does — recording a gap there
// silences the audit and ships the defect. The general lesson, now paid for
// twice: when a guard offers an escape hatch, the hatch is part of the guard,
// and an escape hatch that lives where behaviour cannot is not an exemption
// but a blindfold.
//
// So there is no skip-list here. A universal is satisfied by one of exactly
// two observable behaviours, and nothing else counts:
//
//  1. the document changes the frame — the property is honoured; or
//  2. the document is refused with an address — the property is a known gap,
//     stated to the author's face.
//
// A property that is neither is a silent drop: the scene loads, reports
// success, and the property never happened, so no diagnostic exists anywhere
// and the author's only evidence is a screen that looks wrong.
//
// Why the bar is "refused or honoured" rather than "implemented". PLAN.md's
// forward-compatibility rule is "unknown-but-parseable is a warning", and the
// engine already honours it for node *types*: `button`, `switch`, `slider` and
// `sparkline` are documented, unimplemented, and each draws
// [[UNKNOWN NODE TYPE]], so a v0 document keeps booting under v1 and the
// screen says what it could not do. Properties had the opposite behaviour.
// Making the refusal the floor closes that asymmetry without inventing the
// action vocabulary (Q18, Phase 3) or the animation clock (Q8/Q9, Phase 4)
// ahead of the phases meant to design them.
//
// It lives in internal/engine rather than internal/scene because the question
// is now about frames, and engine imports scene rather than the reverse. That
// is also the right side of the boundary for the audit's own honesty: the
// validating package cannot be the one that certifies the renderer.
//
// The vocabulary is read out of docs/SCENES.md rather than copied into Go, on
// the binds-audit precedent: a hand-copied list has already drifted here in
// both directions, and its failure mode is the expensive one — the audit
// agrees with the code and the contract is the thing nobody checked.
func TestEveryUniversalPropertyIsHonouredOrRefused(t *testing.T) {
	for _, prop := range universalsFromDocument(t) {
		t.Run(prop, func(t *testing.T) {
			probe, ok := universalProbes[prop]
			if !ok {
				t.Fatalf("SCENES.md calls %q universal and this audit has no document that sets it,\n"+
					"so the property's behaviour is unmeasured. An entry nobody exercises is how the\n"+
					"unrenderedFields map became decoration one turn ago.\n"+
					"remedy: add a probe pair for %q to universalProbes — one document setting it, one\n"+
					"without — so the audit can ask whether anything can tell them apart.", prop, prop)
			}

			withDoc, err := scene.ParseDocument([]byte(probe.with))
			if err != nil {
				t.Fatalf("premise broken: the %q probe must parse; got %v", prop, err)
			}
			withoutDoc, err := scene.ParseDocument([]byte(probe.without))
			if err != nil {
				t.Fatalf("premise broken: the %q control must parse; got %v", prop, err)
			}

			// The control has to validate clean, or a refusal below could
			// be about anything at all and the probe would prove nothing
			// about the property. This is the vacuity guard the frame
			// assertion three turns ago did not have.
			if verr := withoutDoc.Validate(); verr != nil {
				t.Fatalf("premise broken: the %q control document must validate clean, or a refusal\n"+
					"of the probe cannot be attributed to the property; got %v", prop, verr)
			}

			// Outcome 2: refused with an address. Checked first because it
			// is the honest answer for a property no phase has built yet,
			// and because a refused document never reaches a frame.
			if verr := withDoc.Validate(); verr != nil {
				var se *scene.Error
				if !errors.As(verr, &se) {
					t.Fatalf("%q is refused, but not as a *scene.Error, so the refusal carries no\n"+
						"address (PLAN.md invariant 4): %v", prop, verr)
				}
				if se.Loc == (scene.Loc{}) {
					t.Errorf("the refusal of %q carries no address: %v", prop, verr)
				}
				if !strings.Contains(verr.Error(), prop) {
					t.Errorf("the refusal does not name %q, so the author cannot tell which property\n"+
						"was rejected: %q", prop, verr.Error())
				}
				return
			}

			// Outcome 3: the property is addressing rather than drawing,
			// so identical frames are correct and the two outcomes above
			// cannot apply. There is exactly one such property and it is
			// named here rather than left to a map, because a map is how
			// the previous version of this audit let two real gaps out.
			//
			// The exemption is not a skip. A skipped case proves nothing
			// and would survive the field being deleted — which is the
			// precise failure this rewrite exists to fix. So the property
			// is held to the strongest bar that is true of it: the parser
			// must round-trip it. That fails if `id` is removed from
			// scene.Node, so the exemption cannot outlive the thing it
			// exempts.
			if prop == "id" {
				assertRoundTrips(t, prop, probe.with)
				return
			}

			// Outcome 1: accepted, so it must change what is drawn. A
			// property that validates clean and renders identically to its
			// own absence is the silent drop.
			if renderedIdentically(t, withDoc, withoutDoc) {
				t.Errorf("SCENES.md calls %q a universal property, the validator accepts it, and a\n"+
					"document setting it renders byte-identically to one that does not.\n"+
					"consequence: the silent drop. The scene loads, reports success, and the property\n"+
					"never happened — no refusal, no warning, no placeholder, so there is no\n"+
					"diagnostic anywhere and the author's only evidence is a screen that looks wrong.\n"+
					"It also contradicts PLAN.md's forward-compatibility rule: an unknown node *type*\n"+
					"draws [[UNKNOWN NODE TYPE]] and keeps the document booting with a visible reason,\n"+
					"so the format would treat its two kinds of unknown construction in opposite ways\n"+
					"— and the silent kind is the one the documentation calls universal.\n"+
					"remedy: render it; or declare the field on scene.Node and list it in\n"+
					"scene.unrenderedFields, which refuses it with an address. Recording the gap in a\n"+
					"map inside a _test.go file is not a remedy: such a map cannot change what the\n"+
					"parser does, so it silences the audit and ships the defect. That was measured —\n"+
					"the previous version of this audit passed in exactly that state.", prop)
			}
		})
	}
}

// universalProbe is a matched pair: one document that sets the property and
// one that does not, identical in every other respect. The pair is what lets
// the audit ask an engine question — "can anything tell these apart?" — rather
// than a source-shaped one. A source question is what the previous version
// asked, and a source question can be answered by deleting the source.
type universalProbe struct {
	with    string
	without string
}

// universalProbes is one pair per universal property in SCENES.md. A property
// with no pair fails the audit rather than skipping: an unexercised entry is
// how the unrenderedFields map became decoration, and the point of this
// rewrite is that nothing here may answer on the engine's behalf.
//
// The probes use ordinary nodes on purpose. "Universal" means any node may
// carry the property, so a probe built on an exotic node type would prove the
// rule for one case and leave the promise untested everywhere else.
var universalProbes = map[string]universalProbe{
	"id": {
		// Measured: nothing in internal/engine reads Node.ID, so the two
		// frames are identical by design — id addresses a node for
		// patches (Phase 2's /ui commands name nodes by it), it does not
		// draw. That is the one honest third outcome, and the audit
		// handles it by name with a round-trip assertion rather than by
		// skipping; see the `prop == "id"` arm above.
		with:    `{ "root": { "id": "named", "type": "text", "text": "hello" } }`,
		without: `{ "root": { "type": "text", "text": "hello" } }`,
	},
	"bind": {
		with:    `{ "root": { "type": "text", "bind": "model.name" } }`,
		without: `{ "root": { "type": "text" } }`,
	},
	"when": {
		// The gate has to be *false* for the pair to differ. A first draft
		// used a bind the probe state sets to a truthy value, so the node
		// drew either way and the frames matched — the audit reported a
		// silent drop for the one universal the previous turn's work
		// proved is honoured on every node type. A probe that cannot
		// distinguish the two cases measures the probe, which is the same
		// error as an injection that fails more than the defect.
		// `ui.max` is empty in the populated state, so the node hides.
		with:    `{ "root": { "type": "text", "text": "hello", "when": "ui.max" } }`,
		without: `{ "root": { "type": "text", "text": "hello" } }`,
	},
	"style": {
		with:    `{ "root": { "type": "text", "text": "hello", "style": { "style": "dim" } } }`,
		without: `{ "root": { "type": "text", "text": "hello" } }`,
	},
	"grow": {
		with: `{ "root": { "type": "stack", "children": [
		  { "type": "text", "text": "a", "grow": 1 },
		  { "type": "text", "text": "b" } ] } }`,
		without: `{ "root": { "type": "stack", "children": [
		  { "type": "text", "text": "a" },
		  { "type": "text", "text": "b" } ] } }`,
	},
	"weight": {
		with: `{ "root": { "type": "row", "children": [
		  { "type": "text", "text": "aaaa", "weight": 5 },
		  { "type": "text", "text": "bbbb", "weight": 1 } ] } }`,
		without: `{ "root": { "type": "row", "children": [
		  { "type": "text", "text": "aaaa" },
		  { "type": "text", "text": "bbbb" } ] } }`,
	},
	"on_press": {
		with:    `{ "root": { "type": "text", "text": "hello", "on_press": "cmd:/help" } }`,
		without: `{ "root": { "type": "text", "text": "hello" } }`,
	},
	"scroll": {
		with: `{ "root": { "type": "markdown", "bind": "chat.history",
		  "scroll": { "speed": 2, "pause_when": "agent.working" } } }`,
		without: `{ "root": { "type": "markdown", "bind": "chat.history" } }`,
	},
}

// assertRoundTrips is the bar for a universal that addresses rather than
// draws: the parser must keep the key, so a document that sets it still
// carries it after a parse.
//
// This is what makes the `id` exemption load-bearing instead of decorative. It
// fails the moment the field stops being part of scene.Node's vocabulary —
// which is exactly the state that made `on_press` and `scroll` invisible, and
// exactly what the skip-list this replaces could not detect.
func assertRoundTrips(t *testing.T, prop, body string) {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: the %q probe must parse; got %v", prop, err)
	}
	if doc.Root == nil {
		t.Fatalf("premise broken: the %q probe has no root", prop)
	}
	if doc.Root.ID == "" {
		t.Errorf("SCENES.md calls %q universal and a document setting it parses into a node\n"+
			"that does not carry it.\n"+
			"consequence: encoding/json discarded the key in silence — the scene parses,\n"+
			"validates and renders with the property gone before any layer could have an\n"+
			"opinion. %q is how Phase 2's /ui commands address a node, so losing it makes\n"+
			"every patch that names a node unaddressable, with no diagnostic.\n"+
			"remedy: keep the field on scene.Node; this property is exempt from the frame\n"+
			"comparison because it addresses rather than draws, not from existing.", prop, prop)
	}
}

// renderedIdentically reports whether two documents draw the same frame.
//
// The state is populated rather than zero, and that is load-bearing: an empty
// fold makes every bound node draw its placeholder, so two documents differing
// only in a bind would match and the audit would report a silent drop that is
// really an empty transcript. Styles are part of the comparison, because
// `style` is one of the properties under test and a plain-text comparison
// would call a working token dropped.
func renderedIdentically(t *testing.T, a, b *scene.Document) bool {
	t.Helper()
	r := Renderer{Width: 40, Height: 8}
	draw := func(d *scene.Document) string {
		f := r.RenderFrame(d, populatedProbeState())
		var sb strings.Builder
		for _, line := range f.Live {
			for _, span := range line {
				sb.WriteString(span.Style)
				sb.WriteByte(0)
				sb.WriteString(span.Text)
				sb.WriteByte(0)
			}
			sb.WriteByte('\n')
		}
		return sb.String()
	}
	return draw(a) == draw(b)
}

// populatedProbeState is a fold with every bind the probes touch actually set,
// so "the frames match" means the property did nothing rather than that the
// state was empty.
func populatedProbeState() fold.State {
	s := fold.Fold(nil)
	s.History = []fold.ChatLine{
		{Role: "user", Text: "a prompt line that is long enough to wrap once"},
		{Role: "assistant", Text: "an answer line that is also long enough to wrap"},
	}
	s.ModelName = "openai/gpt-4o"
	s.AgentWorking = true
	s.BlockedActor = "backend"
	s.SlashMatches = []fold.SlashMatch{
		{Name: "help", Category: "General", Description: "Show available commands"},
		{Name: "model", Category: "Model", Description: "Switch model"},
	}
	return s
}

// universalPropertyPattern matches the backticked names in SCENES.md's
// "Universal:" paragraph, which by the document's convention lists each
// property that way.
//
// The anchored `+` is doing real work and is the reason there is no prose-name
// filter beside this pattern. The paragraph also backticks *examples* while
// explaining `bind` — "absolute `path.state` or relative `row.field` inside a
// template" — and an earlier draft carried a skip-list for those. Measured:
// the list never fired once, because the pattern requires the whole backticked
// span to be [a-z_] and a dotted example cannot match. It was a guard nobody
// could observe doing anything, and deleting it changed no result. What
// protects the parse instead is the floor below.
var universalPropertyPattern = regexp.MustCompile("`([a-z_]+)`")

// universalsFromDocument reads docs/SCENES.md and returns the properties its
// vocabulary section calls universal. The document is the contract; parsing it
// is what keeps this audit honest when the contract changes.
func universalsFromDocument(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "SCENES.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the vocabulary section is the contract this audit checks", path, err)
	}

	block := universalParagraph(strings.Split(string(data), "\n"))
	if block == "" {
		t.Fatalf("found no \"Universal:\" paragraph in %s — the audit would pass vacuously;\n"+
			"the vocabulary section's format likely changed and this parser must follow it", path)
	}

	seen := make(map[string]bool)
	var out []string
	for _, m := range universalPropertyPattern.FindAllStringSubmatch(block, -1) {
		prop := m[1]
		if seen[prop] {
			continue
		}
		seen[prop] = true
		out = append(out, prop)
	}

	// The floor guards against a pattern that silently stops matching. The
	// paragraph lists seven properties today; a parser returning one or two
	// is broken rather than lucky, and a vacuous pass here would hide
	// exactly the class of gap the audit exists to find.
	if len(out) < 5 {
		t.Fatalf("parsed only %d universal properties from %s (%v); the audit is reading the\n"+
			"wrong paragraph and would pass vacuously", len(out), path, out)
	}
	sort.Strings(out)
	return out
}

// universalParagraph extracts the "Universal:" paragraph, which already wraps
// across two lines in the document today — so reading a single line would
// silently drop half the vocabulary, including both properties this audit was
// written to report.
func universalParagraph(lines []string) string {
	nextTopic := regexp.MustCompile(`^[A-Z][a-z]+ `)
	for i, line := range lines {
		if !strings.HasPrefix(line, "Universal:") {
			continue
		}
		block := []string{line}
		for _, next := range lines[i+1:] {
			if next == "" || nextTopic.MatchString(next) {
				break
			}
			block = append(block, next)
		}
		return strings.Join(block, "\n")
	}
	return ""
}
