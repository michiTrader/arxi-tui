package main

import (
	"context"
	"strings"
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
	for _, want := range []string{"build ●", "(submitted: frontend)", "⚠ backend waits for approval: bash", "arxi inbox approve inb-7"} {
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
	if f.key(term.Key{Type: term.KeyDown}) || f.sel != 1 {
		t.Fatalf("down: sel = %d", f.sel)
	}
	f.publish(&st)
	f.key(term.Key{Type: term.KeyDown}) // past the last row
	f.publish(&st)
	if f.sel != 1 {
		t.Fatalf("the highlight must stop at the last member, sel = %d", f.sel)
	}
	if f.key(term.Key{Type: term.KeyRunes, Runes: []rune{'x'}}) {
		t.Error("an ordinary letter must not close the screen")
	}
	for _, k := range []term.Key{{Type: term.KeyEscape}, {Type: term.KeyEnter}, {Type: term.KeyRunes, Runes: []rune{'q'}}} {
		if !f.key(k) {
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
