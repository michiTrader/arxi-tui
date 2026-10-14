package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi/internal/provider"
)

// paceRig runs a two-step tool conversation (one tool call, then the answer) against
// the fake, with the fake's rate-limit headers set by rate, and returns what the
// client was told and how the fake was asked.
func paceRig(t *testing.T, rate func(call int) (string, string)) (*fakeLLM, []chatRetryNotification, error) {
	t.Helper()
	f := setUpFake(t)
	fastRetries(t)
	f.rate = rate
	f.script = []scripted{{name: "run", args: echoCall}}
	var notes []chatRetryNotification
	dir := projectDir(t)
	ctx, err := withTools(context.Background(), dir, "", nil, func(chatToolNotification) {})
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = withRuns(ctx, editsAllow); err != nil {
		t.Fatal(err)
	}
	ctx = withRetryNotice(ctx, func(n chatRetryNotification) { notes = append(notes, n) })
	_, err = chatSendEffort(ctx, "build it", "", "", "", "")
	return f, notes, err
}

func TestSpentBudgetMakesTheNextAskWaitForTheWindow(t *testing.T) {
	// The first ask answers "0 left, window opens in 300ms": the tool loop's next ask
	// must wait that long instead of being sent to be refused.
	start := time.Now()
	f, notes, err := paceRig(t, func(call int) (string, string) {
		if call == 1 {
			return "0", "300ms"
		}
		return "4", "300ms"
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Fatalf("the fake saw %d asks, want 2 (no refused ask in between)", f.calls)
	}
	if len(notes) != 1 || notes[0].Status != paceStatus {
		t.Fatalf("notices = %+v, want exactly one pacing notice", notes)
	}
	if want := int64(300 + 1); notes[0].WaitMs < want-50 || notes[0].WaitMs > want+50 {
		t.Errorf("wait = %dms, want about %dms (the reset plus the margin)", notes[0].WaitMs, want)
	}
	if time.Since(start) < 300*time.Millisecond {
		t.Errorf("the second ask went out after %v, before the window opened", time.Since(start))
	}
}

func TestAnAskWithBudgetLeftDoesNotWait(t *testing.T) {
	f, notes, err := paceRig(t, func(int) (string, string) { return "3", "30s" })
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(notes) != 0 {
		t.Fatalf("asks=%d notices=%+v; a window with requests left must not be waited for", f.calls, notes)
	}
}

func TestAnswersWithoutRateLimitHeadersAreNotPaced(t *testing.T) {
	f, notes, err := paceRig(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(notes) != 0 {
		t.Fatalf("asks=%d notices=%+v", f.calls, notes)
	}
}

func TestAWindowLongerThanTheChatWaitsIsNotServed(t *testing.T) {
	// The provider says the window opens in 10s, longer than the 2s this chat waits: pacing must not freeze the
	// chat for that; the ask goes out and the provider's own answer decides.
	start := time.Now()
	fastRetries(t) // saves the real chatMaxWait, restored last
	chatMaxWait = 2 * time.Second
	f, notes, err := paceRig(t, func(call int) (string, string) {
		if call == 1 {
			return "0", "10s"
		}
		return "", ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(notes) != 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("asks=%d notices=%+v elapsed=%v", f.calls, notes, time.Since(start))
	}
}

func TestARefusalAndItsHeadersAreWaitedForOnce(t *testing.T) {
	// A 429 whose headers also say "0 left" is one wait, the retry's, and not a
	// retry wait followed by a pacing wait for the same window.
	f := chatProvider(t)
	f.failNext, f.failCode, f.failHeader = 1, 429, "1"
	f.rate = func(call int) (string, string) {
		if call == 1 {
			return "0", "1s"
		}
		return "4", "1s"
	}
	var notes []chatRetryNotification
	ctx := withRetryNotice(context.Background(), func(n chatRetryNotification) { notes = append(notes, n) })
	if _, err := chatSendEffort(ctx, "hi", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Status != "429" {
		t.Fatalf("notices = %+v, want only the 429 retry", notes)
	}
}

func TestPacingWaitIsStoppedByCancel(t *testing.T) {
	f := setUpFake(t)
	fastRetries(t)
	f.rate = func(call int) (string, string) { return "0", "30s" }
	f.script = []scripted{{name: "run", args: echoCall}}
	ctx, cancel := context.WithCancel(context.Background())
	var err error
	if ctx, err = withTools(ctx, projectDir(t), "", nil, func(chatToolNotification) {}); err != nil {
		t.Fatal(err)
	}
	if ctx, err = withRuns(ctx, editsAllow); err != nil {
		t.Fatal(err)
	}
	ctx = withRetryNotice(ctx, func(n chatRetryNotification) {
		if n.Status == paceStatus {
			cancel()
		}
	})
	start := time.Now()
	_, err = chatSendEffort(ctx, "build it", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "context canceled") || time.Since(start) > 5*time.Second {
		t.Fatalf("err=%v elapsed=%v; cancelling must end the pacing wait", err, time.Since(start))
	}
}

func TestPacerWaitsOnlyWhileTheWindowIsClosed(t *testing.T) {
	var p pacer
	if p.due() != 0 {
		t.Fatal("a pacer that has heard nothing must not wait")
	}
	p.note(provider.RateLimit{Known: true, Remaining: 0, Reset: time.Hour})
	if d := p.due(); d < time.Hour || d > time.Hour+chatRetryMargin+time.Second {
		t.Fatalf("due = %v, want the reset plus the margin", d)
	}
	// A later answer with budget left means the window moved on: no wait.
	p.note(provider.RateLimit{Known: true, Remaining: 2, Reset: time.Hour})
	if d := p.due(); d != 0 {
		t.Fatalf("due = %v after an answer with requests left, want 0", d)
	}
	// An answer that says nothing about the budget must not leave a stale wait either.
	p.note(provider.RateLimit{Known: true, Remaining: 0, Reset: time.Hour})
	p.note(provider.RateLimit{})
	if d := p.due(); d != 0 {
		t.Fatalf("due = %v after an answer with no rate-limit headers, want 0", d)
	}
	// A window already open is not waited for.
	p.note(provider.RateLimit{Known: true, Remaining: 0, Reset: time.Millisecond})
	time.Sleep(5 * time.Millisecond)
	if d := p.due(); d != 0 {
		t.Fatalf("due = %v for a window that has opened, want 0", d)
	}
}

func TestPacingCountsAgainstTheTotalWaitBudget(t *testing.T) {
	// The window (300ms) fits chatMaxWait but not what is left of the whole call's
	// waiting budget (100ms): it is not served, the ask goes out at once.
	fastRetries(t)
	saved := chatRetryTotal
	chatRetryTotal = 100 * time.Millisecond
	t.Cleanup(func() { chatRetryTotal = saved })
	start := time.Now()
	f, notes, err := paceRig(t, func(call int) (string, string) {
		if call == 1 {
			return "0", "300ms"
		}
		return "", ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(notes) != 0 || time.Since(start) >= 300*time.Millisecond {
		t.Fatalf("asks=%d notices=%+v elapsed=%v", f.calls, notes, time.Since(start))
	}
}
