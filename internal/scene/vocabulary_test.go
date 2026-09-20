package scene

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The guard for the fifth instance of the silent-drop class, and the first
// aimed at the mechanism rather than at one field.
//
// The four before it were each repaired by naming a field, and each repair was
// correct and too narrow, because the thing doing the dropping was never a
// field — it was encoding/json ignoring every key outside Node's tags. So the
// assertions here are about keys the parser does not know, not about any
// particular one: a test enumerating the six properties measured missing would
// be the same hand-maintained inventory that has now drifted three times in
// this package alone.

// The typo case, which is the one that costs a user their screen rather than a
// feature they were told is unimplemented. `chidlren` is not a documented gap
// or a forward-compatibility question; it is two transposed letters that
// delete a subtree while every layer reports success.
func TestAMisspelledKeyIsReportedRatherThanSwallowed(t *testing.T) {
	body := `{ "root": { "type": "stack", "chidlren": [
	  { "type": "text", "text": "the whole transcript" },
	  { "type": "input", "prefix": "> " } ] } }`

	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: the document must parse; a typo'd key is valid JSON: %v", err)
	}

	// The premise of the whole test: the subtree really is gone. If a
	// future change made encoding/json strict, this test would be asserting
	// something no longer true and should be rewritten rather than deleted.
	if len(doc.Root.Children) != 0 {
		t.Fatalf("premise broken: %q was expected to be dropped by encoding/json, but the node has %d children",
			"chidlren", len(doc.Root.Children))
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("premise broken: the document must validate clean, or the warning is not the\n"+
			"only thing standing between the author and a blank screen; got %v", err)
	}

	warnings := doc.Warnings()
	if len(warnings) == 0 {
		t.Fatalf("a node declared %q, encoding/json dropped it, and both children with it — and\n"+
			"nothing said so.\n"+
			"consequence: the silent drop at its worst. Two transposed letters delete the entire\n"+
			"subtree; the document parses, validates clean, and renders an empty stack. Every\n"+
			"layer reports success, so the author's only evidence is a screen that is missing\n"+
			"the interface. This is not a documented gap the author can look up — it is a typo,\n"+
			"and a typo the format refuses to mention is indistinguishable from an engine bug.\n"+
			"remedy: Warnings() must report every key outside the parser's vocabulary.", "chidlren")
	}

	w := warnings[0]
	if !strings.Contains(w.Msg, "chidlren") {
		t.Errorf("the warning does not name the key, so the author cannot find it: %q", w.Msg)
	}
	if w.Loc.Line == 0 {
		t.Errorf("the warning carries no line (PLAN.md invariant 4): %q", w.String())
	}
	// The address must be the node, not the phantom path the typo invented.
	// Recording the key against "root.chidlren" would file it under an
	// address that exists only because of the mistake being reported.
	if w.Loc.Line != 1 {
		t.Errorf("the warning addresses line %d; the node that declared the key opens on line 1.\n"+
			"An address pointing somewhere other than the offending node sends the author to\n"+
			"the wrong place, which is the failure BINDS.md §4.5 names.", w.Loc.Line)
	}
}

// A document whose whole tree sits under the wrong top-level key parses into a
// nil root and validates clean. Nothing renders, and nothing says why.
func TestAMisnamedRootIsReportedRatherThanSwallowed(t *testing.T) {
	doc, err := ParseDocument([]byte(`{ "scene": { "type": "text", "text": "hi" } }`))
	if err != nil {
		t.Fatalf("premise broken: the document must parse: %v", err)
	}
	if doc.Root != nil {
		t.Fatalf("premise broken: %q was expected to leave Root nil", "scene")
	}

	err = doc.RefuseEmpty()
	if err == nil {
		t.Fatalf("a document with no root at all was accepted.\n" +
			"consequence: the interface draws nothing and the author is told nothing. Unlike an\n" +
			"unknown property — which is a forward-compatibility question PLAN.md answers with a\n" +
			"warning — a document with no tree cannot be a later version of anything: there is no\n" +
			"scene to render under any engine, so accepting it can only ever hide a mistake.\n" +
			"remedy: refuse it with an address; the fallback to the raw scene (invariant 3) is\n" +
			"exactly the right outcome, and it only happens if the load path is told.")
	}
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("the refusal is not a *scene.Error, so it carries no address (invariant 4): %v", err)
	}
}

// The general rule, stated as behaviour: a key the parser does not know is
// warned about. The probe is an invented key rather than a documented one on
// purpose — it cannot be satisfied by implementing a feature, so the test
// keeps measuring the mechanism after every gap in SCENES.md is closed.
func TestAnUnknownKeyIsWarnedAboutWithAnAddress(t *testing.T) {
	doc, err := ParseDocument([]byte(
		`{ "root": { "type": "text", "text": "hi", "totally_invented_key": 7 } }`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	warnings := doc.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning for one unknown key, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0].Msg, "totally_invented_key") {
		t.Errorf("the warning does not name the key: %q", warnings[0].Msg)
	}
	if warnings[0].Loc.Line == 0 {
		t.Errorf("the warning carries no address: %q", warnings[0].String())
	}
}

// The counter-assertion, and the one that decides whether this guard survives
// contact with the codebase. A check that fires on documents the project ships
// is a check somebody turns off.
//
// The three golden scenes are the strongest available statement of "correct
// document", and SOBRIA is the factory interface invariant 1 pins
// byte-for-byte. If the vocabulary warned about anything in them, either the
// vocabulary is wrong or the shipped scenes are — and both are findings that
// must stop the suite rather than train a reader to ignore output.
func TestTheShippedScenesWarnAboutNothing(t *testing.T) {
	for _, name := range []string{"RAW.json", "SOBRIA.json", "MAXIMUM.json"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			doc, err := ParseNamed(name, data)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			for _, w := range doc.Warnings() {
				t.Errorf("%s: %s\n"+
					"consequence: a scene this project ships trips its own vocabulary check. Either\n"+
					"the key is real and nodeVocabulary is missing it — in which case every user\n"+
					"document using it gets a false alarm and the check becomes noise people learn to\n"+
					"scroll past — or the shipped scene declares something the engine cannot draw,\n"+
					"which is the silent drop inside the factory interface itself.", name, w.String())
			}
		})
	}
}

// The vocabulary is derived from Node rather than listed, and this is what
// makes that claim testable from outside: every json tag on the struct is in
// it, and nothing else is. A hand-written list would pass this only by
// accident on the day it was written.
func TestTheVocabularyIsExactlyNodesJSONTags(t *testing.T) {
	vocab := make(map[string]bool)
	for _, key := range Vocabulary() {
		vocab[key] = true
	}

	tags := jsonTagsOfNodeFromSource(t)
	if len(tags) < 15 {
		t.Fatalf("read only %d json tags off Node's declaration; the parser is reading the wrong\n"+
			"type and this test would pass vacuously", len(tags))
	}

	for _, tag := range tags {
		if !vocab[tag] {
			t.Errorf("Node declares json tag %q and the vocabulary does not contain it.\n"+
				"consequence: a document setting a field the parser *does* read would be warned\n"+
				"about anyway — a false alarm on correct input, which is how a guard loses its\n"+
				"reader. The vocabulary must be derived from the struct, not maintained beside it.", tag)
		}
	}
	for key := range vocab {
		found := false
		for _, tag := range tags {
			if tag == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the vocabulary contains %q and Node declares no such json tag.\n"+
				"consequence: a key nothing can read is accepted in silence — the exact hole this\n"+
				"file was written to close, reopened from inside the guard.", key)
		}
	}
}

// jsonTagsOfNodeFromSource reads the tags out of node.go's text rather than by
// reflection. Reflection is how Vocabulary() computes them, so a test using
// reflection would be comparing the implementation to itself and would agree
// with any bug they shared — the mistake the token tests made when they
// asserted through the validator's key rather than the scenes'.
func jsonTagsOfNodeFromSource(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("node.go")
	if err != nil {
		t.Fatalf("read node.go: %v", err)
	}
	text := string(data)
	start := strings.Index(text, "type Node struct {")
	if start < 0 {
		t.Fatal("no Node declaration in node.go; this test cannot check what it cannot find")
	}
	end := strings.Index(text[start:], "\n}")
	if end < 0 {
		t.Fatal("Node's declaration is unterminated in node.go")
	}
	body := text[start : start+end]

	var out []string
	for _, m := range sourceJSONTag.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

var sourceJSONTag = regexp.MustCompile("json:\"([a-z_]+)")

// The sixth instance of the silent-drop class, and the first found inside the
// remedy for the fifth.
//
// The vocabulary guard closed the node object and stopped there, with a
// written reason: walking every object in the source would report `style`'s
// token names and `border`'s keys as unknown node properties, and a guard that
// cries wolf gets deleted. The danger was real; the conclusion did not follow.
// "This object has a different vocabulary" had been treated as "this object
// has no vocabulary", and the three sub-objects a scene may contain went on
// swallowing keys exactly as Node had before the fifth repair.
//
// Each test below is a case measured on the tree with the whole suite green.

// A misspelled border key. The engine reads `shape` through an anonymous
// struct, so `shpae` is discarded, BorderShape() returns "" and "" is the
// default shape — the box draws a border, just not the one the document asked
// for. Nothing looks wrong, which is what makes it expensive.
func TestAMisspelledBorderKeyIsReportedRatherThanSwallowed(t *testing.T) {
	doc, err := ParseDocument([]byte(
		`{ "root": { "type": "box", "border": { "shpae": "double" },
		  "children": [ { "type": "text", "text": "hi" } ] } }`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if got := doc.Root.BorderShape(); got != "" {
		t.Fatalf("premise broken: the typo was expected to leave no shape, got %q", got)
	}

	warnings := doc.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning for the border typo, got %d: %v\n"+
			"consequence: \"shpae\" is discarded by the parser and the box falls back to the\n"+
			"theme's default border, so the scene renders something plausible and the author\n"+
			"is never told the border they wrote was not the border they got.", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0].Msg, "shpae") {
		t.Errorf("the warning does not name the key: %q", warnings[0].Msg)
	}
	if warnings[0].Loc.Line == 0 {
		t.Errorf("the warning carries no address: %q", warnings[0].String())
	}
}

// A misspelled style key, which is the worst of the three and different in
// kind from every silent drop this project has recorded. The others lost a
// property. This one also defeats ValidateTokens: the validator can only check
// a token it can find, so a scene naming a token that does not exist in the
// theme — normally a refusal with a file:line — passes clean.
func TestAMisspelledStyleKeyIsReportedRatherThanSwallowed(t *testing.T) {
	const missing = "no_such_token_at_all"
	spelled := `{ "root": { "type": "text", "text": "x", "style": { "token": "` + missing + `" } } }`
	typo := `{ "root": { "type": "text", "text": "x", "style": { "tokne": "` + missing + `" } } }`

	good, err := ParseDocument([]byte(spelled))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if errs := ValidateTokens(good, theme.SOBRIA()); len(errs) != 1 {
		t.Fatalf("premise broken: a correctly spelled undefined token must be refused, got %d errors", len(errs))
	}

	bad, err := ParseDocument([]byte(typo))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if errs := ValidateTokens(bad, theme.SOBRIA()); len(errs) != 0 {
		t.Fatalf("premise broken: the typo was expected to hide the token from the validator, got %d errors", len(errs))
	}

	warnings := bad.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning for the style-key typo, got %d: %v\n"+
			"consequence: the same document differs from the refused one by two transposed\n"+
			"letters and validates clean. The node draws unstyled and the undefined token is\n"+
			"never checked, so the guard that exists to catch exactly this reports success.",
			len(warnings), warnings)
	}
	if !strings.Contains(warnings[0].Msg, "tokne") {
		t.Errorf("the warning does not name the key: %q", warnings[0].Msg)
	}
	// The message must say the token went unchecked, not merely that a key was
	// ignored: an author who reads "unknown style key" fixes a cosmetic
	// problem, and the cosmetic problem is the smaller half.
	if !strings.Contains(warnings[0].Msg, "theme") {
		t.Errorf("the warning does not say the token was never checked against the theme: %q", warnings[0].Msg)
	}
}

// A stray top-level key. RefuseEmpty already catches the case where the whole
// tree moved under a wrong key, because that leaves no root at all. This is
// its quieter relative — a correct root beside a second, misspelled copy —
// where the document loads, draws, and silently ignores half of what the
// author wrote.
func TestAStrayTopLevelKeyIsReportedRatherThanSwallowed(t *testing.T) {
	doc, err := ParseDocument([]byte(
		`{ "root": { "type": "text", "text": "hi" },
		  "roott": { "type": "text", "text": "the edit that never appeared" } }`))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if err := doc.RefuseEmpty(); err != nil {
		t.Fatalf("premise broken: this document has a root and must not be refused: %v", err)
	}

	warnings := doc.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning for the stray top-level key, got %d: %v\n"+
			"consequence: the author edited a tree that is not the one being drawn and got no\n"+
			"signal at all — the scene loads and renders the other copy.", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0].Msg, "roott") {
		t.Errorf("the warning does not name the key: %q", warnings[0].Msg)
	}
}

// The counter-assertion for the sub-object vocabularies, and the reason the
// original guard stopped at node paths. Both spellings of the style key and
// both border keys are legal, and a guard that warns about them is worse than
// no guard: the shipped scenes use them, so every user would see false alarms
// on documents copied out of the factory interface.
func TestTheLegalSubObjectKeysWarnAboutNothing(t *testing.T) {
	for _, src := range []string{
		`{ "root": { "type": "text", "text": "x", "style": { "style": "dim" } } }`,
		`{ "root": { "type": "text", "text": "x", "style": { "token": "dim" } } }`,
		`{ "root": { "type": "box", "border": { "shape": "single", "style": "warn" },
		   "children": [ { "type": "text", "text": "x" } ] } }`,
		`{ "root": { "type": "box", "border": "single",
		   "children": [ { "type": "text", "text": "x" } ] } }`,
	} {
		doc, err := ParseDocument([]byte(src))
		if err != nil {
			t.Fatalf("premise broken: %v", err)
		}
		for _, w := range doc.Warnings() {
			t.Errorf("legal document warned: %s\nsource: %s\n"+
				"consequence: a false alarm on a construction the golden scenes themselves use,\n"+
				"which is how this check becomes noise a reader learns to scroll past.", w.String(), src)
		}
	}
}

// The style vocabulary must be the validator's own list, not a copy. If the
// two disagree, a key the validator reads a token from is reported as unknown
// (a false alarm on correct input) or a key it does not read is accepted in
// silence (the defect). Either way the drift is invisible from the passing
// suite, which is what four inventories in this repository have now taught.
func TestTheStyleVocabularyIsExactlyTheValidatorsKeys(t *testing.T) {
	vocab := styleVocabulary()
	keys := StyleTokenKeys()
	if len(keys) == 0 {
		t.Fatal("StyleTokenKeys is empty; this test would pass vacuously")
	}
	if len(vocab) != len(keys) {
		t.Fatalf("the style vocabulary has %d keys and the validator reads %d: %v vs %v",
			len(vocab), len(keys), vocab, keys)
	}
	for _, key := range keys {
		if !vocab[key] {
			t.Errorf("the validator reads a token from %q and the style vocabulary does not contain it.\n"+
				"consequence: a correctly written scene is warned about — a false alarm produced by\n"+
				"the guard disagreeing with the code it is supposed to describe.", key)
		}
	}
}

// The border vocabulary must follow the accessors that read a border, and
// this test is what makes that claim falsifiable from outside.
//
// It exists because of an injection against the first version of this fix.
// borderVocabulary() reflected over two anonymous structs copied out of
// BorderShape() and BorderStyleName(); teaching one accessor an extra key left
// the copies untouched, and a document using that key rendered correctly and
// was warned about anyway — the whole suite green while the guard contradicted
// the renderer. That is the false-alarm direction, the one that gets a guard
// switched off rather than filed as a bug.
//
// So the assertion is not "the vocabulary contains shape and style". That
// would be a fifth hand-maintained inventory, correct the day it was written,
// and it is exactly what the injection defeated. The assertion is the
// round-trip: every key borderObject declares must be honoured by an accessor
// and accepted by the vocabulary, so a field added to borderObject tomorrow
// joins both or fails here.
func TestTheBorderVocabularyIsExactlyWhatTheAccessorsRead(t *testing.T) {
	vocab := borderVocabulary()

	typ := reflect.TypeOf(borderObject{})
	if typ.NumField() == 0 {
		t.Fatal("borderObject declares no fields; this test would pass vacuously")
	}

	for i := 0; i < typ.NumField(); i++ {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		if !vocab[tag] {
			t.Errorf("borderObject declares json tag %q and the border vocabulary lacks it.\n"+
				"consequence: a border key the engine really reads is reported as unknown — a\n"+
				"false alarm on a construction that works, which is how a reader learns to\n"+
				"ignore this check.", tag)
		}

		// And the key must actually reach an accessor. A tag on the struct
		// that no accessor returns would be accepted by the vocabulary and
		// read by nobody: the silent drop, re-entering through the guard.
		src := `{ "root": { "type": "box", "border": { "` + tag + `": "probe_value" },
		  "children": [ { "type": "text", "text": "x" } ] } }`
		doc, err := ParseDocument([]byte(src))
		if err != nil {
			t.Fatalf("premise broken for %q: %v", tag, err)
		}
		if got := doc.Root.BorderShape() + doc.Root.BorderStyleName(); !strings.Contains(got, "probe_value") {
			t.Errorf("borderObject declares %q and no accessor surfaces it (shape=%q style=%q).\n"+
				"consequence: the vocabulary accepts a key nothing reads, so a document setting it\n"+
				"is told everything is fine and the value is discarded — the silent drop, arriving\n"+
				"through the guard that exists to stop it.",
				tag, doc.Root.BorderShape(), doc.Root.BorderStyleName())
		}
	}

	for key := range vocab {
		found := false
		for i := 0; i < typ.NumField(); i++ {
			tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			if tag == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the border vocabulary contains %q and borderObject declares no such tag.\n"+
				"consequence: a key no accessor reads is accepted in silence.", key)
		}
	}
}
