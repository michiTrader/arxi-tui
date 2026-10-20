package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// readTexts is what texts.json holds ("" when nothing was ever kept).
func readTexts(t *testing.T, cfg string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(cfg, "texts.json"))
	return string(b)
}

func hasRef(rows []fold.ModelMatch, ref string) bool {
	for _, r := range rows {
		if r.Ref == ref {
			return true
		}
	}
	return false
}

// ---- which stage a line is in ---------------------------------------------------------

func TestTheMenuStageIsReadFromTheLineAlone(t *testing.T) {
	cases := []struct {
		line, key, prefix, filter string
		open                      bool
	}{
		{"/settings ", "", "/settings ", "", true},
		{"/settings scroll", "", "/settings ", "scroll", true},
		{"/settings 80", "", "/settings ", "80", true},
		{"/settings chat.light ", "chat.light", "/settings chat.light ", "", true},
		{"/settings chat.light n", "chat.light", "/settings chat.light ", "n", true},
		{"/settings effort.default ", "effort.default", "/settings effort.default ", "", true},
		// The typed surface owns every line below: a menu there would swallow the Enter.
		{"/settings list", "", "", "", false},
		{"/settings scroll.wheel_step", "", "", "", false},
		{"/settings scroll.wheel_step ", "", "", "", false}, // a free number: no list of values
		{"/settings scroll.wheel_step 5", "", "", "", false},
		{"/settings chat.light no", "", "", "", false},  // a whole value
		{"/settings chat.light -", "", "", "", false},   // the factory value
		{"/settings chat.light zzz", "", "", "", false}, // matches no row: Enter must refuse it, not do nothing
		{"/settings autocompact 75", "", "", "", false},
		{"not a settings line", "", "", "", false},
	}
	for _, c := range cases {
		key, prefix, filter, open := settingsMenuStage(c.line)
		if open != c.open || key != c.key || prefix != c.prefix || filter != c.filter {
			t.Errorf("%q: got key=%q prefix=%q filter=%q open=%v; want key=%q prefix=%q filter=%q open=%v",
				c.line, key, prefix, filter, open, c.key, c.prefix, c.filter, c.open)
		}
	}
}

// ---- the rows -------------------------------------------------------------------------

func TestTheFirstStageNamesEverySettingAndKeepsTheAutocompactRows(t *testing.T) {
	rows := settingsRows("", autoCompactSetting{Percent: 80}, 1_000_000, nil, nil)
	for _, k := range settingsKeys {
		if !hasRef(rows, k) {
			t.Errorf("the menu lacks the setting %q: a setting the list shows but the menu hides cannot be found by browsing; add it to settingsKeys's rows", k)
		}
	}
	for _, ref := range []string{settingOff, settingRef(50), settingRef(80), settingRef(90)} {
		if !hasRef(rows, ref) {
			t.Errorf("the autocompact row %q is gone: `/settings` has always opened on these, and users pick them", ref)
		}
	}
	if len(rows) != len(settingsKeys)+1+1+len(autoCompactPercents) {
		t.Errorf("got %d rows, want %d settings + hidden parts + off + %d percentages", len(rows), len(settingsKeys), len(autoCompactPercents))
	}
}

func TestARowSaysTheValueAndTheFactoryOneWhenTheyDiffer(t *testing.T) {
	setWith(t)
	for _, r := range settingsRows("", autoCompactSetting{}, 0, nil, nil) {
		if r.Ref == "chat.scrollbar" && (!strings.Contains(r.Provider, `"yes"`) || strings.Contains(r.Provider, "factory")) {
			t.Errorf("a setting at its factory value must show only the value: %q", r.Provider)
		}
	}
	setWith(t, "chat.scrollbar", "no")
	for _, r := range settingsRows("", autoCompactSetting{}, 0, nil, nil) {
		if r.Ref == "chat.scrollbar" && (!strings.Contains(r.Provider, `"no"`) || !strings.Contains(r.Provider, `factory "yes"`)) {
			t.Errorf("a changed setting must say what it is now and what the factory one is: %q", r.Provider)
		}
	}
}

func TestEveryClosedSetOfValuesIsAcceptedByTheRuleThatChecksWrites(t *testing.T) {
	// A row that the typed surface then refuses would be a menu that offers what it cannot do.
	for _, k := range settingsKeys {
		for _, c := range settingChoices(k) {
			if err := validChatLook(k, c); err != nil {
				t.Errorf("%s offers %q but the rule refuses it (%v): fix the row or the rule so they agree", k, c, err)
			}
		}
	}
	if settingChoices("scroll.wheel_step") != nil || settingChoices("input.max_rows") != nil {
		t.Error("a free number has no closed set of values; listing some would hide that any number is accepted")
	}
}

func TestTheValueRowsMarkWhatIsInForceAndWhatIsFactory(t *testing.T) {
	setWith(t, "status.narrow", "hide")
	rows := settingsRows("status.narrow", autoCompactSetting{}, 0, nil, nil)
	var cur, factory []string
	for _, r := range rows {
		if r.Current {
			cur = append(cur, r.Ref)
		}
		if r.Provider == "factory" {
			factory = append(factory, r.Ref)
		}
	}
	if len(cur) != 1 || cur[0] != "hide" {
		t.Errorf("the row in force is %v, want [hide]: the highlight starts on it", cur)
	}
	if len(factory) != 1 || factory[0] != "wrap" {
		t.Errorf("the factory row is %v, want [wrap]", factory)
	}
}

func TestAYesNoWrittenAsOffStillMarksTheNoRow(t *testing.T) {
	setWith(t, "chat.light", "off")
	for _, r := range settingsRows("chat.light", autoCompactSetting{}, 0, nil, nil) {
		if r.Ref == "no" && !r.Current {
			t.Error(`"off" means no (validSettingValue accepts it): with the no row unmarked the menu shows nothing in force`)
		}
		if r.Ref == "yes" && r.Current {
			t.Error(`"off" must not mark the yes row`)
		}
	}
}

func TestTheEffortRowsCarryTheirMeaning(t *testing.T) {
	rows := settingsRows("effort.default", autoCompactSetting{}, 0, nil, nil)
	if hasRef(rows, "off") {
		t.Error(`"off" is not a default effort (validChatLook refuses it); the menu must not offer it`)
	}
	for _, r := range rows {
		if r.Ref == "high" && !strings.Contains(r.Provider, strings.TrimSpace(effortHints["high"])) {
			t.Errorf("the high row lost its meaning: %q", r.Provider)
		}
	}
}

func TestPickingAFreeNumberTellsWhatToType(t *testing.T) {
	if n := settingPickNotice("scroll.wheel_step"); !strings.Contains(n, "scroll.wheel_step") || !strings.Contains(n, "factory") {
		t.Errorf("a bare `/settings scroll.wheel_step ` does not say what is accepted; the notice must: %q", n)
	}
	if n := settingPickNotice("chat.light"); n != "" {
		t.Errorf("a setting with its own list of values needs no notice, its rows say it: %q", n)
	}
	if got := settingPickLine("chat.light"); got != "/settings chat.light " {
		t.Errorf("the pick leaves %q in the input", got)
	}
}

// ---- through the real loop ------------------------------------------------------------

// frameShowing is the last frame that still has the typed line on screen. The frame
// drawn after the exit keys is the one with the menu already closed, so asking for the
// last frame of the run would look at a menu that is no longer there.
func frameShowing(out, line string) string {
	frames := strings.Split(out, frameBegin)
	for i := len(frames) - 1; i >= 0; i-- {
		if f := ansi.Strip(frames[i]); strings.Contains(f, line) {
			return f
		}
	}
	return ""
}

// menuRun types a line, presses the given keys and leaves, with the default scene (the
// one whose changes are kept). It returns the frame that showed the line, and what
// reached the model.
func menuRun(t *testing.T, line string, keys ...scheduledEvent) (string, []string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed(line), keys...)
	script = append(script, exitKeys()...)
	out, sub := runLoopDefault(t, 110, 40, script)
	f := frameShowing(out, line)
	if f == "" {
		t.Fatalf("no frame ever showed %q:\n%s", line, lastFrame(out))
	}
	return f, sub
}

func TestLoopBareSettingsListsTheSettingsWithTheirValues(t *testing.T) {
	frame, sub := menuRun(t, "/settings ")
	if len(sub) != 0 {
		t.Errorf("opening the menu reached the model: %q", sub)
	}
	if !strings.Contains(frame, "scroll.wheel_step") || !strings.Contains(frame, `"auto"`) {
		t.Errorf("the menu does not show a setting with its value:\n%s", frame)
	}
}

func TestLoopTypingNarrowsTheSettingsMenu(t *testing.T) {
	frame, _ := menuRun(t, "/settings paste")
	if !strings.Contains(frame, "paste.fold_lines") || strings.Contains(frame, "scroll.wheel_step") {
		t.Errorf("the filter did not narrow the menu to the paste settings:\n%s", frame)
	}
}

func TestLoopPickingASettingOpensItsValuesAndPickingOneChangesIt(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	// `/settings chat.scrollbar` typed whole is the typed surface's (it shows the setting);
	// pick from the menu instead: narrow with a fragment of the name, then Enter.
	script := append(typed("/settings scrollb"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{80 * time.Millisecond, keyEvent('n')},
		scheduledEvent{80 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	out, sub := runLoopDefault(t, 110, 40, script)
	if len(sub) != 0 {
		t.Errorf("the menu reached the model: %q", sub)
	}
	if !strings.Contains(stripANSI(out), "chat.scrollbar is now") {
		t.Errorf("the pick was not confirmed on screen:\n%s", lastFrame(out))
	}
	if b := readTexts(t, cfg); !strings.Contains(b, "chat.scrollbar") || !strings.Contains(b, `"no"`) {
		t.Errorf("the change is not in texts.json: %s", b)
	}
}

func TestLoopTheValueStageShowsTheValuesOfThatSetting(t *testing.T) {
	frame, _ := menuRun(t, "/settings status.narrow ")
	if !strings.Contains(frame, "wrap") || !strings.Contains(frame, "hide") || strings.Contains(frame, "scroll.wheel_step") {
		t.Errorf("the second stage must list wrap and hide and nothing else:\n%s", frame)
	}
}

func TestLoopAnAutocompactRowStillPicksAutocompact(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("/settings 70"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	runLoopDefault(t, 110, 40, script)
	if got := loadAutoCompact(settingsPath()); !got.On || got.Percent != 70 {
		t.Fatalf("the autocompact row did not pick autocompact any more: %+v", got)
	}
	if b := readTexts(t, cfg); strings.Contains(b, "autocompact") {
		t.Errorf("autocompact is kept in settings.json, not texts.json: %s", b)
	}
}

func TestLoopAPickedValueThatTheRuleRefusesChangesNothing(t *testing.T) {
	// A closed set the rule rejects is caught by TestEveryClosedSetOfValues...; this is the
	// other half: a typed value that is not a row still reaches the typed surface's refusal.
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	script := append(enterLine("/settings chat.light maybe"), exitKeys()...)
	out, sub := runLoopDefault(t, 110, 40, script)
	if len(sub) != 0 {
		t.Errorf("a refused value reached the model: %q", sub)
	}
	if !strings.Contains(stripANSI(out), "yes or no") {
		t.Errorf("the refusal did not say what is accepted:\n%s", lastFrame(out))
	}
	if b := readTexts(t, cfg); strings.Contains(b, "chat.light") {
		t.Errorf("a refused value was written: %s", b)
	}
}

// `/settings <name> -` puts the factory value back, and it reaches the typed surface only
// because no row's value contains a hyphen, so the fragment "-" matches no row and the menu
// steps aside. A value with a hyphen would turn that line into a menu that swallows the Enter.
func TestNoOfferedValueContainsAHyphenSoTheFactoryDashStaysTheTypedLines(t *testing.T) {
	for _, k := range settingsKeys {
		for _, c := range settingChoices(k) {
			if strings.Contains(c, "-") {
				t.Errorf("%s offers %q: `/settings %s -` would match it and open the menu instead of restoring the factory value; give the menu an explicit case for \"-\" first", k, c, k)
			}
		}
	}
	if _, _, _, open := settingsMenuStage("/settings chat.light -"); open {
		t.Error("`/settings chat.light -` must be the typed surface's: it restores the factory value")
	}
}

// ---- hiding parts of the screen ---------------------------------------------------------

func defaultDoc(t *testing.T) *scene.Document {
	t.Helper()
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func partIDs(doc *scene.Document) []string {
	var ids []string
	for _, p := range hidableParts(doc) {
		ids = append(ids, p.id)
	}
	return ids
}

func idListHas(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// The menu is one Enter away from a mistake that a typed command is not, so it must not
// offer the two things whose hiding the user cannot undo from the screen they are left with.
func TestTheMenuNeverOffersTheInputOrTheMenusToHide(t *testing.T) {
	ids := partIDs(defaultDoc(t))
	for _, never := range []string{"prompt", "menu", "cmds", "model_menu", "models"} {
		if idListHas(ids, never) {
			t.Errorf("%q is offered to hide: hiding it leaves a screen the user cannot type /ui show into, or hides the menu they are using; offered: %v", never, ids)
		}
	}
	for _, want := range []string{"banner", "chat", "status", "where", "shells", "panels"} {
		if !idListHas(ids, want) {
			t.Errorf("%q is not offered to hide although the factory screen has it; offered: %v", want, ids)
		}
	}
}

func TestAContainerOfTheInputIsNotOfferedEither(t *testing.T) {
	// The rule reads structure, not names: a scene of the user's own may wrap the input in a
	// node of any id, and hiding that wrapper hides the input just the same.
	doc, err := scene.ParseDocument([]byte(`{"root":{"type":"stack","children":[
		{"id":"wrap","type":"stack","children":[{"id":"typing","type":"input","bind":"user.input"}]},
		{"id":"log","type":"markdown","bind":"chat.history"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	ids := partIDs(doc)
	if idListHas(ids, "wrap") || idListHas(ids, "typing") {
		t.Errorf("the input or what contains it is offered to hide: %v", ids)
	}
	if !idListHas(ids, "log") {
		t.Errorf("an ordinary node is not offered: %v", ids)
	}
}

func TestTogglingAPartHidesItThenShowsItAndSaysWhich(t *testing.T) {
	doc := defaultDoc(t)
	hidden := map[string]bool{}
	if n := toggleHidden(doc, hidden, "banner"); !hidden["banner"] || !strings.Contains(n, "hidden") {
		t.Errorf("first pick: hidden=%v notice=%q; want banner hidden and said so", hidden, n)
	}
	if n := toggleHidden(doc, hidden, "banner"); hidden["banner"] || !strings.Contains(n, "shown again") {
		t.Errorf("second pick: hidden=%v notice=%q; want banner shown and said so", hidden, n)
	}
}

func TestAStaleRowCannotHideTheInput(t *testing.T) {
	hidden := map[string]bool{}
	n := toggleHidden(defaultDoc(t), hidden, "prompt")
	if hidden["prompt"] || !strings.Contains(n, "not a part the menu hides") {
		t.Errorf("a pick of the input hid it (hidden=%v) or did not refuse: %q; the check must not trust that the row was ever offered", hidden, n)
	}
}

func TestTheHiddenRowsSayWhichPartsAreHidden(t *testing.T) {
	doc := defaultDoc(t)
	var shown, gone string
	for _, r := range hiddenRows(doc, map[string]bool{"banner": true}) {
		switch r.Ref {
		case "banner":
			gone = r.Provider
		case "chat":
			shown = r.Provider
		}
	}
	if !strings.Contains(gone, "HIDDEN") || strings.Contains(shown, "HIDDEN") {
		t.Errorf("rows do not tell hidden from shown: banner=%q chat=%q", gone, shown)
	}
}

func TestTheHiddenStageStaysOpenEvenWhenTheFilterMatchesNothing(t *testing.T) {
	key, _, filter, open := settingsMenuStage("/settings hidden zzzz")
	if !open || key != settingHiddenStage || filter != "zzzz" {
		t.Errorf("got key=%q filter=%q open=%v: with no typed form to fall to, a closed menu would send the words to the model", key, filter, open)
	}
}

func TestTheFirstStageHasTheHiddenPartsRow(t *testing.T) {
	rows := settingsRows("", autoCompactSetting{}, 0, nil, map[string]bool{"status": true})
	for _, r := range rows {
		if r.Ref == settingHiddenStage {
			if !strings.Contains(r.Provider, "status") {
				t.Errorf("the row does not name what is hidden: %q", r.Provider)
			}
			return
		}
	}
	t.Error("the first stage has no hidden-parts row: the only way to hide a part from the menu is gone")
}

func TestLoopPickingAPartHidesItOnScreenAndPickingAgainBringsItBack(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("/settings hidden banner"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, scheduledEvent{200 * time.Millisecond, enterEvent()}) // the menu stays open on the same row
	script = append(script, exitKeys()...)
	out, sub := runLoopDefault(t, 110, 40, script)
	if len(sub) != 0 {
		t.Errorf("the menu reached the model: %q", sub)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "banner is hidden for this session") {
		t.Errorf("the first pick did not say banner is hidden:\n%s", lastFrame(out))
	}
	if !strings.Contains(plain, "banner is shown again") {
		t.Errorf("the second pick did not bring it back:\n%s", lastFrame(out))
	}
}

func TestLoopHidingAPartDoesNotTouchSavedFiles(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	script := append(typed("/settings hidden banner"), scheduledEvent{40 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	runLoopDefault(t, 110, 40, script)
	if b := readTexts(t, cfg); b != "" {
		t.Errorf("a session-only hide wrote texts.json: %s", b)
	}
}
