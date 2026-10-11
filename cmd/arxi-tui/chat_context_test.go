package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// seenChat records every request and answers or fails as told.
type seenChat struct {
	mu   sync.Mutex
	got  []driver.ChatSendParams
	fail func(p driver.ChatSendParams) error
	tool *driver.ToolCall
}

func (c *seenChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (c *seenChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.got = append(c.got, p)
	c.mu.Unlock()
	if c.tool != nil && p.OnTool != nil {
		p.OnTool(*c.tool)
	}
	if c.fail != nil {
		if err := c.fail(p); err != nil {
			return nil, err
		}
	}
	return &driver.ChatSendResult{Text: "answer to " + p.Prompt, Model: "m", Provider: "p"}, nil
}
func (c *seenChat) last() driver.ChatSendParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[len(c.got)-1]
}

func sendAndWait(t *testing.T, c *chatSession, out chan fold.Event, text, until string) {
	t.Helper()
	if err := c.send(context.Background(), text); err != nil {
		t.Fatal(err)
	}
	drain(out, until, 2*time.Second)
}

// "ping" carries nothing: no tools, no history, no thinking, no project rules. Counter-
// factual: the same words inside a longer message are an ordinary turn.
func TestAPingIsAnsweredWithNothingAttached(t *testing.T) {
	core := &seenChat{}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	c.setEdits("ask")
	c.setEffort("high")
	sendAndWait(t, c, out, "arregla el banner", "agent.turn_done")

	sendAndWait(t, c, out, "¿Qué modelo eres?", "agent.turn_done")
	p := core.last()
	if p.Workdir != "" || p.Edits != "" || p.Effort != "" || len(p.History) != 0 {
		t.Errorf("a light turn carried baggage: workdir=%q edits=%q effort=%q history=%d", p.Workdir, p.Edits, p.Effort, len(p.History))
	}
	if p.System != chatSystem() {
		t.Errorf("a light turn's system must be the bare sentence: %q", p.System)
	}

	sendAndWait(t, c, out, "ping, y arregla el banner", "agent.turn_done")
	if p := core.last(); p.Workdir == "" || len(p.History) == 0 || p.Effort != "high" {
		t.Errorf("a message that only starts with ping is an ordinary turn: %+v", p)
	}
	// The light exchange itself is part of the conversation afterwards.
	if h := core.last().History; !strings.Contains(h[len(h)-2].Text, "modelo") {
		t.Errorf("the light turn did not enter the history: %+v", h)
	}
}

func TestLightTurnsCanBeSwitchedOffAndTheListEdited(t *testing.T) {
	if isLightMessage("hola", lightPhrasesDefault, false) {
		t.Error("switched off, nothing is light")
	}
	if !isLightMessage("  HOLA!! ", lightPhrasesDefault, true) || !isLightMessage("¿Qué modelo eres?", lightPhrasesDefault, true) {
		t.Error("case, accents and punctuation must not matter")
	}
	for _, m := range []string{"reintenta", "ok", "arregla eso", "hola arregla eso", ""} {
		if isLightMessage(m, lightPhrasesDefault, true) {
			t.Errorf("%q needs the conversation and must not be light", m)
		}
	}
	if !isLightMessage("buen día equipo", "buen dia equipo, otra", true) {
		t.Error("the user's own phrases must work")
	}
}

// The failure that made "reintenta" useless: the request of a turn that failed, and then
// the model was switched, never reached the next model. Counterfactual: the old history
// held only answered questions.
func TestARetryAfterAFailureReachesTheNextModelWithTheRequest(t *testing.T) {
	core := &seenChat{fail: func(p driver.ChatSendParams) error {
		if p.Prompt == "renombra el archivo del escritorio" {
			return errors.New("rate limited")
		}
		return nil
	}, tool: &driver.ToolCall{Name: "run", Arg: "dir C:\\Users\\x\\Desktop", OK: true}}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	sendAndWait(t, c, out, "renombra el archivo del escritorio", "chat.error")
	c.noteModelSwitch(context.Background(), "a/one")
	c.takeModelNote()
	c.noteModelSwitch(context.Background(), "b/two")
	core.tool = nil
	sendAndWait(t, c, out, "reintenta", "agent.turn_done")

	p := core.last()
	var joined string
	for _, h := range p.History {
		joined += h.Role + ": " + h.Text + "\n"
	}
	for _, want := range []string{"renombra el archivo del escritorio", "No reply was given", "dir C:", "retry"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the next model was not given %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(p.System, "yours to carry on") {
		t.Errorf("the model note must not tell it to disown the conversation: %q", p.System)
	}
}

func TestACancelledRequestStaysInTheHistory(t *testing.T) {
	core := &ctxChat{started: make(chan struct{})}
	out := make(chan fold.Event, 16)
	c := newChatSession(core, out)
	if err := c.send(context.Background(), "haz algo largo"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	c.cancelTurn()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) != 2 || c.history[0].Text != "haz algo largo" || !strings.Contains(c.history[1].Text, "cancelled") {
		t.Errorf("history = %+v", c.history)
	}
}

func TestTheToolsOfAnAnswerAreRecordedInTheHistory(t *testing.T) {
	core := &seenChat{tool: &driver.ToolCall{Name: "read", Arg: "main.go", OK: true}}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	sendAndWait(t, c, out, "mira main.go", "agent.turn_done")
	c.mu.Lock()
	defer c.mu.Unlock()
	if got := c.history[1].Text; !strings.Contains(got, "answer to mira main.go") || !strings.Contains(got, "read(main.go)") {
		t.Errorf("assistant history entry = %q", got)
	}
}

func TestAResumedConversationKeepsItsUnansweredRequest(t *testing.T) {
	ev := func(typ, text string) fold.Event { return fold.Event{Type: typ, Payload: map[string]any{"text": text}} }
	h := historyFromEvents([]fold.Event{ev("run.prompt", "uno"), ev("llm.response", "a"), ev("run.prompt", "dos"), ev("chat.error", "boom"), ev("run.prompt", "reintenta")})
	if len(h) != 4 || h[2].Text != "dos" || !strings.Contains(h[3].Text, "No reply") {
		t.Errorf("history = %+v", h)
	}
}
