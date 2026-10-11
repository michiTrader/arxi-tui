package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// editChat asks the user about one change, as a core under edits=ask does, and
// records the answer it got and the policy it was given.
type editChat struct {
	old bool

	mu    sync.Mutex
	edits []string
	runs  []string
	// noRuns makes the core refuse runs the way a core that predates it does.
	noRuns bool
	answer chan bool
}

func (*editChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }

func (c *editChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.edits = append(c.edits, p.Edits)
	c.runs = append(c.runs, p.Runs)
	c.mu.Unlock()
	if c.old && p.Edits != "" {
		return nil, driver.ErrEditsUnsupported
	}
	if c.noRuns && p.Runs != "" {
		return nil, driver.ErrRunsUnsupported
	}
	// A real core only asks under "ask"; under deny it never offers the tool.
	if p.OnApproval != nil && (p.Edits == "ask" || p.Runs == "ask") {
		c.answer <- p.OnApproval(ctx, driver.Approval{CallID: "c1", Name: "edit", Arg: "main.go", Summary: "Added 1 line", Diff: "    1 +x"})
	}
	return &driver.ChatSendResult{Text: "done"}, nil
}

func wait(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestAChangeWaitsForTheUsersAnswer(t *testing.T) {
	for _, allow := range []bool{true, false} {
		out := make(chan fold.Event, 32)
		core := &editChat{answer: make(chan bool, 1)}
		c := newChatSession(core, out)
		c.setWorkdir("/proj")
		c.setEdits("ask")
		if err := c.send(context.Background(), "change it"); err != nil {
			t.Fatal(err)
		}
		wait(t, c.pendingNow)
		select {
		case got := <-core.answer:
			t.Fatalf("the turn went on before the user answered: %v", got)
		case <-time.After(30 * time.Millisecond):
		}
		if !c.decide(allow) {
			t.Fatal("there was a change waiting")
		}
		if got := <-core.answer; got != allow {
			t.Errorf("the core was told %v, the user said %v", got, allow)
		}
		wait(t, func() bool { return !c.busyNow() })
		if c.pendingNow() || c.decide(true) {
			t.Error("nothing should wait any more")
		}
	}
}

func TestCancellingTheTurnDeclinesTheWaitingChange(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &editChat{answer: make(chan bool, 1)}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	c.setEdits("ask")
	if err := c.send(context.Background(), "change it"); err != nil {
		t.Fatal(err)
	}
	wait(t, c.pendingNow)
	c.cancelTurn()
	if got := <-core.answer; got {
		t.Error("a cancelled turn must never allow a change")
	}
}

func TestTheModeDecidesWhatTheModelMayDo(t *testing.T) {
	for mode, want := range map[string]string{"ask": "ask", "auto": "allow", "plan": "deny", "full access": "allow"} {
		sd := &serveDriver{chat: newChatSession(&editChat{}, make(chan fold.Event, 4))}
		sd.SetMode(mode)
		if sd.chat.edits != want {
			t.Errorf("mode %q: edits = %q, want %q", mode, sd.chat.edits, want)
		}
	}
}

func TestTheModeAlsoDecidesWhetherCommandsMayRun(t *testing.T) {
	for mode, want := range map[string]string{"ask": "ask", "auto": "ask", "plan": "deny", "full access": "allow"} {
		sd := &serveDriver{chat: newChatSession(&editChat{}, make(chan fold.Event, 4))}
		sd.SetMode(mode)
		if sd.chat.runs != want {
			t.Errorf("mode %q: runs = %q, want %q", mode, sd.chat.runs, want)
		}
	}
}

func TestACommandWaitsForTheUsersAnswerLikeAChange(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &editChat{answer: make(chan bool, 1)}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	c.setRuns("ask")
	if err := c.send(context.Background(), "run the tests"); err != nil {
		t.Fatal(err)
	}
	wait(t, c.pendingNow)
	if !c.decide(true) {
		t.Fatal("there was a command waiting")
	}
	if got := <-core.answer; !got {
		t.Error("the core was not told the user allowed it")
	}
	if core.runs[0] != "ask" {
		t.Errorf("runs sent = %q", core.runs)
	}
}

func TestADeniedRunsPolicyIsNotSentAtAll(t *testing.T) {
	// Plan mode: an older core must not be made to refuse the whole request over a
	// parameter that only says "no".
	out := make(chan fold.Event, 32)
	core := &editChat{noRuns: true, answer: make(chan bool, 1)}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	c.setRuns("deny")
	if err := c.send(context.Background(), "list the files here"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() })
	if len(core.runs) != 1 || core.runs[0] != "" {
		t.Errorf("runs sent = %q; want one request without runs", core.runs)
	}
}

func TestACoreThatCannotRunKeepsEditingAndSaysSo(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &editChat{noRuns: true, answer: make(chan bool, 1)}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	c.setEdits("ask")
	c.setRuns("ask")
	if err := c.send(context.Background(), "list the files here"); err != nil {
		t.Fatal(err)
	}
	wait(t, c.pendingNow)
	c.decide(true)
	wait(t, func() bool { return !c.busyNow() })
	if len(core.runs) != 2 || core.runs[0] != "ask" || core.runs[1] != "" {
		t.Errorf("runs sent = %q; want ask, then nothing", core.runs)
	}
	if len(core.edits) != 2 || core.edits[1] != "ask" {
		t.Errorf("edits sent = %q; the second try must keep editing on", core.edits)
	}
	close(out)
	var warned bool
	for ev := range out {
		if ev.Type == "chat.warn" {
			warned = true
		}
	}
	if !warned {
		t.Error("the user must be told commands are unavailable")
	}
}

func TestACoreThatCannotEditStillLooksAndSaysSo(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := &editChat{old: true}
	c := newChatSession(core, out)
	c.setWorkdir("/proj")
	c.setEdits("ask")
	if err := c.send(context.Background(), "list the files here"); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { return !c.busyNow() })
	if len(core.edits) != 2 || core.edits[0] != "ask" || core.edits[1] != "" {
		t.Errorf("edits sent = %q; want ask, then nothing", core.edits)
	}
	close(out)
	var warned, answered bool
	for ev := range out {
		warned = warned || ev.Type == "chat.warn"
		answered = answered || ev.Type == "llm.response"
	}
	if !warned || !answered {
		t.Errorf("warned=%v answered=%v", warned, answered)
	}
}

func TestOnlyYNAndEscAnswerAChange(t *testing.T) {
	r := func(c rune) term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{c}} }
	for _, tc := range []struct {
		k             term.Key
		allow, decide bool
	}{
		{r('y'), true, true}, {r('Y'), true, true}, {r('n'), false, true}, {r('N'), false, true},
		{term.Key{Type: term.KeyEscape}, false, true},
		{r('x'), false, false}, {r(' '), false, false}, {term.Key{Type: term.KeyEnter}, false, false},
	} {
		allow, decided := approvalKey(tc.k)
		if allow != tc.allow || decided != tc.decide {
			t.Errorf("key %+v: allow=%v decided=%v, want %v %v", tc.k, allow, decided, tc.allow, tc.decide)
		}
	}
}
