package main

import (
	"context"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// /pick writes one group's choice, clears it with the group alone, and refuses misuse by
// name.
// Consequence: a tab press that writes the wrong slot, or a typo that is taken silently, leaves
// the user on a tab they did not choose.
// Remedy: one parser for the typed line and the pressed button (applyPick).
func TestApplyPick(t *testing.T) {
	picks := map[string]string{}
	if m, why := applyPick(picks, "/pick tabs one"); !m || why != "" || picks["tabs"] != "one" {
		t.Fatalf("/pick tabs one: matched=%v why=%q picks=%v", m, why, picks)
	}
	if m, why := applyPick(picks, "pick side log"); !m || why != "" || picks["side"] != "log" || picks["tabs"] != "one" {
		t.Fatalf("the bare form (what on_press gives) must work and leave the other group alone: %v %q %v", m, why, picks)
	}
	if m, why := applyPick(picks, "/pick tabs two"); !m || why != "" || picks["tabs"] != "two" || picks["side"] != "log" {
		t.Fatalf("changing a group must not touch another: %v", picks)
	}
	if m, why := applyPick(picks, "/pick tabs"); !m || why != "" {
		t.Fatalf("/pick tabs: matched=%v why=%q", m, why)
	} else if _, still := picks["tabs"]; still || picks["side"] != "log" {
		t.Errorf("/pick <group> must clear only that group: %v", picks)
	}
	for _, line := range []string{"/pick", "/pick a b c", "/pick Tabs one", "/pick tabs On.e"} {
		before := len(picks)
		m, why := applyPick(picks, line)
		if !m || why == "" || !strings.Contains(why, "/pick") {
			t.Errorf("%q: matched=%v why=%q, want a refusal that names /pick", line, m, why)
		}
		if len(picks) != before {
			t.Errorf("%q changed the choices: %v", line, picks)
		}
	}
	if m, _ := applyPick(picks, "/picking x y"); m {
		t.Errorf("/picking is not /pick")
	}
	if m, _ := applyPick(picks, "hello"); m {
		t.Errorf("plain text was taken as /pick")
	}
	if m, why := applyPick(nil, "/pick a b"); !m || why == "" {
		t.Errorf("with no choices to write the refusal must say so: %q", why)
	}
}

// A pressed cmd:/pick button writes the group's choice itself, with no round trip through the
// command queue and no line sent to the model.
// Consequence: without the direct write a press depends on the queue being wired (a surface
// with no queue refuses it), and a tab would work on one surface and not another.
// Remedy: dispatchPress runs applyPick on the state's choices before it falls to the queue.
func TestPressCmdPickWritesTheGroupWithoutTheQueue(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"go","type":"button","text":"tab","on_press":"cmd:/pick tabs two"}
	]}}`)
	notice := ""
	st := fold.State{UIPick: map[string]string{"other": "keep"}}
	drv := &testDriver{evCh: make(chan fold.Event, 4)}
	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "", "go", &doc, st, &notice, map[string]bool{}, nil, nil, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a cmd:/pick button was not handled")
	}
	if st.UIPick["tabs"] != "two" || st.UIPick["other"] != "keep" {
		t.Errorf("choices = %v, want tabs=two and the other group untouched", st.UIPick)
	}
	if notice != "" {
		t.Errorf("a plain pick left a notice: %q (a surface with no queue must not refuse it)", notice)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the press submitted %v to the model", drv.submitted)
	}
}
