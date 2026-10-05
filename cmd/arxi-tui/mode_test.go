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

func TestModeTableUsesTheCoreSpellingAndLeastPrivilegeFirst(t *testing.T) {
	want := map[string][3]string{
		"ask":         {"allow", "ask", "ask"},
		"auto":        {"allow", "allow", "ask"},
		"plan":        {"allow", "deny", "deny"},
		"full access": {"allow", "allow", "allow"},
	}
	for name, p := range want {
		m, ok := modeByName(name)
		if !ok {
			t.Fatalf("mode %q missing", name)
		}
		got := [3]string{m.policy(classRead), m.policy(classEdit), m.policy(classRun)}
		if got != p {
			t.Errorf("%s policies = %v, want %v", name, got, p)
		}
	}
	if agentModes[0].name != defaultMode {
		t.Errorf("the first mode must be the default, got %q", agentModes[0].name)
	}
	m, _ := modeByName(" Full Access ")
	if m.name != "full access" {
		t.Error("lookup should ignore case and space")
	}
	if m.policy(toolClass("teleport")) != policyDeny {
		t.Error("an unclassified tool class must be denied")
	}
}

func TestNextModeWalksAndWraps(t *testing.T) {
	var seen []string
	m := defaultMode
	for range agentModes {
		m = nextMode(m)
		seen = append(seen, m)
	}
	if got := strings.Join(seen, ","); got != "auto,plan,full access,ask" {
		t.Errorf("walk = %s", got)
	}
	if nextMode("nonsense") != defaultMode {
		t.Error("an unknown mode starts over from the default")
	}
}

func TestModeMenuAndCommand(t *testing.T) {
	if _, open := modeMenuOpen("/mode "); !open {
		t.Error("`/mode ` should open the menu")
	}
	if _, open := modeMenuOpen("/mode"); open {
		t.Error("`/mode` without the space must not")
	}
	if !modeCommand("/mode", 0, "All") || modeCommand("/model", 0, "All") || modelCommand("/mode", 0, "All") {
		t.Error("/mode and /model must stay distinct")
	}
	rows := modeMenuData("auto")
	if len(rows) != len(agentModes) || !rows[1].Current || rows[0].Current {
		t.Errorf("rows = %+v", rows)
	}
	found := false
	for _, m := range fold.FilterSlashCategory("mode", fold.SlashAll) {
		found = found || m.Name == "mode"
	}
	if !found {
		t.Error("/mode should be listed in the slash menu")
	}
}

func TestLoopModeShowsInTheBarAndShiftTabCyclesIt(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	shiftTab := scheduledEvent{20 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyTab, Mod: term.ModShift}}}
	script := []scheduledEvent{
		{300 * time.Millisecond, keyEvent('x')}, // let the first frame land
		{5 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}},
		shiftTab,
		{300 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')},
	}
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	out := stripANSI(tty.output())
	if !strings.Contains(out, "ask") {
		t.Errorf("the bar never showed the default mode:\n%s", out)
	}
	if !strings.Contains(out, "auto") {
		t.Errorf("Shift+Tab never moved to the next mode:\n%s", out)
	}
	if strings.Contains(out, "idle") {
		t.Errorf("the bar still says idle:\n%s", out)
	}
}

func TestCtrlOIsTheExpandKeyAndNothingElseIs(t *testing.T) {
	ctrl := func(r rune) term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune{r}, Mod: term.ModCtrl} }
	if !isCtrlO(ctrl('o')) {
		t.Error("ctrl+o must expand")
	}
	for _, k := range []term.Key{
		{Type: term.KeyRunes, Runes: []rune{'o'}},
		ctrl('c'), ctrl('p'),
		{Type: term.KeyEnter},
	} {
		if isCtrlO(k) {
			t.Errorf("%+v is not ctrl+o", k)
		}
	}
}
