//go:build !windows

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// These drive the real loop (real fold, renderer, session log, packageAuto) with only the
// terminal and the model faked, as the other package loop tests do.

// postPackage is a package "sp" with an output.post rule that runs rule.sh, and an event
// on turn.done that prints what it was given as heard<...>. The event is the witness of
// what the rest of the app was told: it reads the model's own words.
func postPackage(rule string) map[string]string {
	return map[string]string{
		"sp/package.json": execPkg("sp", "",
			`"rules":[{"id":"r","stage":"output.post","op":{"kind":"exec","argv":["./rule.sh","${input}"]}}],`+
				`"events":[{"id":"e","on":"turn.done","op":{"kind":"exec","argv":["./e.sh","${text}"]}}]`),
		"sp/rule.sh": "#!/bin/sh\n" + rule + "\n",
		"sp/e.sh":    "#!/bin/sh\nprintf 'heard<%s>' \"$1\"\n",
	}
}

// onlyTheChat is the screen without what the event printed, so a word the event heard is
// not mistaken for a word the chat drew.
func onlyTheChat(out string) string { return heardRe.ReplaceAllString(screen(out), "") }

// firstSessionID is the one conversation file the run wrote.
func firstSessionID(t *testing.T, cfg string) string {
	t.Helper()
	list := listSessions(cfg + "/sessions")
	if len(list) != 1 {
		t.Fatalf("want one saved conversation, found %d", len(list))
	}
	return list[0].ID
}

const colour = `printf '%s' "$1" | sed 's/colour/color/g'`

func answerTurn(base int64, text string) []fold.Event {
	return []fold.Event{
		{Type: "run.prompt", Seq: base, Payload: map[string]any{"text": "what is it"}},
		answer(base+1, text),
		{Type: "agent.turn_done", Seq: base + 2},
	}
}

// What the user reads is the rule's wording; what the model, the saved conversation and
// the packages' events are given is the model's own. The consequence of mixing them up is
// a model that reads its own answer reworded (and a transcript that never says what it said).
func TestLoopOutputPostChangesWhatIsShownNotWhatTheModelSaid(t *testing.T) {
	out, cfg := loopFeeding(t, postPackage(colour), "sp", exitAfter(900), func(send func(fold.Event)) {
		time.Sleep(150 * time.Millisecond)
		for _, e := range answerTurn(2000, "the colour is red") {
			send(e)
		}
	})
	chat := onlyTheChat(out)
	if !strings.Contains(chat, "the color is red") || strings.Contains(chat, "colour") {
		t.Errorf("the chat must show the reworded answer; the chat drew:\n%s", lastFrame(out))
	}
	if got := heardBy(out); len(got) != 1 || got[0] != "the colour is red" {
		t.Errorf("the turn.done event heard %q, want the model's own words [the colour is red]: ${text} is what the model said, and a rule only changes what is drawn", got)
	}
	evs, err := loadSession(cfg+"/sessions", firstSessionID(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	var saved string
	for _, e := range evs {
		if e.Type == "llm.response" {
			saved, _ = e.Payload["text"].(string)
		}
	}
	if saved != "the colour is red" {
		t.Errorf("the saved conversation holds %q, want the model's words \"the colour is red\": /resume would hand the model a rewording it never wrote", saved)
	}
	if h := historyFromEvents(evs); len(h) != 2 || h[1].Text != "the colour is red" {
		t.Errorf("the history rebuilt for the model is %+v; it must be what the model said", h)
	}
}

// A rule that is slow holds the answer for a moment, and what comes after waits behind it:
// the end of the turn must not run before its answer is there, or the event hears nothing.
func TestLoopOutputPostKeepsTheOrderWhileARuleIsSlow(t *testing.T) {
	out, _ := loopFeeding(t, postPackage("sleep 0.3; "+colour), "sp", exitAfter(1100), func(send func(fold.Event)) {
		time.Sleep(150 * time.Millisecond)
		for _, e := range answerTurn(2000, "the colour is red") {
			send(e)
		}
	})
	if got := heardBy(out); len(got) != 1 || got[0] != "the colour is red" {
		t.Fatalf("the turn.done event heard %q: the end of the turn arrived while the rule ran and was handled before the answer it ends; last frame:\n%s", got, lastFrame(out))
	}
	s := screen(out)
	a, b := strings.Index(s, "the color is red"), strings.Index(s, "heard<")
	if a < 0 || b < 0 || a > b {
		t.Errorf("the answer must be drawn before the note the end of its turn caused (answer at %d, note at %d); last frame:\n%s", a, b, lastFrame(out))
	}
}

// Two answers in a row, the first held: the second is held behind it and both come out in
// order, each reworded.
func TestLoopOutputPostTwoAnswersInARowComeOutInOrder(t *testing.T) {
	out, _ := loopFeeding(t, postPackage("sleep 0.2; "+colour), "sp", exitAfter(1300), func(send func(fold.Event)) {
		time.Sleep(150 * time.Millisecond)
		send(answer(2000, "first colour"))
		send(answer(2001, " and second colour"))
		send(fold.Event{Type: "agent.turn_done", Seq: 2002})
	})
	if got := heardBy(out); len(got) != 1 || got[0] != "first colour and second colour" {
		t.Errorf("the event heard %q, want the two pieces in order, as the model said them", got)
	}
	if chat := onlyTheChat(out); !strings.Contains(chat, "first color and second color") {
		t.Errorf("both pieces must be reworded and in order; last frame:\n%s", lastFrame(out))
	}
}

// A rule that fails must not cost the user the answer: it is shown as the model said it,
// with a warning that says which rule failed.
func TestLoopOutputPostAFailingRuleShowsTheAnswerAndTellsWhy(t *testing.T) {
	out, _ := loopFeeding(t, postPackage("echo no >&2; exit 3"), "sp", exitAfter(700), func(send func(fold.Event)) {
		time.Sleep(150 * time.Millisecond)
		for _, e := range answerTurn(2000, "the colour is red") {
			send(e)
		}
	})
	s := screen(out)
	if !strings.Contains(onlyTheChat(out), "the colour is red") {
		t.Errorf("the answer must still be shown, as the model said it; last frame:\n%s", lastFrame(out))
	}
	if !strings.Contains(s, "rule sp/r") {
		t.Errorf("the failure must name the rule; last frame:\n%s", lastFrame(out))
	}
}

// An answer that is still with the rules when the conversation is cleared must not appear
// in the new, empty one.
func TestLoopOutputPostALateAnswerDoesNotLandInAClearedConversation(t *testing.T) {
	script := enterLine("/clear")
	script[0].delay = 300 * time.Millisecond // after the answer is with the rules
	script = append(script, exitAfter(900)...)
	out, _ := loopFeeding(t, postPackage("sleep 0.6; "+colour), "sp", script, func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		send(answer(2000, "the colour is red"))
	})
	if s := screen(out); strings.Contains(s, "color is red") || strings.Contains(s, "colour is red") {
		t.Errorf("the answer of the old conversation was drawn after /clear; last frame:\n%s", lastFrame(out))
	}
}

// With no output.post rule nothing is held: an answer is drawn at once. (Only a package
// with that stage costs the loop anything.)
func TestLoopWithoutAnOutputPostRuleAnswersAreNotHeldBack(t *testing.T) {
	files := map[string]string{
		"sp/package.json": execPkg("sp", "", `"rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","argv":["./rule.sh","${input}"]}}]`),
		"sp/rule.sh":      "#!/bin/sh\nsleep 5\nprintf '%s' \"$1\"\n",
	}
	out, _ := loopFeeding(t, files, "sp", exitAfter(500), func(send func(fold.Event)) {
		time.Sleep(150 * time.Millisecond)
		send(answer(2000, "the colour is red"))
	})
	if !strings.Contains(screen(out), "the colour is red") {
		t.Errorf("an input.pre rule must not touch answers; last frame:\n%s", lastFrame(out))
	}
}

// /resume replaces the conversation as /clear does, so an answer still with the rules must
// not land in the one that was brought back; and the answers that follow are reworded again.
func TestLoopOutputPostALateAnswerDoesNotLandInAResumedConversation(t *testing.T) {
	rule := `case "$1" in *slow*) sleep 1.2;; esac; ` + colour
	script := typed("/clear")
	script[0].delay = 300 * time.Millisecond
	script = append(script, scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, typed("/resume ")...)
	script = append(script, scheduledEvent{300 * time.Millisecond, enterEvent()})
	script = append(script, exitAfter(1900)...)
	out, _ := loopFeeding(t, postPackage(rule), "sp", script, func(send func(fold.Event)) {
		time.Sleep(100 * time.Millisecond)
		for _, e := range answerTurn(2000, "first colour") { // saved, then cleared, then resumed
			send(e)
		}
		time.Sleep(350 * time.Millisecond) // after /clear, before the resume is chosen
		send(answer(2010, "slow colour"))
		time.Sleep(850 * time.Millisecond) // after the resume
		send(answer(2011, " third colour"))
	})
	s := screen(out)
	if !strings.Contains(s, "first colour") {
		t.Fatalf("the conversation was not resumed, so this proves nothing; last frame:\n%s", lastFrame(out))
	}
	if strings.Contains(s, "slow") {
		t.Errorf("the answer of the replaced conversation was drawn after /resume; last frame:\n%s", lastFrame(out))
	}
	if !strings.Contains(onlyTheChat(out), "third color") {
		t.Errorf("an answer after the resume must be reworded again (the rules must not stay busy); last frame:\n%s", lastFrame(out))
	}
}
