package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// scriptChat answers every question and, before answering, runs the tool calls scripted
// for that question (by order of asking), as the core's tool loop would.
type scriptChat struct {
	mu    sync.Mutex
	got   []driver.ChatSendParams
	turns [][]driver.ToolCall
	fail  map[int]bool // questions (0-based) that fail after their tools ran
}

func (c *scriptChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }
func (c *scriptChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	n := len(c.got)
	c.got = append(c.got, p)
	c.mu.Unlock()
	if n < len(c.turns) && p.OnTool != nil {
		for _, t := range c.turns[n] {
			p.OnTool(t)
		}
	}
	if c.fail[n] {
		return nil, fmt.Errorf("boom")
	}
	return &driver.ChatSendResult{Text: "answer to " + p.Prompt, Model: "m", Provider: "p"}, nil
}
func (c *scriptChat) system(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[i].System
}

func readOf(path, body string) driver.ToolCall {
	return driver.ToolCall{Name: "read", Arg: path, OK: true, Output: body}
}

func chatOn(core chatSender) (*chatSession, chan fold.Event) {
	out := make(chan fold.Event, 4096)
	return newChatSession(core, out), out
}

// What a turn read is in the system prompt of the next one, so the model does not read
// it again; the first turn has nothing, and the text says what it is.
func TestWhatEarlierTurnsReadReachesTheNextOne(t *testing.T) {
	core := &scriptChat{turns: [][]driver.ToolCall{{readOf("main.go", "package main // UNIQUE-BODY")}}}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "mira main.go", "agent.turn_done")
	sendAndWait(t, c, out, "y que mas", "agent.turn_done")
	if strings.Contains(core.system(0), "UNIQUE-BODY") {
		t.Error("the first turn had nothing to remember")
	}
	sys := core.system(1)
	for _, want := range []string{"UNIQUE-BODY", "read(main.go) returned", "kept by the app", "do not copy it"} {
		if !strings.Contains(sys, want) {
			t.Errorf("the next turn's system prompt lacks %q", want)
		}
	}
}

// Reading the same thing again replaces the old copy: one copy of a file, the newest.
func TestAFileReadTwiceIsRememberedOnce(t *testing.T) {
	core := &scriptChat{turns: [][]driver.ToolCall{
		{readOf("a.go", "OLD-VERSION")},
		{readOf("a.go", "NEW-VERSION")},
	}}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "uno", "agent.turn_done")
	sendAndWait(t, c, out, "dos", "agent.turn_done")
	sendAndWait(t, c, out, "tres", "agent.turn_done")
	sys := core.system(2)
	if strings.Contains(sys, "OLD-VERSION") || strings.Count(sys, "NEW-VERSION") != 1 {
		t.Errorf("want only the newest copy once:\n%s", sys)
	}
}

// A write or edit to a file drops what was remembered of it; another file's reading, which
// the change cannot have touched, stays. A command could have changed anything.
func TestWhatACallCouldHaveChangedIsForgotten(t *testing.T) {
	cases := []struct {
		name     string
		change   driver.ToolCall
		wantA    bool // a.go still remembered
		wantB    bool
		wantGrep bool
	}{
		{"edit of a.go", driver.ToolCall{Name: "edit", Arg: "a.go", OK: true}, false, true, false},
		{"write of a.go", driver.ToolCall{Name: "write", Arg: "a.go", OK: true}, false, true, false},
		{"a command", driver.ToolCall{Name: "run", Arg: "make", OK: true, Output: "ok"}, false, false, false},
		{"a failed edit still may have", driver.ToolCall{Name: "edit", Arg: "a.go", OK: false}, false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core := &scriptChat{turns: [][]driver.ToolCall{
				{readOf("a.go", "BODY-A"), readOf("b.go", "BODY-B"), {Name: "grep", Arg: "x", OK: true, Output: "GREP-HIT"}},
				{tc.change},
			}}
			c, out := chatOn(core)
			sendAndWait(t, c, out, "uno", "agent.turn_done")
			sendAndWait(t, c, out, "dos", "agent.turn_done")
			sendAndWait(t, c, out, "tres", "agent.turn_done")
			sys := core.system(2)
			for _, w := range []struct {
				body string
				want bool
			}{{"BODY-A", tc.wantA}, {"BODY-B", tc.wantB}, {"GREP-HIT", tc.wantGrep}} {
				if got := strings.Contains(sys, w.body); got != w.want {
					t.Errorf("%s remembered = %v, want %v", w.body, got, w.want)
				}
			}
		})
	}
}

// Only what looks is remembered: a command's output and a failed read are not the state
// of anything.
func TestOnlyWhatLooksIsRemembered(t *testing.T) {
	core := &scriptChat{turns: [][]driver.ToolCall{{
		{Name: "run", Arg: "ls", OK: true, Output: "RUN-OUTPUT"},
		{Name: "read", Arg: "gone.go", OK: false, Output: "error: no such file"},
		{Name: "web_fetch", Arg: "https://x.test", OK: true, Output: "WEB-PAGE"},
		readOf("kept.go", "KEPT-BODY"),
	}}}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "uno", "agent.turn_done")
	sendAndWait(t, c, out, "dos", "agent.turn_done")
	sys := core.system(1)
	if !strings.Contains(sys, "KEPT-BODY") {
		t.Error("the read was not remembered")
	}
	for _, no := range []string{"RUN-OUTPUT", "no such file", "WEB-PAGE"} {
		if strings.Contains(sys, no) {
			t.Errorf("%q must not be remembered", no)
		}
	}
}

// A turn that failed leaves its reads to the note the history keeps for it, and what it
// changed is forgotten all the same.
func TestAFailedTurnStillForgetsWhatItChanged(t *testing.T) {
	core := &scriptChat{
		turns: [][]driver.ToolCall{{readOf("a.go", "BODY-A")}, {readOf("c.go", "BODY-C"), {Name: "edit", Arg: "a.go", OK: true}}},
		fail:  map[int]bool{1: true},
	}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "uno", "agent.turn_done")
	sendAndWait(t, c, out, "dos", "agent.failed")
	sendAndWait(t, c, out, "tres", "agent.turn_done")
	sys := core.system(2)
	if strings.Contains(sys, "returned:\nBODY-A") {
		t.Error("a.go was edited by the failed turn; its old text must not be remembered")
	}
	if strings.Contains(sys, "kept by the app so you need not read it again") && strings.Contains(sys, "read(c.go) returned:\nBODY-C") {
		t.Error("a failed turn's own reads belong to its note, not to the memory")
	}
}

// The memory is bounded: newest first within the byte budget, whole results only.
func TestTheMemoryFitsItsBudgetNewestFirst(t *testing.T) {
	var mem []toolEcho
	big := strings.Repeat("x", memoryBudget/3+1)
	for i := 0; i < 5; i++ {
		mem = remember(mem, []toolEcho{echoOf(readOf(fmt.Sprintf("f%d", i), big), fmt.Sprintf("read(f%d)", i))})
	}
	text := toolMemoryText(mem)
	if len(text) > memoryBudget+2048 {
		t.Errorf("text is %d bytes, over the budget of %d", len(text), memoryBudget)
	}
	if !strings.Contains(text, "read(f4)") || !strings.Contains(text, "read(f3)") || strings.Contains(text, "read(f0)") {
		t.Errorf("want the newest kept and the oldest out: %v", strings.Count(text, "returned:"))
	}
	var many []toolEcho
	for i := 0; i < memoryMaxEntries+10; i++ {
		many = remember(many, []toolEcho{echoOf(readOf(fmt.Sprintf("g%d", i), "tiny"), fmt.Sprintf("read(g%d)", i))})
	}
	if len(many) != memoryMaxEntries {
		t.Errorf("entries = %d, want %d", len(many), memoryMaxEntries)
	}
	if toolMemoryText(nil) != "" {
		t.Error("nothing to remember must say nothing")
	}
}

// /clear forgets it, and so does a compaction.
func TestClearAndCompactionEmptyTheMemory(t *testing.T) {
	core := &scriptChat{turns: [][]driver.ToolCall{{readOf("a.go", "BODY-A")}}}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "uno", "agent.turn_done")
	c.mu.Lock()
	n := len(c.memory)
	c.mu.Unlock()
	if n != 1 {
		t.Fatalf("memory = %d entries before /clear", n)
	}
	c.reset()
	c.mu.Lock()
	n = len(c.memory)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("/clear left %d entries", n)
	}

	sum := &summariser{reply: "SUMMARY"}
	c2, out2 := chatWith(sum, longHistory(6, 1000))
	c2.setMemory([]toolEcho{{entry: "read(a.go)", output: "BODY-A", name: "read", arg: "a.go", ok: true}})
	if err := c2.compact(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	collect(out2, "agent.turn_done")
	c2.mu.Lock()
	n = len(c2.memory)
	c2.mu.Unlock()
	if n != 0 {
		t.Errorf("a compaction left %d entries", n)
	}
}

// A conversation brought back with /resume remembers what it had read, rebuilt from the
// saved events the same way the live one folds them.
func TestAResumedConversationRemembersWhatItRead(t *testing.T) {
	ev := func(typ, text string) fold.Event { return fold.Event{Type: typ, Payload: map[string]any{"text": text}} }
	tool := func(name, arg string, ok bool, output string) fold.Event {
		return fold.Event{Type: "chat.tool", Payload: map[string]any{"name": name, "arg": arg, "ok": ok, "output": output}}
	}
	evs := []fold.Event{
		ev("run.prompt", "uno"), tool("read", "a.go", true, "BODY-A"), tool("read", "b.go", true, "BODY-B"), ev("llm.response", "ok"),
		ev("run.prompt", "dos"), tool("edit", "a.go", true, ""), ev("llm.response", "hecho"), ev("llm.response", " otra pieza"),
		ev("run.prompt", "tres"), tool("read", "c.go", true, "BODY-C"), tool("run", "make", true, "x"), fold.Event{Type: "chat.error", Payload: map[string]any{"text": "boom"}},
		ev("run.prompt", "cuatro"), tool("read", "d.go", true, "BODY-D"), ev("llm.response", "ok"),
	}
	mem := memoryFromEvents(evs)
	var got []string
	for _, m := range mem {
		got = append(got, m.entry)
	}
	// a.go was edited; the failed turn ran a command, which empties everything before it;
	// only the last answered turn's read is left.
	if strings.Join(got, "|") != "read(d.go)" {
		t.Errorf("memory = %v", got)
	}
	evs2 := []fold.Event{ev("run.prompt", "uno"), tool("read", "a.go", true, "BODY-A"), ev("llm.response", "ok"),
		ev("run.prompt", "dos"), tool("read", "b.go", true, "BODY-B"), ev("llm.response", "ok"), ev("llm.response", " streamed piece")}
	if m := memoryFromEvents(evs2); len(m) != 2 {
		t.Errorf("a streamed answer must fold once: %d entries", len(m))
	}
	evs3 := append(evs2, fold.Event{Type: "chat.compacted", Payload: map[string]any{"summary": "s", "keep": float64(0)}})
	if m := memoryFromEvents(evs3); len(m) != 0 {
		t.Errorf("a compaction must empty the rebuilt memory: %d entries", len(m))
	}

	core := &scriptChat{}
	c, out := chatOn(core)
	sd := &serveDriver{chat: c}
	sd.ResumeSession([]driver.ChatTurn{{Role: "user", Text: "uno"}, {Role: "assistant", Text: "ok"}})
	sd.ResumeMemory(memoryFromEvents(evs2))
	sendAndWait(t, c, out, "y ahora", "agent.turn_done")
	if s := core.system(0); !strings.Contains(s, "BODY-A") || !strings.Contains(s, "BODY-B") {
		t.Errorf("the resumed memory never reached the system prompt: %.300s", s)
	}
}

// A light turn (no history) carries no memory either, like it carries no tool record.
func TestATurnWithoutHistoryCarriesNoMemory(t *testing.T) {
	core := &scriptChat{}
	c, out := chatOn(core)
	c.setMemory([]toolEcho{{entry: "read(a.go)", output: "BODY-A", name: "read", arg: "a.go", ok: true}})
	sendAndWait(t, c, out, "ping", "agent.turn_done")
	if strings.Contains(core.system(0), "BODY-A") {
		t.Error("a turn with no history must not carry the memory")
	}
}

// A listing and a search are looked at, so they are remembered like a read.
func TestListingsAndSearchesAreRememberedToo(t *testing.T) {
	core := &scriptChat{turns: [][]driver.ToolCall{{
		{Name: "list", Arg: ".", OK: true, Output: "LIST-RESULT"},
		{Name: "grep", Arg: "needle", OK: true, Output: "GREP-RESULT"},
	}}}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "uno", "agent.turn_done")
	sendAndWait(t, c, out, "dos", "agent.turn_done")
	sys := core.system(1)
	for _, want := range []string{"LIST-RESULT", "GREP-RESULT"} {
		if !strings.Contains(sys, want) {
			t.Errorf("%q was not remembered", want)
		}
	}
}

// What a turn that got no answer read is NOT remembered, live or after a resume: it
// already lives in the note the history keeps for that turn, and keeping it twice would
// double its cost.
func TestWhatAFailedTurnReadIsLeftToItsNoteLiveAndOnResume(t *testing.T) {
	ev := func(typ, text string) fold.Event { return fold.Event{Type: typ, Payload: map[string]any{"text": text}} }
	tool := func(name, arg, output string) fold.Event {
		return fold.Event{Type: "chat.tool", Payload: map[string]any{"name": name, "arg": arg, "ok": true, "output": output}}
	}
	failed := fold.Event{Type: "chat.error", Payload: map[string]any{"text": "boom"}}
	cancelled := fold.Event{Type: "chat.cancelled"}
	for name, end := range map[string]fold.Event{"error": failed, "cancelled": cancelled} {
		evs := []fold.Event{ev("run.prompt", "uno"), tool("read", "a.go", "BODY-A"), end}
		if m := memoryFromEvents(evs); len(m) != 0 {
			t.Errorf("%s: a turn with no answer was remembered on resume: %d entries", name, len(m))
		}
		// The next question, with the failed one still pending, has the same effect.
		evs = []fold.Event{ev("run.prompt", "uno"), tool("read", "a.go", "BODY-A"), ev("run.prompt", "dos")}
		if m := memoryFromEvents(evs); len(m) != 0 {
			t.Errorf("%s: an abandoned turn was remembered on resume: %d entries", name, len(m))
		}
	}
	core := &scriptChat{
		turns: [][]driver.ToolCall{{readOf("a.go", "BODY-A")}},
		fail:  map[int]bool{0: true},
	}
	c, out := chatOn(core)
	sendAndWait(t, c, out, "uno", "agent.failed")
	c.mu.Lock()
	n := len(c.memory)
	c.mu.Unlock()
	if n != 0 {
		t.Errorf("live: a failed turn was remembered: %d entries", n)
	}
}

// Cancelling a turn that had already changed a file forgets what was remembered of that
// file: the change happened even though no answer came.
func TestACancelledTurnStillForgetsWhatItChanged(t *testing.T) {
	out := make(chan fold.Event, 64)
	core := &editThenWait{started: make(chan struct{})}
	c := newChatSession(core, out)
	c.setMemory([]toolEcho{{entry: "read(a.go)", output: "OLD-A", name: "read", arg: "a.go", ok: true},
		{entry: "read(b.go)", output: "BODY-B", name: "read", arg: "b.go", ok: true}})
	c.setHistory([]driver.ChatTurn{{Role: "user", Text: "x"}, {Role: "assistant", Text: "y"}})
	if err := c.send(context.Background(), "cambia a.go"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	if !c.cancelTurn() {
		t.Fatal("nothing to cancel")
	}
	c.mu.Lock()
	var got []string
	for _, m := range c.memory {
		got = append(got, m.entry)
	}
	c.mu.Unlock()
	if strings.Join(got, "|") != "read(b.go)" {
		t.Errorf("memory after the cancel = %v, want only read(b.go)", got)
	}
}

// editThenWait runs an edit of a.go and then waits to be cancelled.
type editThenWait struct{ started chan struct{} }

func (e *editThenWait) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (e *editThenWait) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	p.OnTool(driver.ToolCall{Name: "edit", Arg: "a.go", OK: true})
	close(e.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

// A result that arrived clipped is not remembered: the model would take half a file for
// the whole of it. A whole one of the same size is.
func TestAClippedResultIsNeverRemembered(t *testing.T) {
	whole := strings.Repeat("line\n", 100)
	clipped := whole + "\n[output cut]"
	stored := strings.Repeat("y", sessionFieldM) + "…"
	paged := whole + "[showing lines 1-100 of 5000; read again with offset 101 for the rest]\n"
	var mem []toolEcho
	mem = remember(mem, []toolEcho{
		echoOf(readOf("whole.go", whole), "read(whole.go)"),
		echoOf(readOf("clipped.go", clipped), "read(clipped.go)"),
		echoOf(readOf("stored.go", stored), "read(stored.go)"),
		echoOf(readOf("paged.go", paged), "read(paged.go)"),
	})
	if len(mem) != 1 || mem[0].entry != "read(whole.go)" {
		t.Errorf("remembered %+v, want only the whole read", mem)
	}
	if cutOutput("a file that ends with an ellipsis…") {
		t.Error("a short result ending in … is not a cut one")
	}
}
