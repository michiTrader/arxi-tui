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

// seqChat asks the user about n commands in a row inside ONE turn, as a core does when the
// model chains several tool calls; the turn took its policy ("ask") when it began.
type seqChat struct {
	n       int
	name    string
	started chan struct{}
	got     chan []bool
}

func (*seqChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }

func (c *seqChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	var answers []bool
	for i := 0; i < c.n; i++ {
		if i == 0 {
			close(c.started)
		}
		answers = append(answers, p.OnApproval(ctx, driver.Approval{CallID: "c", Name: c.name, Arg: "x", Summary: "s"}))
	}
	c.got <- answers
	return &driver.ChatSendResult{Text: "done"}, nil
}

func seqRig(t *testing.T, name string, n int) (*serveDriver, *seqChat) {
	t.Helper()
	core := &seqChat{n: n, name: name, started: make(chan struct{}), got: make(chan []bool, 1)}
	c := newChatSession(core, make(chan fold.Event, 64))
	c.setWorkdir("/proj")
	sd := &serveDriver{chat: c}
	sd.SetMode("ask")
	if err := c.send(context.Background(), "do several things"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	return sd, core
}

// The reported bug: answer yes to the first command, switch to full access, and the
// turn kept asking for every next one.
func TestSwitchingToFullAccessStopsTheQuestionsOfTheTurnInProgress(t *testing.T) {
	for _, name := range []string{"run", "edit", "write", "web_fetch", "web_search"} {
		sd, core := seqRig(t, name, 3)
		wait(t, sd.chat.pendingNow)
		sd.chat.decide(true) // the user says yes to the first
		// The second question may or may not be up yet; the mode change must settle it
		// and every later one without a key press.
		sd.SetMode("full access")
		select {
		case got := <-core.got:
			for i, a := range got {
				if !a {
					t.Errorf("%s: answer %d = false, want all allowed: %v", name, i, got)
				}
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: the turn is still waiting for the user after full access", name)
		}
	}
}

// A question that is on screen when the mode changes is answered by the new mode.
func TestAWaitingQuestionIsAnsweredWhenTheModeAllowsIt(t *testing.T) {
	sd, core := seqRig(t, "run", 1)
	wait(t, sd.chat.pendingNow)
	sd.SetMode("full access")
	select {
	case got := <-core.got:
		if len(got) != 1 || !got[0] {
			t.Errorf("got %v, want the waiting command allowed", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the waiting question was not settled by the mode change")
	}
}

// Moving to a mode that does NOT allow it (plan, auto for a command) must not answer yes.
func TestAModeThatDoesNotAllowItLeavesTheQuestionWaiting(t *testing.T) {
	for _, mode := range []string{"auto", "plan", "ask"} {
		sd, core := seqRig(t, "run", 1)
		wait(t, sd.chat.pendingNow)
		sd.SetMode(mode)
		time.Sleep(40 * time.Millisecond)
		if !sd.chat.pendingNow() {
			t.Errorf("mode %q answered a command on its own", mode)
		}
		sd.chat.decide(false)
		<-core.got
	}
}

// Auto allows edits, not commands: switching ask -> auto settles an edit but not a run.
func TestAutoSettlesAnEditButNotACommand(t *testing.T) {
	sd, core := seqRig(t, "edit", 1)
	wait(t, sd.chat.pendingNow)
	sd.SetMode("auto")
	select {
	case got := <-core.got:
		if !got[0] {
			t.Error("auto allows edits")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an edit under auto still waits")
	}
}

// What a mode never decides (a change to the interface that sets what their keyboard
// does later) is not settled by it: liveAllows only knows the four kinds.
func TestFullAccessDoesNotAnswerWhatNoModeCovers(t *testing.T) {
	c := newChatSession(&editChat{}, make(chan fold.Event, 4))
	c.setEdits(policyAllow)
	c.setRuns(policyAllow)
	c.setWeb(policyAllow)
	c.setOutside(policyAllow)
	for _, name := range []string{"ui_edit", "pkg_x_run", "something_else"} {
		if c.liveAllows(driver.Approval{Name: name}) {
			t.Errorf("%q was allowed by a mode", name)
		}
	}
	if !c.liveAllows(driver.Approval{Name: "folder"}) || !c.liveAllows(driver.Approval{Name: "run"}) {
		t.Error("the four kinds a mode decides must follow it")
	}
}

func TestUpgradedOnlyGoesFromAskToAllow(t *testing.T) {
	for _, tc := range []struct{ started, live, want string }{
		{policyAsk, policyAllow, policyAllow},
		{policyAsk, policyAsk, policyAsk},
		{policyAsk, policyDeny, policyAsk},
		{policyAllow, policyAsk, policyAllow}, // never narrows what the turn started with
		{policyDeny, policyAllow, policyDeny}, // a tool not offered at the start stays out
	} {
		if got := upgraded(tc.started, tc.live); got != tc.want {
			t.Errorf("upgraded(%s,%s) = %s, want %s", tc.started, tc.live, got, tc.want)
		}
	}
}

// A package tool that runs a program is decided by the policy of the turn; if the user moved
// to full access after the turn began, it must not ask any more.
func TestAPackageToolFollowsTheModeChangedDuringTheTurn(t *testing.T) {
	core := &seenChat{}
	b := newPackageToolbox()
	b.publish(packageSet{pkgs: []installedPackage{toolPkg(t, "p", true, execT("go"))}})
	c, out := pkgToolChat(t, core, b)
	c.setRuns(policyAsk)
	sendAndWait(t, c, out, "arregla el banner", "agent.turn_done")
	p := core.last()
	if p.OnClientTool == nil {
		t.Fatal("no client tools lent")
	}
	// The user is asked once before the switch ... (nobody answers: the turn would block, so
	// answer no from a goroutine)
	go func() {
		wait(t, c.pendingNow)
		c.decide(false)
	}()
	if res := p.OnClientTool(context.Background(), callOf("pkg_p_go", "x")); res.Summary != "You did not allow this" {
		t.Fatalf("under ask the tool must wait for the user: %+v", res)
	}
	// ... and not at all after it.
	c.setRuns(policyAllow)
	done := make(chan driver.ClientToolResult, 1)
	go func() { done <- p.OnClientTool(context.Background(), callOf("pkg_p_go", "x")) }()
	select {
	case res := <-done:
		if res.Summary == "You did not allow this" {
			t.Errorf("full access still asked: %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the tool is still waiting for the user after full access")
	}
}
