package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// These tests pin the modal's capture-and-answer contract, the loop-visible half
// of the consent flow. The keypress→answer mapping itself is consentAnswerForKey's
// (tested there); what is pinned here is the modal's job on top of it: while a
// screen is up it owns EVERY non-panic key so nothing meant for the prompt leaks
// into chat, a decided key answers the blocked worker and closes the screen, and
// an undecided key leaves the screen standing.

// runeKey (a single-rune key event) is defined in consent_prompt_test.go, which
// shares this package's test binary.

func TestInstallModalNotCapturingWhenIdle(t *testing.T) {
	var im installModal
	if im.capturing() {
		t.Fatal("a fresh modal reports it is capturing keys; with no consent screen up, keys must reach the normal input path")
	}
	if im.handleKey(runeKey('y')) {
		t.Fatal("an idle modal consumed a key; with no screen up it must decline every key so the loop routes it normally")
	}
}

func TestInstallModalGrantAnswersTheWorkerAndCloses(t *testing.T) {
	var im installModal
	reply := make(chan supervisor.ConsentAnswer, 1)
	im.beginConsent(&scene.Document{}, []string{"events.emit"}, reply)
	if !im.capturing() {
		t.Fatal("after beginConsent the modal must report capturing so the loop shows the screen and routes keys to it")
	}

	if !im.handleKey(runeKey('r')) {
		t.Fatal("a key pressed against the screen must be consumed, not passed to chat")
	}
	select {
	case ans := <-reply:
		if ans.Rejected {
			t.Fatal("'r' is a grant-and-remember, not a rejection")
		}
		if !ans.Remember {
			t.Fatal("'r' must ask the gate to remember the grant; without Remember the plugin re-prompts every launch")
		}
		if len(ans.Granted) != 1 || ans.Granted[0] != "events.emit" {
			t.Fatalf("the answer must grant the declared set verbatim; got %v", ans.Granted)
		}
	default:
		t.Fatal("a decided key produced no answer on the reply channel; the worker's Prompt is blocked on that receive and would hang forever")
	}
	if im.capturing() {
		t.Fatal("the screen must close once answered; a second answer to a blocked-then-returned Prompt would panic on a closed exchange")
	}
}

func TestInstallModalUndecidedKeyLeavesThePromptStanding(t *testing.T) {
	var im installModal
	reply := make(chan supervisor.ConsentAnswer, 1)
	im.beginConsent(&scene.Document{}, []string{"events.emit"}, reply)

	// A key that is none of the three choices: it must be consumed (so it does not
	// type into chat behind the modal) yet leave the screen up and send no answer.
	if !im.handleKey(runeKey('q')) {
		t.Fatal("an unrelated key must still be consumed while the modal is up; otherwise a 'q' meant to dismiss lands in the chat buffer behind the screen")
	}
	select {
	case ans := <-reply:
		t.Fatalf("an unrelated key sent an answer (%v); 'no answer is not a yes' — only y/r/n/Esc may answer", ans)
	default:
	}
	if !im.capturing() {
		t.Fatal("an unrelated key closed the screen; the prompt must stand until the user actually chooses")
	}
}

func TestInstallModalEscapeRejects(t *testing.T) {
	var im installModal
	reply := make(chan supervisor.ConsentAnswer, 1)
	im.beginConsent(&scene.Document{}, []string{"events.emit"}, reply)

	if !im.handleKey(term.Key{Type: term.KeyEscape}) {
		t.Fatal("Escape against the consent screen must be consumed")
	}
	select {
	case ans := <-reply:
		if !ans.Rejected {
			t.Fatal("Escape must reject: dismissing the screen without choosing is a no, and a no must not spawn the process")
		}
	default:
		t.Fatal("Escape produced no answer; the blocked worker would hang")
	}
	if im.capturing() {
		t.Fatal("Escape must close the screen")
	}
}
