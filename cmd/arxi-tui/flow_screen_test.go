package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func flowEv(seq int64, typ, actor string, p map[string]any) fold.Event {
	if p == nil {
		p = map[string]any{}
	}
	return fold.Event{Seq: seq, Type: typ, Actor: actor, Payload: p}
}

// teamLog is a feature-team run stopped on an approval.
func teamLog() []fold.Event {
	return []fold.Event{
		flowEv(1, "run.started", "", map[string]any{"budget_usd": float64(20)}),
		flowEv(2, "stage.entered", "", map[string]any{"stage": "build", "index": float64(0)}),
		flowEv(3, "agent.activated", "backend", map[string]any{"agent": "backend", "role": "implementer"}),
		flowEv(4, "agent.activated", "frontend", map[string]any{"agent": "frontend", "role": "implementer"}),
		flowEv(5, "stage.submitted", "frontend", nil),
		flowEv(6, "agent.blocked", "backend", map[string]any{
			"blocked_on":  "approval",
			"blocked_ref": map[string]any{"inbox_id": "inb-7", "tool": "bash"},
		}),
	}
}

func TestTheEmbeddedFlowSceneIsValid(t *testing.T) {
	if _, err := loadFlowScene(); err != nil {
		t.Fatal(err)
	}
}

func TestFlowSaysSoForAPlainChat(t *testing.T) {
	st := fold.Fold([]fold.Event{
		flowEv(1, "run.prompt", "", map[string]any{"text": "hi"}),
		flowEv(2, "llm.response", "", map[string]any{"text": "hello"}),
	})
	if !flowIsChat(&st) {
		t.Fatal("a conversation with no stages and no team is a plain chat")
	}
	if d := flowDetail(&st); !strings.Contains(d, "plain chat with one agent") {
		t.Fatalf("detail = %q", d)
	}
}

func TestFlowDrawsStagesMembersAndTheBlocker(t *testing.T) {
	st := fold.Fold(teamLog())
	if flowIsChat(&st) {
		t.Fatal("a team in a stage is not a plain chat")
	}
	var f flowScreen
	f.publish(&st)
	if len(st.HubRows) != 2 {
		t.Fatalf("rows = %+v", st.HubRows)
	}
	if !strings.Contains(st.HubRows[0].Label, "backend") || !strings.Contains(st.HubRows[0].Label, "implementer") {
		t.Errorf("row 0 = %+v", st.HubRows[0])
	}
	if !strings.Contains(st.HubRows[0].Status, "… waiting") {
		t.Errorf("backend waits on an approval, row = %+v", st.HubRows[0])
	}
	if !strings.Contains(st.HubRows[1].Status, "✓ submitted") {
		t.Errorf("frontend submitted, row = %+v", st.HubRows[1])
	}
	for _, want := range []string{"build ●", "(submitted: frontend)", "⚠ backend waits for approval: bash", "Press a to approve it, or r to reject it."} {
		if !strings.Contains(st.HubDetail, want) {
			t.Errorf("detail lacks %q:\n%s", want, st.HubDetail)
		}
	}
	if !strings.Contains(st.HubTitle, "stage build") || !strings.Contains(st.HubTitle, "of 2 working") {
		t.Errorf("title = %q", st.HubTitle)
	}
}

func TestFlowShowsWhoWaitsForWhom(t *testing.T) {
	log := append(teamLog()[:5], flowEv(6, "agent.blocked", "backend", map[string]any{
		"blocked_on": "peer", "blocked_ref": map[string]any{"peer": "frontend"}}))
	st := fold.Fold(log)
	var f flowScreen
	f.publish(&st)
	if !strings.Contains(st.HubRows[0].Status, "waiting for frontend") {
		t.Fatalf("row = %+v", st.HubRows[0])
	}
}

func TestFlowSaysNothingHoldsTheRunUp(t *testing.T) {
	st := fold.Fold(teamLog()[:5])
	if d := flowDetail(&st); !strings.Contains(d, "Nothing is holding the run up.") {
		t.Fatalf("detail = %q", d)
	}
}

func TestFlowKeysMoveAndClose(t *testing.T) {
	st := fold.Fold(teamLog())
	f := flowScreen{}
	if closed, _ := f.key(term.Key{Type: term.KeyDown}); closed || f.sel != 1 {
		t.Fatalf("down: sel = %d", f.sel)
	}
	f.publish(&st)
	f.key(term.Key{Type: term.KeyDown}) // past the last row
	f.publish(&st)
	if f.sel != 1 {
		t.Fatalf("the highlight must stop at the last member, sel = %d", f.sel)
	}
	if closed, _ := f.key(term.Key{Type: term.KeyRunes, Runes: []rune{'x'}}); closed {
		t.Error("an ordinary letter must not close the screen")
	}
	for _, k := range []term.Key{{Type: term.KeyEscape}, {Type: term.KeyEnter}, {Type: term.KeyRunes, Runes: []rune{'q'}}} {
		if closed, _ := f.key(k); !closed {
			t.Errorf("%+v should close", k)
		}
	}
}

// TestLoopFlowOpensOnATeamRunAndEscCloses drives the real loop: the events of a
// team run arrive, /flow opens the screen over the chat, and Esc brings the chat back.
func TestLoopFlowOpensOnATeamRunAndEscCloses(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &testDriver{evCh: make(chan fold.Event, 64)}
	for _, e := range teamLog() {
		drv.evCh <- e
	}
	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent('/')}}
	script = append(script, keys("flow")...)
	script = append(script,
		scheduledEvent{40 * time.Millisecond, enterEvent()},
		// A letter typed on the screen must not reach the chat input.
		scheduledEvent{60 * time.Millisecond, keyEvent('z')},
		scheduledEvent{150 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEscape}}},
		scheduledEvent{150 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	frames := strings.Split(tty.output(), "\x1b[?2026h")
	var open string
	for _, f := range frames {
		s := stripANSI(f)
		if strings.Contains(s, "backend waits for approval: bash") {
			open = s
		}
	}
	if open == "" {
		t.Fatalf("no frame showed the flow screen:\n%s", stripANSI(tty.output()))
	}
	for _, want := range []string{"flow", "backend", "frontend", "✓ submitted", "esc close"} {
		if !strings.Contains(open, want) {
			t.Errorf("flow screen lacks %q:\n%s", want, open)
		}
	}
	last := stripANSI(frames[len(frames)-1])
	if strings.Contains(last, "esc close") {
		t.Errorf("Esc did not close the screen:\n%s", last)
	}
	if strings.Contains(last, "› z") || strings.Contains(last, "┃ z") {
		t.Errorf("a key typed on the flow screen leaked into the chat input:\n%s", last)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("/flow reached the driver as %q", drv.submitted)
	}
}

// publishedBlock folds the approval log and publishes it on a fresh screen.
func publishedBlock(t *testing.T) (*flowScreen, fold.State) {
	t.Helper()
	st := fold.Fold(teamLog())
	f := &flowScreen{}
	f.publish(&st)
	if f.item != "inb-7" {
		t.Fatalf("item = %q, want inb-7", f.item)
	}
	return f, st
}

func TestFlowNeverTellsThePersonToTypeACommand(t *testing.T) {
	f, st := publishedBlock(t)
	for _, text := range []string{st.HubDetail, st.HubHint, st.HubTitle} {
		if strings.Contains(text, "arxi ") {
			t.Errorf("flow shows a command to type: %q", text)
		}
	}
	if !strings.Contains(st.HubHint, "a approve") || !strings.Contains(st.HubHint, "r reject") {
		t.Errorf("the legend must offer both answers: %q", st.HubHint)
	}
	_ = f
}

func TestFlowAApprovesTheBlockedItem(t *testing.T) {
	f, st := publishedBlock(t)
	closed, task := f.key(rk('a'))
	if closed || task == nil || task.kind != "approve" || task.item != "inb-7" {
		t.Fatalf("closed=%v task=%+v", closed, task)
	}
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "Approving …") {
		t.Errorf("detail must say it is working: %s", st.HubDetail)
	}
	// While the core is busy a second press is ignored: no double answer.
	if _, again := f.key(rk('a')); again != nil {
		t.Error("a second answer must not start while one is in flight")
	}
	f.apply(flowOutcome{answered: "✓ approved — the run goes on"})
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "✓ approved") || strings.Contains(st.HubDetail, "Approving") {
		t.Errorf("detail after the answer: %s", st.HubDetail)
	}
}

func TestFlowRejectAsksForAReasonFirst(t *testing.T) {
	f, st := publishedBlock(t)
	if closed, task := f.key(rk('r')); closed || task != nil || !f.rejecting {
		t.Fatalf("r must open the reason, not send: closed=%v task=%v rejecting=%v", closed, task, f.rejecting)
	}
	f.publish(&st)
	if !strings.Contains(st.HubDetail, "Why reject it?") || st.HubHint != flowReasonHint {
		t.Errorf("reason prompt missing: %q / %q", st.HubDetail, st.HubHint)
	}
	// An empty reason is refused here, without bothering the core.
	if _, task := f.key(tk(term.KeyEnter)); task != nil {
		t.Fatal("a rejection with no reason must not be sent")
	}
	if !strings.Contains(f.banner, "Say why") {
		t.Errorf("banner = %q", f.banner)
	}
	// 'a' and 'q' are letters of the reason here, not commands.
	for _, r := range "too risky aq" {
		f.key(rk(r))
	}
	f.key(tk(term.KeyBackspace))
	f.paste(" now")
	_, task := f.key(tk(term.KeyEnter))
	if task == nil || task.kind != "reject" || task.reason != "too risky a now" || task.item != "inb-7" {
		t.Fatalf("task = %+v", task)
	}
}

func TestFlowEscLeavesTheReasonWithoutClosingTheScreen(t *testing.T) {
	f, _ := publishedBlock(t)
	f.key(rk('r'))
	if closed, _ := f.key(tk(term.KeyEscape)); closed || f.rejecting {
		t.Fatalf("esc must only leave the reason: closed=%v rejecting=%v", closed, f.rejecting)
	}
	if closed, _ := f.key(tk(term.KeyEscape)); !closed {
		t.Error("a second esc closes the screen")
	}
}

func TestFlowOffersNoAnswerWhenNothingWaits(t *testing.T) {
	st := fold.Fold(teamLog()[:5])
	f := &flowScreen{}
	f.publish(&st)
	if f.item != "" || strings.Contains(st.HubHint, "approve") {
		t.Errorf("nothing waits, yet item=%q hint=%q", f.item, st.HubHint)
	}
	if _, task := f.key(rk('a')); task != nil {
		t.Error("a must do nothing when there is no approval")
	}
}

type fakeResumer struct {
	got  flowTask
	err  error
	save bool
}

func (r *fakeResumer) AnswerAndResume(_ context.Context, kind, item, reason string, recorded func()) error {
	r.got = flowTask{kind: kind, item: item, reason: reason}
	if r.save && recorded != nil {
		recorded()
	}
	return r.err
}

func TestStartFlowAnswerReportsSavedThenFailure(t *testing.T) {
	done := make(chan flowOutcome, 2)
	r := &fakeResumer{save: true, err: errors.New("the run is held elsewhere")}
	startFlowAnswer(context.Background(), r, flowTask{kind: "approve", item: "i1"}, done)
	first, second := <-done, <-done
	if !strings.Contains(first.answered, "approved") {
		t.Errorf("first = %+v", first)
	}
	if !strings.Contains(second.err, "answer is saved") || !strings.Contains(second.err, "held elsewhere") {
		t.Errorf("second = %+v", second)
	}

	r = &fakeResumer{err: errors.New("nothing pending")}
	startFlowAnswer(context.Background(), r, flowTask{kind: "reject", item: "i1", reason: "no"}, done)
	if o := <-done; o.err != "nothing pending" || r.got.reason != "no" {
		t.Errorf("outcome = %+v, got %+v", o, r.got)
	}
}

// answeringDriver is a test driver that can answer an approval, and remembers it.
type answeringDriver struct {
	testDriver
	mu   sync.Mutex
	got  []flowTask
	fail error
}

func (d *answeringDriver) AnswerAndResume(_ context.Context, kind, item, reason string, recorded func()) error {
	d.mu.Lock()
	d.got = append(d.got, flowTask{kind: kind, item: item, reason: reason})
	d.mu.Unlock()
	if d.fail != nil {
		return d.fail
	}
	recorded()
	return nil
}

// flowAnswerScript opens /flow on the blocked run and then plays the keys.
func flowAnswerScript(then ...scheduledEvent) []scheduledEvent {
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent('/')}}
	for _, r := range "flow" {
		script = append(script, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	script = append(script, scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, then...)
	return append(script,
		scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
}

func runFlowAnswer(t *testing.T, drv *answeringDriver, script []scheduledEvent) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range teamLog() {
		drv.evCh <- e
	}
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return stripANSI(tty.output())
}

// TestLoopFlowApprovesWithOneKey drives the real loop: a blocked run, /flow, one
// key, and the driver is asked to approve exactly that item. No command is shown.
func TestLoopFlowApprovesWithOneKey(t *testing.T) {
	drv := &answeringDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	out := runFlowAnswer(t, drv, flowAnswerScript(
		scheduledEvent{150 * time.Millisecond, keyEvent('a')}))
	if len(drv.got) != 1 || drv.got[0].kind != "approve" || drv.got[0].item != "inb-7" {
		t.Fatalf("the driver was asked %+v", drv.got)
	}
	for _, want := range []string{"Press a to approve it, or r to reject it.", "a approve", "✓ approved"} {
		if !strings.Contains(out, want) {
			t.Errorf("no frame showed %q", want)
		}
	}
	if strings.Contains(out, "arxi inbox") {
		t.Error("the screen told the person to type a command")
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the key reached the chat as %q", drv.submitted)
	}
}

// TestLoopFlowRejectsWithAReason: r opens the reason, the typed words (including
// the letters a and q) are the reason, Enter sends it.
func TestLoopFlowRejectsWithAReason(t *testing.T) {
	drv := &answeringDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	then := []scheduledEvent{{150 * time.Millisecond, keyEvent('r')}}
	for _, r := range "no, quick fix" {
		then = append(then, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	then = append(then, scheduledEvent{20 * time.Millisecond, enterEvent()})
	out := runFlowAnswer(t, drv, flowAnswerScript(then...))
	if len(drv.got) != 1 || drv.got[0].kind != "reject" || drv.got[0].reason != "no, quick fix" || drv.got[0].item != "inb-7" {
		t.Fatalf("the driver was asked %+v", drv.got)
	}
	for _, want := range []string{"Why reject it?", "✓ rejected"} {
		if !strings.Contains(out, want) {
			t.Errorf("no frame showed %q", want)
		}
	}
}

// TestLoopFlowShowsWhyAnAnswerFailed: a refusal from the core stays on the screen
// as one sentence and the person can try again.
func TestLoopFlowShowsWhyAnAnswerFailed(t *testing.T) {
	drv := &answeringDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, fail: errors.New("another process is driving this run")}
	out := runFlowAnswer(t, drv, flowAnswerScript(
		scheduledEvent{150 * time.Millisecond, keyEvent('a')},
		scheduledEvent{200 * time.Millisecond, keyEvent('a')}))
	if !strings.Contains(out, "✗ another process is driving this run") {
		t.Errorf("the failure was not shown")
	}
	if len(drv.got) != 2 {
		t.Errorf("after a refusal the person can answer again, got %d answers", len(drv.got))
	}
}
