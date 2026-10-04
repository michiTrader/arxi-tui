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
// answer (or a clear error) back to the loop. Failures are never swallowed: they go
// to the notices channel, which the loop shows in the banner.
type chatSession struct {
	core  chatSender
	out   chan<- fold.Event
	notes chan<- string

	mu      sync.Mutex
	history []driver.ChatTurn
	busy    bool
	seq     int64
}

func newChatSession(core chatSender, out chan<- fold.Event, notes chan<- string) *chatSession {
	return &chatSession{core: core, out: out, notes: notes}
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
// the network is reported through the notices channel.
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
	hist := append([]driver.ChatTurn(nil), c.history...)
	if len(hist) > chatMaxHistory {
		hist = hist[len(hist)-chatMaxHistory:]
	}
	c.mu.Unlock()
	go c.run(ctx, text, hist)
	return nil
}

func (c *chatSession) nextSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}

func (c *chatSession) emit(ctx context.Context, typ string, payload map[string]any) {
	ev := fold.Event{Type: typ, Seq: c.nextSeq(), Actor: "assistant", Payload: payload}
	select {
	case c.out <- ev:
	case <-ctx.Done():
	}
}

func (c *chatSession) note(msg string) {
	select {
	case c.notes <- msg:
	default:
	}
}

func (c *chatSession) run(ctx context.Context, text string, hist []driver.ChatTurn) {
	defer func() {
		c.mu.Lock()
		c.busy = false
		c.mu.Unlock()
	}()
	c.emit(ctx, "run.prompt", map[string]any{"text": text})
	c.emit(ctx, "agent.activated", map[string]any{"agent": "assistant"})
	res, err := c.core.SubmitChatSend(ctx, driver.ChatSendParams{Prompt: text, System: chatSystemPrompt, History: hist})
	if err != nil {
		c.emit(ctx, "agent.failed", map[string]any{"agent": "assistant"})
		c.note(chatErrorText(err))
		return
	}
	c.mu.Lock()
	c.history = append(c.history, driver.ChatTurn{Role: "user", Text: text}, driver.ChatTurn{Role: "assistant", Text: res.Text})
	c.mu.Unlock()
	model := res.Model
	if res.Provider != "" && !strings.Contains(model, "/") {
		model = res.Provider + "/" + res.Model
	}
	c.emit(ctx, "llm.response", map[string]any{
		"text": res.Text, "model": model, "agent": "assistant",
		"tokens_in": float64(res.InputTokens), "tokens_out": float64(res.OutputTokens),
	})
	c.emit(ctx, "agent.turn_done", map[string]any{"agent": "assistant"})
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
