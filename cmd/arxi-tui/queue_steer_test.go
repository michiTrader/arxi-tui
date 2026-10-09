package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// scriptedCore answers each question from a script: it can report tool calls, wait to be
// released (or cancelled), fail, and it records the history it was asked with.
type scriptedCore struct {
	mu      sync.Mutex
	asked   []string
	hists   [][]driver.ChatTurn
	tools   map[string][]driver.ToolCall // prompt -> calls to report before answering
	release map[string]chan struct{}     // prompt -> gate (absent = answers at once)
	fail    map[string]error
	started chan string
}

func newScriptedCore() *scriptedCore {
	return &scriptedCore{tools: map[string][]driver.ToolCall{}, release: map[string]chan struct{}{},
		fail: map[string]error{}, started: make(chan string, 32)}
}

func (s *scriptedCore) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (s *scriptedCore) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	s.mu.Lock()
	s.asked = append(s.asked, p.Prompt)
	s.hists = append(s.hists, p.History)
	gate := s.release[p.Prompt]
	calls := s.tools[p.Prompt]
	err := s.fail[p.Prompt]
	s.mu.Unlock()
	s.started <- p.Prompt
	for _, c := range calls {
		if p.OnTool != nil {
			p.OnTool(c)
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &driver.ChatSendResult{Text: "answer to " + p.Prompt, Model: "m", Provider: "p"}, nil
}

func (s *scriptedCore) history(i int) []driver.ChatTurn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hists[i]
}

func (s *scriptedCore) askCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asked)
}

func waitStarted(t *testing.T, s *scriptedCore, prompt string) {
	t.Helper()
	select {
	case got := <-s.started:
		if got != prompt {
			t.Fatalf("the core was asked %q, want %q", got, prompt)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("the core was never asked %q", prompt)
	}
}

func eventsOf(t *testing.T, out chan fold.Event, until string) []fold.Event {
	t.Helper()
	got := drain(out, until, 3*time.Second)
	if len(got) == 0 || got[len(got)-1].Type != until {
		t.Fatalf("never saw %s; saw %d events", until, len(got))
	}
	return got
}

func idle(t *testing.T, c *chatSession) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if !c.busyNow() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the session never went idle")
}

// ---- memory: what the model did stays in the conversation -------------------------

func TestWhatTheModelDidIsPartOfTheNextQuestionsHistory(t *testing.T) {
	core := newScriptedCore()
	core.tools["make it round"] = []driver.ToolCall{
		{Name: "ui_edit", Arg: "interface", OK: false, Summary: "The change was refused"},
		{Name: "ui_edit", Arg: "interface", OK: true, Summary: "round border on the input"},
	}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	if err := c.send(context.Background(), "make it round"); err != nil {
		t.Fatal(err)
	}
	eventsOf(t, out, "agent.turn_done")
	idle(t, c)
	if err := c.send(context.Background(), "did it work?"); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, core, "make it round")
	waitStarted(t, core, "did it work?")
	h := core.history(1)
	if len(h) != 2 {
		t.Fatalf("history = %+v", h)
	}
	a := h[1].Text
	for _, want := range []string{"ui_edit(interface): The change was refused [did not succeed]", "ui_edit(interface): round border on the input", "answer to make it round"} {
		if !strings.Contains(a, want) {
			t.Errorf("the model is not told %q about its last turn; it reads:\n%s", want, a)
		}
	}
	if strings.Index(a, "did not succeed") > strings.Index(a, "answer to make it round") {
		t.Errorf("the record must come before the answer:\n%s", a)
	}
}

func TestATurnWithoutToolsLeavesTheHistoryAsItWas(t *testing.T) {
	core := newScriptedCore()
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	_ = c.send(context.Background(), "hello")
	eventsOf(t, out, "agent.turn_done")
	idle(t, c)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.history) != 2 || c.history[1].Text != "answer to hello" {
		t.Errorf("history = %+v", c.history)
	}
}

func TestACancelledTurnStaysInTheHistoryWithWhatItDid(t *testing.T) {
	core := newScriptedCore()
	core.tools["slow"] = []driver.ToolCall{{Name: "read", Arg: "a.go", OK: true, Summary: "Read 10 lines"}}
	core.release["slow"] = make(chan struct{})
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	_ = c.send(context.Background(), "slow")
	waitStarted(t, core, "slow")
	eventsOf(t, out, "chat.tool")
	if !c.cancelTurn() {
		t.Fatal("nothing to cancel")
	}
	_ = c.send(context.Background(), "again")
	waitStarted(t, core, "again")
	h := core.history(1)
	if len(h) != 2 || h[0].Text != "slow" {
		t.Fatalf("the cancelled question vanished from the history: %+v", h)
	}
	if !strings.Contains(h[1].Text, "read(a.go): Read 10 lines") || !strings.Contains(h[1].Text, interruptedNote) {
		t.Errorf("what the cancelled turn did, and that it was cut short, must be in the history:\n%s", h[1].Text)
	}
}

func TestAFailedTurnStaysInTheHistoryToo(t *testing.T) {
	core := newScriptedCore()
	core.tools["boom"] = []driver.ToolCall{{Name: "grep", Arg: "x", OK: true, Summary: "3 matches"}}
	core.fail["boom"] = &driver.Refusal{Code: "failed", Message: "provider down"}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	_ = c.send(context.Background(), "boom")
	eventsOf(t, out, "chat.error")
	idle(t, c)
	_ = c.send(context.Background(), "retry")
	waitStarted(t, core, "boom")
	waitStarted(t, core, "retry")
	h := core.history(1)
	if len(h) != 2 || !strings.Contains(h[1].Text, "grep(x): 3 matches") || !strings.Contains(h[1].Text, "provider down") {
		t.Errorf("a failed turn must leave its question, its work and its error behind: %+v", h)
	}
}

func TestTheRecordIsBounded(t *testing.T) {
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, traceLine("read", strings.Repeat("p", 500), strings.Repeat("s", 900), true))
	}
	got := traceText(lines)
	if n := strings.Count(got, "\n- read"); n != traceMaxLines {
		t.Errorf("%d calls listed, want %d", n, traceMaxLines)
	}
	if !strings.Contains(got, "(28 earlier calls not listed)") {
		t.Errorf("the record must say what it left out:\n%s", got)
	}
	if len(got) > traceMaxLines*(traceMaxLine+120)+400 {
		t.Errorf("the record grew to %d bytes", len(got))
	}
}

func TestTheResumedConversationKeepsWhatWasDone(t *testing.T) {
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "q1"}},
		{Type: "chat.tool", Payload: map[string]any{"name": "write", "arg": "a.go", "ok": true, "summary": "Wrote a.go"}},
		{Type: "llm.response", Payload: map[string]any{"text": "done"}},
		{Type: "run.prompt", Payload: map[string]any{"text": "q2"}},
		{Type: "chat.tool", Payload: map[string]any{"name": "ui_edit", "arg": "interface", "ok": false, "summary": "refused"}},
		{Type: "chat.cancelled"},
	}
	h := historyFromEvents(evs)
	if len(h) != 4 {
		t.Fatalf("history = %+v", h)
	}
	if !strings.Contains(h[1].Text, "write(a.go): Wrote a.go") || !strings.HasSuffix(h[1].Text, "done") {
		t.Errorf("answer 1 = %q", h[1].Text)
	}
	if h[2].Text != "q2" || !strings.Contains(h[3].Text, "ui_edit(interface): refused [did not succeed]") || !strings.Contains(h[3].Text, interruptedNote) {
		t.Errorf("the cut-short question was lost on resume: %+v", h[2:])
	}
}

// ---- queue ------------------------------------------------------------------------

func TestALineSentWhileTheAgentWorksWaitsAndGoesWhenTheAnswerEnds(t *testing.T) {
	core := newScriptedCore()
	gate := make(chan struct{})
	core.release["first"] = gate
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.whileBusy = func() string { return busyQueue }
	_ = c.send(context.Background(), "first")
	waitStarted(t, core, "first")

	if err := c.send(context.Background(), "second"); err != nil {
		t.Fatalf("a line sent during a turn must wait, not fail: %v", err)
	}
	if err := c.send(context.Background(), "third"); err != nil {
		t.Fatal(err)
	}
	if n := c.queuedNow(); n != 2 {
		t.Fatalf("%d lines wait, want 2", n)
	}
	if n := core.askCount(); n != 1 {
		t.Fatalf("the queued lines must not reach the core before the answer: %d asks", n)
	}
	close(gate)
	waitStarted(t, core, "second")
	waitStarted(t, core, "third")
	// Each one is asked in the conversation that includes the earlier ones.
	if h := core.history(2); len(h) != 4 || h[2].Text != "second" {
		t.Errorf("the third line was asked without the second in the history: %+v", h)
	}
}

func TestQueuedLinesAreShownInOrderAndStopWaitingWhenSent(t *testing.T) {
	evs := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "first"}},
		{Type: "chat.queued", Payload: map[string]any{"text": "second"}},
		{Type: "chat.queued", Payload: map[string]any{"text": "third"}},
	}
	st := fold.Fold(evs)
	if len(st.Queued) != 2 || st.Queued[0] != "second" {
		t.Fatalf("queued = %v", st.Queued)
	}
	evs = append(evs, fold.Event{Type: "llm.response", Payload: map[string]any{"text": "a"}},
		fold.Event{Type: "run.prompt", Payload: map[string]any{"text": "second"}})
	if st := fold.Fold(evs); len(st.Queued) != 1 || st.Queued[0] != "third" {
		t.Errorf("a line that is being sent must stop being shown as waiting: %v", st.Queued)
	}
	evs = append(evs, fold.Event{Type: "chat.unqueued"})
	if st := fold.Fold(evs); len(st.Queued) != 0 {
		t.Errorf("queued after unqueued = %v", st.Queued)
	}
}

func TestEscStopsTheTurnAndGivesTheQueuedLinesBack(t *testing.T) {
	core := newScriptedCore()
	core.release["first"] = make(chan struct{})
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.whileBusy = func() string { return busyQueue }
	_ = c.send(context.Background(), "first")
	waitStarted(t, core, "first")
	_ = c.send(context.Background(), "second")
	_ = c.send(context.Background(), "third")
	if !c.cancelTurn() {
		t.Fatal("nothing to cancel")
	}
	got := eventsOf(t, out, "chat.unqueued")
	last := got[len(got)-1]
	lines, _ := last.Payload["lines"].([]any)
	if len(lines) != 2 || lines[0] != "second" || lines[1] != "third" {
		t.Errorf("the words were not handed back in order: %v", lines)
	}
	if c.queuedNow() != 0 {
		t.Error("the queue must be empty once its lines were handed back")
	}
	time.Sleep(50 * time.Millisecond)
	if n := core.askCount(); n != 1 {
		t.Errorf("a stopped turn must not send its queue on; the core was asked %d times", n)
	}
	if got := unqueuedText(last); got != "second\nthird" {
		t.Errorf("unqueuedText = %q", got)
	}
}

func TestAFailureGivesTheQueuedLinesBackToo(t *testing.T) {
	core := newScriptedCore()
	gate := make(chan struct{})
	core.release["first"] = gate
	core.fail["first"] = &driver.Refusal{Code: "failed", Message: "provider down"}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.whileBusy = func() string { return busyQueue }
	_ = c.send(context.Background(), "first")
	waitStarted(t, core, "first")
	_ = c.send(context.Background(), "second")
	close(gate)
	got := eventsOf(t, out, "chat.unqueued")
	if lines, _ := got[len(got)-1].Payload["lines"].([]any); len(lines) != 1 || lines[0] != "second" {
		t.Errorf("lines = %v", lines)
	}
}

func TestTheQueueIsBounded(t *testing.T) {
	core := newScriptedCore()
	core.release["first"] = make(chan struct{})
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.whileBusy = func() string { return busyQueue }
	_ = c.send(context.Background(), "first")
	waitStarted(t, core, "first")
	for i := 0; i < chatMaxQueue; i++ {
		if err := c.send(context.Background(), "q"); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
	}
	err := c.send(context.Background(), "one too many")
	if err == nil || !strings.Contains(err.Error(), "queue") || !strings.Contains(err.Error(), "Esc") {
		t.Errorf("a full queue must say so and how to get out of it: %v", err)
	}
}

func TestClearForgetsTheQueue(t *testing.T) {
	core := newScriptedCore()
	core.release["first"] = make(chan struct{})
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.whileBusy = func() string { return busyQueue }
	_ = c.send(context.Background(), "first")
	waitStarted(t, core, "first")
	_ = c.send(context.Background(), "second")
	c.reset()
	if c.queuedNow() != 0 {
		t.Error("/clear must drop the lines waiting for the old conversation")
	}
}

// ---- steering ---------------------------------------------------------------------

func TestSteeringStopsTheTurnAndSendsTheLineWithWhatWasDone(t *testing.T) {
	core := newScriptedCore()
	core.tools["first"] = []driver.ToolCall{{Name: "read", Arg: "main.go", OK: true, Summary: "Read 200 lines"}}
	core.release["first"] = make(chan struct{})
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	c.whileBusy = func() string { return busySteer }
	_ = c.send(context.Background(), "first")
	waitStarted(t, core, "first")
	eventsOf(t, out, "chat.tool")

	if err := c.send(context.Background(), "no, do this instead"); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, core, "no, do this instead")
	h := core.history(1)
	if len(h) != 2 || h[0].Text != "first" {
		t.Fatalf("history = %+v", h)
	}
	if !strings.Contains(h[1].Text, "read(main.go): Read 200 lines") || !strings.Contains(h[1].Text, interruptedNote) {
		t.Errorf("the steering line must be asked knowing what was done and that it was cut short:\n%s", h[1].Text)
	}
	if c.queuedNow() != 0 {
		t.Error("steering must not leave the line in the queue")
	}
	// The interrupted turn says nothing more; only the new one answers.
	got := eventsOf(t, out, "agent.turn_done")
	for _, e := range got {
		if e.Type == "llm.response" {
			if txt, _ := e.Payload["text"].(string); txt != "answer to no, do this instead" {
				t.Errorf("the interrupted turn answered: %q", txt)
			}
		}
	}
}

func TestAltEnterIsTheOtherWay(t *testing.T) {
	for _, tc := range []struct {
		setting string
		queued  bool
	}{{busyQueue, false}, {busySteer, true}} {
		core := newScriptedCore()
		core.release["first"] = make(chan struct{})
		out := make(chan fold.Event, 64)
		c := newChatSession(core, out)
		c.whileBusy = func() string { return tc.setting }
		_ = c.send(context.Background(), "first")
		waitStarted(t, core, "first")
		if err := c.sendOtherWay(context.Background(), "x"); err != nil {
			t.Fatal(err)
		}
		if got := c.queuedNow() == 1; got != tc.queued {
			t.Errorf("setting %s: Alt+Enter queued=%v, want %v", tc.setting, got, tc.queued)
		}
	}
}

func TestWithNothingRunningEveryWayIsAPlainSend(t *testing.T) {
	core := newScriptedCore()
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	_ = c.sendOtherWay(context.Background(), "hello")
	waitStarted(t, core, "hello")
	if c.queuedNow() != 0 {
		t.Error("nothing to wait for")
	}
}

// Alt+Enter reaches the driver as the other way; plain Enter does not.
type otherWayDriver struct {
	Driver
	plain, other []string
}

func (d *otherWayDriver) SubmitPrompt(_ context.Context, text string) error {
	d.plain = append(d.plain, text)
	return nil
}
func (d *otherWayDriver) SubmitPromptOtherWay(_ context.Context, text string) error {
	d.other = append(d.other, text)
	return nil
}

func TestAltEnterReachesTheDriverAsTheOtherWay(t *testing.T) {
	d := &otherWayDriver{}
	typeKey("go on", 5, term.Key{Type: term.KeyEnter, Mod: term.ModAlt}, context.Background(), d)
	typeKey("hello", 5, term.Key{Type: term.KeyEnter}, context.Background(), d)
	if len(d.other) != 1 || d.other[0] != "go on" || len(d.plain) != 1 || d.plain[0] != "hello" {
		t.Errorf("other=%v plain=%v", d.other, d.plain)
	}
}

// ---- the setting ------------------------------------------------------------------

func TestEnterWhileBusyIsAUserSettingWithAValidator(t *testing.T) {
	steer, queue := busySteer, busyQueue
	b, err := applyBehaviourPatch(behaviour{}, behaviourPatch{EnterWhileBusy: &steer})
	if err != nil || b.EnterWhileBusy != busySteer {
		t.Fatalf("steer: %v %+v", err, b)
	}
	b, err = applyBehaviourPatch(b, behaviourPatch{EnterWhileBusy: &queue})
	if err != nil || !b.empty() {
		t.Errorf("queue is the factory way and must leave the document empty: %v %+v", err, b)
	}
	bad := "interrupt"
	_, err = applyBehaviourPatch(behaviour{}, behaviourPatch{EnterWhileBusy: &bad})
	if err == nil || !strings.Contains(err.Error(), "queue") || !strings.Contains(err.Error(), "steer") {
		t.Errorf("a refusal must say what is allowed: %v", err)
	}
	if !(behaviourPatch{EnterWhileBusy: &steer}).touchesWhatRuns() {
		t.Error("changing what Enter does is always put to the user")
	}
	if !strings.Contains(behaviourGuide(behaviour{}), "enter_while_busy") {
		t.Error("the guide must teach enter_while_busy")
	}
	p, _, ok, perr := uiBehaviourCommand("/ui busy steer")
	if !ok || perr != nil || p.EnterWhileBusy == nil || *p.EnterWhileBusy != busySteer {
		t.Errorf("/ui busy steer: %+v %v %v", p, ok, perr)
	}
	if _, _, ok, perr := uiBehaviourCommand("/ui busy nonsense"); !ok || perr == nil {
		t.Error("/ui busy with a wrong word must be refused with the usage")
	}
	raw := encodeBehaviour(behaviour{EnterWhileBusy: busySteer})
	if !strings.Contains(string(raw), `"enter_while_busy": "steer"`) {
		t.Errorf("not written to behaviour.json: %s", raw)
	}
}
