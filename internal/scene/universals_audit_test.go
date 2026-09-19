package scene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The SCENES <-> Node audit: every property SCENES.md calls universal must be
// a field the parser keeps.
//
// TestEveryNodeFieldIsEitherRenderedRefusedOrJustified guards the other
// direction — a field on Node that nothing renders. Both are needed, and the
// asymmetry between them is a real blind spot rather than symmetry for its own
// sake. That audit enumerates the fields of Node and asks what reads them, so
// it can only see properties that reached the struct. A property the format
// promises and Node never declared is invisible to it: there is no field to
// enumerate, so the audit reports full coverage precisely *because* the
// property is missing entirely.
//
// This is the same defect class the project has now paid for five times — a
// construction that clears every check and never reaches the screen — but the
// mechanism is one step earlier than the four before it. Those were fields the
// validator understood and the renderer dropped. These are properties no layer
// ever knew about: encoding/json discards unknown keys in silence, so the
// document parses, validates, renders, and the property is gone before any
// code could have had an opinion about it.
//
// Measured on `on_press` and `scroll`, both listed as universal in SCENES.md's
// vocabulary section: a text node with `on_press` and one without produce
// byte-identical frames, and a markdown node's `scroll: {speed, pause_when}` —
// the Scene 4 animation property — changes nothing either. Neither is refused,
// neither warns, and neither survives a round-trip through the parser.
//
// Why this matters more than the missing feature does. PLAN.md's forward-
// compatibility rule is "unknown-but-parseable is a warning", and the engine
// honours it for node *types*: an unknown type draws [[UNKNOWN NODE TYPE]] on
// screen, which is how a v0 document keeps booting under v1 while telling the
// reader what it could not do. Properties get no such treatment. So the format
// has two classes of unknown construction with opposite behaviour, and the
// silent one is the class the documentation calls universal.
//
// The fixture here is deliberately the document rather than a list written in
// Go. A hand-copied list is what the bind inventory already drifted from in
// both directions, and its failure mode is the expensive one: the audit agrees
// with the code, and the contract is the thing nobody checked.
func TestEveryUniversalPropertyInTheDocumentIsAFieldTheParserKeeps(t *testing.T) {
	universals := universalsFromDocument(t)

	// The struct's json tags are the parser's actual vocabulary: a key not
	// among them is discarded by encoding/json without a word.
	kept := jsonTagsOfNode(t)

	for _, prop := range universals {
		t.Run(prop, func(t *testing.T) {
			if kept[prop] {
				return
			}
			if reason, ok := acceptedAbsentUniversals[prop]; ok {
				t.Skipf("%s (justified: %s)", prop, reason)
			}
			t.Errorf("SCENES.md calls %q a universal property and scene.Node has no field for it.\n"+
				"consequence: encoding/json drops the key silently. A scene setting %q parses,\n"+
				"validates, renders, and the property never existed — no refusal, no warning, no\n"+
				"placeholder. This is worse than the unrendered-field class the other audit\n"+
				"covers, because there is no field for that audit to enumerate: it reports full\n"+
				"coverage exactly because the property is missing.\n"+
				"It also contradicts PLAN.md's forward-compatibility rule. An unknown node *type*\n"+
				"draws [[UNKNOWN NODE TYPE]] and keeps the document booting with a visible\n"+
				"reason; an unknown *property* says nothing at all, so the format treats its two\n"+
				"kinds of unknown construction in opposite ways.\n"+
				"remedy: add the field and render it; or add it to acceptedAbsentUniversals with\n"+
				"the phase that owns it, so the gap is recorded rather than rediscovered.",
				prop, prop)
		})
	}
}

// acceptedAbsentUniversals are properties the document promises that Node does
// not carry, and that is the honest state for now. Each entry names the phase
// that owns the work, so the entry is a dated debt rather than a shrug.
//
// They are recorded rather than implemented for the same reason row_template is
// refused rather than drawn: both are behaviour, and behaviour is Phase 3 and
// Phase 4 in PLAN.md. Adding a field now would mean inventing the action
// vocabulary and the animation clock ahead of the phases meant to design them —
// the mistake PLAN.md names about pinning a golden before its phase lands.
//
// What is NOT acceptable, and is why this map holds reasons rather than just
// names: shipping them as silent no-ops. A scene that says on_press today gets
// no refusal and no placeholder, which is the one outcome the project has
// decided never to produce. Whichever way that is settled — a field plus a
// visible placeholder, or a refusal with an address like row_template's — this
// map is the list of decisions owed.
var acceptedAbsentUniversals = map[string]string{
	"on_press": "actions are a closed per-surface vocabulary (SCENES.md Q18) and the " +
		"interaction model is Phase 3/4; no field, and today no diagnostic either",
	"scroll": "scroll: {speed, pause_when} is Scene 4 animation on the host clock " +
		"(SCENES.md Q8/Q9), which PLAN.md schedules after the golden set",
}

// universalPropertyPattern matches the backticked names in SCENES.md's
// "Universal:" paragraph, which by the document's convention lists each
// property that way.
//
// The anchored `+` is doing real work and is the reason there is no
// prose-name filter beside this pattern. The paragraph also backticks
// *examples* while explaining `bind` — "absolute `path.state` or relative
// `row.field` inside a template" — and a first draft carried a skip-list for
// path/state/row/field/template to keep those out. Measured: the list never
// fired once. A dotted example does not match, because the pattern requires
// the whole backticked span to be [a-z_]. The list was a guard that could not
// be observed doing anything, and deleting it changed no result — which is the
// definition the `when` work arrived at for a guard worth removing rather than
// keeping. What replaces it is the floor below, which fails loudly if this
// pattern ever stops matching the real vocabulary.
var universalPropertyPattern = regexp.MustCompile("`([a-z_]+)`")

// universalsFromDocument reads docs/SCENES.md and returns the properties its
// vocabulary section calls universal. The document is the contract; parsing it
// is what keeps this test honest when the contract changes.
func universalsFromDocument(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "SCENES.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the vocabulary section is the contract this test audits", path, err)
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
	// exactly the class of gap the test exists to find.
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

// jsonTagsOfNode returns the set of json key names scene.Node declares. These
// are exactly the keys encoding/json will keep; anything else in a document is
// discarded without a diagnostic, which is the behaviour this audit bounds.
func jsonTagsOfNode(t *testing.T) map[string]bool {
	t.Helper()
	fields := nodeFieldsFromSource(t)
	if len(fields) < 15 {
		t.Fatalf("parsed only %d fields off Node; the audit is reading the wrong type", len(fields))
	}
	tags := make(map[string]bool, len(fields))
	for _, f := range fields {
		if f.jsonName != "" {
			tags[f.jsonName] = true
		}
	}
	return tags
}
