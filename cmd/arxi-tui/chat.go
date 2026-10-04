package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// chatMaxHistory caps how many earlier messages ride along with a prompt, so a long
// session cannot grow a request without bound.
const chatMaxHistory = 40

// chatSystemPrompt is the standing instruction for a plain chat turn.
const chatSystemPrompt = "You are a helpful assistant inside a terminal chat. Answer clearly and concisely."

// errChatBusy is returned when a second line is sent while an answer is pending.
var errChatBusy = errors.New("still waiting for the previous answer; wait for it to finish before sending another message")

// chatSender is the part of the core a chat turn needs.
type chatSender interface {
	Hello() *driver.Hello
	SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error)
}

// chatSession turns each typed line into one chat.send round-trip and feeds the
// answer (or a clear error) back to the loop. Failures are never swallowed and never
// pushed into a banner: they become a chat.error event, which the fold turns into an
// ordinary line of the conversation.
type chatSession struct {
	core chatSender
	out  chan<- fold.Event

	mu      sync.Mutex
	history []driver.ChatTurn
	busy    bool
	seq     int64
	// gen counts sessions. A turn remembers the generation it started in and
	// drops everything it would report once /clear has bumped it, so an answer
	// that was in flight cannot land in the fresh conversation.
	gen int64
}

func newChatSession(core chatSender, out chan<- fold.Event) *chatSession {
	return &chatSession{core: core, out: out}
}

// chatRequires says why this core cannot chat, or nil when it can.
func chatRequires(h *driver.Hello) error {
	if h == nil {
		return errors.New("the core has not said hello yet, so chat is not available")
	}
	for _, v := range h.Implemented {
		if v == "chat.send" {
			return nil
		}
	}
	return fmt.Errorf("this arxi core is too old to chat (it lacks chat.send); %s", rebuildRemedy)
}

// send starts one turn. It returns an error immediately for anything that can be
// decided up front (old core, a turn already running); everything that happens on
// the network is reported in the chat as a chat.error event.
func (c *chatSession) send(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if err := chatRequires(c.core.Hello()); err != nil {
		return err
	}
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return errChatBusy
	}
	c.busy = true
	gen := c.gen
	hist := append([]driver.ChatTurn(nil), c.history...)
	if len(hist) > chatMaxHistory {
		hist = hist[len(hist)-chatMaxHistory:]
	}
	c.mu.Unlock()
	go c.run(ctx, text, hist, gen)
	return nil
}

// reset starts a new conversation: the history is forgotten, a turn still in
// flight is orphaned (its result is discarded), and the next line may be sent
// at once. The core keeps no chat state of its own (chat.send is stateless, the
// history rides along with each prompt), so forgetting it here is the whole job.
func (c *chatSession) reset() {
	c.mu.Lock()
	c.gen++
	c.history = nil
	c.busy = false
	c.mu.Unlock()
}

// current reports whether gen is still the live session.
func (c *chatSession) current(gen int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen == gen
}

func (c *chatSession) nextSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func (c *chatSession) emit(ctx context.Context, gen int64, typ string, payload map[string]any) {
	if !c.current(gen) {
		return
	}
	ev := fold.Event{Type: typ, Seq: c.nextSeq(), Actor: "assistant", Payload: payload}
	select {
	case c.out <- ev:
	case <-ctx.Done():
	}
}

// fail puts an error in the conversation. It never blocks the caller: the relay
// channel is buffered, and if it is momentarily full the event waits on its own
// goroutine rather than being dropped.
func (c *chatSession) fail(ctx context.Context, gen int64, msg string) {
	if !c.current(gen) {
		return
	}
	ev := fold.Event{Type: "chat.error", Seq: c.nextSeq(), Actor: "assistant", Payload: map[string]any{"text": msg}}
	select {
	case c.out <- ev:
	default:
		go func() {
			select {
			case c.out <- ev:
			case <-ctx.Done():
			}
		}()
	}
}

func (c *chatSession) run(ctx context.Context, text string, hist []driver.ChatTurn, gen int64) {
	defer func() {
		c.mu.Lock()
		if c.gen == gen {
			c.busy = false
		}
		c.mu.Unlock()
	}()
	c.emit(ctx, gen, "run.prompt", map[string]any{"text": text})
	c.emit(ctx, gen, "agent.activated", map[string]any{"agent": "assistant"})
	res, err := c.core.SubmitChatSend(ctx, driver.ChatSendParams{Prompt: text, System: chatSystemPrompt, History: hist})
	if err != nil {
		c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
		c.fail(ctx, gen, chatErrorText(err))
		return
	}
	c.mu.Lock()
	if c.gen != gen {
		c.mu.Unlock()
		return
	}
	c.history = append(c.history, driver.ChatTurn{Role: "user", Text: text}, driver.ChatTurn{Role: "assistant", Text: res.Text})
	c.mu.Unlock()
	model := res.Model
	if res.Provider != "" && !strings.Contains(model, "/") {
		model = res.Provider + "/" + res.Model
	}
	c.emit(ctx, gen, "llm.response", map[string]any{
		"text": res.Text, "model": model, "agent": "assistant",
		"tokens_in": float64(res.InputTokens), "tokens_out": float64(res.OutputTokens),
	})
	c.emit(ctx, gen, "agent.turn_done", map[string]any{"agent": "assistant"})
}

// chatErrorText is the sentence the user reads when a turn fails: the core's own
// words when it refused, a plain description otherwise.
func chatErrorText(err error) string {
	var r *driver.Refusal
	if errors.As(err, &r) && r.Message != "" {
		return r.Message
	}
	return "could not get an answer: " + err.Error()
}
