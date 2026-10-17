package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// Two things the status bar got wrong, both reported from a real session:
//
//  1. A turn that read files and then timed out left only the NAMES of the files in the
//     history, so "continua" started from a few hundred tokens and the bar fell from
//     ctx 36k to ctx 3.5k, as if the conversation had restarted. The model also had to
//     read everything again. What the tools returned is now kept (within a budget).
//  2. /resume showed no context: the figures were never saved. They are now.

func bigOutput(n int) string { return strings.Repeat("x", n) }

func TestAFailedTurnKeepsWhatItsToolsReturnedSoContinueDoesNotStartOver(t *testing.T) {
	core := &seenChat{
		tool: &driver.ToolCall{Name: "read", Arg: "strategies/smc/_strategy.py", OK: true, Output: "PRECIOUS-FILE-CONTENT " + bigOutput(4000)},
		fail: func(p driver.ChatSendParams) error {
			if p.Prompt == "mira la estrategia" {
				return errors.New("context deadline exceeded")
			}
			return nil
		},
	}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	c.setEdits("ask")
	sendAndWait(t, c, out, "mira la estrategia", "agent.failed")
	core.tool = nil
	sendAndWait(t, c, out, "continua", "agent.turn_done")

	var carried string
	for _, h := range core.last().History {
		carried += h.Text + "\n"
	}
	if !strings.Contains(carried, "PRECIOUS-FILE-CONTENT") {
		t.Fatalf("what the failed turn read was thrown away; the next request carries only: %.300s", carried)
	}
	if !strings.Contains(carried, "read(strategies/smc/_strategy.py)") {
		t.Errorf("the tool is no longer named: %.300s", carried)
	}
	if !strings.Contains(carried, "kept by the app") {
		t.Errorf("the model is not told this is the app's record, not its own words: %.400s", carried)
	}
}

func TestWhatAFailedTurnKeepsIsBoundedAndNewestFirst(t *testing.T) {
	var echoes []toolEcho
	for i := 0; i < 20; i++ {
		echoes = append(echoes, toolEcho{entry: "read(f" + string(rune('a'+i)) + ")", output: "OUT-" + string(rune('a'+i)) + bigOutput(8*1024)})
	}
	got := unansweredWith(whyFailed, echoes)
	if len(got) > echoBudget+8*1024 {
		t.Errorf("kept %d bytes, the budget is %d", len(got), echoBudget)
	}
	if !strings.Contains(got, "OUT-t") { // the newest
		t.Error("the newest result must be the one kept")
	}
	if strings.Contains(got, "OUT-a") { // the oldest
		t.Error("the oldest result should have been dropped to stay in the budget")
	}
	if !strings.Contains(got, "left out") {
		t.Error("dropping results must be said, so the model knows it may re-read")
	}
	if !strings.Contains(got, "read(fa)") {
		t.Error("a dropped result is still named")
	}
}

func TestAFailedTurnWithoutToolOutputIsAsBefore(t *testing.T) {
	got := unansweredWith(whyFailed, []toolEcho{{entry: "run(ls)"}})
	if got != unansweredText(whyFailed, []string{"run(ls)"}) {
		t.Errorf("no output, nothing to add: %q", got)
	}
	if unansweredWith(whyFailed, nil) != unansweredText(whyFailed, nil) {
		t.Error("no tools, no change")
	}
}

func TestACancelledTurnKeepsWhatItsToolsReturnedToo(t *testing.T) {
	core := &seenChat{tool: &driver.ToolCall{Name: "read", Arg: "a.go", OK: true, Output: "CANCELLED-TURN-READ"},
		fail: func(p driver.ChatSendParams) error { <-make(chan struct{}); return nil }}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	if err := c.send(context.Background(), "lee a.go"); err != nil {
		t.Fatal(err)
	}
	drain(out, "chat.tool", 2*time.Second)
	if !c.cancelTurn() {
		t.Fatal("nothing to cancel")
	}
	var carried string
	c.mu.Lock()
	for _, h := range c.history {
		carried += h.Text
	}
	c.mu.Unlock()
	if !strings.Contains(carried, "CANCELLED-TURN-READ") {
		t.Errorf("a cancelled turn lost what it read: %.200s", carried)
	}
}

// /resume must rebuild the same history the live session would have had.
func TestResumeRebuildsWhatAFailedTurnRead(t *testing.T) {
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "mira la estrategia"}},
		{Type: "chat.tool", Payload: map[string]any{"name": "read", "arg": "s.py", "ok": true, "output": "RESUMED-READ"}},
		{Type: "chat.error", Payload: map[string]any{"text": "context deadline exceeded"}},
		{Type: "run.prompt", Payload: map[string]any{"text": "continua"}},
		{Type: "chat.tool", Payload: map[string]any{"name": "list", "arg": ".", "ok": true, "output": "SECOND-TURN-LIST"}},
		{Type: "llm.response", Payload: map[string]any{"text": "listo"}},
	}
	h := historyFromEvents(evs)
	if len(h) != 3 {
		t.Fatalf("history = %+v", h)
	}
	if !strings.Contains(h[0].Text, "RESUMED-READ") {
		t.Errorf("the failed turn's read is gone after /resume: %q", h[0].Text)
	}
	if strings.Contains(h[0].Text, "SECOND-TURN-LIST") || strings.Contains(h[2].Text, "RESUMED-READ") {
		t.Errorf("one turn's tool output leaked into another: %q / %q", h[0].Text, h[2].Text)
	}
	if strings.Contains(h[2].Text, "SECOND-TURN-LIST") {
		t.Errorf("an answered turn keeps its tools out of the history (they ride in the tool record): %q", h[2].Text)
	}
}

// The bar after /resume, from the saved file, through the same fold the screen uses.
func TestResumedConversationRestoresTheContextFigure(t *testing.T) {
	l, dir := newLog(t)
	l.Record(ev("run.prompt", "hello"))
	l.Record(fold.Event{Type: "llm.response", Payload: map[string]any{"text": "hi", "tokens_in": 90000.0, "tokens_out": 500.0, "context_tokens": 20000.0}})
	l.Record(ev("run.prompt", "mira"))
	l.Record(fold.Event{Type: "chat.usage", Payload: map[string]any{"context_tokens": 30000.0}})
	l.Record(fold.Event{Type: "chat.usage", Payload: map[string]any{"context_tokens": 36000.0}})
	l.Record(ev("chat.error", "context deadline exceeded"))

	evs, err := loadSession(dir, listSessions(dir)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := fold.Fold(evs).LastContext; got != 36000 {
		t.Fatalf("a resumed conversation whose last turn failed shows ctx %d, want the 36000 the last ask held", got)
	}
}

func TestAnAnsweredTurnNeedsNoSeparateUsageLine(t *testing.T) {
	l, dir := newLog(t)
	l.Record(ev("run.prompt", "q"))
	for i := 0; i < 30; i++ { // a long tool loop reports once per ask
		l.Record(fold.Event{Type: "chat.usage", Payload: map[string]any{"context_tokens": float64(1000 * (i + 1))}})
	}
	l.Record(fold.Event{Type: "llm.response", Payload: map[string]any{"text": "a", "tokens_in": 99.0, "tokens_out": 1.0, "context_tokens": 30000.0}})
	evs, _ := loadSession(dir, listSessions(dir)[0].ID)
	n := 0
	for _, e := range evs {
		if e.Type == "chat.usage" {
			n++
		}
	}
	if n != 0 {
		t.Errorf("%d chat.usage lines were written for an answered turn; the answer carries its own size", n)
	}
	if got := fold.Fold(evs).LastContext; got != 30000 {
		t.Errorf("LastContext = %d, want 30000", got)
	}
}

func TestOnlyTheLastUsageOfAFailedTurnIsSaved(t *testing.T) {
	l, dir := newLog(t)
	l.Record(ev("run.prompt", "q"))
	for i := 0; i < 30; i++ {
		l.Record(fold.Event{Type: "chat.usage", Payload: map[string]any{"context_tokens": float64(1000 * (i + 1))}})
	}
	l.Record(ev("chat.error", "boom"))
	evs, _ := loadSession(dir, listSessions(dir)[0].ID)
	n := 0
	for _, e := range evs {
		if e.Type == "chat.usage" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d usage lines for one failed turn, want exactly the last one", n)
	}
}

func TestACancelledTurnAndANewQuestionAlsoFlushTheUsage(t *testing.T) {
	for _, next := range []fold.Event{{Type: "chat.cancelled"}, {Type: "run.prompt", Payload: map[string]any{"text": "again"}}} {
		l, dir := newLog(t)
		l.Record(ev("run.prompt", "q"))
		l.Record(fold.Event{Type: "chat.usage", Payload: map[string]any{"context_tokens": 7000.0}})
		l.Record(next)
		evs, _ := loadSession(dir, listSessions(dir)[0].ID)
		if got := fold.Fold(evs).LastContext; got != 7000 {
			t.Errorf("after %s the resumed ctx is %d, want 7000", next.Type, got)
		}
	}
}

func TestACompactionIsSavedSoResumeKnowsTheShrunkenSize(t *testing.T) {
	l, dir := newLog(t)
	l.Record(ev("run.prompt", "q"))
	l.Record(fold.Event{Type: "llm.response", Payload: map[string]any{"text": "a", "tokens_in": 1.0, "tokens_out": 1.0, "context_tokens": 200000.0}})
	l.Record(fold.Event{Type: "chat.compacted", Payload: map[string]any{"summary": "S", "keep": 0.0, "context_tokens": 3000.0}})
	evs, _ := loadSession(dir, listSessions(dir)[0].ID)
	if got := fold.Fold(evs).LastContext; got != 3000 {
		t.Errorf("resumed ctx after a compaction = %d, want the compacted 3000", got)
	}
}

// With no figure at all (the first turn failed, or a file saved by an older arxi), the bar
// estimates from the history and says so with a "~".
type histDriver struct {
	testDriver
	h []driver.ChatTurn
}

func (d *histDriver) ChatHistory() []driver.ChatTurn { return d.h }

func TestHeldContextEstimatesFromTheHistoryOnlyWhenNothingWasReported(t *testing.T) {
	d := &histDriver{h: []driver.ChatTurn{{Role: "user", Text: bigOutput(4000)}, {Role: "assistant", Text: bigOutput(4000)}}}
	if n, est := heldContext(0, d); n != 2000 || !est {
		t.Errorf("no figure: got %d est=%v, want an estimate of 2000", n, est)
	}
	if n, est := heldContext(36000, d); n != 36000 || est {
		t.Errorf("a reported figure must win and not be marked: got %d est=%v", n, est)
	}
	if n, est := heldContext(0, &histDriver{}); n != 0 || est {
		t.Errorf("an empty conversation shows nothing: got %d est=%v", n, est)
	}
	if n, est := heldContext(0, &testDriver{}); n != 0 || est {
		t.Errorf("a driver with no history: got %d est=%v", n, est)
	}
}

func TestTheBarMarksAnEstimate(t *testing.T) {
	got := statusUsageHeld(3000, true, 0, 0, 270_000, autoCompactSetting{})
	if !strings.HasPrefix(got, "ctx ~3k/270k (1%)") {
		t.Errorf("bar = %q", got)
	}
	if got := statusUsageHeld(3000, false, 1, 1, 270_000, autoCompactSetting{}); strings.Contains(got, "~") {
		t.Errorf("a real count must not be marked: %q", got)
	}
}

// One failed turn's reads must not be pinned onto the next failed turn.
func TestOneFailedTurnsReadsDoNotLeakIntoTheNext(t *testing.T) {
	core := &seenChat{
		tool: &driver.ToolCall{Name: "read", Arg: "a.go", OK: true, Output: "FIRST-TURN-READ"},
		fail: func(p driver.ChatSendParams) error { return errors.New("boom") },
	}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.setWorkdir(t.TempDir())
	sendAndWait(t, c, out, "primero", "agent.failed")
	core.tool = nil // the second turn runs no tool at all
	sendAndWait(t, c, out, "segundo", "agent.failed")

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) != 2 {
		t.Fatalf("history = %+v", c.history)
	}
	if !strings.Contains(c.history[0].Text, "FIRST-TURN-READ") {
		t.Errorf("the first turn lost its read: %q", c.history[0].Text)
	}
	if strings.Contains(c.history[1].Text, "FIRST-TURN-READ") {
		t.Errorf("the first turn's read was pinned onto the second: %q", c.history[1].Text)
	}
}

// A log can lack the error line (the program died mid-turn). The next question closes the
// unanswered one, and its reads belong to it alone.
func TestResumeDoesNotLeakReadsAcrossAnUnclosedTurn(t *testing.T) {
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "uno"}},
		{Type: "chat.tool", Payload: map[string]any{"name": "read", "arg": "a.go", "ok": true, "output": "UNCLOSED-TURN-READ"}},
		{Type: "run.prompt", Payload: map[string]any{"text": "dos"}},
		{Type: "chat.error", Payload: map[string]any{"text": "boom"}},
	}
	h := historyFromEvents(evs)
	if len(h) != 2 {
		t.Fatalf("history = %+v", h)
	}
	if !strings.Contains(h[0].Text, "UNCLOSED-TURN-READ") {
		t.Errorf("the unclosed turn lost its read: %q", h[0].Text)
	}
	if strings.Contains(h[1].Text, "UNCLOSED-TURN-READ") {
		t.Errorf("its read leaked onto the next turn: %q", h[1].Text)
	}
}

// The note about a request that got no answer used to be stored as the ASSISTANT's reply
// ("[No reply was given ...]"). A model that sees such a reply in its own voice copies it,
// and the user read that sentence as the answer to "reintenta" three times in a row.
// Whatever ended the turn, the note is the app's, in the user's message, and nothing is put
// in the assistant's mouth.
func TestAnUnansweredRequestPutsNoWordsInTheAssistantsMouth(t *testing.T) {
	for name, end := range map[string]func(c *chatSession, core *seenChat){
		"failed":    func(c *chatSession, core *seenChat) {},
		"cancelled": nil,
	} {
		t.Run(name, func(t *testing.T) {
			core := &seenChat{fail: func(p driver.ChatSendParams) error {
				if p.Prompt == "haz algo" && name == "failed" {
					return errors.New("boom")
				}
				return nil
			}}
			out := make(chan fold.Event, 64)
			c := newChatSession(core, out)
			c.setWorkdir(t.TempDir())
			c.setEdits("ask")
			if end == nil {
				c.mu.Lock()
				c.busy, c.cancel, c.turnText = true, func() {}, "haz algo"
				c.mu.Unlock()
				c.cancelTurn()
			} else {
				sendAndWait(t, c, out, "haz algo", "agent.failed")
			}
			sendAndWait(t, c, out, "reintenta", "agent.turn_done")
			h := core.last().History
			if len(h) != 1 || h[0].Role != "user" {
				t.Fatalf("history = %+v; want one user message and no assistant reply", h)
			}
			for _, want := range []string{"haz algo", "not typed by the user", "Never repeat this note"} {
				if !strings.Contains(h[0].Text, want) {
					t.Errorf("the note lacks %q: %q", want, h[0].Text)
				}
			}
			if strings.Index(h[0].Text, "haz algo") > strings.Index(h[0].Text, "No reply") {
				t.Errorf("the request must come before the note: %q", h[0].Text)
			}
		})
	}
}
