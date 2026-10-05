package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// chatMaxHistory caps how many earlier messages ride along with a prompt, so a long
// session cannot grow a request without bound.
const chatMaxHistory = 40

// chatSystemPrompt is the standing instruction for a plain chat turn.
//
// It is sent with every message and the provider counts it, so it is kept to one short
// sentence: a bare "hola" should not cost dozens of tokens before the model reads it.
const chatSystemPrompt = "You are a concise assistant in a terminal."

// errChatBusy is returned when a second line is sent while an answer is pending.
var errChatBusy = errors.New("still waiting for the previous answer; press Esc to cancel it, or wait for it to finish")

// chatDialer opens a connection of its own for one chat turn and returns it with the
// function that closes it. Cancelling a turn is closing that connection: the core's
// protocol has no cancel verb and answers strictly in order, so the only way to stop a
// request that is in flight, without leaving its late answer to be read by the next
// request, is to give the turn a connection it alone owns and drop it.
type chatDialer func(ctx context.Context) (chatSender, func(), error)

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
	// effort is the thinking level sent with each turn ("" = the model decides).
	// It is a setting, not conversation: reset() leaves it alone.
	effort string

	// dial gives each turn its own connection (nil = every turn shares core, which
	// is what the tests and the mock use; cancelling then only abandons the answer).
	dial chatDialer
	// turn numbers the turns; cancel is the live turn's way to stop (nil when idle).
	turn   int64
	cancel context.CancelFunc
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

// setEffort chooses the thinking level for the turns that start from now on; "" means
// "send nothing". A turn already in flight keeps the level it began with.
func (c *chatSession) setEffort(level string) {
	c.mu.Lock()
	c.effort = level
	c.mu.Unlock()
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
	effort := c.effort
	hist := append([]driver.ChatTurn(nil), c.history...)
	if len(hist) > chatMaxHistory {
		hist = hist[len(hist)-chatMaxHistory:]
	}
	c.turn++
	turn := c.turn
	turnCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()
	go c.run(turnCtx, text, hist, gen, effort, turn)
	return nil
}

// cancelTurn stops the turn in flight, if any, and puts a "request failed:
// Cancelled" line in the conversation. It reports whether there was a turn to stop, so a key that means
// "cancel" can fall back to its other meaning when nothing is running. The turn is
// over at once: the next line may be sent without waiting for the abandoned
// goroutine to notice.
func (c *chatSession) cancelTurn() bool {
	c.mu.Lock()
	if !c.busy || c.cancel == nil {
		c.mu.Unlock()
		return false
	}
	cancel, gen := c.cancel, c.gen
	c.cancel = nil
	c.busy = false
	c.mu.Unlock()
	cancel()
	c.post(context.Background(), gen, "chat.cancelled", nil)
	return true
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
	cancel := c.cancel
	c.cancel = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// busyNow reports whether a turn is in flight.
func (c *chatSession) busyNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy
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
	if ctx.Err() != nil || !c.current(gen) {
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
	c.post(ctx, gen, "chat.error", map[string]any{"text": msg})
}

// post queues a conversation event without ever blocking its caller (see fail).
func (c *chatSession) post(ctx context.Context, gen int64, typ string, payload map[string]any) {
	if !c.current(gen) {
		return
	}
	ev := fold.Event{Type: typ, Seq: c.nextSeq(), Actor: "assistant", Payload: payload}
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

func (c *chatSession) run(ctx context.Context, text string, hist []driver.ChatTurn, gen int64, effort string, turn int64) {
	defer func() {
		c.mu.Lock()
		// Only the turn that is still current frees the session: a cancelled turn
		// winding down late must not mark its successor idle.
		if c.gen == gen && c.turn == turn {
			c.busy = false
			c.cancel = nil
		}
		c.mu.Unlock()
	}()
	c.emit(ctx, gen, "run.prompt", map[string]any{"text": text})
	c.emit(ctx, gen, "agent.activated", map[string]any{"agent": "assistant"})
	started := time.Now()
	core := c.core
	if c.dial != nil {
		conn, closeConn, err := c.dial(ctx)
		if err != nil {
			if ctx.Err() == nil {
				c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
				c.fail(ctx, gen, chatErrorText(err))
			}
			return
		}
		defer closeConn()
		core = conn
	}
	res, err := core.SubmitChatSend(ctx, driver.ChatSendParams{
		Prompt: text, System: chatSystemPrompt, History: hist, Effort: effort,
		// The model's thinking is shown live in the Thinking line.
		OnThinking: func(fragment string) {
			c.post(ctx, gen, "chat.thinking", map[string]any{"text": fragment})
		},
	})
	if ctx.Err() != nil {
		// Cancelled (cancelTurn already told the conversation) or the program is
		// closing: whatever came back is not wanted.
		return
	}
	if err != nil {
		c.emit(ctx, gen, "agent.failed", map[string]any{"agent": "assistant"})
		c.fail(ctx, gen, chatErrorText(err))
		return
	}
	c.mu.Lock()
	if c.gen != gen || c.turn != turn {
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
		"duration_ms": float64(time.Since(started).Milliseconds()),
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
