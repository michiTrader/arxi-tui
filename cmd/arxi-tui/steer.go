package main

import (
	"context"
	"errors"
	"strings"
)

// Steering a turn that is running (ADR-0055 in core/): when the core says the turn takes
// steering, a line sent with queue.steer goes to the turn itself and the model reads it
// at its next step. Nothing it had read or written is thrown away, which is the whole
// difference from stopForSteer (chat.go), still the way for a core that does not know it.

// errNotSteerable is what steer returns when the turn in flight cannot take a line: no
// turn, an older core, or a turn with no tools (the core only reads steering between
// tool rounds). The caller then stops the turn and sends a new one.
var errNotSteerable = errors.New("this turn cannot take a line while it runs")

// steerable is called by the driver when the core says the turn takes steering. A call
// for a turn that is no longer the current one is ignored.
func (c *chatSession) steerable(gen, turn int64, f func(string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen == gen && c.turn == turn && c.busy {
		c.steerFn = f
	}
}

// canSteer reports whether steer would write to a turn right now.
func (c *chatSession) canSteer() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.busy && c.steerFn != nil && !c.compacting
}

// steer writes text to the turn in flight. It returns errNotSteerable when there is no
// such turn, and the driver's error when the line could not be written; in both cases
// nothing was sent and the text is still the caller's. On success the text is owed: it
// stays so until the core says it took it (steered) or the turn ends without it, when
// it is handed back as an ordinary message (giveBackSteers).
func (c *chatSession) steer(text string) error {
	text = strings.TrimSpace(text)
	c.mu.Lock()
	f := c.steerFn
	if !c.busy || f == nil || c.compacting || text == "" {
		c.mu.Unlock()
		return errNotSteerable
	}
	// Owed before it is written: the core can answer faster than this returns.
	c.steerOwed = append(c.steerOwed, text)
	c.mu.Unlock()
	if err := f(text); err != nil {
		c.mu.Lock()
		c.dropOwedLocked(text)
		c.mu.Unlock()
		return err
	}
	return nil
}

// steered is the core saying it took text into the turn: the owed texts it joined, oldest
// first, with a blank line between. They stop being owed, become part of the request the
// history keeps, and show in the conversation.
func (c *chatSession) steered(ctx context.Context, gen, turn int64, text string) {
	c.mu.Lock()
	if c.gen != gen || c.turn != turn {
		c.mu.Unlock()
		return
	}
	took := c.takeJoinedLocked(text)
	// One entry per chat.steered line, the same text the session file records, so the
	// history a live session keeps and the one rebuilt from the file say the same.
	joined := strings.Join(took, "\n\n")
	c.turnSteers = append(c.turnSteers, joined)
	c.mu.Unlock()
	c.post(ctx, gen, "chat.steered", map[string]any{"text": joined})
}

// takeJoinedLocked removes from the owed texts the ones the core joined into text and
// returns them. The core takes every text written since its last ask, so they are a
// prefix of what is owed; the match is by the joined text, and when the core changed one
// (it cuts a very long text) the ones that appear in it are taken instead. If nothing
// matches, the core's text is returned as the one that was taken, so what the user sees
// is what the model read.
func (c *chatSession) takeJoinedLocked(text string) []string {
	joined := ""
	for i, o := range c.steerOwed {
		if i > 0 {
			joined += "\n\n"
		}
		joined += o
		if joined == text {
			took := append([]string(nil), c.steerOwed[:i+1]...)
			c.steerOwed = append([]string(nil), c.steerOwed[i+1:]...)
			return took
		}
	}
	var took, rest []string
	for _, o := range c.steerOwed {
		if strings.Contains(text, o) {
			took = append(took, o)
		} else {
			rest = append(rest, o)
		}
	}
	if len(took) == 0 {
		return []string{text}
	}
	c.steerOwed = rest
	return took
}

// dropOwedLocked forgets one owed text (the first equal one).
func (c *chatSession) dropOwedLocked(text string) {
	for i, o := range c.steerOwed {
		if o == text {
			c.steerOwed = append(c.steerOwed[:i:i], c.steerOwed[i+1:]...)
			return
		}
	}
}

// takeOwedLocked empties and returns the texts still owed.
func (c *chatSession) takeOwedLocked() []string {
	o := c.steerOwed
	c.steerOwed, c.steerFn = nil, nil
	return o
}

// endSteering closes steering for the turn that just ended and returns what the core
// never took. A turn that is no longer current (it was cancelled or steered away from)
// already handed its texts back and owes nothing.
func (c *chatSession) endSteering(gen, turn int64) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen || c.turn != turn {
		return nil
	}
	return c.takeOwedLocked()
}

// giveBackSteers hands the loop the texts the core never took, as one event each. The loop
// sends them as ordinary messages once the session is free, or puts them back on the line
// if the turn did not end well, like every other message that was waiting. Without this a
// line written just before the answer ended would be accepted by the program and
// answered by nobody.
func (c *chatSession) giveBackSteers(gen int64, texts []string) {
	for _, t := range texts {
		c.post(context.Background(), gen, "chat.steer_unsent", map[string]any{"text": t})
	}
}

// steersNote is what the history says about the lines added to a request while it ran, so
// "retry" or "continue" after a stopped turn still means the request as it had become.
func steersNote(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	return "\n\n[App note, not typed by the user: while this request was being worked on the user added: " +
		strings.Join(texts, " / ") + "]"
}
