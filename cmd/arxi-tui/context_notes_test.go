package main

import (
	"context"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The model knows a conversation only through the history the app sends. These tests pin
// that the errors the user saw and the switches of the chat model are in it.

func TestAFailedTurnTellsTheNextModelWhyItFailed(t *testing.T) {
	core := newScriptedCore()
	core.fail["add a round border"] = &driver.Refusal{Code: "failed", Message: "tokenharbor/x: the provider's reply is not JSON (invalid character '<')"}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	_ = c.send(context.Background(), "add a round border")
	eventsOf(t, out, "chat.error")
	idle(t, c)
	_ = c.send(context.Background(), "what did we talk about?")
	waitStarted(t, core, "add a round border")
	waitStarted(t, core, "what did we talk about?")
	h := core.history(1)
	if len(h) != 2 || h[0].Text != "add a round border" {
		t.Fatalf("the failed question must be in the history: %+v", h)
	}
	for _, want := range []string{"the provider's reply is not JSON", "the user saw this error"} {
		if !strings.Contains(h[1].Text, want) {
			t.Errorf("the failed turn's note lacks %q; it reads:\n%s\nConsequence: a model asked later what happened answers that nothing did.", want, h[1].Text)
		}
	}
	if strings.Contains(h[1].Text, "cancelled or interrupted") {
		t.Errorf("a failure is not an interruption by the user:\n%s", h[1].Text)
	}
}

func TestAnErrorPageIsKeptShort(t *testing.T) {
	note := failureNote("the provider's reply is not JSON: <!DOCTYPE html>" + strings.Repeat("<div class=x>", 500))
	if len(note) > failureNoteMax+300 {
		t.Errorf("the failure note is %d bytes; a provider's whole web page must not ride in every later question", len(note))
	}
}

func TestACancelledTurnIsStillAnInterruptionNotAFailure(t *testing.T) {
	if got := endedNote(nil, ""); got != interruptedNote {
		t.Errorf("a stopped turn is noted as %q", got)
	}
	if got := endedNote([]string{"- read(a)"}, "boom"); !strings.Contains(got, "boom") || !strings.HasPrefix(got, "\n\n") {
		t.Errorf("a failed turn after tool calls is noted as %q", got)
	}
}

func TestSwitchingTheModelIsToldToTheNextQuestion(t *testing.T) {
	core := newScriptedCore()
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	_ = c.send(context.Background(), "q1")
	eventsOf(t, out, "agent.turn_done")
	idle(t, c)
	c.noteModelChange("tokenharbor/deepseek", "vyceai/claude-sonnet-4-6")
	_ = c.send(context.Background(), "q2")
	waitStarted(t, core, "q1")
	waitStarted(t, core, "q2")
	h := core.history(1)
	if len(h) != 2 {
		t.Fatalf("history = %+v", h)
	}
	for _, want := range []string{"answer to q1", "from tokenharbor/deepseek to vyceai/claude-sonnet-4-6"} {
		if !strings.Contains(h[1].Text, want) {
			t.Errorf("the new model is not told %q:\n%s", want, h[1].Text)
		}
	}
	idle(t, c)
	_ = c.send(context.Background(), "q3")
	waitStarted(t, core, "q3")
	if n := strings.Count(core.history(2)[1].Text, "switched the chat model"); n != 1 {
		t.Errorf("the switch is written %d times after two questions, want once", n)
	}
}

func TestTheSwitchIsAnEventSoAResumedConversationKnowsIt(t *testing.T) {
	core := newScriptedCore()
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.noteModelChange("a/m1", "b/m2")
	got := eventsOf(t, out, eventModelChanged)
	if !sessionKinds[eventModelChanged] {
		t.Fatal("the switch is not saved with the conversation, so /resume forgets it")
	}
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "q1"}},
		{Type: "llm.response", Payload: map[string]any{"text": "a1"}},
		got[len(got)-1],
		{Type: "run.prompt", Payload: map[string]any{"text": "q2"}},
		{Type: "llm.response", Payload: map[string]any{"text": "a2"}},
	}
	h := historyFromEvents(evs)
	if len(h) != 4 || !strings.Contains(h[1].Text, "from a/m1 to b/m2") || strings.Contains(h[3].Text, "switched") {
		t.Errorf("the resumed history lacks the switch, or has it in the wrong place: %+v", h)
	}
}

func TestAResumedConversationKeepsTheFailedQuestionAndItsError(t *testing.T) {
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "q1"}},
		{Type: "chat.error", Payload: map[string]any{"text": "provider down", "turn_failed": true}},
		{Type: "run.prompt", Payload: map[string]any{"text": "q2"}},
		{Type: "chat.error", Payload: map[string]any{"text": "the queue is full"}}, // not a failed turn
		{Type: "llm.response", Payload: map[string]any{"text": "a2"}},
	}
	h := historyFromEvents(evs)
	if len(h) != 4 || !strings.Contains(h[1].Text, "provider down") {
		t.Fatalf("the failed question and its error must survive /resume: %+v", h)
	}
	if strings.Contains(h[3].Text, "queue is full") {
		t.Errorf("an error that did not end a turn must not be pinned on one: %q", h[3].Text)
	}
}

func TestAModelChangeWithoutAnAnswerBeforeItSaysNothing(t *testing.T) {
	core := newScriptedCore()
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.noteModelChange("a/m1", "b/m2") // nothing was said yet: there is nothing to explain
	_ = c.send(context.Background(), "q1")
	waitStarted(t, core, "q1")
	if h := core.history(0); len(h) != 0 {
		t.Errorf("a fresh conversation was handed a history: %+v", h)
	}
}

func TestClearForgetsPendingNotes(t *testing.T) {
	c := newChatSession(newScriptedCore(), make(chan fold.Event, 8))
	c.noteModelChange("a/m1", "b/m2")
	c.reset()
	if len(c.notes) != 0 {
		t.Error("/clear left a switch note for the next conversation")
	}
}
