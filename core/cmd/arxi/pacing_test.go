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
	f, notes, _, err := paceRigTimed(t, rate, nil)
	return f, notes, err
}

// paceRigTimed is paceRig that also says when each notice was given.
// tweak, when set, runs after the fast-retry settings and before the turn.
func paceRigTimed(t *testing.T, rate func(call int) (string, string), tweak func()) (*fakeLLM, []chatRetryNotification, []time.Time, error) {
	t.Helper()
	f := setUpFake(t)
	fastRetries(t)
	if tweak != nil {
		tweak()
	}
	f.rate = rate
	f.script = []scripted{{name: "run", args: echoCall}}
	var notes []chatRetryNotification
	var at []time.Time
	dir := projectDir(t)
	ctx, err := withTools(context.Background(), dir, "", nil, func(chatToolNotification) {})
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = withRuns(ctx, editsAllow); err != nil {
		t.Fatal(err)
	}
	ctx = withRetryNotice(ctx, func(n chatRetryNotification) { notes = append(notes, n); at = append(at, time.Now()) })
	_, err = chatSendEffort(ctx, "build it", "", "", "", "")
	return f, notes, at, err
}

func TestSpentBudgetMakesTheNextAskWaitForTheWindow(t *testing.T) {
	// The first ask answers "0 left, window opens in 300ms": the tool loop's next ask
	// must wait for that window instead of being sent to be refused.
	//
	// The window is counted from the moment the answer arrived, and the tool the model
	// asked for runs before the next ask, so the wait that is left is SHORTER than
	// the reset by however long that took (50 ms and more for a process on Windows). A
	// test that expects "about 301 ms" is right on a quiet machine and wrong on a slow one,
	// which is how it failed. The bounds below follow from the code, with no slack to tune:
	//   wait <= reset + margin           (the window opens no later than that after the answer)
	//   wait >= reset + margin - (notice - the moment the fake answered)
	// because the answer was noted no earlier than the fake sent it.
	// A margin far bigger than the timing slack, so that dropping it is visible.
	const margin = 100 * time.Millisecond
	start := time.Now()
	var answered time.Time
	f, notes, at, err := paceRigTimed(t, func(call int) (string, string) {
		if call == 1 {
			answered = time.Now()
			return "0", "300ms"
		}
		return "4", "300ms"
	}, func() { chatRetryMargin = margin }) // fastRetries restores the real one afterwards
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Fatalf("the fake saw %d asks, want 2 (no refused ask in between)", f.calls)
	}
	if len(notes) != 1 || notes[0].Status != paceStatus {
		t.Fatalf("notices = %+v, want exactly one pacing notice", notes)
	}
	full := int64(300) + margin.Milliseconds()
	spent := at[0].Sub(answered).Milliseconds()
	if got := notes[0].WaitMs; got > full || got < full-spent-2 { // -2: WaitMs and spent are both truncated to whole ms
		t.Errorf("wait = %dms; it must be in [%d, %d] (the reset plus the margin, less the %dms that passed after the answer)",
			got, full-spent-2, full, spent)
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
	//
	// Not served is shown by the absence of a pacing notice: a wait always announces itself
	// before it sleeps. An earlier version also required the call to finish in under the
	// window's 300ms, which a slow Windows runner overran (341ms) with the right answer
	// (two asks, no notice): wall-clock time says how loaded the machine is, not whether
	// the pacer waited.
	fastRetries(t)
	saved := chatRetryTotal
	chatRetryTotal = 100 * time.Millisecond
	t.Cleanup(func() { chatRetryTotal = saved })
	f, notes, err := paceRig(t, func(call int) (string, string) {
		if call == 1 {
			return "0", "300ms"
		}
		return "", ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 || len(notes) != 0 {
		t.Fatalf("asks=%d notices=%+v; Consequence: a window longer than the call's remaining wait budget was waited for, so one call can hold a user far past the budget. Remedy: pacing must not serve a wait that exceeds what is left of chatRetryTotal.", f.calls, notes)
	}
}
