package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The boot path of invariant 3. PLAN.md states it in full and stresses that the
// boot path cannot inherit it from the hot path:
//
//	"a scene document that is corrupt **on disk** when the instance starts —
//	written by a crashed session, or a stranger's download — falls back to the
//	raw scene with the `file:line:` notice on screen. At boot there is no
//	'last good' to stay on, so the fallback is explicit, invariant-listed, and
//	tested — not implied by the hot-reload code."
//
// The fallback half was implemented and untested; the notice half was not
// implemented at all — loadScene discarded the error, so a user whose scene
// failed to load got the raw interface and no statement of why. BINDS.md §2
// signs the channel for it: `host.scene.error`, "text | null", "the last-good
// scene notice (invariant 3); null means the active scene validated" — and
// calls it "the one bind the scene may render but the core never provides".

// writeScene puts a scene document in a temp dir and returns its path.
func writeScene(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestCorruptSceneOnDiskFallsBackWithAnAddressedNotice is the invariant as a
// test, both halves at once: the interface boots on the raw scene, and it can
// say where the refusal was.
func TestCorruptSceneOnDiskFallsBackWithAnAddressedNotice(t *testing.T) {
	cases := []struct {
		name string
		body string
		// wantLine is the line the notice must name: the point of the
		// exercise is that the user can open the file and go there.
		wantLine string
		// wantReason is a distinctive fragment of the diagnosis, so a
		// notice that carries an address but loses the cause still fails.
		wantReason string
	}{
		{
			// The crashed-session case: a truncated write. The address is
			// the end of the file (line 5 — the empty line after the last
			// newline), not the last line carrying text, because that is
			// where the input ran out: for a truncation the honest answer
			// to "where is the problem" is "the document stops here".
			name:       "truncated by a crashed session",
			body:       "{\n  \"root\": {\n    \"type\": \"stack\",\n    \"children\": [ { \"type\": \"markdown\",\n",
			wantLine:   ":5",
			wantReason: "unexpected end of JSON input",
		},
		{
			// The stranger's-download case: valid JSON, unsatisfiable
			// scene. PLAN.md invariant 3 makes this equivalent to a
			// syntax error on purpose, and that equivalence is the part
			// most likely to be broken by someone "simplifying" the
			// boot path later.
			name: "valid JSON binding vocabulary the host does not have",
			body: `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history" },
    { "id": "evil", "type": "text", "bind": "stranger.telemetry" }
]}}`,
			wantLine:   ":3",
			wantReason: "unsigned bind",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeScene(t, "user.json", c.body)

			doc, notice, err := loadScene(path, factoryRAW)
			if err != nil {
				t.Fatalf("loadScene returned a hard error: %v\n"+
					"consequence: a corrupt scene on disk took the session down at boot — the exact failure invariant 3 forbids, and the one with no recovery path because there is no 'last good' scene yet.\n"+
					"remedy: fall back to the factory raw scene and report the reason in the notice, never in the error.", err)
			}
			if doc == nil {
				t.Fatal("loadScene returned no document\n" +
					"consequence: nothing to render, so the interface does not boot.\n" +
					"remedy: return the parsed fallback scene.")
			}

			// The fallback must be the raw scene, and it must actually
			// render — a document that parses but draws nothing is not a
			// usable fallback.
			r := engine.Renderer{Width: 80, Height: 24}
			f := r.RenderFrame(doc, fold.Fold(nil))
			if strings.TrimSpace(f.Plain()) == "" && len(f.Live) == 0 {
				t.Error("the fallback scene rendered an empty frame\n" +
					"consequence: the user is left staring at a blank terminal with no way to know the interface is alive.\n" +
					"remedy: the fallback must be the two-node raw scene, parsed and renderable.")
			}

			if notice == "" {
				t.Fatalf("loadScene fell back silently for %s\n"+
					"consequence: the user's scene was refused and the interface cannot say why — invariant 3 requires the file:line: notice on screen, and BINDS.md §2 signs host.scene.error as the channel for it. A silent fallback looks like the user's edit was ignored.\n"+
					"remedy: return the addressed refusal as the notice; do not discard the error.", c.name)
			}
			if !strings.Contains(notice, c.wantLine) {
				t.Errorf("notice %q does not name line %s\n"+
					"consequence: the user is told the scene is broken but not where, so a large document has to be bisected by hand.\n"+
					"remedy: parse through scene.ParseFile so the refusal carries file:line:col.", notice, c.wantLine)
			}
			if !strings.Contains(notice, filepath.Base(path)) {
				t.Errorf("notice %q does not name the file\n"+
					"consequence: with presets and user documents composed together, a line number alone is ambiguous.\n"+
					"remedy: loadScene must parse by path, not by bytes.", notice)
			}
			if !strings.Contains(notice, c.wantReason) {
				t.Errorf("notice %q does not state the reason (want %q)\n"+
					"consequence: an address with no diagnosis tells the user where to look but not what to fix.\n"+
					"remedy: keep the validator's message in the notice alongside its address.", notice, c.wantReason)
			}
		})
	}
}

// TestNoticeReachesTheBoundSceneField closes the loop. A notice returned by
// loadScene and never rendered would satisfy the test above and still leave the
// screen silent, which is the same bug one layer up — so this asserts the text
// actually reaches a scene that binds host.scene.error.
func TestNoticeReachesTheBoundSceneField(t *testing.T) {
	// A scene that displays the notice. host.scene.error is signed in the
	// bootstrap set precisely so the failure can be shown by a document.
	noticeScene := `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history" },
    { "id": "notice", "type": "text", "bind": "host.scene.error" }
]}}`
	doc, err := scene.ParseDocument([]byte(noticeScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the notice scene must validate: %v\n"+
			"consequence: if a scene cannot bind host.scene.error, invariant 3's notice has no way onto the screen.\n"+
			"remedy: keep host.scene.error signed in BINDS.md §2 and in signedBinds.", err)
	}

	const want = "user.json:4:3: invalid JSON: unexpected end of JSON input"
	state := fold.Fold(nil)
	state.SceneError = want

	r := engine.Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	out := f.Plain()
	if !strings.Contains(out, "user.json:4:3") {
		t.Errorf("the rendered frame does not show the notice\nframe:\n%s\n"+
			"consequence: the notice exists in host state but never reaches the user, so the interface silently drops the only explanation of why their scene is not showing.\n"+
			"remedy: the boot path must set State.SceneError, and it must be re-applied on every repaint because Fold rebuilds State per frame (ADR-0004).", out)
	}
}

// TestAMissingSceneFileIsNotAnError protects the other direction: absence is
// the default install, not a fault. A notice here would greet every first run
// with a complaint about a file the user never wrote.
func TestAMissingSceneFileIsNotAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")

	doc, notice, err := loadScene(missing, factoryRAW)
	if err != nil {
		t.Fatalf("loadScene: %v\n"+
			"consequence: the interface refuses to boot without a user scene, so a clean install cannot start.\n"+
			"remedy: a missing file uses the factory scene.", err)
	}
	if doc == nil {
		t.Fatal("no document for a missing scene file")
	}
	if notice != "" {
		t.Errorf("a missing scene file produced the notice %q\n"+
			"consequence: BINDS.md §2 defines host.scene.error as null when \"the active scene validated\"; reporting absence as a refusal means every default install boots showing an error about a file the user never created, which trains them to ignore the notice that matters.\n"+
			"remedy: treat os.IsNotExist as the factory-scene path and return an empty notice.", notice)
	}
}

// TestAValidSceneReportsNoNotice is the null case BINDS.md §2 specifies in so
// many words, and the guard against a notice that is always populated — which
// would make the field useless exactly as an always-on warning light is.
func TestAValidSceneReportsNoNotice(t *testing.T) {
	path := writeScene(t, "good.json", `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
    { "id": "prompt", "type": "input", "bind": "user.input", "placeholder": "> " }
]}}`)

	doc, notice, err := loadScene(path, factoryRAW)
	if err != nil {
		t.Fatalf("loadScene: %v", err)
	}
	if notice != "" {
		t.Errorf("a valid scene produced the notice %q\n"+
			"consequence: host.scene.error must be null when the active scene validated; a notice that is always set is a warning light that is always on.\n"+
			"remedy: only populate the notice on the fallback paths.", notice)
	}
	// The document returned must be the user's, not the fallback: silently
	// substituting the factory scene for a valid document would be the
	// quietest possible way to break customization.
	if doc.Root == nil || len(doc.Root.Children) != 2 {
		t.Fatalf("loadScene did not return the user's scene")
	}
	if got := doc.Root.Children[1].PrefixText(); got != "" {
		t.Errorf("the factory scene was substituted for a valid user document (prefix %q)\n"+
			"consequence: the user's scene is ignored with no notice at all.\n"+
			"remedy: return the parsed document when it validates.", got)
	}
}

// TestTheNonInteractivePathShowsTheNoticeToo guards the pipe path. It exists
// because a second render path is where an invariant quietly stops holding:
// `arxi-tui | cat` on a broken scene must still explain itself, and that output
// is how the failure gets pasted into a bug report.
func TestTheNonInteractivePathShowsTheNoticeToo(t *testing.T) {
	noticeScene := `{ "root": { "type": "stack", "children": [
    { "id": "notice", "type": "text", "bind": "host.scene.error" }
]}}`
	doc, err := scene.ParseDocument([]byte(noticeScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	const notice = "user.json:2:5: unsigned bind \"stranger.telemetry\""

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := runNonInteractive(doc, theme.SOBRIA(), notice)
	w.Close()
	os.Stdout = stdout

	if runErr != nil {
		t.Fatalf("runNonInteractive: %v", runErr)
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := r.Read(buf)
		sb.Write(buf[:n])
		if readErr != nil {
			break
		}
	}
	if !strings.Contains(sb.String(), "user.json:2:5") {
		t.Errorf("piped output omits the notice\ngot:\n%s\n"+
			"consequence: the non-interactive path is a second renderer, and an invariant that holds in only one of them is not an invariant. This output is also what gets pasted into a bug report.\n"+
			"remedy: thread the notice into runNonInteractive's fold state.", sb.String())
	}
}

// The boot half of the vocabulary guard, and the reason it is here rather than
// only in internal/scene.
//
// scene.Warnings() computing a correct warning proves nothing on its own: this
// project has now twice shipped a remedy that satisfied its guard and changed
// nothing a user could see — the unrenderedFields entry that refused nothing,
// and the skip-list in a _test.go that could not reach the parser. A warning
// that never leaves the package it is computed in would be the third. What is
// asserted here is the delivery: the document still loads, and the notice
// carrying the reason arrives on the channel invariant 3 already built.
func TestAnUnknownPropertyLoadsTheSceneAndSaysWhatItIgnored(t *testing.T) {
	path := writeScene(t, "future.json", `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1, "shine": "gold" },
    { "id": "prompt", "type": "input", "bind": "user.input", "placeholder": "> " }
]}}`)

	doc, notice, err := loadScene(path, factoryRAW)
	if err != nil {
		t.Fatalf("loadScene returned a hard error for a document with an unknown property: %v\n"+
			"consequence: PLAN.md's forward-compatibility rule is \"unknown-but-parseable is a\n"+
			"warning\". Refusing here means a scene written for a later arxi-tui cannot boot at\n"+
			"all under this one, which is the opposite of the promise.", err)
	}

	// The user's document, not the fallback. This is the assertion that
	// separates a warning from a refusal, and getting it wrong would break
	// every v1 document under a v0 engine while looking like caution.
	if doc == nil || doc.Root == nil || len(doc.Root.Children) != 2 {
		t.Fatalf("the document was replaced rather than loaded with a warning\n" +
			"consequence: an unknown property would cost the user their whole scene, when the\n" +
			"engine understood every other thing in it.")
	}

	if notice == "" {
		t.Fatalf("a scene declaring %q — a property SCENES.md names and this engine does not\n"+
			"implement — loaded with no notice at all.\n"+
			"consequence: the silent drop, which is the class this repository has now paid for\n"+
			"five times. The scene loads, reports success, and the property never happened, so\n"+
			"the author's only evidence is a screen that looks wrong. Worse, the same silence\n"+
			"covers a typo: a misspelled \"children\" deletes the whole subtree on this path.\n"+
			"remedy: loadScene must surface scene.Warnings() on host.scene.error.", "shine")
	}
	if !strings.Contains(notice, "shine") {
		t.Errorf("the notice does not name the ignored property, so the author cannot act on it: %q", notice)
	}
	if !strings.Contains(notice, "future.json") {
		t.Errorf("the notice carries no file address (invariant 4): %q\n"+
			"remedy: the warning's Loc must come from the parsed document, which knows the path\n"+
			"because loadScene parses by path.", notice)
	}
}

// A document whose tree sits under the wrong top-level key draws nothing under
// any engine, so it is the one shape that is refused rather than warned about.
// The distinction is the whole design: forward compatibility protects
// constructions a later version might understand, and there is no version in
// which a scene with no root renders.
func TestASceneWithNoRootFallsBackAndSaysSo(t *testing.T) {
	path := writeScene(t, "rootless.json", `{ "scene": { "type": "text", "text": "hi" } }`)

	doc, notice, err := loadScene(path, factoryRAW)
	if err != nil {
		t.Fatalf("loadScene returned a hard error: %v\n"+
			"consequence: invariant 3 says a corrupt scene falls back, never crashes.", err)
	}
	if notice == "" {
		t.Fatalf("a document with no \"root\" loaded with no notice.\n" +
			"consequence: the interface draws nothing and never says why. This parsed, validated\n" +
			"clean and rendered an empty screen before the refusal existed — the failure looks\n" +
			"exactly like an engine bug to the one person who could fix it.")
	}
	// The fallback must actually be showing, because the user's document
	// has nothing to show. This is the half invariant 3 names explicitly
	// and the half that was missing once before.
	if doc == nil || doc.Root == nil {
		t.Fatal("no document at all: the fallback did not fire, so the session has no scene")
	}
}

// The sub-object warnings must reach the screen too, and this test exists
// because the delivery path is the place this project has been burned twice:
// a finding computed and never delivered is the same silence one layer
// further out.
//
// It is a separate test from the unknown-property one rather than another
// case in it, because the two ask different questions. That one asks whether
// loadScene surfaces warnings at all; this asks whether the warnings it
// surfaces cover the objects a node owns. The first would keep passing with
// the sub-object vocabularies deleted.
func TestASubObjectTypoAlsoReachesTheNotice(t *testing.T) {
	path := writeScene(t, "styled.json", `{ "root": { "type": "box",
    "border": { "shpae": "double" },
    "style": { "tokne": "no_such_token_at_all" },
    "children": [ { "type": "text", "text": "hi" } ] } }`)

	doc, notice, err := loadScene(path, factoryRAW)
	if err != nil {
		t.Fatalf("loadScene returned a hard error for two sub-object typos: %v\n"+
			"consequence: these are warnings, not refusals — the scene parses and draws.", err)
	}
	if doc == nil || doc.Root == nil {
		t.Fatalf("the document was replaced rather than loaded with a warning")
	}

	if notice == "" {
		t.Fatalf("a scene whose border and style keys are both misspelled loaded with no\n" +
			"notice at all.\n" +
			"consequence: the box draws the theme's default border instead of the double one\n" +
			"asked for, the node draws unstyled, and the undefined token it names is never\n" +
			"checked against the theme — the load reports success on a document that is wrong\n" +
			"in three ways.\n" +
			"remedy: Warnings() must check each sub-object against its own vocabulary.")
	}
	// The notice is one line and shows the first warning in full with a count
	// for the rest — a deliberate decision documented on warningNotice, and
	// not one this test may quietly overturn. The first draft asserted that
	// both keys appeared in the notice and failed, and the failure was the
	// test's: it was measuring the probe, not the code. So the assertion is
	// split to match what each layer actually promises.
	if !strings.Contains(notice, "shpae") {
		t.Errorf("the notice does not name the first offending key: %q", notice)
	}
	if !strings.Contains(notice, "styled.json") {
		t.Errorf("the notice carries no file address (invariant 4): %q", notice)
	}

	// Both typos must have been *found*, whatever the one-line notice has room
	// to print. Asserting only on the notice would let the style vocabulary be
	// deleted without this test noticing, because the border warning alone
	// fills the line.
	warned := map[string]bool{}
	for _, w := range doc.Warnings() {
		for _, key := range []string{"shpae", "tokne"} {
			if strings.Contains(w.Msg, key) {
				warned[key] = true
			}
		}
	}
	for _, key := range []string{"shpae", "tokne"} {
		if !warned[key] {
			t.Errorf("no warning names %q\n"+
				"consequence: this sub-object is still swallowing keys, and the notice that did\n"+
				"appear would make the load look adequately reported.", key)
		}
	}
}
