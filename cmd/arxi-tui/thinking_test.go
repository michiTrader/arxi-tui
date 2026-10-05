package main

import (
	"context"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// thinkingChat reports two fragments of thinking through the callback the session
// hands it, then answers.
type thinkingChat struct{}

func (thinkingChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}
func (thinkingChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	if p.OnThinking != nil {
		p.OnThinking("Let me ")
		p.OnThinking("think.")
	}
	return &driver.ChatSendResult{Text: "done", Model: "m", Provider: "p"}, nil
}

// The model's thinking reaches the conversation as chat.thinking events, in order,
// before the answer, and the answer clears it.
func TestChatSessionRelaysThinking(t *testing.T) {
	out := make(chan fold.Event, 32)
	c := newChatSession(thinkingChat{}, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	events := drain(out, "agent.turn_done", 2*time.Second)
	var types []string
	var thinking string
	for _, ev := range events {
		types = append(types, ev.Type)
		if ev.Type == "chat.thinking" {
			thinking += ev.Payload["text"].(string)
		}
	}
	if thinking != "Let me think." {
		t.Fatalf("thinking relayed as %q, want %q (events: %v)", thinking, "Let me think.", types)
	}
	if st := fold.Fold(events); st.ThinkingText != "" {
		t.Fatalf("thinking survived the answer: %q", st.ThinkingText)
	}
}

func TestThinkingLabel(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{0, "• Thinking (0s) "},
		{3400 * time.Millisecond, "• Thinking (3s) "},
		{59 * time.Second, "• Thinking (59s) "},
		{65 * time.Second, "• Thinking (1m 5s) "},
	} {
		if got := thinkingLabel(tc.d); got != tc.want {
			t.Errorf("thinkingLabel(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
