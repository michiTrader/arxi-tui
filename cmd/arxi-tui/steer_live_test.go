package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// fakeLiveCore is a core whose turn announces that it takes steering, records what is
// written to it, and ends when told to. took, when set, is what it reports as read.
type fakeLiveCore struct {
	mu       sync.Mutex
	wrote    []string
	started  chan struct{}
	finish   chan struct{}
	took     string // reported through OnSteered before the turn ends, when not empty
	prompts  []driver.ChatSendParams
	writeErr error
	silent   bool // does not announce steering: a core that does not know it
}

func newFakeLiveCore() *fakeLiveCore {
	return &fakeLiveCore{started: make(chan struct{}, 4), finish: make(chan struct{}, 4)}
}

func (s *fakeLiveCore) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}

func (s *fakeLiveCore) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	s.mu.Lock()
	s.prompts = append(s.prompts, p)
	s.mu.Unlock()
	if p.OnSteerable != nil && !s.silent {
		p.OnSteerable(func(text string) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.writeErr != nil {
				return s.writeErr
			}
			s.wrote = append(s.wrote, text)
			return nil
		})
	}
	s.started <- struct{}{}
	select {
	case <-s.finish:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	took := s.took
	s.mu.Unlock()
	if took != "" && p.OnSteered != nil {
		p.OnSteered(took)
	}
	return &driver.ChatSendResult{Text: "done", Model: "m", Provider: "p"}, nil
}

func (s *fakeLiveCore) written() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.wrote...)
}

func steerSession(t *testing.T) (*chatSession, *fakeLiveCore, chan fold.Event) {
	t.Helper()
	out := make(chan fold.Event, 64)
	core := newFakeLiveCore()
	c := newChatSession(core, out)
	if err := c.send(context.Background(), "build the thing"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	return c, core, out
}

func eventTexts(evs []fold.Event, typ string) []string {
	var got []string
	for _, e := range evs {
		if e.Type == typ {
			s, _ := e.Payload["text"].(string)
			got = append(got, s)
		}
	}
	return got
}

// A turn that has not said it takes steering is not written to.
// Consequence: a line typed to an older core would be answered as a response and read by the
// turn as its own result. Remedy: steer refuses until the core announces it.
func TestSteerRefusesUntilTheCoreAnnouncesIt(t *testing.T) {
	out := make(chan fold.Event, 8)
	c := newChatSession(&ctxChat{started: make(chan struct{})}, out)
	if err := c.steer("x"); !errors.Is(err, errNotSteerable) {
		t.Fatalf("no turn at all: got %v, want errNotSteerable", err)
	}
	if err := c.send(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if c.canSteer() {
		t.Fatal("canSteer said yes before the core announced steering")
	}
	if err := c.steer("x"); !errors.Is(err, errNotSteerable) {
		t.Fatalf("turn without announcement: got %v, want errNotSteerable", err)
	}
	c.cancelTurn()
}

// A line written to a steerable turn is owed until the core reads it, then shows in the
// conversation as the user's line and becomes part of the request the history keeps.
// Consequence: without the chat.steered line the user would not see what the model read,
// and "retry" after a stopped turn would mean the request without the correction.
func TestSteerOnceTakenShowsAndEntersTheHistory(t *testing.T) {
	c, core, out := steerSession(t)
	if !c.canSteer() {
		t.Fatal("the core announced steering and canSteer said no")
	}
	if err := c.steer("  use tabs  "); err != nil {
		t.Fatal(err)
	}
	if got := core.written(); !reflect.DeepEqual(got, []string{"use tabs"}) {
		t.Fatalf("written to the core: %q, want the trimmed text once", got)
	}
	core.mu.Lock()
	core.took = "use tabs"
	core.mu.Unlock()
	core.finish <- struct{}{}
	evs := drain(out, "agent.turn_done", 2*time.Second)
	if got := eventTexts(evs, "chat.steered"); !reflect.DeepEqual(got, []string{"use tabs"}) {
		t.Fatalf("chat.steered lines: %q, want [use tabs]", got)
	}
	if got := eventTexts(evs, "chat.steer_unsent"); len(got) != 0 {
		t.Fatalf("a text the core took was also given back: %q", got)
	}
	c.mu.Lock()
	last := c.history[len(c.history)-2].Text
	c.mu.Unlock()
	if !strings.HasPrefix(last, "build the thing") || !strings.Contains(last, "use tabs") {
		t.Fatalf("history keeps %q, want the request plus the added line", last)
	}
}

// Two lines the core read together are one chat.steered line with both, and neither stays owed.
// Consequence: the conversation would show the lines twice, or give one back as a new message.
func TestSteerJoinedTextsAreTakenTogether(t *testing.T) {
	c, core, out := steerSession(t)
	for _, s := range []string{"one", "two"} {
		if err := c.steer(s); err != nil {
			t.Fatal(err)
		}
	}
	core.mu.Lock()
	core.took = "one\n\ntwo"
	core.mu.Unlock()
	core.finish <- struct{}{}
	evs := drain(out, "agent.turn_done", 2*time.Second)
	if got := eventTexts(evs, "chat.steered"); !reflect.DeepEqual(got, []string{"one\n\ntwo"}) {
		t.Fatalf("chat.steered: %q, want one joined line", got)
	}
	if got := eventTexts(evs, "chat.steer_unsent"); len(got) != 0 {
		t.Fatalf("lines the core took were given back: %q", got)
	}
}

// What the core never read goes back to the loop when the turn ends.
// Consequence: a line accepted by the program just before the answer ended would be
// answered by nobody. Remedy: it comes back as chat.steer_unsent, before the turn's end.
func TestSteerNeverReadIsGivenBack(t *testing.T) {
	c, core, out := steerSession(t)
	if err := c.steer("too late"); err != nil {
		t.Fatal(err)
	}
	core.finish <- struct{}{}
	evs := drain(out, "agent.turn_done", 2*time.Second)
	if got := eventTexts(evs, "chat.steer_unsent"); !reflect.DeepEqual(got, []string{"too late"}) {
		t.Fatalf("given back: %q, want [too late]", got)
	}
	idx := func(typ string) int {
		for i, e := range evs {
			if e.Type == typ {
				return i
			}
		}
		return -1
	}
	if idx("chat.steer_unsent") > idx("agent.turn_done") {
		t.Fatal("the unsent line came after the turn's end: the loop would have sent nothing for it")
	}
	if len(eventTexts(evs, "chat.steered")) != 0 {
		t.Fatal("a line the core never read was shown as read")
	}
}

// A write that fails leaves the text with the caller and owes nothing.
// Consequence: the text would be both returned to the user and given back at the end, sent twice.
func TestSteerWriteFailureOwesNothing(t *testing.T) {
	c, core, out := steerSession(t)
	core.mu.Lock()
	core.writeErr = driver.ErrTurnOver
	core.mu.Unlock()
	if err := c.steer("x"); !errors.Is(err, driver.ErrTurnOver) {
		t.Fatalf("got %v, want the driver's error", err)
	}
	core.finish <- struct{}{}
	evs := drain(out, "agent.turn_done", 2*time.Second)
	if got := eventTexts(evs, "chat.steer_unsent"); len(got) != 0 {
		t.Fatalf("a text that was never written was given back: %q", got)
	}
}

// Cancelling gives back what was owed, before the cancelled line.
// Consequence: Esc would swallow a line the user had typed to the turn.
func TestSteerCancelGivesBackWhatWasOwed(t *testing.T) {
	c, _, out := steerSession(t)
	if err := c.steer("keep me"); err != nil {
		t.Fatal(err)
	}
	c.cancelTurn()
	evs := drain(out, "chat.cancelled", 2*time.Second)
	if got := eventTexts(evs, "chat.steer_unsent"); !reflect.DeepEqual(got, []string{"keep me"}) {
		t.Fatalf("given back: %q, want [keep me]", got)
	}
	if evs[len(evs)-1].Type != "chat.cancelled" {
		t.Fatalf("the cancelled line must come last, got %+v", evs)
	}
}

// stopForSteer folds what was owed into the note instead of giving it back.
// Consequence: the replacement line would be followed by the old one, answering something
// the user has just overtaken.
func TestSteerStopForSteerFoldsOwedIntoTheNote(t *testing.T) {
	c, _, out := steerSession(t)
	if err := c.steer("older"); err != nil {
		t.Fatal(err)
	}
	if !c.stopForSteer() {
		t.Fatal("a turn was running and stopForSteer did not stop it")
	}
	evs := drain(out, "never", 150*time.Millisecond)
	if got := eventTexts(evs, "chat.steer_unsent"); len(got) != 0 {
		t.Fatalf("owed text given back by stopForSteer: %q", got)
	}
	c.mu.Lock()
	h := c.history[len(c.history)-1].Text
	c.mu.Unlock()
	if !strings.Contains(h, "older") {
		t.Fatalf("history %q lost the line that was written to the turn", h)
	}
}

// A steerable or steered call from a turn that is no longer current changes nothing.
// Consequence: a late announcement from a cancelled turn would let the next turn's lines go
// to a connection that is gone, and a late "taken" would put a line in the next turn's
// conversation. Remedy: both check the generation and the turn they belong to.
func TestSteerFromAnOldTurnIsIgnored(t *testing.T) {
	c, core, out := steerSession(t)
	c.mu.Lock()
	gen, turn := c.gen, c.turn
	c.mu.Unlock()
	c.cancelTurn()
	drain(out, "chat.cancelled", 2*time.Second)

	// The next turn is running, and its core did not announce steering.
	core.mu.Lock()
	core.silent = true
	core.mu.Unlock()
	if err := c.send(context.Background(), "next"); err != nil {
		t.Fatal(err)
	}
	<-core.started

	c.steerable(gen, turn, func(string) error { return nil })
	if c.canSteer() {
		t.Fatal("an old turn's announcement made the running turn steerable")
	}
	c.steered(context.Background(), gen, turn, "late")
	evs := drain(out, "never", 150*time.Millisecond)
	if got := eventTexts(evs, "chat.steered"); len(got) != 0 {
		t.Fatalf("an old turn's chat.steered reached the conversation: %q", got)
	}
	c.mu.Lock()
	n := len(c.turnSteers)
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("the running turn's request grew by %d line(s) it never had", n)
	}
	if got := c.endSteering(gen, turn-1); got != nil {
		t.Fatalf("endSteering for another turn returned %q", got)
	}
}

// steersNote says nothing for no lines and names them otherwise.
func TestSteersNote(t *testing.T) {
	if steersNote(nil) != "" {
		t.Fatal("a note for no lines")
	}
	n := steersNote([]string{"a", "b"})
	if !strings.Contains(n, "a / b") || !strings.Contains(n, "not typed by the user") {
		t.Fatalf("note %q must list the lines and say the app wrote it", n)
	}
}

// The history a live session keeps and the one rebuilt from its file say the same about a
// line added to a running request.
// Consequence: "retry" would mean one request in the session that lived it and another
// after a resume. Remedy: both build the note from the chat.steered lines.
func TestSteerHistoryLiveAndResumedAgree(t *testing.T) {
	c, core, out := steerSession(t)
	for _, s := range []string{"one", "two"} {
		if err := c.steer(s); err != nil {
			t.Fatal(err)
		}
	}
	core.mu.Lock()
	core.took = "one\n\ntwo"
	core.mu.Unlock()
	core.finish <- struct{}{}
	evs := drain(out, "agent.turn_done", 2*time.Second)
	c.mu.Lock()
	live := c.history[len(c.history)-2].Text
	c.mu.Unlock()

	file := []fold.Event{{Type: "run.prompt", Payload: map[string]any{"text": "build the thing"}}}
	for _, e := range evs {
		if e.Type == "chat.steered" {
			file = append(file, e)
		}
	}
	file = append(file, fold.Event{Type: "llm.response", Payload: map[string]any{"text": "done"}})
	h := historyFromEvents(file)
	if len(h) != 2 || h[0].Text != live {
		t.Fatalf("resumed history %+v differs from the live one %q", h, live)
	}
}

// A line added to a request that then failed is part of the unanswered request.
// Consequence: after a resume, "retry" would redo the request without what the user had added.
func TestSteerHistoryFromEventsKeepsTheAddedLineOnAFailedTurn(t *testing.T) {
	file := []fold.Event{
		{Type: "run.prompt", Payload: map[string]any{"text": "build the thing"}},
		{Type: "chat.steered", Payload: map[string]any{"text": "use tabs"}},
		{Type: "chat.error", Payload: map[string]any{"text": "boom"}},
		{Type: "run.prompt", Payload: map[string]any{"text": "retry"}},
	}
	h := historyFromEvents(file)
	if len(h) == 0 || !strings.Contains(h[0].Text, "use tabs") || !strings.Contains(h[0].Text, "build the thing") {
		t.Fatalf("history %+v lost the request or the line added to it", h)
	}
}
