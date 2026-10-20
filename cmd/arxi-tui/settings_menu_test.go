package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/fold"
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
	rows := settingsRows("", autoCompactSetting{Percent: 80}, 1_000_000)
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
	if len(rows) != len(settingsKeys)+1+len(autoCompactPercents) {
		t.Errorf("got %d rows, want %d settings + off + %d percentages", len(rows), len(settingsKeys), len(autoCompactPercents))
	}
}

func TestARowSaysTheValueAndTheFactoryOneWhenTheyDiffer(t *testing.T) {
	setWith(t)
	for _, r := range settingsRows("", autoCompactSetting{}, 0) {
		if r.Ref == "chat.scrollbar" && (!strings.Contains(r.Provider, `"yes"`) || strings.Contains(r.Provider, "factory")) {
			t.Errorf("a setting at its factory value must show only the value: %q", r.Provider)
		}
	}
	setWith(t, "chat.scrollbar", "no")
	for _, r := range settingsRows("", autoCompactSetting{}, 0) {
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
	rows := settingsRows("status.narrow", autoCompactSetting{}, 0)
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
	for _, r := range settingsRows("chat.light", autoCompactSetting{}, 0) {
		if r.Ref == "no" && !r.Current {
			t.Error(`"off" means no (validSettingValue accepts it): with the no row unmarked the menu shows nothing in force`)
		}
		if r.Ref == "yes" && r.Current {
			t.Error(`"off" must not mark the yes row`)
		}
	}
}

func TestTheEffortRowsCarryTheirMeaning(t *testing.T) {
	rows := settingsRows("effort.default", autoCompactSetting{}, 0)
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
