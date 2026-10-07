package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func TestEffortMenuOpensOnlyWithTheSpace(t *testing.T) {
	for in, want := range map[string]bool{"/effort ": true, "/effort hi": true, "/effort": false, "/model ": false, "": false} {
		if _, open := effortMenuOpen(in); open != want {
			t.Errorf("effortMenuOpen(%q) = %v, want %v", in, open, want)
		}
	}
}

func TestEffortMenuListsEveryLevelAndMarksTheCurrent(t *testing.T) {
	var mm modelMenu
	mm.setRows(effortMenuData([]string{"minimal", "low", "medium", "high"}, true, "high"))
	rows, sel := mm.view("")
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	if got := strings.Join(names, ","); got != "minimal,low,medium,high" {
		t.Errorf("levels = %s", got)
	}
	if rows[sel].Name != "high" || !rows[sel].Current {
		t.Errorf("the highlight should start on the level in use, got %+v", rows[sel])
	}
	if got := len(filterModels(mm.models, "lo")); got != 1 {
		t.Errorf("typing `lo` should leave only `low`, got %d rows", got)
	}
}

func TestEffortCommandFromTheCommandMenu(t *testing.T) {
	for _, in := range []string{"/effort", "/eff"} {
		if !effortCommand(in, 0, "All") {
			t.Errorf("%q should open the effort menu", in)
		}
	}
	for _, in := range []string{"/model", "/effort high", "hello", "/zzz"} {
		if effortCommand(in, 0, "All") {
			t.Errorf("%q must not open the effort menu", in)
		}
	}
	if modelCommand("/effort", 0, "All") {
		t.Error("/effort must not open the model menu")
	}
}

func TestEffortMenuRendersLikeTheModelMenu(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	var mm modelMenu
	mm.setRows(effortMenuData(nil, false, "low"))
	rows, sel := mm.view("")
	st := fold.State{UserInput: "/effort ", ModelActive: true, ModelMatches: rows, ModelSelected: sel}
	r := engine.Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, st).Plain()
	for _, want := range []string{"  low", "a little thinking", "  medium", "  high", "✓"} {
		if !strings.Contains(got, want) {
			t.Errorf("the effort menu is missing %q:\n%s", want, got)
		}
	}
}

func TestEffortCommandIsInTheMenuUnderModel(t *testing.T) {
	found := false
	for _, m := range fold.FilterSlashCategory("eff", fold.SlashAll) {
		if m.Name == "effort" && m.Category == "Model" {
			found = true
		}
	}
	if !found {
		t.Error("/effort should be listed in the Model category")
	}
}

// effortChat records the thinking level each chat.send asked for.
type effortChat struct {
	mu      sync.Mutex
	efforts []string
}

func (c *effortChat) Hello() *driver.Hello {
	return &driver.Hello{Implemented: []string{"chat.send"}}
}

func (c *effortChat) SubmitChatSend(_ context.Context, p driver.ChatSendParams) (*driver.ChatSendResult, error) {
	c.mu.Lock()
	c.efforts = append(c.efforts, p.Effort)
	c.mu.Unlock()
	return &driver.ChatSendResult{Text: "ok", Model: "m", Provider: "p"}, nil
}

func TestChatCarriesTheThinkingLevelAndClearKeepsIt(t *testing.T) {
	core := &effortChat{}
	out := make(chan fold.Event, 64)
	c := newChatSession(core, out)
	send := func(text string) {
		t.Helper()
		if err := c.send(context.Background(), text); err != nil {
			t.Fatal(err)
		}
		drain(out, "agent.turn_done", 2*time.Second)
	}
	send("one") // nothing chosen: nothing sent
	c.setEffort("high")
	send("two")
	c.reset() // /clear
	send("three")
	c.setEffort("")
	send("four")
	core.mu.Lock()
	defer core.mu.Unlock()
	if got := strings.Join(core.efforts, ","); got != ",high,high," {
		t.Errorf("effort per request = %q, want \",high,high,\" (none, high, kept across /clear, cleared)", got)
	}
}

func TestEffortMenuShowsOnlyWhatTheCoreSaysTheModelTakes(t *testing.T) {
	names := func(levels []string, known bool) string {
		var out []string
		for _, r := range effortMenuData(levels, known, "") {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}
	for name, c := range map[string]struct {
		levels []string
		known  bool
		want   string
	}{
		"deepseek v4: switch, no medium, has max": {[]string{"off", "low", "high", "max"}, true, "off,low,high,max"},
		"switch only":               {[]string{"off", "on"}, true, "off,on"},
		"claude with max and xhigh": {[]string{"low", "medium", "high", "xhigh", "max"}, true, "low,medium,high,xhigh,max"},
		"core did not say":          {nil, false, "low,medium,high"},
		"model with no levels":      {[]string{}, true, "no levels"},
	} {
		if got := names(c.levels, c.known); got != c.want {
			t.Errorf("%s: menu = %q, want %q", name, got, c.want)
		}
	}
	if !effortAllowed([]string{"off", "low", "high", "max"}, true, "max") {
		t.Error("max is on this model's list")
	}
	if effortAllowed([]string{"off", "low", "high", "max"}, true, "medium") {
		t.Error("medium is not on this model's list")
	}
	if effortAllowed([]string{}, true, "high") {
		t.Error("a model with no levels takes none")
	}
	if !effortAllowed(nil, false, "medium") {
		t.Error("with no word from the core the common three are allowed")
	}
}

func TestEffortLabelReadsTheSwitchWords(t *testing.T) {
	for in, want := range map[string]string{"off": "thinking off", "on": "thinking on", "max": "max", "": ""} {
		if got := effortLabel(in); got != want {
			t.Errorf("effortLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHubDataEffortsOfFollowsTheModelRow(t *testing.T) {
	d := hubData{models: []driver.ModelRow{
		{Provider: "a", ID: "m1", Efforts: []string{"off", "on"}},
		{Provider: "a", ID: "m2", Efforts: []string{}},
		{Provider: "a", ID: "old"}, // an older core sends no efforts
	}}
	if l, known := d.effortsOf("a/m1"); !known || strings.Join(l, ",") != "off,on" {
		t.Errorf("m1 = %v %v", l, known)
	}
	if l, known := d.effortsOf("a/m2"); !known || len(l) != 0 {
		t.Errorf("m2 = %v %v", l, known)
	}
	if _, known := d.effortsOf("a/old"); known {
		t.Error("a model row without efforts must read as not said")
	}
	if _, known := d.effortsOf("a/ghost"); known {
		t.Error("an unknown model must read as not said")
	}
}

func TestEffortAfterPickTogglesTheLevelInUse(t *testing.T) {
	if got := effortAfterPick("", "high"); got != "high" {
		t.Errorf("first pick = %q", got)
	}
	if got := effortAfterPick("high", "high"); got != "" {
		t.Errorf("picking the level in use should clear it, got %q", got)
	}
	if got := effortAfterPick("high", "low"); got != "low" {
		t.Errorf("changing level = %q", got)
	}
}

// effortDriver is a testDriver that records the thinking level the loop sets.
type effortDriver struct {
	testDriver
	mu     sync.Mutex
	levels []string
}

func (d *effortDriver) SetEffort(l string) {
	d.mu.Lock()
	d.levels = append(d.levels, l)
	d.mu.Unlock()
}

func TestLoopEffortMenuEndToEnd(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	down := scheduledEvent{30 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyDown}}}
	script := append([]scheduledEvent{}, keys("/effort ")...)
	// With no model chosen the menu lists low, medium, high; `low` is highlighted
	// first, so two Downs reach `high`.
	script = append(script, down, down, scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, keys("hello")...)
	script = append(script, scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 1 || drv.levels[0] != "high" {
		t.Errorf("levels set = %v, want [high]", drv.levels)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the effort line reached the chat as %q", drv.submitted)
	}
	if out := stripANSI(tty.output()); !strings.Contains(out, "high") {
		t.Errorf("the status bar never showed the new level:\n%s", out)
	}
}
