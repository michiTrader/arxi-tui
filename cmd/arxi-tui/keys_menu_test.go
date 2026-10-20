package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// withBehaviour makes b the behaviour in force for one test and puts the factory one
// back after, because the active behaviour is process-wide.
func withActiveBehaviour(t *testing.T, b behaviour) {
	t.Helper()
	setActiveBehaviour(b)
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
}

func readBehaviourFile(t *testing.T, cfg string) string {
	t.Helper()
	b, _ := os.ReadFile(cfg + "/behaviour.json")
	return string(b)
}

// ---- the registry says what the code does ---------------------------------------------

// A row that names a key the handler does not take is a list that lies: a user would read
// "ctrl+o does X", press it and get nothing. Every row with a probe is checked against the
// real handler, so the day a handler changes, this fails and names the row to update.
func TestEveryFactoryKeyRowIsTakenByTheHandlerItNames(t *testing.T) {
	for _, f := range factoryKeys {
		for _, name := range f.Keys {
			k, ok := term.ParseKey(name)
			if !ok {
				t.Errorf("row %q: %q is not a key name; fix the spelling (term.Key.String writes the real ones)", f.What, name)
				continue
			}
			if k.String() != name {
				t.Errorf("row %q: %q is spelled %q by the app; use that spelling so a keymap diff is stable", f.What, name, k.String())
			}
			if f.Spent != nil && !f.Spent(k) {
				t.Errorf("row %q lists %s but its handler does not take it: the list would lie to the user; fix the row or the handler", f.What, name)
			}
		}
	}
}

// A key listed twice would be two rows that disagree about one key.
func TestNoKeyIsListedTwiceInTheFactoryRows(t *testing.T) {
	seen := map[string]string{}
	for _, f := range factoryKeys {
		for _, k := range f.Keys {
			if prev, dup := seen[k]; dup {
				t.Errorf("%s is listed for %q and for %q; keep it where it is read first and say the other use in the sentence", k, prev, f.What)
			}
			seen[k] = f.What
		}
	}
}

// Ctrl-C is the way out (invariant 6). The row must say it cannot move, and the rule that
// keeps it fixed must still refuse it for a menu key and a shortcut alike.
func TestCtrlCIsListedAsFixedAndEveryWayToMoveItIsRefused(t *testing.T) {
	var row *factoryKey
	for i := range factoryKeys {
		if contains(factoryKeys[i].Keys, "ctrl+c") {
			row = &factoryKeys[i]
		}
	}
	if row == nil || !strings.Contains(row.Note, "cannot") {
		t.Fatalf("the list must show ctrl+c and say no setting can move it: %+v", row)
	}
	user := behaviour{}
	for _, line := range []string{"prev ctrl+c", "ctrl+c cmd:/effort max", "pick ctrl+c"} {
		if out := runKeysLine(line, user, user); out.err == "" || out.changed {
			t.Errorf("/keys %s was accepted: %+v; ctrl+c can never be taken by a setting", line, out)
		}
	}
}

// ---- which stage a line is in ---------------------------------------------------------

func TestTheKeysStageIsReadFromTheLineAlone(t *testing.T) {
	withActiveBehaviour(t, behaviour{})
	cases := []struct {
		line, act, prefix, filter string
		open                      bool
	}{
		{"/keys ", "", "/keys ", "", true},
		{"/keys scroll", "", "/keys ", "scroll", true},
		{"/keys prev ", "prev", "/keys prev ", "", true},
		{"/keys prev al", "prev", "/keys prev ", "al", true},
		// typed lines: the menu must step aside so Enter reaches them
		{"/keys list", "", "", "", false},
		{"/keys prev up", "", "", "", false},      // a whole key on offer
		{"/keys prev -", "", "", "", false},       // the factory dash
		{"/keys prev factory", "", "", "", false}, // the same, spelled out
		{"/keys prev ctrl+k", "", "", "", false},  // a key the menu does not offer: still settable
		{"/keys f5 cmd:/effort max", "", "", "", false},
		{"/keys f5 off", "", "", "", false},
		{"not a keys line", "", "", "", false},
	}
	for _, c := range cases {
		act, prefix, filter, open := keysStage(c.line)
		if open != c.open || act != c.act || prefix != c.prefix || filter != c.filter {
			t.Errorf("%q: got act=%q prefix=%q filter=%q open=%v; want act=%q prefix=%q filter=%q open=%v",
				c.line, act, prefix, filter, open, c.act, c.prefix, c.filter, c.open)
		}
	}
}

// ---- rows -----------------------------------------------------------------------------

func TestTheFirstStageListsMenuKeysThenYourShortcutsThenTheFixedKeys(t *testing.T) {
	b := behaviour{Keys: map[string]actionList{"f5": {"cmd:/effort max"}}}
	rows := keysRows("", b)
	idx := func(prefix string) int {
		for i, r := range rows {
			if strings.HasPrefix(r.Ref, prefix) {
				return i
			}
		}
		return -1
	}
	menu, mine, fixed := idx("menu:prev"), idx("key:f5"), idx("fixed:0")
	if menu < 0 || mine < 0 || fixed < 0 || !(menu < mine && mine < fixed) {
		t.Fatalf("order is menu=%d shortcut=%d fixed=%d; the changeable rows must come first", menu, mine, fixed)
	}
	if got := len(rows); got != len(menuNavActions)+1+len(factoryKeys) {
		t.Errorf("%d rows; want one per menu action, per shortcut and per factory row", got)
	}
}

func TestAMovedMenuKeyShowsWhatTheFactoryOneWas(t *testing.T) {
	b := behaviour{MenuKeys: map[string][]string{"next": {"right"}}}
	var got string
	for _, r := range keysRows("", b) {
		if strings.HasPrefix(r.Ref, "menu:next") {
			got = r.Provider
		}
	}
	if !strings.Contains(got, "right") || !strings.Contains(got, "factory down") {
		t.Errorf("the row says %q; a changed key must say what it was, so the user can undo it without a README", got)
	}
}

func TestTheSecondStageMarksTheKeysInUse(t *testing.T) {
	rows := keysRows("prev", behaviour{})
	var current []string
	for _, r := range rows {
		if r.Current {
			current = append(current, r.Ref)
		}
	}
	if len(current) != 1 || current[0] != "up" {
		t.Errorf("keys marked in use: %v; want [up]", current)
	}
	if last := rows[len(rows)-1]; last.Ref != keysFactoryRef || !strings.Contains(last.Provider, "up") {
		t.Errorf("the last row must put the factory keys back and say which: %+v", last)
	}
}

// Offering a key that another action or a shortcut already has would be a pick that is
// refused: the row is left out instead, so every row on screen is one Enter can take.
func TestTheSecondStageLeavesOutKeysThatAnotherActionOrAShortcutOwns(t *testing.T) {
	b := behaviour{
		MenuKeys: map[string][]string{"next": {"left"}, "close": {"esc"}},
		Keys:     map[string]actionList{"alt+k": {"say:hi"}},
	}
	for _, r := range keysRows("prev", b) {
		switch r.Ref {
		case "left":
			t.Errorf("left is offered for prev but next owns it: Enter on it would be refused")
		case "alt+k":
			t.Errorf("alt+k is offered for prev but it is the user's shortcut: Enter on it would be refused")
		}
	}
	// Every row offered must really be accepted by the rule that decides.
	for _, act := range menuNavActions {
		for _, r := range keysRows(act, b) {
			if r.Ref == keysFactoryRef || r.Current {
				continue
			}
			if out := runKeysLine(act+" "+r.Ref, b, b); out.err != "" {
				t.Errorf("/keys %s offers %q but picking it is refused: %s", act, r.Ref, out.err)
			}
		}
	}
}

// `/keys <action> -` and `/keys <action> factory` put the factory keys back, and they reach
// the typed line only because no key on offer contains a hyphen or the word "factory": the
// fragment then matches nothing and the menu steps aside. A key that did would turn those
// lines into a menu that swallows the Enter.
func TestNoOfferedKeyContainsTheFactoryDashOrWordSoBothStayTheTypedLines(t *testing.T) {
	for _, act := range menuNavActions {
		for _, k := range keyCandidates(act, behaviour{}) {
			if strings.Contains(k, "-") || strings.Contains(k, keysFactoryRef) {
				t.Errorf("%s offers %q: `/keys %s -` would match it and open the menu instead of restoring the factory keys; give the menu an explicit case first", act, k, act)
			}
		}
		for _, w := range []string{"-", keysFactoryRef} {
			if _, _, _, open := keysStage("/keys " + act + " " + w); open {
				t.Errorf("/keys %s %s must be the typed line's: it restores the factory keys", act, w)
			}
		}
	}
}

// ---- the typed line -------------------------------------------------------------------

func TestAKeyThatTheActionLacksIsAddedAndOneItHasIsTakenAway(t *testing.T) {
	user := behaviour{}
	out := runKeysLine("next right", user, user)
	if !out.changed || !sameKeySet(out.next.MenuKeys["next"], []string{"down", "right"}) {
		t.Fatalf("adding right: %+v; want next on down and right", out)
	}
	out = runKeysLine("next right", out.next, out.next)
	if !out.changed {
		t.Fatalf("taking right away was refused: %+v", out)
	}
	if _, kept := out.next.MenuKeys["next"]; kept {
		t.Errorf("back on the factory keys the entry must be gone, not kept as a copy of them: %+v", out.next.MenuKeys)
	}
}

func TestTheOnlyKeyOfAnActionCannotBeTakenAway(t *testing.T) {
	user := behaviour{}
	out := runKeysLine("pick enter", user, user)
	if out.changed || !strings.Contains(out.err, "only key") || !strings.Contains(out.err, "-") {
		t.Errorf("%+v; removing the last key must say an action needs one and name the way back", out)
	}
}

func TestAKeyThatDoesTwoThingsIsRefusedWithTheRuleOfTheOtherCommands(t *testing.T) {
	user := behaviour{}
	out := runKeysLine("prev down", user, user) // down is next's
	if out.changed || !strings.Contains(out.err, "both") {
		t.Errorf("%+v; the one rule (a key steers a menu one way) must refuse it", out)
	}
	out = runKeysLine("prev a", user, user) // a bare letter would stop the filter typing
	if out.changed || !strings.Contains(out.err, "plain character") {
		t.Errorf("%+v; a bare letter must be refused: the menu needs it to filter", out)
	}
}

func TestAPickIsBuiltOverTheUsersLayerNeverTheFoldersOne(t *testing.T) {
	user := behaviour{}
	eff := behaviour{MenuKeys: map[string][]string{"next": {"alt+j"}}} // from the folder
	out := runKeysLine("next right", user, eff)
	if !out.changed {
		t.Fatalf("%+v", out)
	}
	if keys := out.next.MenuKeys["next"]; contains(keys, "alt+j") {
		t.Errorf("the folder's alt+j was written into the user's own file: %v", keys)
	}
}

func TestAShortcutIsBoundAndFreedWithTheSameRulesAsUiKey(t *testing.T) {
	user := behaviour{}
	out := runKeysLine("f5 cmd:/effort max", user, user)
	if !out.changed || len(out.next.Keys["f5"]) != 1 {
		t.Fatalf("binding f5: %+v", out)
	}
	if out2 := runKeysLine("f5 off", out.next, out.next); !out2.changed || len(out2.next.Keys) != 0 {
		t.Errorf("freeing f5: %+v", out2)
	}
	// A reserved chord is refused by the same rule /ui key uses.
	if bad := runKeysLine("ctrl+o cmd:/effort max", user, user); bad.changed || bad.err == "" {
		t.Errorf("ctrl+o is the expand key and may not be shadowed: %+v", bad)
	}
	// Nonsense in the first word says what the words are, not a /ui message.
	if bad := runKeysLine("zzz", user, user); bad.changed || !strings.Contains(bad.err, "/keys") {
		t.Errorf("%+v", bad)
	}
}

func TestListingNamesTheKeysYouCanChangeAndTheFixedOnesAndCtrlC(t *testing.T) {
	s := keysListing(behaviour{Keys: map[string]actionList{"f5": {"say:hi"}}})
	for _, want := range []string{"menu next", "f5", "ctrl+c", "ctrl+o", "Fixed keys"} {
		if !strings.Contains(s, want) {
			t.Errorf("the listing lacks %q:\n%s", want, s)
		}
	}
}

// ---- through the real loop --------------------------------------------------------------

func TestLoopSlashKeysOpensTheListAndSendsNothing(t *testing.T) {
	frame, sub := menuRun(t, "/keys ")
	if len(sub) != 0 {
		t.Errorf("opening /keys reached the model: %q", sub)
	}
	if !strings.Contains(frame, "menu prev") {
		t.Errorf("the list does not show the menu keys:\n%s", frame)
	}
}

func TestLoopTypingNarrowsTheKeysListToTheScrollKeys(t *testing.T) {
	frame, _ := menuRun(t, "/keys scroll")
	if !strings.Contains(frame, "pgup") || strings.Contains(frame, "menu prev") {
		t.Errorf("the filter did not narrow the list to the scroll keys:\n%s", frame)
	}
}

func TestLoopPickingAMenuKeyOpensItsKeysAndPickingOneKeepsItInBehaviourJson(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
	// "/keys menu" narrows to the six menu rows; Enter picks the first (prev), which opens
	// its keys; "alt+k" narrows that to one row; Enter picks it.
	script := append(typed("/keys menu"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, typed("alt+k")...)
	script = append(script, scheduledEvent{80 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	out, sub := runLoopDefault(t, 110, 40, script)
	if len(sub) != 0 {
		t.Errorf("the menu reached the model: %q", sub)
	}
	if b := readBehaviourFile(t, cfg); !strings.Contains(b, "alt+k") || !strings.Contains(b, "prev") {
		t.Errorf("the change is not in behaviour.json: %q\n%s", b, lastFrame(out))
	}
	if !strings.Contains(stripANSI(out), "now answers to") {
		t.Errorf("the pick was not confirmed on screen:\n%s", lastFrame(out))
	}
}

func TestLoopATypedKeyThatTheMenuDoesNotOfferStillReachesTheTypedLine(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
	script := append(enterLine("/keys next alt+n"), exitKeys()...)
	_, sub := runLoopDefault(t, 110, 40, script)
	if len(sub) != 0 {
		t.Errorf("a typed /keys line reached the model: %q", sub)
	}
	if b := readBehaviourFile(t, cfg); !strings.Contains(b, "alt+n") {
		t.Errorf("Enter did nothing on a complete line: %q; the menu must step aside once the key is not on offer", b)
	}
}

func TestLoopAFixedRowExplainsItselfAndChangesNothing(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
	// "ctrl+c" narrows to the one row that lists it; Enter picks it.
	script := append(typed("/keys ctrl+c"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	out, sub := runLoopDefault(t, 110, 40, script)
	if len(sub) != 0 {
		t.Errorf("a fixed row reached the model: %q", sub)
	}
	if !strings.Contains(stripANSI(out), "any setting") {
		t.Errorf("the row did not say why ctrl+c cannot move:\n%s", lastFrame(out))
	}
	if b := readBehaviourFile(t, cfg); b != "" {
		t.Errorf("picking a fixed row wrote behaviour.json: %q", b)
	}
}

func TestLoopKeysListPrintsEveryKeyIntoTheConversation(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	script := append(enterLine("/keys list"), exitKeys()...)
	out, sub := runLoopDefault(t, 110, 50, script)
	if len(sub) != 0 {
		t.Errorf("/keys list reached the model: %q", sub)
	}
	if s := stripANSI(out); !strings.Contains(s, "Fixed keys") || !strings.Contains(s, "ctrl+o") {
		t.Errorf("the listing is not on screen:\n%s", lastFrame(out))
	}
}

// A key moved by /keys is a key moved for every menu, including the ones that are not
// /keys: the effect has to be real, not a row that says so. /mode opens on "ask" and
// "down" would highlight "auto"; once next lives on alt+n, alt+n must do that, and Enter
// must then switch the mode to auto.
func TestLoopAMovedMenuKeyReallySteersAnotherMenu(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Cleanup(func() { setActiveBehaviour(behaviour{}) })
	altN := scheduledEvent{40 * time.Millisecond, term.Event{Kind: term.EventKey,
		Key: term.Key{Type: term.KeyRunes, Runes: []rune{'n'}, Mod: term.ModAlt}}}
	script := enterLine("/keys next alt+n")
	script = append(script, enterLine("/keys next down")...)
	script = append(script, typed("/mode ")...)
	script = append(script, altN, scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	out, _ := runLoopDefault(t, 110, 40, script)
	if b := readBehaviourFile(t, cfg); !strings.Contains(b, "alt+n") || strings.Contains(b, `"down"`) {
		t.Fatalf("setup failed, behaviour.json is %q\n%s", b, lastFrame(out))
	}
	if f := lastFrame(out); !strings.Contains(f, "auto") || strings.Contains(f, "ask ·") {
		t.Errorf("alt+n did not move the /mode highlight, so Enter did not pick auto:\n%s", f)
	}
}
