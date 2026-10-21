package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/expr"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/keymap"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/verb"
)

// ---- the host side of the keymap ---------------------------------------------------------

// A name the schema lets a condition read but lookup does not answer would swallow a key in
// silence the day somebody writes it in a keymap.
func TestEveryNameOfTheKeymapSchemaIsAnswered(t *testing.T) {
	for name, kind := range keymapSchema {
		v, ok := keymapState{}.lookup(name)
		if !ok {
			t.Errorf("keymapSchema has %q but lookup does not answer it. Consequence: a binding that reads it fails to decide and its key is lost. Remedy: add the case in keymapState.lookup, or take the name out of the schema.", name)
			continue
		}
		if v.K != kind {
			t.Errorf("%q is %v in the schema and lookup answers a %v", name, kind, v.K)
		}
	}
	if _, ok := (keymapState{}).lookup("no.such.name"); ok {
		t.Error("lookup answered a name nobody published")
	}
}

func TestEachFlagOfTheStateReachesItsOwnName(t *testing.T) {
	cases := map[string]keymapState{
		"menu.open":        {menuOpen: true},
		"key.user_bound":   {userBound: true},
		"screen.open":      {screenOpen: true},
		"approval.pending": {approvalPending: true},
		"find.open":        {findOpen: true},
	}
	if len(cases) != len(keymapSchema) {
		t.Fatalf("the schema has %d names and this test %d; add the new name here", len(keymapSchema), len(cases))
	}
	for name, st := range cases {
		for other := range keymapSchema {
			v, _ := st.lookup(other)
			want := other == name
			if v.B != want {
				t.Errorf("with only %s set, %s answers %v. Remedy: wire each flag to its own name in keymapState.lookup.", name, other, v.B)
			}
		}
	}
}

func TestTheFactoryKeymapLoadsAndEveryActionItCarriesRuns(t *testing.T) {
	m, err := factoryKeymap()
	if err != nil {
		t.Fatalf("the factory keymap does not load: %v. Consequence: ctrl+o and ctrl+f do nothing. Remedy: fix internal/keymap/factory.json.", err)
	}
	for _, n := range m.Names() {
		if _, ok := keymapSchema[n]; !ok {
			t.Errorf("factory keymap reads %q which the host does not publish", n)
		}
	}
	for _, b := range m.Bindings() {
		name := strings.TrimPrefix(b.Do, "do:")
		if name == b.Do {
			continue
		}
		verbName, _, _ := strings.Cut(name, " ")
		if _, ok := verb.Find(verbName); !ok {
			t.Errorf("binding %s runs verb %q which is not in the catalogue", b.Key, verbName)
		}
		if _, ok := verbImpl[verbName]; !ok {
			t.Errorf("binding %s runs verb %q which has no implementation", b.Key, verbName)
		}
	}
}

// ctrl+f is the factory keymap's two bindings; this table is what they must decide, row by
// row, so a change of the file that moves a situation shows here with its name.
func TestCtrlFConditionsInEverySituation(t *testing.T) {
	m, err := factoryKeymap()
	if err != nil {
		t.Fatal(err)
	}
	ctrlF := term.Key{Type: term.KeyRunes, Runes: []rune{'f'}, Mod: term.ModCtrl}
	rows := []struct {
		name string
		st   keymapState
		want bool
	}{
		{"idle chat", keymapState{}, true},
		{"find bar open", keymapState{findOpen: true}, true},
		{"slash menu open", keymapState{menuOpen: true}, false},
		{"user bound ctrl+f", keymapState{userBound: true}, false},
		{"a screen is up", keymapState{screenOpen: true}, false},
		{"approval pending", keymapState{approvalPending: true}, false},
		{"bar open but a screen is up", keymapState{findOpen: true, screenOpen: true}, false},
		{"bar open but approval pending", keymapState{findOpen: true, approvalPending: true}, false},
		// A menu can be open while the bar is: the bar keeps the key, it is how the user closes it.
		{"bar open and a menu shows", keymapState{findOpen: true, menuOpen: true}, true},
		{"bar open and the key is user bound", keymapState{findOpen: true, userBound: true}, true},
	}
	for _, r := range rows {
		do, ok, why := resolveKey(m, ctrlF, r.st)
		if why != "" {
			t.Errorf("%s: %s", r.name, why)
		}
		if ok != r.want || (ok && do != "do:find.toggle") {
			t.Errorf("%s: ctrl+f taken=%v do=%q, want taken=%v. Consequence: the find bar opens where it must not, or does not where it must. Remedy: fix the `when` of the ctrl+f bindings in internal/keymap/factory.json.", r.name, ok, do, r.want)
		}
	}
}

func TestCtrlOIsTakenInEverySituation(t *testing.T) {
	m, _ := factoryKeymap()
	ctrlO := term.Key{Type: term.KeyRunes, Runes: []rune{'o'}, Mod: term.ModCtrl}
	for _, st := range []keymapState{{}, {menuOpen: true}, {screenOpen: true}, {approvalPending: true}, {findOpen: true}, {userBound: true}} {
		if do, ok, _ := resolveKey(m, ctrlO, st); !ok || do != "do:view.expand" {
			t.Errorf("with %+v ctrl+o resolved to (%q, %v); it expanded in every situation before and must keep doing so", st, do, ok)
		}
	}
}

// A condition the host cannot answer is a notice and never a swallowed key.
func TestAnUndecidableConditionIsANoticeAndTheKeyIsNotTaken(t *testing.T) {
	sch := expr.Schema{"gone.name": expr.Bool}
	m, err := keymap.Parse([]byte(`{"version":1,"bindings":[{"key":"ctrl+g","when":"gone.name","do":"do:view.expand"}]}`), sch, keymapActionCheck)
	if err != nil {
		t.Fatal(err)
	}
	k := term.Key{Type: term.KeyRunes, Runes: []rune{'g'}, Mod: term.ModCtrl}
	do, ok, why := resolveKey(m, k, keymapState{})
	if ok || do != "" || !strings.Contains(why, "could not be decided") {
		t.Fatalf("got (%q, %v, %q). Consequence: a key whose condition is broken either does something unasked or vanishes without a word. Remedy: return the reason as a notice.", do, ok, why)
	}
}

func TestAKeymapMayNotCarryWhatOnlyATypedLineMayStart(t *testing.T) {
	// A key is pressed, not typed into the line: a Typed verb behind one would run without
	// the person having written it.
	n := 0
	for _, v := range verb.All() {
		if v.Protection != verb.Typed {
			continue
		}
		n++
		do := "do:" + v.Example
		if err := keymapActionCheck(do); err == nil {
			t.Errorf("a key may carry %q, a verb that only a typed line may start. Consequence: a pressed key (or a pasted keymap) starts it. Remedy: keep Typed verbs out of keymapActionCheck.", do)
		}
	}
	if n == 0 {
		t.Error("premise: the catalogue has a Typed verb (shell.start); this test checks nothing without one")
	}
}

func TestTheFactoryKeymapFileIsEmbeddedAsWritten(t *testing.T) {
	disk, err := os.ReadFile("../../internal/keymap/factory.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != string(keymap.FactoryJSON) {
		t.Error("the embedded keymap differs from the file; rebuild")
	}
}

// The data keymap cannot take a key the contract does not list.
func TestEveryKeyOfTheFactoryKeymapHasARowInDocsKeys(t *testing.T) {
	m, err := factoryKeymap()
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, line := range contractSection(t, "keys-contract") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		for _, k := range cellKeys(strings.Split(line, "|")[1]) {
			listed[k] = true
		}
	}
	for _, k := range m.Keys() {
		if !listed[k] {
			t.Errorf("internal/keymap/factory.json takes %s and docs/KEYS.md has no row for it. Consequence: a factory key exists that the contract and /keys do not show. Remedy: decide it in docs/KEYS.md (section 4) and add the row in keys_menu.go.", k)
		}
	}
}

// ---- the keys in the real loop -------------------------------------------------------------

// approvingDriver is a driver with a change waiting for y/n.
type approvingDriver struct{ testDriver }

func (d *approvingDriver) PendingApproval() bool { return true }
func (d *approvingDriver) Decide(bool) bool      { return false }

const sixRows = "row one\nrow two\nrow three\nrow four\nrow five\nrow six"

// keyLoop runs the real loop, on the built-in scene, with a six-row message already in the
// chat (the transcript cuts it to three rows until it is expanded) and plays keys.
func keyLoop(t *testing.T, drv Driver, evCh chan fold.Event, keys []scheduledEvent) []string {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	evCh <- fold.Event{Type: "run.prompt", Seq: 3000, Actor: "assistant", Payload: map[string]any{"text": sixRows}}
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, keys)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	tty.mu.Lock()
	defer tty.mu.Unlock()
	out := make([]string, len(tty.frames))
	for i, f := range tty.frames {
		out[i] = stripANSI(f)
	}
	return out
}

func lastWith(frames []string, piece string) int {
	for i := len(frames) - 1; i >= 0; i-- {
		if strings.Contains(frames[i], piece) {
			return i
		}
	}
	return -1
}

func expandScript(extra ...scheduledEvent) []scheduledEvent {
	keys := []scheduledEvent{{400 * time.Millisecond, ctrlCharEvent('o')}}
	keys = append(keys, extra...)
	return append(keys, scheduledEvent{200 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
}

func TestLoopCtrlOExpandsTheCutMessageAndCutsItAgain(t *testing.T) {
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	frames := keyLoop(t, drv, drv.evCh, expandScript(scheduledEvent{300 * time.Millisecond, ctrlCharEvent('o')}))
	first := -1
	for i, f := range frames {
		if strings.Contains(f, "row three") {
			first = i
			break
		}
	}
	if first < 0 || strings.Contains(frames[first], "row six") {
		t.Fatalf("premise: before ctrl+o the message shows three rows and hides the sixth (first frame with the message: %d)", first)
	}
	opened := lastWith(frames, "row six")
	if opened < 0 {
		t.Fatalf("ctrl+o did not show the cut rows. Consequence: the expand key stops working. Remedy: ctrl+o runs do:view.expand in internal/keymap/factory.json, and the loop must carry it out.\n%s", frames[len(frames)-1])
	}
	if opened == len(frames)-1 {
		t.Errorf("a second ctrl+o did not cut the message again:\n%s", frames[opened])
	}
}

func TestLoopCtrlOStillExpandsWhileAChangeWaitsForAnAnswer(t *testing.T) {
	drv := &approvingDriver{testDriver{evCh: make(chan fold.Event, 64), seq: 1000}}
	frames := keyLoop(t, drv, drv.evCh, expandScript())
	if lastWith(frames, "row six") < 0 {
		t.Fatalf("ctrl+o was swallowed by the question. It expanded before the keymap and must keep doing so; the factory binding has no `when`.\n%s", frames[len(frames)-1])
	}
}

func TestLoopCtrlFDoesNotOpenTheBarWhileAChangeWaits(t *testing.T) {
	drv := &approvingDriver{testDriver{evCh: make(chan fold.Event, 64), seq: 1000}}
	keys := []scheduledEvent{{400 * time.Millisecond, ctrlCharEvent('f')}}
	frames := keyLoop(t, drv, drv.evCh, append(keys, scrollQuit()...))
	for _, f := range frames {
		if hasBar(f) {
			t.Fatalf("the find bar opened over a question that waits for y/n:\n%s", f)
		}
	}
}

// A key is pressed by the user, so it may carry what a shortcut of theirs may carry: an
// address to open and a plugin action. It may not answer the agent's question for them.
func TestAKeyMayOpenAnAddressAndCallAPluginButNotAnswerForTheUser(t *testing.T) {
	for _, ok := range []string{"open:https://example.com/", "ext:tick:refresh", "cmd:/effort max", "say:hello", "do:view.expand"} {
		if err := keymapActionCheck(ok); err != nil {
			t.Errorf("a key may carry %q and the keymap refuses it: %v. Consequence: a keymap that a shortcut of the user's could have said is refused. Remedy: call validateActions with allowExt true.", ok, err)
		}
	}
	for _, bad := range []string{"answer:approve", "do:shell.start npm run dev", "nonsense"} {
		if err := keymapActionCheck(bad); err == nil {
			t.Errorf("a key may not carry %q and the keymap accepts it", bad)
		}
	}
}

// With the automations screen up, ctrl+f must not open the find bar behind it: the bar would
// be waiting, open, when the screen closes.
func TestLoopCtrlFDoesNotOpenTheBarBehindAScreen(t *testing.T) {
	fixedClock(t)
	core, root := newFakeAutoCore(t)
	t.Chdir(root)
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	drv := &autoTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: autoHubCore{fakeAutoCore: core}}
	script := []scheduledEvent{{120 * time.Millisecond, keyEvent('/')}}
	script = append(script, typeKeys("auto")...)
	script = append(script,
		scheduledEvent{40 * time.Millisecond, enterEvent()},
		scheduledEvent{1500 * time.Millisecond, ctrlCharEvent('f')},
		scheduledEvent{300 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEscape}}},
		scheduledEvent{600 * time.Millisecond, ctrlCharEvent('c')},
		scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	tty := newFakeTTY(110, 40, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	tty.mu.Lock()
	defer tty.mu.Unlock()
	sawScreen := false
	for _, f := range tty.frames {
		s := stripANSI(f)
		if strings.Contains(s, "New automation") {
			sawScreen = true
		}
		if sawScreen && hasBar(s) {
			t.Fatalf("ctrl+f opened the find bar while the automations screen was up. Consequence: the bar is open, unseen, when the screen closes and swallows the next keys. Remedy: keep !screen.open in both ctrl+f bindings of factory.json.\n%s", s)
		}
	}
	if !sawScreen {
		t.Fatal("premise: the automations screen never showed")
	}
}
