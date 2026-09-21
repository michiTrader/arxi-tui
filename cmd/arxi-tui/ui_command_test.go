package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// enter is the only key these tests send. The dispatch is defined by what it
// does with a submitted line, and a helper keeps that visible at each call
// site instead of burying it in a struct literal five times.
func enter() term.Key { return term.Key{Type: term.KeyEnter} }

// liveScene loads the shipped default the way run() does, so the tests
// exercise the document the user actually gets. A fixture written to suit the
// test would not have the property that matters here — real ids, real `when`
// gates, and real nodes with no id at all.
func liveScene(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := scene.ParseFile("../../testdata/SOBRIA.json")
	if err != nil {
		t.Fatalf("the shipped default scene must parse, and it did not: %v", err)
	}
	return doc
}

// TestACompleteUiCommandIsNotSwallowedByTheSlashMenu is the guard for the dead
// end this wiring was written to fix, and it is the one test here that fails
// against the code as it stood before the dispatch existed.
//
// The slash menu filters on the whole typed string, so the match count drops
// to zero as soon as an argument is typed ("ui" -> 1, "ui style" -> 0,
// "ui style status dim" -> 0), and slashMenuKey's Enter branch returns the
// buffer untouched when there are no matches. A correct command therefore did
// nothing at all: no patch, no refusal, no prompt — with the menu showing an
// empty list, which reads as the interface being busy rather than as a key
// being dropped.
//
// The guard asks the dispatch to claim the key. If /ui is ever moved back
// behind the menu, this fails instead of the command silently going quiet.
func TestACompleteUiCommandIsNotSwallowedByTheSlashMenu(t *testing.T) {
	doc := liveScene(t)
	notice := ""

	handled, next := uiCommandKey("/ui style status dim", enter(), &doc, &notice)
	if !handled {
		t.Fatal("a complete /ui command must be claimed by the command surface.\nConsequence: the slash menu filters on the whole typed string, so it matches nothing once an argument is present, and its Enter branch returns the buffer untouched — the command does nothing at all, with no refusal and no prompt.\nRemedy: keep the /ui dispatch ahead of the slash menu in the key handler.")
	}
	if next != "" {
		t.Errorf("a submitted command must clear the input buffer.\n  got: %q\nConsequence: the line stays on screen after it ran, so the user cannot tell whether it was applied and re-submits it.\nRemedy: return an empty buffer once the command is handled.", next)
	}
	if !strings.Contains(notice, "status") {
		t.Errorf("the notice must say what changed.\n  got: %q\nConsequence: PLAN.md requires the change be shown before it is trusted; a silent success is indistinguishable from a silent no-op.\nRemedy: report the patch summary through host.scene.error.", notice)
	}
}

// TestAUiCommandActuallyChangesTheDocumentTheLoopDraws checks the half a
// notice cannot prove.
//
// Reporting "styled status as dim" while leaving the old document in place is
// the exact false-pass this repo has caught repeatedly: the success message is
// the thing under test, so a test that reads only the message agrees with the
// bug. This one re-reads the scene.
func TestAUiCommandActuallyChangesTheDocumentTheLoopDraws(t *testing.T) {
	doc := liveScene(t)
	before := doc
	notice := ""

	if handled, _ := uiCommandKey("/ui style status dim", enter(), &doc, &notice); !handled {
		t.Fatalf("the command must be handled; notice was %q", notice)
	}
	if doc == before {
		t.Fatal("the loop's document pointer was not replaced.\nConsequence: the next repaint draws the unpatched scene while the notice says the patch succeeded — a reported result that was never measured.\nRemedy: assign the patched document back through the pointer.")
	}
	if !strings.Contains(string(doc.Source()), `"dim"`) {
		t.Errorf("the patched document does not carry the change.\n  source: %s\nConsequence: as above — success is reported and the screen is unchanged.\nRemedy: return the re-parsed document from patch.Apply and store it.", doc.Source())
	}
}

// TestAnInvalidUiCommandKeepsTheSceneAndSaysWhy is invariant 3 at the host
// boundary: an invalid patch never kills the session.
//
// The patch surface has its own test for refusing a bad document; this asks
// the different question of what the *loop* does with that refusal, because
// the surface could be perfectly correct and the caller could still overwrite
// the good scene with a nil.
func TestAnInvalidUiCommandKeepsTheSceneAndSaysWhy(t *testing.T) {
	doc := liveScene(t)
	before := doc
	notice := ""

	handled, _ := uiCommandKey("/ui set status bind not.a.signed.bind", enter(), &doc, &notice)
	if !handled {
		t.Fatal("a /ui line must be claimed even when the patch is refused.\nConsequence: an unclaimed line falls through to the prompt path and is sent to the model as chat, so a typo becomes a conversation turn.\nRemedy: handle the key and report the refusal.")
	}
	if doc != before {
		t.Error("a refused patch replaced the live document.\nConsequence: PLAN.md invariant 3 says the last good scene stays; replacing it means one bad command can blank the interface the user needs in order to fix it.\nRemedy: only assign the document when Apply returns no error.")
	}
	if notice == "" {
		t.Fatal("a refused patch produced no notice.\nConsequence: the command vanishes with no feedback and the user cannot tell it failed.\nRemedy: put the error on host.scene.error.")
	}
	if !strings.Contains(notice, "SOBRIA.json:") {
		t.Errorf("the refusal shown to the user must carry its address.\n  got: %q\nConsequence: invariant 4 and the Phase 2 repair loop both need file:line; without it the user — or the model reading the same message — must guess which of several hundred lines is wrong.\nRemedy: keep the patch source-to-source so the re-parsed document has an offset table.", notice)
	}
}

// TestOnlyUiLinesAreClaimed pins the boundary between this surface and
// everything else the input bar does.
//
// Two ways to get it wrong, and both are silent. Claiming too much turns a
// chat message into a command refusal; claiming too little sends a command to
// the model as prose. The "/uize" case is the specific one a bare
// strings.HasPrefix produces, and it is worse than a miss: it answers a chat
// line with a confident list of /ui verbs.
func TestOnlyUiLinesAreClaimed(t *testing.T) {
	notClaimed := []string{
		"hello there",
		"/help",
		"/uize the thing",
		"tell me about /ui",
	}
	for _, line := range notClaimed {
		doc := liveScene(t)
		notice := ""
		if handled, _ := uiCommandKey(line, enter(), &doc, &notice); handled {
			t.Errorf("%q must not be claimed by the /ui surface.\nConsequence: an ordinary line is answered with a command refusal instead of reaching the model — and for a near-miss like \"/uize\" the refusal confidently lists /ui verbs, which is a wrong diagnosis rather than a missing one.\nRemedy: require \"/ui\" to be followed by a space or the end of the line.", line)
		}
	}

	doc := liveScene(t)
	notice := ""
	if handled, _ := uiCommandKey("/ui", enter(), &doc, &notice); !handled {
		t.Error("a bare \"/ui\" must be claimed so the surface can say what verbs exist.\nConsequence: it is submitted to the model as the prompt \"ui\", which answers a control command with prose.\nRemedy: claim \"/ui\" with no arguments and refuse it with the verb list.")
	}
}

// TestOnlyEnterSubmits keeps the dispatch off every other key.
//
// The handler sits ahead of both the slash menu and the typing path, so a
// version that claimed more than Enter would silently disable navigation and
// then typing itself — and the failure would look like a dead keyboard rather
// than like a bug in a command surface.
func TestOnlyEnterSubmits(t *testing.T) {
	keys := map[string]term.Key{
		"up":        {Type: term.KeyUp},
		"down":      {Type: term.KeyDown},
		"tab":       {Type: term.KeyTab},
		"escape":    {Type: term.KeyEscape},
		"backspace": {Type: term.KeyBackspace},
		"rune":      {Type: term.KeyRunes, Runes: []rune{'x'}},
	}
	for name, k := range keys {
		doc := liveScene(t)
		notice := ""
		if handled, _ := uiCommandKey("/ui style status dim", k, &doc, &notice); handled {
			t.Errorf("the /ui dispatch claimed %s.\nConsequence: it runs ahead of the slash menu and the typing path, so claiming any key but Enter disables navigation and then typing — which presents as a dead keyboard, not as a command-surface bug.\nRemedy: return early unless the key is Enter.", name)
		}
	}
}

// TestAHandBuiltSceneIsRefusedRatherThanSilentlyRewritten covers the one input
// the source-to-source design cannot serve.
//
// A Document with no source bytes has nothing to patch. The tempting fallback
// is to serialise the tree — and that is the trap: scene.Node has already
// dropped every key it does not declare, so the "source" produced that way is
// a *different* document, and handing it back would present the user's scene
// minus its unknown properties as though nothing had happened.
func TestAHandBuiltSceneIsRefusedRatherThanSilentlyRewritten(t *testing.T) {
	doc := &scene.Document{Root: &scene.Node{ID: "root", Type: "stack"}}
	notice := ""

	handled, _ := uiCommandKey("/ui style root dim", enter(), &doc, &notice)
	if !handled {
		t.Fatal("the line must still be claimed so the user gets an answer.\nConsequence: it falls through to the prompt path and the control command is sent to the model as chat.\nRemedy: claim the line and refuse it.")
	}
	if !strings.Contains(notice, "no source") {
		t.Errorf("a scene with no source text must be refused by saying so.\n  got: %q\nConsequence: the alternative is to serialise the tree, but scene.Node has already dropped every key it does not declare — so the patch would silently hand back the user's scene minus its unknown properties and report success.\nRemedy: refuse when Document.Source() is nil.", notice)
	}
}
