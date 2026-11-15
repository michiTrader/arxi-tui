package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

func pairsHistory(pairs int) []driver.ChatTurn {
	var h []driver.ChatTurn
	for i := 0; i < pairs; i++ {
		h = append(h, driver.ChatTurn{Role: "user", Text: fmt.Sprintf("ask number %d", i)},
			driver.ChatTurn{Role: "assistant", Text: fmt.Sprintf("answer number %d", i)})
	}
	return h
}

// Consequence if this fails: past the cap the model silently loses the original task and
// invents one when the user says "continua". Remedy: the cut turns must leave an outline.
func TestCapHistoryOutlinesWhatItCuts(t *testing.T) {
	h := pairsHistory(30) // 60 messages > 40
	out, dropped := capHistory(h, chatMaxHistory, chatMaxHistoryBytes)
	if dropped == 0 || len(out) > chatMaxHistory {
		t.Fatalf("dropped=%d len=%d", dropped, len(out))
	}
	if out[0].Role != "user" || !strings.Contains(out[0].Text, "ask number 0") || !strings.Contains(out[0].Text, appNoteMarker) {
		t.Errorf("the oldest task is missing from the outline: %q", out[0].Text)
	}
	if last := out[len(out)-1]; last.Text != "answer number 29" {
		t.Errorf("newest turn lost: %q", last.Text)
	}
	if out[1].Role != "user" && out[1].Role != "assistant" {
		t.Errorf("bad role %q", out[1].Role)
	}
}

func TestCapHistoryChangesNothingUnderTheCap(t *testing.T) {
	h := pairsHistory(5)
	out, dropped := capHistory(h, chatMaxHistory, chatMaxHistoryBytes)
	if dropped != 0 || len(out) != len(h) {
		t.Fatalf("dropped=%d len=%d", dropped, len(out))
	}
}

// Consequence if this fails: /compact's summary, the memory of everything before it, is
// the first thing the cap throws away. Remedy: keep the pinned pair at the head.
func TestCapHistoryNeverCutsTheCompactSummary(t *testing.T) {
	h := compactedHistory("the goal is X", pairsHistory(30))
	out, dropped := capHistory(h, chatMaxHistory, chatMaxHistoryBytes)
	if dropped == 0 || !strings.HasPrefix(out[0].Text, summaryHeader) || out[1].Role != "assistant" {
		t.Fatalf("summary not pinned: dropped=%d first=%q", dropped, out[0].Text)
	}
}

func TestOutlineIsBoundedAndStripsAppNotes(t *testing.T) {
	var cut []driver.ChatTurn
	for i := 0; i < 400; i++ {
		cut = append(cut, driver.ChatTurn{Role: "user", Text: strings.Repeat("x", 900) + "\n\n" + appNoteMarker + " secret note]"},
			driver.ChatTurn{Role: "assistant", Text: strings.Repeat("y", 900)})
	}
	o := outlineText(cut)
	if len(o) > digestBudgetBytes+400 {
		t.Errorf("outline is %d bytes", len(o))
	}
	if strings.Contains(o, "secret note") {
		t.Error("app notes must not be copied into the outline")
	}
}

func TestAutoCompactNearTheCapNeedsNoWindow(t *testing.T) {
	h := pairsHistory(18) // 36 messages: within 4 of the cap
	for i := range h {
		h[i].Text += strings.Repeat(".", 300)
	}
	if !shouldAutoCompact(autoCompactSetting{On: true, Percent: 80}, 0, 0, h) {
		t.Error("a history about to be cut must compact even with no window figure")
	}
	if shouldAutoCompact(autoCompactSetting{On: false, Percent: 80}, 0, 0, h) {
		t.Error("autocompact off must stay off")
	}
	if shouldAutoCompact(autoCompactSetting{On: true, Percent: 80}, 0, 0, pairsHistory(3)) {
		t.Error("a short history must not compact")
	}
}

type recChat struct{ seen chan []driver.ChatTurn }

func (r recChat) Hello() *driver.Hello { return &driver.Hello{Implemented: []string{"chat.send"}} }
func (r recChat) SubmitChatSend(ctx context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	r.seen <- p.History
	return &driver.ChatSendResult{Text: "ok", Model: "m", Provider: "p"}, nil
}

func TestSendPastTheCapWarnsOnceAndKeepsTheFullHistory(t *testing.T) {
	out := make(chan fold.Event, 64)
	rc := recChat{seen: make(chan []driver.ChatTurn, 4)}
	c := newChatSession(rc, out)
	c.history = pairsHistory(30)
	before := len(c.history)
	warns := 0
	for i := 0; i < 2; i++ {
		if err := c.send(context.Background(), "next"); err != nil {
			t.Fatal(err)
		}
		select {
		case h := <-rc.seen:
			if len(h) > chatMaxHistory {
				t.Errorf("sent %d messages", len(h))
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no request reached the core")
		}
		for deadline := time.After(2 * time.Second); ; {
			c.mu.Lock()
			busy := c.busy
			c.mu.Unlock()
			if !busy {
				break
			}
			select {
			case <-deadline:
				t.Fatal("turn never ended")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	for len(out) > 0 {
		if ev := <-out; ev.Type == "chat.warn" {
			if s, _ := ev.Payload["text"].(string); strings.Contains(s, "/compact") {
				warns++
			}
		}
	}
	if warns != 1 {
		t.Errorf("the cut notice appeared %d times, want once", warns)
	}
	if len(c.history) < before {
		t.Errorf("the stored history shrank from %d to %d: the cap must only shape what is sent", before, len(c.history))
	}
}
