package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// withBehaviourLayer installs a behaviour for one test and puts the factory one back: the
// layer is package state, so a test that leaked it would change every test after it.
func withBehaviourLayer(t *testing.T, b behaviour) {
	t.Helper()
	setActiveBehaviour(b)
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
}

func patchOf(t *testing.T, js string) behaviourPatch {
	t.Helper()
	p, err := parseBehaviourPatch(json.RawMessage(js))
	if err != nil {
		t.Fatalf("patch %s: %v", js, err)
	}
	return p
}

// Every refusal the layer promises, each with the words that name what to do about it.
func TestBehaviourRefusesWhatItCannotHonour(t *testing.T) {
	cases := []struct{ name, patch, want string }{
		{"escape hatch as a shortcut", `{"keys":{"ctrl+c":"cmd:/mode plan"}}`, "Ctrl-C"},
		{"escape hatch as a menu key", `{"menu_keys":{"next":["ctrl+c"]}}`, "Ctrl-C"},
		{"a typing key as a shortcut", `{"keys":{"a":"cmd:/mode plan"}}`, "F-key"},
		{"an arrow as a shortcut", `{"keys":{"up":"cmd:/mode plan"}}`, "F-key"},
		{"a plain letter steering a menu", `{"menu_keys":{"next":["j"]}}`, "filter"},
		{"an unknown key name", `{"keys":{"hyper+x":"cmd:/mode plan"}}`, "not a key name"},
		{"a chord the editor owns", `{"keys":{"ctrl+o":"cmd:/mode plan"}}`, "already means"},
		{"answering for the user", `{"keys":{"f5":"answer:approve"}}`, "only a button you press"},
		{"a plugin from a hook", `{"hooks":[{"on":"effort","do":"ext:p:go"}]}`, "without a keypress"},
		{"an action with no prefix", `{"keys":{"f5":"effort max"}}`, "no action prefix"},
		{"a command line not starting with a slash", `{"keys":{"f5":"cmd:effort max"}}`, "starts with '/'"},
		{"an unknown menu action", `{"menu_keys":{"sideways":["left"]}}`, "not a menu action"},
		{"one key, two meanings", `{"menu_keys":{"next":["left"],"prev":["left"]}}`, "one way only"},
		{"a shortcut on a menu key", `{"menu_keys":{"next":["f5"]},"keys":{"f5":"cmd:/mode plan"}}`, "also a menu key"},
		{"shadowing a factory command", `{"commands":[{"name":"effort","run":"cmd:/mode plan"}]}`, "already a command"},
		{"a command calling itself", `{"commands":[{"name":"loop","run":"cmd:/loop"}]}`, "itself"},
		{"a bad command name", `{"commands":[{"name":"Deep Think","run":"cmd:/mode plan"}]}`, "lowercase"},
		{"a hook on nothing", `{"hooks":[{"on":"weather","do":"cmd:/mode plan"}]}`, "cannot watch"},
		{"a hook value that never happens", `{"hooks":[{"on":"effort","is":"ludicrous","do":"cmd:/mode plan"}]}`, "never"},
		{"a control character in a description", "{\"commands\":[{\"name\":\"x\",\"description\":\"a\\u001b[2Jb\",\"run\":\"cmd:/mode plan\"}]}", "control character"},
		{"a one-colour animation", `{"animations":{"flat":{"colors":["red"]}}}`, "at least"},
		{"an animation name with a space", `{"animations":{"two words":{"colors":["red","blue"]}}}`, "lowercase"},
		{"an unknown part", `{"colours":{}}`, "unknown field"},
	}
	for _, c := range cases {
		p, err := parseBehaviourPatch(json.RawMessage(c.patch))
		if err == nil {
			_, err = applyBehaviourPatch(behaviour{}, p)
		}
		if err == nil {
			t.Errorf("%s: accepted %s", c.name, c.patch)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: refusal %q does not say %q", c.name, err, c.want)
		}
	}
}

// The counterfactual: the same shapes, written legally, are accepted -- so the refusals
// above are about the named defect and not about the whole document being unreadable.
func TestBehaviourAcceptsTheLegalShapes(t *testing.T) {
	ok := []string{
		`{"keys":{"f5":"cmd:/effort max","ctrl+g":["cmd:/mode plan","cmd:/ui hide status"]}}`,
		`{"menu_keys":{"prev":["up","left"],"next":["down","right"],"first":["pgup"],"last":["pgdown"]}}`,
		`{"commands":[{"name":"deep","description":"max effort, plan mode","run":["cmd:/effort max","cmd:/mode plan"]}]}`,
		`{"hooks":[{"on":"effort","is":"max","do":"cmd:/ui style status header"},{"on":"mode","do":"cmd:/style bar"}]}`,
		`{"animations":{"rainbow":{"colors":["red","yellow","green","cyan","blue","magenta"],"spread":1,"fps":12,"attrs":["bold"]}}}`,
		`{"keys":{"f5":"focus:input"}}`,
	}
	for _, js := range ok {
		if _, err := applyBehaviourPatch(behaviour{}, patchOf(t, js)); err != nil {
			t.Errorf("refused a legal document %s: %v", js, err)
		}
	}
}

func TestBehaviourPatchMergesMapsAndReplacesLists(t *testing.T) {
	b, err := applyBehaviourPatch(behaviour{}, patchOf(t, `{"keys":{"f5":"cmd:/mode plan","f6":"cmd:/mode ask"},"commands":[{"name":"a","run":"cmd:/mode plan"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err = applyBehaviourPatch(b, patchOf(t, `{"keys":{"f6":null,"f7":"cmd:/mode auto"},"commands":[{"name":"b","run":"cmd:/mode plan"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := b.Keys["f5"]; !kept {
		t.Error("an entry the patch did not mention was dropped")
	}
	if _, gone := b.Keys["f6"]; gone {
		t.Error("null did not remove the entry")
	}
	if _, added := b.Keys["f7"]; !added {
		t.Error("a new entry was not added")
	}
	if len(b.Commands) != 1 || b.Commands[0].Name != "b" {
		t.Errorf("commands = %+v; a list replaces the whole list", b.Commands)
	}
	if b.Commands[0].Category != "Custom" {
		t.Errorf("category = %q, want the default", b.Commands[0].Category)
	}
}

// A refused patch changes nothing: the base is what comes back.
func TestBehaviourRefusalLeavesTheBaseAlone(t *testing.T) {
	base, _ := applyBehaviourPatch(behaviour{}, patchOf(t, `{"keys":{"f5":"cmd:/mode plan"}}`))
	got, err := applyBehaviourPatch(base, patchOf(t, `{"keys":{"f6":"cmd:/mode ask","ctrl+c":"cmd:/mode ask"}}`))
	if err == nil {
		t.Fatal("accepted")
	}
	if !sameBehaviour(got, base) {
		t.Error("a refused patch returned something other than the base")
	}
}

func TestBehaviourIsSavedBootedUndoneAndReset(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	doc := builtinDoc(t)
	b, err := applyBehaviourPatch(behaviour{}, patchOf(t, `{"keys":{"f5":"cmd:/effort high"},"animations":{"rainbow":{"colors":["red","blue"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := saveInterface(doc, nil, nil, b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userScenePath()); !os.IsNotExist(err) {
		t.Error("a behaviour-only change saved a copy of the built-in layout")
	}
	got, err := loadUserBehaviour()
	if err != nil || !sameBehaviour(got, b) {
		t.Fatalf("saved behaviour read back as %+v, %v", got, err)
	}
	_, _, _, back, err := undoInterface()
	if err != nil || !back.empty() {
		t.Fatalf("undo gave %+v, %v; it must return the factory behaviour", back, err)
	}
	if _, err := os.Stat(userBehaviourPath()); !os.IsNotExist(err) {
		t.Error("undo left behaviour.json on disk; the next session would still run it")
	}
	_, _, _, again, err := undoInterface()
	if err != nil || !sameBehaviour(again, b) {
		t.Fatalf("a second undo gave %+v, %v; it must bring the behaviour back", again, err)
	}
	if _, _, _, z, err := resetInterface(); err != nil || !z.empty() {
		t.Fatalf("reset gave %+v, %v", z, err)
	}
	if _, _, _, z, err := undoInterface(); err != nil || !sameBehaviour(z, b) {
		t.Errorf("undo after reset gave %+v, %v; the reset must keep what it removed", z, err)
	}
}

// A saved file that was hand-edited into something unsafe is refused at boot, not run.
func TestASavedBehaviourFileIsValidatedAtBoot(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userBehaviourPath(), []byte(`{"keys":{"ctrl+c":"cmd:/mode plan"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadUserBehaviour(); err == nil || !strings.Contains(err.Error(), "Ctrl-C") {
		t.Fatalf("boot accepted a Ctrl-C binding: %v", err)
	}
}

func TestMenuKeysSteerAndTheFactoryKeyIsGivenUp(t *testing.T) {
	b, err := applyBehaviourPatch(behaviour{}, patchOf(t, `{"menu_keys":{"prev":["left"],"next":["right"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	withBehaviourLayer(t, b)
	key := func(ty term.KeyType) term.Key { return term.Key{Type: ty} }
	if got := navAction(key(term.KeyRight)); got != "next" {
		t.Errorf("right = %q, want next", got)
	}
	if got := navAction(key(term.KeyLeft)); got != "prev" {
		t.Errorf("left = %q, want prev", got)
	}
	if got := navAction(key(term.KeyDown)); got != "inert" {
		t.Errorf("down = %q; a key the user took from an action must not half-work", got)
	}
	if got := navAction(key(term.KeyEnter)); got != "pick" {
		t.Errorf("enter = %q; a key the user did not move keeps its meaning", got)
	}
	// Counterfactual: with no behaviour the factory keys are exactly the old ones.
	setActiveBehaviour(behaviour{})
	if navAction(key(term.KeyDown)) != "next" || navAction(key(term.KeyRight)) != "" {
		t.Error("the factory navigation changed with no behaviour installed")
	}
}

func TestMenuKeysMoveTheHighlightLeftAndRight(t *testing.T) {
	mm := &modelMenu{}
	mm.setRows(effortMenuData([]string{"low", "medium", "high"}, true, ""))
	b, _ := applyBehaviourPatch(behaviour{}, patchOf(t, `{"menu_keys":{"prev":["left"],"next":["right"],"last":["end"]}}`))
	withBehaviourLayer(t, b)
	in := effortPrefix
	press := func(ty term.KeyType) {
		choiceMenuKey(mm, effortPrefix, in, len([]rune(in)), term.Key{Type: ty})
	}
	press(term.KeyRight)
	if mm.sel != 1 {
		t.Errorf("right: sel = %d, want 1", mm.sel)
	}
	press(term.KeyLeft)
	press(term.KeyLeft)
	if mm.sel != 2 {
		t.Errorf("left twice wraps to the last row: sel = %d, want 2", mm.sel)
	}
	mm.sel = 0
	press(term.KeyEnd)
	if mm.sel != 2 {
		t.Errorf("end: sel = %d, want the last row", mm.sel)
	}
	mm.sel = 0
	press(term.KeyDown)
	if mm.sel != 0 {
		t.Errorf("a given-up key moved the highlight to %d", mm.sel)
	}
}

// Ctrl-C reaches no layer: navAction names no action for it whatever the layer says, and
// the validators refuse to bind it.
func TestCtrlCIsNeverAMenuOrShortcutKey(t *testing.T) {
	ctrlC := term.Key{Type: term.KeyRunes, Runes: []rune{'c'}, Mod: term.ModCtrl}
	if got := navAction(ctrlC); got != "" {
		t.Errorf("navAction(ctrl+c) = %q", got)
	}
	if err := (behaviour{Keys: map[string]actionList{"ctrl+c": {"cmd:/mode plan"}}}).validate(); err == nil {
		t.Error("validate accepted a Ctrl-C shortcut")
	}
	if err := (behaviour{MenuKeys: map[string][]string{"close": {"ctrl+C"}}}).validate(); err == nil {
		t.Error("validate accepted Ctrl-C (upper-case spelling) as a menu key")
	}
}

func TestUserCommandsJoinTheMenuAndLeaveWithTheLayer(t *testing.T) {
	b, err := applyBehaviourPatch(behaviour{}, patchOf(t, `{"commands":[{"name":"deep","description":"max effort, plan mode","run":["cmd:/effort high","cmd:/mode plan"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	withBehaviourLayer(t, b)
	found := false
	for _, m := range fold.FilterSlashMatches("deep") {
		found = found || (m.Name == "deep" && m.Category == "Custom")
	}
	if !found {
		t.Error("the user's command is not in the / menu")
	}
	if n := len(fold.Commands); n == 0 {
		t.Fatal("no factory commands")
	}
	for _, c := range fold.Commands {
		if c.Name == "deep" {
			t.Error("the user's command leaked into the factory registry")
		}
	}
	if c, ok := b.commandFor("/deep", 0, fold.SlashAll); !ok || len(c.Run) != 2 {
		t.Errorf("commandFor(/deep) = %+v, %v", c, ok)
	}
	if _, ok := b.commandFor("/effort", 0, fold.SlashAll); ok {
		t.Error("a factory command was claimed by the user's layer")
	}
	setActiveBehaviour(behaviour{})
	for _, m := range fold.FilterSlashMatches("deep") {
		if m.Name == "deep" {
			t.Error("the user's command stayed in the menu after the layer was removed")
		}
	}
}

func TestHooksSelectByEventAndValue(t *testing.T) {
	b, err := applyBehaviourPatch(behaviour{}, patchOf(t, `{"hooks":[{"on":"effort","is":"max","do":"cmd:/mode plan"},{"on":"effort","do":"cmd:/style band"},{"on":"mode","do":"cmd:/style bar"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := b.hooksFor("effort", "max"); len(got) != 2 {
		t.Errorf("effort=max ran %v, want both effort hooks", got)
	}
	if got := b.hooksFor("effort", "low"); len(got) != 1 || got[0] != "cmd:/style band" {
		t.Errorf("effort=low ran %v, want only the any-change hook", got)
	}
	if got := b.hooksFor("style", "bar"); len(got) != 0 {
		t.Errorf("style ran %v with no hook on it", got)
	}
}

func TestAnimationsBecomeTokensAndRefuseRecolouring(t *testing.T) {
	b, err := applyBehaviourPatch(behaviour{}, patchOf(t, `{"animations":{"rainbow":{"colors":["red","blue"],"spread":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	thm := withBehaviour(theme.SOBRIA(), b)
	if !thm.Has("rainbow") {
		t.Fatal("an animation is not a token any node may name")
	}
	if _, ok := thm.Cycle("rainbow"); !ok {
		t.Fatal("the animation is not animated")
	}
	if _, err := applyColors(nil, thm, map[string]string{"rainbow": "fg=red"}); err == nil || !strings.Contains(err.Error(), "animation") {
		t.Errorf("a plain colour over an animation was accepted (%v); it would be hidden by it", err)
	}
	// Counterfactual: a plain token is still recolourable.
	if _, err := applyColors(nil, thm, map[string]string{"dim": "fg=red"}); err != nil {
		t.Errorf("recolouring a plain token was refused: %v", err)
	}
}

func TestTypedBehaviourCommands(t *testing.T) {
	p, _, ok, err := uiBehaviourCommand("/ui animate fire red,yellow fps=12 spread=2 bold")
	if !ok || err != nil {
		t.Fatalf("animate: %v %v", ok, err)
	}
	b, err := applyBehaviourPatch(behaviour{}, p)
	if err != nil {
		t.Fatal(err)
	}
	d := b.Animations["fire"]
	if len(d.Colors) != 2 || d.FPS != 12 || d.Spread != 2 || len(d.Attrs) != 1 {
		t.Errorf("fire = %+v", d)
	}
	p, _, _, _ = uiBehaviourCommand("/ui key F5 cmd:/effort high; cmd:/mode plan")
	b, err = applyBehaviourPatch(b, p)
	if err != nil || len(b.Keys["f5"]) != 2 {
		t.Fatalf("key: %+v, %v", b.Keys, err)
	}
	p, _, _, _ = uiBehaviourCommand("/ui menukeys next right")
	b, err = applyBehaviourPatch(b, p)
	if err != nil || len(b.MenuKeys["next"]) != 1 {
		t.Fatalf("menukeys: %+v, %v", b.MenuKeys, err)
	}
	p, _, _, _ = uiBehaviourCommand("/ui key f5 off")
	if b, _ = applyBehaviourPatch(b, p); len(b.Keys) != 0 {
		t.Error("off did not free the key")
	}
	if _, _, ok, _ := uiBehaviourCommand("/ui hide status"); ok {
		t.Error("a document verb was taken as a behaviour command")
	}
	if _, _, _, err := uiBehaviourCommand("/ui key ctrl+c cmd:/mode plan"); err != nil {
		t.Log("(refused at parse: fine)")
	}
	p, _, _, _ = uiBehaviourCommand("/ui key ctrl+c cmd:/mode plan")
	if _, err := applyBehaviourPatch(behaviour{}, p); err == nil {
		t.Error("the typed surface accepted a Ctrl-C binding")
	}
}

// ---- the real loop ---------------------------------------------------------------

func seedBehaviour(t *testing.T, js string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userBehaviourPath(), []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
}

func runLoop(t *testing.T, drv Driver, evCh chan fold.Event, script []scheduledEvent) *fakeTTY {
	t.Helper()
	tty := newFakeTTY(110, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, builtinDoc(t), theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty
}

func typeBeh(s string) []scheduledEvent {
	var out []scheduledEvent
	for _, r := range s {
		out = append(out, scheduledEvent{5 * time.Millisecond, keyEvent(r)})
	}
	return out
}

func fkey(ty term.KeyType) scheduledEvent {
	return scheduledEvent{30 * time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: ty}}}
}

var quitScript = []scheduledEvent{{300 * time.Millisecond, ctrlCharEvent('c')}, {50 * time.Millisecond, ctrlCharEvent('c')}}

// A shortcut changes the setting with no line sent to the chat.
func TestLoopShortcutSetsTheEffort(t *testing.T) {
	seedBehaviour(t, `{"keys":{"f5":"cmd:/effort high"}}`)
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := append([]scheduledEvent{fkey(term.KeyF5)}, quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 1 || drv.levels[0] != "high" {
		t.Errorf("levels = %v, want [high]", drv.levels)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the shortcut reached the chat as %q", drv.submitted)
	}
}

// The draft the user was typing survives a shortcut pressed in the middle of it.
func TestLoopShortcutKeepsTheDraft(t *testing.T) {
	seedBehaviour(t, `{"keys":{"f5":"cmd:/effort high"}}`)
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := append(typeBeh("half a thou"), fkey(term.KeyF5))
	script = append(script, typeBeh("ght")...)
	script = append(script, scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.submitted) != 1 || drv.submitted[0] != "half a thought" {
		t.Errorf("submitted = %q, want the draft intact around the shortcut", drv.submitted)
	}
}

// Counterfactual: with no behaviour F5 is not ours and nothing changes.
func TestLoopWithoutBehaviourF5DoesNothing(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := append([]scheduledEvent{fkey(term.KeyF5)}, quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 0 {
		t.Errorf("levels = %v with no shortcut defined", drv.levels)
	}
}

// The motivating case: the effort menu answers to Left/Right.
func TestLoopEffortMenuNavigatesWithLeftAndRight(t *testing.T) {
	seedBehaviour(t, `{"menu_keys":{"prev":["left"],"next":["right"]}}`)
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	script := append(typeBeh("/effort "), fkey(term.KeyRight), fkey(term.KeyRight), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 1 || drv.levels[0] != "high" {
		t.Errorf("levels = %v, want [high] after two Rights from low", drv.levels)
	}
}

// A command of the user's runs its actions in order when picked from the menu.
func TestLoopUserCommandRunsItsActions(t *testing.T) {
	seedBehaviour(t, `{"commands":[{"name":"deep","description":"d","run":["cmd:/effort high","cmd:/mode plan"]}]}`)
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append(typeBeh("/deep"), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 1 || drv.levels[0] != "high" {
		t.Errorf("levels = %v, want [high]", drv.levels)
	}
	if len(drv.modes) == 0 || drv.modes[len(drv.modes)-1] != "plan" {
		t.Errorf("modes = %v, want plan last", drv.modes)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the command reached the chat as %q", drv.submitted)
	}
}

// A hook reacts to the user changing a setting, and a hook's own action does not fire
// hooks (so two hooks that name each other cannot loop).
func TestLoopHookFiresOnceAndNeverChains(t *testing.T) {
	seedBehaviour(t, `{"hooks":[{"on":"effort","do":"cmd:/mode plan"},{"on":"mode","do":"cmd:/effort low"}]}`)
	drv := &modeEffortDriver{effortDriver: effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}}
	script := append(typeBeh("/effort "), fkey(term.KeyDown), scheduledEvent{20 * time.Millisecond, enterEvent()}) // medium
	script = append(script, quitScript...)
	runLoop(t, drv, drv.evCh, script)
	drv.mu.Lock()
	defer drv.mu.Unlock()
	if len(drv.levels) != 1 || drv.levels[0] != "medium" {
		t.Errorf("levels = %v; the mode hook must not have fired the effort hook back (want [medium])", drv.levels)
	}
	if len(drv.modes) == 0 || drv.modes[len(drv.modes)-1] != "plan" {
		t.Errorf("modes = %v; the effort hook never ran", drv.modes)
	}
}

// Ctrl-C leaves the program however the behaviour is set: the double press ends the loop.
func TestLoopCtrlCStillLeavesWithEveryLayerSet(t *testing.T) {
	seedBehaviour(t, `{"menu_keys":{"close":["f9"]},"keys":{"f5":"cmd:/mode plan"},"hooks":[{"on":"mode","do":"cmd:/effort low"}]}`)
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	done := make(chan struct{})
	go func() { runLoop(t, drv, drv.evCh, append(typeBeh("/effort "), quitScript...)); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Ctrl-C did not leave the program with behaviour installed")
	}
}

// The animated max row in the effort menu actually moves when the user wires it up.
func TestBehaviourAnimationReachesTheEmittedFrames(t *testing.T) {
	seedBehaviour(t, `{"animations":{"rainbow":{"colors":["red","green","blue"],"fps":30,"spread":1}}}`)
	drv := &effortDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}}
	// Put the effort word in the status bar under the animation by style_by, via the real
	// patch surface, then pick a level and let several animation ticks pass.
	line := `/ui set status_effort style_by {"high":"rainbow"}`
	script := append(typeBeh(line), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, typeBeh("/effort ")...)
	script = append(script, fkey(term.KeyDown), fkey(term.KeyDown), scheduledEvent{20 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{600 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')})
	tty := runLoop(t, drv, drv.evCh, script)
	out := tty.output()
	seen := map[string]bool{}
	for _, c := range []string{"38;5;1m", "38;5;2m", "38;5;4m", "31m", "32m", "34m"} {
		if strings.Contains(out, c) {
			seen[c] = true
		}
	}
	if len(seen) < 2 {
		t.Errorf("the effort word never changed colour across ticks; saw %v", seen)
	}
}

// modeEffortDriver also records the modes the loop sets.
type modeEffortDriver struct {
	effortDriver
	modes []string
}

func (d *modeEffortDriver) SetMode(m string) {
	d.mu.Lock()
	d.modes = append(d.modes, m)
	d.mu.Unlock()
}
