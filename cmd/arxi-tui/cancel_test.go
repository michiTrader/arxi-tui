package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// ctxChat blocks until the turn's context is cancelled, like a request on a
// connection that is dropped.
type ctxChat struct{ started chan struct{} }

func (c *ctxChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (c *ctxChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	close(c.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

// byPrompt answers "first" only when released and everything else at once, so one
// session can have a slow turn abandoned and a fast one after it.
type byPrompt struct{ release chan struct{} }

func (b *byPrompt) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (b *byPrompt) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	if p.Prompt == "first" {
		<-b.release
	}
	return &driver.ChatSendResult{Text: "answer to " + p.Prompt, Model: "m", Provider: "p"}, nil
}

// Cancelling a turn puts a cancelled event in the conversation, frees the session at once, and lets nothing else about that turn through.
func TestCancelTurnReportsItAndFreesTheSession(t *testing.T) {
	out := make(chan fold.Event, 16)
	core := &ctxChat{started: make(chan struct{})}
	c := newChatSession(core, out)
	if c.cancelTurn() {
		t.Fatal("there was nothing to cancel, yet cancelTurn said it cancelled")
	}
	if err := c.send(context.Background(), "What can fx do differently?"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	if !c.cancelTurn() {
		t.Fatal("a turn was running and cancelTurn did not stop it")
	}
	events := drain(out, "chat.cancelled", 2*time.Second)
	last := events[len(events)-1]
	if last.Type != "chat.cancelled" {
		t.Fatalf("expected a chat.cancelled event, got %+v", events)
	}
	// Give the abandoned goroutine time to wind down, then make sure it said nothing more.
	select {
	case ev := <-out:
		t.Fatalf("the cancelled turn kept talking: %s", ev.Type)
	case <-time.After(150 * time.Millisecond):
	}
	// The next line may be sent straight away.
	if c.busyNow() {
		t.Fatal("the session is still busy after the cancel")
	}
}

// A cancelled turn winding down late must not mark the turn that replaced it idle,
// and its answer must not land in the new turn's conversation.
func TestCancelledTurnCannotTouchItsSuccessor(t *testing.T) {
	out := make(chan fold.Event, 32)
	release := make(chan struct{})
	c := newChatSession(&byPrompt{release: release}, out)
	if err := c.send(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	drain(out, "agent.activated", 2*time.Second)
	if !c.cancelTurn() {
		t.Fatal("no turn to cancel")
	}
	drain(out, "chat.cancelled", 2*time.Second)

	if err := c.send(context.Background(), "second"); err != nil {
		t.Fatalf("second send refused: %v", err)
	}
	drain(out, "agent.turn_done", 2*time.Second)
	close(release) // the first turn's answer arrives, far too late
	time.Sleep(150 * time.Millisecond)
	for {
		select {
		case ev := <-out:
			if ev.Type == "llm.response" && strings.Contains(ev.Payload["text"].(string), "first") {
				t.Fatalf("the cancelled turn's answer reached the conversation: %v", ev.Payload)
			}
		default:
			return
		}
	}
}

// /clear also stops whatever is in flight, so its context is not left dangling.
func TestResetCancelsTheTurnInFlight(t *testing.T) {
	out := make(chan fold.Event, 8)
	core := &ctxChat{started: make(chan struct{})}
	c := newChatSession(core, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	c.reset()
	c.mu.Lock()
	cancel, busy := c.cancel, c.busy
	c.mu.Unlock()
	if cancel != nil || busy {
		t.Fatalf("reset left a live turn behind: cancel=%v busy=%v", cancel != nil, busy)
	}
}

// With a dialer, each turn runs on a connection of its own and that connection is
// closed when the turn ends, however it ends.
func TestTurnRunsOnItsOwnConnectionAndClosesIt(t *testing.T) {
	out := make(chan fold.Event, 16)
	core := &ctxChat{started: make(chan struct{})}
	closed := make(chan struct{})
	c := newChatSession(&ctxChat{started: make(chan struct{})}, out)
	c.dial = func(ctx context.Context) (chatSender, func(), error) {
		return core, func() { close(closed) }, nil
	}
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	<-core.started
	c.cancelTurn()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the turn's connection was not closed after the cancel")
	}
}

// The answer's duration and tokens ride on its llm.response event.
func TestAnswerCarriesItsDuration(t *testing.T) {
	out := make(chan fold.Event, 16)
	c := newChatSession(fakeChat{hello: &driver.Hello{Implemented: []string{"chat.send"}},
		res: &driver.ChatSendResult{Text: "ok", Model: "m", Provider: "p", InputTokens: 2, OutputTokens: 57}}, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	for _, ev := range drain(out, "llm.response", 2*time.Second) {
		if ev.Type == "llm.response" {
			if _, ok := ev.Payload["duration_ms"].(float64); !ok {
				t.Fatalf("no duration_ms on the answer: %v", ev.Payload)
			}
			return
		}
	}
	t.Fatal("no llm.response")
}
