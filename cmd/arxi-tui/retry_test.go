package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// retryChat announces one automatic retry and then answers, blocking in between until
// released so the countdown can be read mid-wait.
type retryChat struct{ release chan struct{} }

func (retryChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }
func (r retryChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	if p.OnRetry != nil {
		p.OnRetry(driver.Retry{Provider: "apinex", Model: "mimo", Attempt: 2, Of: 6, Wait: 27 * time.Second, Status: "429", Reason: "Rate limit exceeded. Retry in 27s."})
		<-r.release
	}
	return &driver.ChatSendResult{Text: "done", Model: "mimo", Provider: "apinex"}, nil
}

func TestRetryIsShownAsACountdownAndALine(t *testing.T) {
	out := make(chan fold.Event, 32)
	core := retryChat{release: make(chan struct{})}
	c := newChatSession(core, out)
	if err := c.send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var label string
	for time.Now().Before(deadline) {
		if label = c.RetryLabel(time.Now()); label != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(label, "Retrying in 27s") || !strings.Contains(label, "rate limited") || !strings.Contains(label, "(2/6)") {
		t.Fatalf("label = %q, want a countdown naming the cause and the try", label)
	}
	if got := c.RetryLabel(time.Now().Add(10 * time.Second)); !strings.Contains(got, "Retrying in 17s") {
		t.Errorf("ten seconds later the label = %q, want 17s left", got)
	}
	if got := c.RetryLabel(time.Now().Add(time.Minute)); got != "" {
		t.Errorf("after the wait the label = %q, want none", got)
	}
	close(core.release)
	events := drain(out, "agent.turn_done", 2*time.Second)
	var line string
	for _, ev := range events {
		if ev.Type == "chat.warn" {
			line, _ = ev.Payload["text"].(string)
		}
	}
	if !strings.Contains(line, "apinex/mimo") || !strings.Contains(line, "Retrying in 27s (try 2 of 6)") {
		t.Errorf("the conversation line = %q", line)
	}
	if c.RetryLabel(time.Now()) != "" {
		t.Error("the countdown outlived the turn")
	}
}

func TestWaitTextRoundsUp(t *testing.T) {
	for d, want := range map[time.Duration]string{
		200 * time.Millisecond: "1s", 27 * time.Second: "27s", 26100 * time.Millisecond: "27s", 65 * time.Second: "1m 5s",
	} {
		if got := waitText(d); got != want {
			t.Errorf("waitText(%v) = %q, want %q", d, got, want)
		}
	}
}
