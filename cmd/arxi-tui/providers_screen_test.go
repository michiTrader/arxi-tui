package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func twoModels() []fold.ProviderModel {
	return []fold.ProviderModel{
		{Provider: "moonshot", ID: "kimi-k2", Enabled: true},
		{Provider: "deepseek", ID: "deepseek-chat", Enabled: false},
	}
}

// TestTheEmbeddedProvidersSceneMatchesTheFixture holds the screen the binary opens
// equal to the document the engine goldens render. If they drifted, the goldens
// would keep passing while users saw a different screen.
func TestTheEmbeddedProvidersSceneMatchesTheFixture(t *testing.T) {
	data, err := os.ReadFile("../../testdata/PROVIDERS.json")
	if err != nil {
		t.Fatalf("read PROVIDERS.json: %v", err)
	}
	if strings.TrimSpace(string(data)) != strings.TrimSpace(factoryProviders) {
		t.Fatalf("factoryProviders differs from testdata/PROVIDERS.json.\n" +
			"consequence: the golden the tests pin is not the screen users open.\n" +
			"remedy: copy testdata/PROVIDERS.json into the factoryProviders constant (or the reverse).")
	}
	if _, err := loadProvidersScene(); err != nil {
		t.Fatalf("the embedded providers scene does not load: %v", err)
	}
}

func TestProvidersScreenMoveWrapsAndClamps(t *testing.T) {
	s := &providersScreen{}
	s.move(1) // empty: must not panic or go negative
	if s.selected != 0 {
		t.Fatalf("move on an empty list set selected=%d, want 0", s.selected)
	}
	s.setModels(twoModels())
	s.move(-1)
	if s.selected != 1 {
		t.Errorf("Up from the first row landed on %d, want 1 (wrap to the last)", s.selected)
	}
	s.move(1)
	if s.selected != 0 {
		t.Errorf("Down from the last row landed on %d, want 0 (wrap to the first)", s.selected)
	}
	s.selected = 1
	s.setModels(twoModels()[:1])
	if s.selected != 0 {
		t.Errorf("a list that shrank left selected=%d; Enter would toggle a row that is not there", s.selected)
	}
}

func TestProvidersToggleFollowsRowState(t *testing.T) {
	s := &providersScreen{}
	if _, ok := s.toggleAction(); ok {
		t.Fatal("toggleAction on an empty list returned an action; there is no row to act on")
	}
	s.setModels(twoModels())
	act, ok := s.toggleAction()
	if !ok || act.Verb != "model.disable" || act.Ref != "moonshot/kimi-k2" || !act.Refresh {
		t.Errorf("Enter on an enabled row = %+v, want model.disable moonshot/kimi-k2 with Refresh", act)
	}
	s.move(1)
	act, ok = s.toggleAction()
	if !ok || act.Verb != "model.enable" || act.Ref != "deepseek/deepseek-chat" || !act.Refresh {
		t.Errorf("Enter on a disabled row = %+v, want model.enable deepseek/deepseek-chat with Refresh", act)
	}
}

func TestRouteProvidersKey(t *testing.T) {
	s := &providersScreen{}
	s.setModels(twoModels())

	if r := routeProvidersKey(s, "", term.Key{Type: term.KeyEscape}); !r.close {
		t.Error("Esc did not close the screen")
	}
	routeProvidersKey(s, "", term.Key{Type: term.KeyDown})
	if s.selected != 1 {
		t.Errorf("Down left selected=%d, want 1", s.selected)
	}
	r := routeProvidersKey(s, "", term.Key{Type: term.KeyEnter})
	if r.dispatch == nil || r.dispatch.Verb != "model.enable" {
		t.Errorf("Enter on an empty line = %+v, want a model.enable dispatch", r)
	}
	r = routeProvidersKey(s, "/provider add acme --api-key-env ACME_KEY", term.Key{Type: term.KeyEnter})
	if r.dispatch == nil || r.dispatch.Verb != "provider.add" || !r.dispatch.Refresh {
		t.Errorf("typed /provider add = %+v, want provider.add with Refresh so the new rows show", r)
	}
	r = routeProvidersKey(s, "/provider add", term.Key{Type: term.KeyEnter})
	if r.dispatch != nil || r.notice == "" {
		t.Errorf("a malformed typed command = %+v, want a notice and no dispatch", r)
	}
	r = routeProvidersKey(s, "hello there", term.Key{Type: term.KeyEnter})
	if r.dispatch != nil || r.notice == "" {
		t.Errorf("plain chat text on the providers screen = %+v; it must be refused with the grammar, "+
			"not sent to an agent whose transcript is hidden", r)
	}
	if r := routeProvidersKey(s, "", term.Key{Type: term.KeyRunes, Runes: []rune{'a'}}); !r.edited {
		t.Error("a printable key was not reported as input editing")
	}
	empty := &providersScreen{}
	if r := routeProvidersKey(empty, "", term.Key{Type: term.KeyEnter}); r.notice != providersEmptyNotice {
		t.Errorf("Enter on an empty list gave notice %q, want the empty-list guidance", r.notice)
	}
}

func TestMenuHostCommandOnlyClaimsTheScreens(t *testing.T) {
	// Filtered to "provider" the only row is provider.
	if line, ok := menuHostCommand("/provider", 0); !ok || line != "/provider" {
		t.Errorf("menuHostCommand(/provider) = %q,%v; want /provider,true", line, ok)
	}
	if line, ok := menuHostCommand("/mod", 0); !ok || line != "/model" {
		t.Errorf("menuHostCommand(/mod) = %q,%v; want /model,true", line, ok)
	}
	if line, ok := menuHostCommand("/help", 0); ok {
		t.Errorf("menuHostCommand(/help) claimed %q; help still follows the Phase 0 contract", line)
	}
	if _, ok := menuHostCommand("provider", 0); ok {
		t.Error("menuHostCommand claimed a line that is not a slash command")
	}
}

func TestRunProviderWork(t *testing.T) {
	rows := &driver.ModelListResult{Models: []driver.ModelRow{{Provider: "a", ID: "m", Enabled: true}}}

	fake := &fakeProviderManager{listResult: rows}
	out := runProviderWork(context.Background(), fake, providerAction{Verb: "model.list", OpenScreen: true})
	if !out.open || !out.hasModels || len(out.models) != 1 || out.notice != "" {
		t.Errorf("open with rows = %+v, want open, 1 row, no notice", out)
	}

	fake = &fakeProviderManager{listResult: &driver.ModelListResult{}}
	out = runProviderWork(context.Background(), fake, providerAction{Verb: "model.list", OpenScreen: true})
	if !out.open || out.notice != providersEmptyNotice {
		t.Errorf("open with no rows = %+v, want open with the empty-list guidance", out)
	}

	fake = &fakeProviderManager{listErr: context.DeadlineExceeded}
	out = runProviderWork(context.Background(), fake, providerAction{Verb: "model.list", OpenScreen: true})
	if out.open || out.hasModels || out.notice == "" {
		t.Errorf("open with a failed read = %+v; the screen must not open on an error", out)
	}

	fake = &fakeProviderManager{
		enResult:   &driver.ModelEnableResult{Provider: "a", Model: "m", Enabled: false, Changed: true},
		listResult: &driver.ModelListResult{Models: []driver.ModelRow{{Provider: "a", ID: "m", Enabled: false}}},
	}
	out = runProviderWork(context.Background(), fake,
		providerAction{Verb: "model.disable", Ref: "a/m", Refresh: true})
	if fake.enRef != "a/m" || fake.enOn {
		t.Errorf("worker sent ref=%q on=%v, want a/m on=false", fake.enRef, fake.enOn)
	}
	if !out.hasModels || out.models[0].Enabled || out.open {
		t.Errorf("toggle outcome = %+v, want a refreshed list with the row off and no re-open", out)
	}

	fake.listErr = context.DeadlineExceeded
	out = runProviderWork(context.Background(), fake,
		providerAction{Verb: "model.disable", Ref: "a/m", Refresh: true})
	if out.hasModels || !strings.Contains(out.notice, "disabled") {
		t.Errorf("toggle with a failed re-read = %+v; the success notice must survive and no stale list be applied", out)
	}
}

// --- loop level: the user-visible contract ---

func runScript(t *testing.T, drv Driver, script []scheduledEvent) string {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	tty := newFakeTTY(100, 30, script)
	evCh := make(chan fold.Event, 64)
	switch d := drv.(type) {
	case *testDriver:
		d.evCh = evCh
	case *providerLoopDriver:
		d.evCh = evCh
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
	return tty.output()
}

// providerLoopDriver is a Driver that also manages providers, with a stateful list
// so a toggle is visible in the next read.
type providerLoopDriver struct {
	fakeProviderManager
	evCh      chan fold.Event
	submitted []string
	rows      []driver.ModelRow
}

func (d *providerLoopDriver) SubmitPrompt(_ context.Context, text string) error {
	d.submitted = append(d.submitted, text)
	return nil
}
func (d *providerLoopDriver) SubmitModelList(context.Context) (*driver.ModelListResult, error) {
	return &driver.ModelListResult{Models: append([]driver.ModelRow(nil), d.rows...)}, nil
}
func (d *providerLoopDriver) SubmitModelEnable(_ context.Context, ref string, on bool) (*driver.ModelEnableResult, error) {
	d.enRef, d.enOn = ref, on
	for i := range d.rows {
		if d.rows[i].Provider+"/"+d.rows[i].ID == ref {
			d.rows[i].Enabled = on
			return &driver.ModelEnableResult{Provider: d.rows[i].Provider, Model: d.rows[i].ID, Enabled: on, Changed: true}, nil
		}
	}
	return nil, context.DeadlineExceeded
}

func newProviderLoopDriver() *providerLoopDriver {
	d := &providerLoopDriver{rows: []driver.ModelRow{
		{Provider: "moonshot", ID: "kimi-k2", Enabled: true},
		{Provider: "deepseek", ID: "deepseek-chat", Enabled: false},
	}}
	d.hello = allProviderVerbs()
	return d
}

func typeLine(s string) []scheduledEvent {
	var out []scheduledEvent
	for _, r := range s {
		out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	return out
}

func quit() []scheduledEvent {
	return []scheduledEvent{
		{150 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
}

// TestTypedProviderOpensTheScreenAndNeverReachesTheChat is the user's bug: typing
// /provider and pressing Enter used to put the word in the chat. It must open the
// screen with the real rows and submit nothing.
func TestTypedProviderOpensTheScreenAndNeverReachesTheChat(t *testing.T) {
	drv := newProviderLoopDriver()
	script := append(typeLine("/provider"), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{80 * time.Millisecond, keyEvent('x')})
	script = append(script, quit()...)
	out := runScript(t, drv, script)

	if len(drv.submitted) != 0 {
		t.Errorf("/provider reached the chat as %q; it is a host command", drv.submitted)
	}
	for _, want := range []string{"· providers", "kimi-k2", "deepseek-chat"} {
		if !strings.Contains(out, want) {
			t.Errorf("the providers screen never showed %q; the model.list round-trip did not reach the screen", want)
		}
	}
}

// TestSlashMenuPickOpensTheProvidersScreen covers the second route to the same
// screen: highlight the row in the menu and press Enter.
func TestSlashMenuPickOpensTheProvidersScreen(t *testing.T) {
	drv := newProviderLoopDriver()
	// Only "/prov" is typed: Enter then runs the highlighted menu row, not a
	// fully typed command, which is the route that used to send the word.
	script := append(typeLine("/prov"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, quit()...)
	out := runScript(t, drv, script)
	if len(drv.submitted) != 0 {
		t.Errorf("a menu pick of provider reached the chat as %q", drv.submitted)
	}
	if !strings.Contains(out, "kimi-k2") {
		t.Error("picking provider from the menu did not open the screen with the rows")
	}
}

// TestEnterTogglesTheHighlightedModelAndTheRowFlips drives the whole point-2 path:
// open, Down, Enter, and the refreshed list shows the other state.
func TestEnterTogglesTheHighlightedModelAndTheRowFlips(t *testing.T) {
	drv := newProviderLoopDriver()
	script := append(typeLine("/provider"), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script,
		scheduledEvent{100 * time.Millisecond, arrowEvent(term.KeyDown)},
		scheduledEvent{20 * time.Millisecond, enterEvent()},
	)
	script = append(script, quit()...)
	out := runScript(t, drv, script)

	if drv.enRef != "deepseek/deepseek-chat" || !drv.enOn {
		t.Fatalf("Enter on the second row sent ref=%q on=%v, want deepseek/deepseek-chat on=true", drv.enRef, drv.enOn)
	}
	if !drv.rows[1].Enabled {
		t.Error("the core's state did not change")
	}
	// After the refresh both rows are enabled, so the last frame carries two
	// [ disable ] buttons and no [ enable ] one.
	frames := strings.Split(out, frameBegin)
	last := stripANSI(frames[len(frames)-1])
	if strings.Count(last, "[ disable ]") != 2 || strings.Contains(last, "[ enable ]") {
		t.Errorf("the screen did not refresh after the toggle; last frame:\n%s", last)
	}
}

// TestEscClosesTheProvidersScreenAndCtrlCStillExits proves the screen does not
// capture the panic gesture (invariant 6) and that Esc gives the chat back.
func TestEscClosesTheProvidersScreenAndCtrlCStillExits(t *testing.T) {
	drv := newProviderLoopDriver()
	script := append(typeLine("/provider"), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{100 * time.Millisecond, arrowEvent(term.KeyEscape)})
	script = append(script, quit()...)
	out := runScript(t, drv, script) // returning at all proves Ctrl-C twice exits
	frames := strings.Split(out, frameBegin)
	if strings.Contains(stripANSI(frames[len(frames)-1]), "· providers") {
		t.Error("Esc did not close the providers screen")
	}
}

// TestProviderWithoutALiveCoreOpensAnExplainedScreen: the mock driver cannot manage
// providers; the screen still opens and says why, rather than a refusal on a chat
// line that reads as "it sent the word".
func TestProviderWithoutALiveCoreOpensAnExplainedScreen(t *testing.T) {
	drv := &testDriver{}
	script := append(typeLine("/provider"), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, quit()...)
	out := runScript(t, drv, script)
	if len(drv.submitted) != 0 {
		t.Errorf("/provider reached the chat as %q", drv.submitted)
	}
	if !strings.Contains(out, "ARXI_BIN") || !strings.Contains(out, "· providers") {
		t.Error("without a core the screen must open and name ARXI_BIN as the fix")
	}
}
