package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The delivery guard for invariant 3, asked of the scene the fallback actually
// shows rather than of a scene written to pass it.
//
// # Why boot_test.go could not catch this
//
// boot_test.go already proves the two halves that were missing before:
// loadScene returns an addressed notice, and a scene binding host.scene.error
// renders it. Both still pass, and neither is the boot path — because the
// document binding the notice in those tests is written by the test, and the
// document the user is looking at after a refusal is factoryRAW.
//
// Run against the built binary with a scene carrying one unsigned bind:
//
//	$ arxi-tui | head
//	(twenty-four blank rows)
//
// while the notice existed, complete and addressed, the whole time:
//
//	testdata/SOBRIA.json:3:3: unsigned bind "model.curent" in node type
//	"input"; every bind must appear in BINDS.md §4.5
//
// A warning delivered to an unbound field is indistinguishable from a warning
// never computed, and worse than either, because every guard on the
// computation passes.
//
// # Why the axis is the fallback document, not the notice
//
// The tempting assertion is that the renderer can draw host.scene.error, and
// that assertion already existed and always passed. The question never asked
// was *which document is on screen when the notice is set*, and at boot the
// answer is fixed: the fallback. PLAN.md names the pair in one sentence —
// "falls back to the raw scene **with the `file:line:` notice on screen**" —
// and the halves were tested separately, which is how a conjunction passes
// with one half missing.
//
// So this drives loadScene with a genuinely refused document and asserts
// against the frame, never against the string. Asserting on the notice
// re-tests boot_test.go; asserting on the frame is the only form that could
// fail while every existing guard was green — which is what it did.
func TestTheFallbackSceneItselfShowsTheNotice(t *testing.T) {
	// One unsigned bind: a plausible typo, refused by the validator, so the
	// fallback fires for the reason a real user would trigger it.
	path := writeScene(t, "user.json", `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
    { "id": "prompt", "type": "input", "bind": "model.curent" }
]}}`)

	doc, notice, err := loadScene(path, factoryRAW)
	if err != nil {
		t.Fatalf("loadScene: %v\n"+
			"consequence: invariant 3 says a corrupt scene falls back, never crashes.", err)
	}
	if notice == "" {
		t.Fatalf("no notice for a refused scene; boot_test.go covers this and would fail first")
	}

	// The frame the user is actually looking at, rendered as the boot path
	// renders it.
	state := fold.Fold(nil)
	state.SceneError = notice
	r := engine.Renderer{Width: 80, Height: 24}
	out := r.RenderFrame(doc, state).Plain()

	if !strings.Contains(out, "user.json") {
		t.Errorf("the fallback scene renders no notice.\nnotice computed and dropped:\n  %s\nframe:\n%s\n"+
			"consequence: invariant 3 failing in the half PLAN.md spells out — \"falls back to the\n"+
			"raw scene *with the file:line: notice on screen*\". The scene was refused, the reason\n"+
			"was assembled in full with its address, and the screen is blank. The user cannot tell\n"+
			"a refusal from a crash, an ignored edit, or an engine bug, and the one string that\n"+
			"would end the search died one function short of a pixel.\n"+
			"remedy: the fallback document must carry a node bound to host.scene.error. BINDS.md §2\n"+
			"signs it as \"the one bind the scene may render but the core never provides\"; a channel\n"+
			"no shipped document reads is not a channel.", notice, out)
	}
}

// The scope half. The defect was found on the fallback, but the fallback is not
// where most users meet it: a scene that *loads* with a warning is the common
// case, and it was equally silent.
//
// Measured on the built binary before the fix, with `reveal` — a property
// SCENES.md names and this engine does not implement — added to shipped SOBRIA:
// the scene drew normally and the word `reveal` appeared nowhere on screen.
//
// This is a separate test from the fallback one rather than a case in it,
// because the two can regress independently: the notice node could be restored
// to factoryRAW alone and this would stay broken, which is precisely the
// partial fix the narrow test would have blessed.
func TestASceneThatLoadsWithAWarningAlsoShowsIt(t *testing.T) {
	path := writeScene(t, "future.json", `{ "root": { "type": "stack", "children": [
    { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1, "reveal": "typewriter" },
    { "id": "prompt", "type": "input", "bind": "user.input", "placeholder": "> " }
]}}`)

	doc, notice, err := loadScene(path, factorySobria)
	if err != nil {
		t.Fatalf("loadScene: %v", err)
	}
	if notice == "" {
		t.Fatalf("no notice for an unimplemented property; boot_test.go covers this and would fail first")
	}
	// The user's own document loaded, which is the forward-compatibility
	// promise: a warning is not a refusal.
	if doc == nil || doc.Root == nil || len(doc.Root.Children) != 2 {
		t.Fatalf("the document was replaced rather than loaded with a warning")
	}

	// The scene on screen here is the user's, and the user's scene has no
	// notice node — so the delivery this test measures is the one the
	// *shipped* documents owe. Rendering factorySobria with the notice set
	// is the honest model of "a shipped scene is active and the host has
	// something to say".
	shipped, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument(factorySobria): %v", err)
	}
	state := fold.Fold(nil)
	state.SceneError = notice
	r := engine.Renderer{Width: 100, Height: 24}
	out := r.RenderFrame(shipped, state).Plain()

	if !strings.Contains(out, "reveal") {
		t.Errorf("the shipped sobria scene renders no notice.\nnotice:\n  %s\nframe:\n%s\n"+
			"consequence: the common case is worse than the fallback one. The scene loads and draws,\n"+
			"reports success, and the property the engine silently ignored is never named anywhere\n"+
			"the author can see it — so their only evidence is a screen that looks subtly wrong. The\n"+
			"same silence covers a misspelled \"children\", which deletes an entire subtree.\n"+
			"remedy: every shipped document binds host.scene.error, not just the raw fallback.", notice, out)
	}
}

// The other direction, and the one that decides whether the remedy may be a
// node in the shipped scenes at all.
//
// A notice row always present is the always-on warning light
// TestAValidSceneReportsNoNotice rejects one layer up, and it would cost a row
// of a two-node scene whose entire argument is that it is two nodes. The node
// must vanish when there is nothing to say — which `when` does with no new
// engine capability, because host.scene.error is "text | null" and evalWhen
// already reads the empty string as false.
//
// Asserted on the raw scene's shape rather than on a missing substring: a test
// looking for the notice text being absent would pass on a scene rendering an
// empty row, and an empty row at the top of the minimal scene is a visible
// regression against invariant 1.
func TestTheFallbackSceneIsUnchangedWhenThereIsNothingToReport(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	state := fold.Fold(nil)
	state.SceneError = "" // BINDS.md §2: null means the active scene validated.
	r := engine.Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)

	if got := len(f.Live); got != 24 {
		t.Fatalf("the raw scene rendered %d rows, want 24", got)
	}
	// The input is the last row and the transcript owns everything above it.
	// A notice row that failed to hide would push one of the two, and
	// invariant 1 makes that a review event rather than a detail.
	last := f.Live[len(f.Live)-1].Text()
	if !strings.Contains(last, ">") {
		t.Errorf("the raw scene's last row is %q, not the input prompt\n"+
			"consequence: a notice node that does not hide when there is nothing to report costs a\n"+
			"row of the minimal scene on every clean boot, and invariant 1 says the factory scene\n"+
			"draws byte-identical frames.\n"+
			"remedy: gate the notice node with when: host.scene.error.", last)
	}
	// Every row above the input must be blank on an empty fold. This is the
	// assertion that fails if the gate stops working, since an ungated
	// notice node draws its (empty) row at the top.
	for i, line := range f.Live[:len(f.Live)-1] {
		if strings.TrimSpace(line.Text()) != "" {
			t.Errorf("row %d of the empty raw scene is %q, want blank\n"+
				"consequence: the minimal scene grew a row it did not have, which is invariant 1.",
				i, line.Text())
			break
		}
	}
}
