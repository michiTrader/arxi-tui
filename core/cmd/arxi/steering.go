package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/michiTrader/arxi/internal/turn"
)

// Steering is how a user changes what a running turn is doing without throwing the
// turn away (ADR-0055). The client writes a line
//
//	{"type":"chat.steer","text":"..."}
//
// while chat.send is in flight. The turn reads it between two tool rounds, adds the
// text to the conversation as a user message, tells the client with a chat.steered
// notification, and asks the model again, so the model's next step already sees it.
// Everything the turn did before is kept: that is the difference from stopping the
// turn and sending a new request, which pays for the same work twice.

// chatSteerableNotification is written once, when the turn starts, to say this turn
// reads steering lines. A client steers only after it arrives: a core that does not
// know chat.steer would answer the line as an unknown request, and a client reading
// that answer in the middle of a turn takes it for the turn's own response.
type chatSteerableNotification struct {
	Type string `json:"type"`
}

// chatSteeredNotification tells the client a steering line has joined the
// conversation. It has a type and no id, like every notification. A client keeps the
// text it sent until this arrives; if the turn ends first, the text was not taken and
// the client sends it as an ordinary message.
type chatSteeredNotification struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// maxSteerBytes bounds one steering text. It rides in every later request of the
// turn, like any message, so a runaway one is a cost on each of them.
const maxSteerBytes = 16 << 10

// withSteering lets the turn's tool loop take steering lines from the connection.
// It starts the connection's pump, since the loop cannot otherwise look at the
// connection without waiting for a line that may never come.
func withSteering(ctx context.Context, c *connStreams) context.Context {
	tb := toolsFrom(ctx)
	if tb == nil || c == nil || c.src == nil {
		return ctx
	}
	c.src.startPump()
	_ = c.w.write(chatSteerableNotification{Type: "chat.steerable"})
	t := *tb
	t.steer = c.takeSteerTexts
	t.steered = func(text string) { _ = c.w.write(chatSteeredNotification{Type: "chat.steered", Text: text}) }
	return context.WithValue(ctx, toolsKey{}, &t)
}

// takeSteerTexts is the texts of the steering lines that have arrived, oldest first.
// A line without text steers nothing and is dropped.
func (c *connStreams) takeSteerTexts() []string {
	var out []string
	for _, l := range c.src.takeSteers() {
		var d struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(l.text), &d) != nil {
			continue
		}
		text := strings.TrimSpace(d.Text)
		if text == "" {
			continue
		}
		if len(text) > maxSteerBytes {
			text = text[:maxSteerBytes]
		}
		out = append(out, text)
	}
	return out
}

// endSteering drops the steering lines a finished turn never took. They were said to
// that turn; handing them to the next one would answer a question nobody asked it.
func (c *connStreams) endSteering() {
	if c != nil && c.src != nil {
		c.src.steers = nil
	}
}

// appendSteers adds what the user said since the last ask as one user message, and
// reports it. With nothing said the messages are returned as they were.
func appendSteers(msgs []turn.Message, tb *chatToolbox) []turn.Message {
	if tb.steer == nil {
		return msgs
	}
	texts := tb.steer()
	if len(texts) == 0 {
		return msgs
	}
	text := strings.Join(texts, "\n\n")
	if tb.steered != nil {
		tb.steered(text)
	}
	return append(msgs, turn.Message{Role: turn.RoleUser, Content: []turn.ContentBlock{{
		Type: turn.BlockText,
		Text: "The user sent this while you were working. Read it before your next step and change course if it asks for that:\n\n" + text,
	}}})
}
