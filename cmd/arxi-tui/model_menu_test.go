package main

import (
	"context"
	"errors"
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

func sampleModels() hubData {
	return hubData{
		models: []driver.ModelRow{
			{Provider: "tokenharbor", ID: "deepseek-v4.1-flash:free", Enabled: true},
			{Provider: "tokenharbor", ID: "deepseek-v4.1-pro", Enabled: true},
			{Provider: "openai", ID: "gpt-4o", Enabled: true},
			{Provider: "openai", ID: "hidden", Enabled: false},
		},
		def: "openai/gpt-4o",
	}
}

func TestModelMenuOpensOnlyWithTheSpace(t *testing.T) {
	for in, want := range map[string]bool{"/model ": true, "/model deeps": true, "/model": false, "/models ": false, "hello": false, "": false} {
		if _, open := modelMenuOpen(in); open != want {
			t.Errorf("modelMenuOpen(%q) = %v, want %v", in, open, want)
		}
	}
	if f, _ := modelMenuOpen("/model  deeps"); f != "deeps" {
		t.Errorf("filter = %q", f)
	}
}

func TestModelMenuListsEnabledModelsAndMarksTheCurrent(t *testing.T) {
	var mm modelMenu
	mm.setData(sampleModels())
	if len(mm.models) != 3 {
		t.Fatalf("a disabled model leaked into the menu: %+v", mm.models)
	}
	rows, sel := mm.view("")
	if !rows[sel].Current || rows[sel].Ref != "openai/gpt-4o" {
		t.Errorf("the highlight should start on the chat model, got %+v", rows[sel])
	}
}

func TestModelMenuFiltersLiveByEveryWord(t *testing.T) {
	var mm modelMenu
	mm.setData(sampleModels())
	for filter, want := range map[string]int{"": 3, "deeps": 2, "deeps flash": 1, "DEEPS FLASH": 1, "zzz": 0, "openai": 1} {
		if got := len(filterModels(mm.models, filter)); got != want {
			t.Errorf("filter %q -> %d rows, want %d", filter, got, want)
		}
	}
}

func TestModelMenuKeysNavigateWrapAndPick(t *testing.T) {
	var mm modelMenu
	mm.setData(sampleModels())
	mm.sel = 0
	in, caret := "/model ", 7

	_, _, _ = modelMenuKey(&mm, in, caret, term.Key{Type: term.KeyUp})
	if mm.sel != 2 {
		t.Errorf("Up from the first row should wrap to the last, sel=%d", mm.sel)
	}
	_, _, _ = modelMenuKey(&mm, in, caret, term.Key{Type: term.KeyDown})
	if mm.sel != 0 {
		t.Errorf("Down from the last row should wrap to the first, sel=%d", mm.sel)
	}

	// Typing filters and puts the highlight back on the first row.
	mm.sel = 2
	in, caret, _ = modelMenuKey(&mm, in, caret, term.Key{Type: term.KeyRunes, Runes: []rune{'d'}})
	if in != "/model d" || mm.sel != 0 {
		t.Errorf("typing: in=%q sel=%d", in, mm.sel)
	}

	// Enter picks the highlighted model of the filtered list and clears the line.
	next, c, pick := modelMenuKey(&mm, in, caret, term.Key{Type: term.KeyEnter})
	if pick != "tokenharbor/deepseek-v4.1-flash:free" || next != "" || c != 0 {
		t.Errorf("enter: pick=%q next=%q caret=%d", pick, next, c)
	}

	// Esc closes without picking.
	next, _, pick = modelMenuKey(&mm, "/model d", 8, term.Key{Type: term.KeyEscape})
	if next != "" || pick != "" {
		t.Errorf("esc: next=%q pick=%q", next, pick)
	}

	// Enter on an empty filter result does nothing.
	next, _, pick = modelMenuKey(&mm, "/model zzz", 10, term.Key{Type: term.KeyEnter})
	if pick != "" || next != "/model zzz" {
		t.Errorf("enter with no rows: next=%q pick=%q", next, pick)
	}

	// Backspacing through the space leaves `/model`, which closes the menu.
	next, _, _ = modelMenuKey(&mm, "/model ", 7, term.Key{Type: term.KeyBackspace})
	if _, open := modelMenuOpen(next); open || next != "/model" {
		t.Errorf("backspace: next=%q", next)
	}
}

func TestModelCommandFromTheCommandMenu(t *testing.T) {
	if !modelCommand("/model", 0, "") || !modelCommand("/mod", 0, "") {
		t.Error("`/model` (typed or the single filtered row) should open the model menu")
	}
	if modelCommand("/model deeps", 0, "") || modelCommand("/help", 0, "") {
		t.Error("only a bare /model opens the model menu from the command line")
	}
}

func TestModelMenuRendersMinimalAndAlignedWithTheInput(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	var mm modelMenu
	mm.setData(sampleModels())
	rows, sel := mm.view("")
	st := fold.State{UserInput: "/model ", ModelActive: true, ModelMatches: rows, ModelSelected: sel}
	r := engine.Renderer{Width: 80, Height: 24}
	got := r.RenderFrame(doc, st).Plain()

	if strings.Contains(got, "Choose the model") || strings.Contains(got, "type to filter") || strings.Contains(got, "→") {
		t.Errorf("the old long text or arrow layout is back:\n%s", got)
	}
	if !strings.Contains(got, "  deepseek-v4.1-flash:free  tokenharbor") || !strings.Contains(got, "  gpt-4o") || !strings.Contains(got, "✓") {
		t.Errorf("rows are not `  name  provider` with a check on the chat model:\n%s", got)
	}
	if strings.Contains(got, "Commands ") {
		t.Errorf("the command menu must not show beside the model menu:\n%s", got)
	}
	styled := r.RenderFrame(doc, st).Styled()
	if !strings.Contains(styled, "«menu.name.selected:gpt-4o»") {
		t.Errorf("the chat model should be the highlighted row:\n%s", styled)
	}
}

// modelCore is a hub core with a few models, recording which one was made default.
type modelCore struct {
	mu     sync.Mutex
	models []driver.ModelRow
	def    string
}

func (c *modelCore) Hello() *driver.Hello {
	return &driver.Hello{Implemented: append([]string{}, hubVerbs...)}
}

func (c *modelCore) SubmitProviderAdd(context.Context, driver.ProviderAddParams) (*driver.ProviderAddResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitProviderUpdate(context.Context, driver.ProviderUpdateParams) (*driver.ProviderAddResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitProviderRemove(context.Context, string) (*driver.ProviderRemoveResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitProviderList(context.Context) (*driver.ProviderListResult, error) {
	return &driver.ProviderListResult{}, nil
}

func (c *modelCore) SubmitModelList(context.Context) (*driver.ModelListResult, error) {
	return &driver.ModelListResult{Models: c.models}, nil
}

func (c *modelCore) SubmitModelAdd(context.Context, driver.ModelAddParams) (*driver.ProviderAddResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitModelEnable(context.Context, string, bool) (*driver.ModelEnableResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitModelDiscover(context.Context, string) (*driver.ModelDiscoverResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitModelRemove(context.Context, string) (*driver.ModelRemoveResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitModelUpdate(context.Context, driver.ModelUpdateParams) (*driver.ModelUpdateResult, error) {
	return nil, errors.New("not used")
}

func (c *modelCore) SubmitModelDefault(_ context.Context, ref string) (*driver.ModelDefaultResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ref != "" {
		c.def = ref
	}
	prov, id, _ := strings.Cut(c.def, "/")
	return &driver.ModelDefaultResult{Default: c.def, Provider: prov, Model: id}, nil
}

func (c *modelCore) current() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.def
}

// hubTestDriver is a testDriver that also exposes a hub core.
type hubTestDriver struct {
	testDriver
	hub hubCore
}

func (d *hubTestDriver) Hub() hubCore { return d.hub }

func TestLoopModelMenuEndToEnd(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	core := &modelCore{def: "openai/gpt-4o"}
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, hub: core}
	core.models = sampleModels().models

	keys := func(s string) []scheduledEvent {
		var out []scheduledEvent
		for _, r := range s {
			out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
		}
		return out
	}
	script := append([]scheduledEvent{}, keys("/model ")...)
	script = append(script, scheduledEvent{300 * time.Millisecond, keyEvent('x')})
	script = append(script, scheduledEvent{5 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}})
	script = append(script, keys("deeps flash")...)
	script = append(script, scheduledEvent{50 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})

	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if core.current() != "tokenharbor/deepseek-v4.1-flash:free" {
		t.Errorf("the chat model is %q; Enter should have switched it", core.current())
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the model line reached the chat as %q", drv.submitted)
	}
	out := stripANSI(tty.output())
	if !strings.Contains(out, "deepseek-v4.1-flash:free") {
		t.Errorf("the status bar never showed the new model:\n%s", out)
	}
}

// TestStatusModelNamesTheChatModelBeforeAnyReply pins the status bar's model: the
// core's default shows from the first frame, a reply's model is the fallback, and
// once the core has answered with none the bar says how to pick one.
func TestStatusModelNamesTheChatModelBeforeAnyReply(t *testing.T) {
	cases := []struct {
		name    string
		def     string
		known   bool
		replied string
		want    string
	}{
		{"still reading, nothing known", "", false, "", ""},
		{"default known, no reply yet", "deepseek/v4-flash", true, "", "deepseek/v4-flash"},
		{"default wins over a reply", "a/b", true, "c/d", "a/b"},
		{"reply is the fallback", "", false, "c/d", "c/d"},
		{"core answered with no model", "", true, "", noModelLabel},
	}
	for _, c := range cases {
		if got := statusModel(c.def, c.known, c.replied); got != c.want {
			t.Errorf("%s: statusModel = %q, want %q", c.name, got, c.want)
		}
	}
}
