package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func setWith(t *testing.T, pairs ...string) {
	t.Helper()
	setTexts(t, pairs...)
}

// ---- the three numbers that used to be constants -------------------------------------

func TestTheWheelStepDefaultsAreTheOldOnes(t *testing.T) {
	withPhone(t, false, false)
	if phoneWheelStep() != 3 {
		t.Errorf("off the phone a notch is 3 rows, got %d", phoneWheelStep())
	}
	withPhone(t, true, false)
	if phoneWheelStep() != 1 {
		t.Errorf("on the phone a notch is 1 row, got %d", phoneWheelStep())
	}
}

func TestTheWheelStepIsTheUsersWhenTheyChooseOne(t *testing.T) {
	withPhone(t, false, false)
	setWith(t, "scroll.wheel_step", "7")
	if phoneWheelStep() != 7 {
		t.Errorf("a chosen step of 7 was ignored: %d", phoneWheelStep())
	}
	// "auto" is the old behaviour, on both kinds of terminal.
	setWith(t, "scroll.wheel_step", "auto")
	if phoneWheelStep() != 3 {
		t.Errorf("auto is 3 off the phone: %d", phoneWheelStep())
	}
	// A hand-edited bad value falls back, it never makes the wheel stop or jump 9999 rows.
	setWith(t, "scroll.wheel_step", "9999")
	if phoneWheelStep() != 3 {
		t.Errorf("an out-of-range step must fall back to the default: %d", phoneWheelStep())
	}
}

func TestThePageOverlapIsTheUsersAndTheDefaultIsFour(t *testing.T) {
	if scrollPage(24) != 20 {
		t.Fatalf("the factory page is the screen less 4: %d", scrollPage(24))
	}
	setWith(t, "scroll.page_overlap", "0")
	if scrollPage(24) != 24 {
		t.Errorf("overlap 0 pages the whole screen: %d", scrollPage(24))
	}
	setWith(t, "scroll.page_overlap", "10")
	if scrollPage(24) != 14 {
		t.Errorf("overlap 10 on 24 rows is 14: %d", scrollPage(24))
	}
	if scrollPage(3) != 1 {
		t.Errorf("a tiny screen still pages one row: %d", scrollPage(3))
	}
}

// ---- the same checks as /ui text --------------------------------------------------

func TestTheNewSettingsRefuseWhatMeansNothing(t *testing.T) {
	bad := map[string][]string{
		"scroll.wheel_step":   {"0", "51", "-1", "fast", "2.5"},
		"scroll.page_overlap": {"-1", "21", "auto", "x"},
		"input.max_rows":      {"2", "201", "-4", "tall"},
	}
	for key, vals := range bad {
		for _, v := range vals {
			if _, err := applyTexts(nil, map[string]string{key: v}); err == nil {
				t.Errorf("%s = %q was accepted", key, v)
			}
		}
	}
	good := map[string][]string{
		"scroll.wheel_step":   {"1", "50", "auto", " 5 "},
		"scroll.page_overlap": {"0", "20", "4"},
		"input.max_rows":      {"3", "200", "auto", "10"},
	}
	for key, vals := range good {
		for _, v := range vals {
			if _, err := applyTexts(nil, map[string]string{key: v}); err != nil {
				t.Errorf("%s = %q refused: %v", key, v, err)
			}
		}
	}
}

// Every name the surface lists must be a declared key, so a list can never name
// something /settings cannot change.
func TestEverySettingIsADeclaredKeyWithAnExplanation(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range settingsKeys {
		if seen[k] {
			t.Errorf("%s is listed twice", k)
		}
		seen[k] = true
		d, ok := textIndex[k]
		if !ok {
			t.Errorf("%s is listed as a setting but is not a text key", k)
			continue
		}
		if len(d.role) < 20 {
			t.Errorf("%s has no real explanation: %q", k, d.role)
		}
		if d.paragraph {
			t.Errorf("%s is a paragraph, not a setting", k)
		}
	}
}

// settingNumber is given the key by name, so the guard that checks uiText("…") calls does
// not see it: a typo would read as "auto" for ever. This reads the source for it.
func TestEveryKeyGivenToSettingNumberIsDeclared(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	found := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, line := range strings.Split(string(b), "\n") {
			i := strings.Index(line, `settingNumber("`)
			if i < 0 || strings.HasPrefix(strings.TrimSpace(line), "//") || strings.Contains(line, "func settingNumber") {
				continue
			}
			rest := line[i+len(`settingNumber("`):]
			key := rest[:strings.Index(rest, `"`)]
			found++
			if _, ok := textIndex[key]; !ok {
				t.Errorf("%s reads setting %q, which is not declared", f, key)
			}
		}
	}
	if found < 3 {
		t.Errorf("expected the wheel, the overlap and the input rows to read their settings; found %d", found)
	}
}

// ---- the surface itself ---------------------------------------------------------------

func TestSettingsLineRecognition(t *testing.T) {
	yes := []string{"/settings list", "/settings scroll.wheel_step", "/settings scroll.wheel_step 5",
		"/settings autocompact off", "/settings autocompact 70", "  /settings list  "}
	no := []string{"/settings", "/settings ", "/settings auto", "/settingsx list", "/settings autocompact",
		"/settings nonsense 3", "settings list", "/ui text scroll.wheel_step 5"}
	for _, s := range yes {
		if _, ok := settingsSurfaceLine(s); !ok {
			t.Errorf("%q should be the settings surface's", s)
		}
	}
	for _, s := range no {
		if _, ok := settingsSurfaceLine(s); ok {
			t.Errorf("%q must stay the menu's (or plain text)", s)
		}
	}
}

func TestTheListNamesEverySettingWithItsValueAndWhatItDoes(t *testing.T) {
	setWith(t, "scroll.wheel_step", "9")
	out := runSettingsSurface("list", userTexts{"scroll.wheel_step": "9"}, autoCompactSetting{On: true, Percent: 70}, map[string]bool{"banner": true})
	if out.err != "" || out.wordsChanged || out.acChanged {
		t.Fatalf("a list changes nothing: %+v", out)
	}
	for _, k := range settingsKeys {
		if !strings.Contains(out.note, k) {
			t.Errorf("the list lacks %s", k)
		}
		if !strings.Contains(out.note, firstClause(textIndex[k].role)) {
			t.Errorf("the list does not say what %s does", k)
		}
	}
	for _, want := range []string{`"9"  (factory "auto")`, "on at 70%", "hidden parts", "banner"} {
		if !strings.Contains(out.note, want) {
			t.Errorf("the list lacks %q:\n%s", want, out.note)
		}
	}
}

func TestOneSettingInFullAndItsChange(t *testing.T) {
	d := runSettingsSurface("scroll.page_overlap", nil, autoCompactSetting{}, nil)
	for _, want := range []string{"scroll.page_overlap", `"4" (factory)`, "PgUp/PgDn", "/settings scroll.page_overlap <value>"} {
		if !strings.Contains(d.note, want) {
			t.Errorf("the detail lacks %q:\n%s", want, d.note)
		}
	}
	c := runSettingsSurface("scroll.page_overlap 8", nil, autoCompactSetting{}, nil)
	if !c.wordsChanged || c.words["scroll.page_overlap"] != "8" || !strings.Contains(c.note, "now") {
		t.Errorf("the change did not happen: %+v", c)
	}
	// The same value again is not a change (nothing to save, nothing to undo).
	same := runSettingsSurface("scroll.page_overlap 4", nil, autoCompactSetting{}, nil)
	if same.wordsChanged || same.err != "" {
		t.Errorf("setting the factory value on a factory setting is a no-op: %+v", same)
	}
	// "-" puts the factory value back.
	back := runSettingsSurface("scroll.page_overlap -", userTexts{"scroll.page_overlap": "8"}, autoCompactSetting{}, nil)
	if !back.wordsChanged || len(back.words) != 0 {
		t.Errorf("'-' must remove the user's value: %+v", back)
	}
}

func TestABadValueIsRefusedWithTheReasonAndChangesNothing(t *testing.T) {
	for _, line := range []string{"scroll.wheel_step 99", "scroll.wheel_step fast", "input.max_rows 1", "paste.fold_lines many", "chat.scrollbar maybe"} {
		out := runSettingsSurface(line, nil, autoCompactSetting{}, nil)
		if out.err == "" || out.wordsChanged {
			t.Errorf("%q was accepted: %+v", line, out)
		}
		if !strings.Contains(out.err, "/settings:") {
			t.Errorf("%q: the refusal does not say where it came from: %q", line, out.err)
		}
	}
}

func TestAutocompactThroughTheSurface(t *testing.T) {
	cur := autoCompactSetting{Percent: 80}
	on := runSettingsSurface("autocompact 70", nil, cur, nil)
	if !on.acChanged || !on.ac.On || on.ac.Percent != 70 {
		t.Errorf("autocompact 70: %+v", on)
	}
	off := runSettingsSurface("autocompact off", nil, autoCompactSetting{On: true, Percent: 70}, nil)
	if !off.acChanged || off.ac.On || off.ac.Percent != 70 {
		t.Errorf("off must keep the percentage: %+v", off)
	}
	for _, bad := range []string{"autocompact 5", "autocompact 100", "autocompact soon"} {
		if o := runSettingsSurface(bad, nil, cur, nil); o.err == "" || o.acChanged {
			t.Errorf("%q accepted: %+v", bad, o)
		}
	}
}

// ---- through the real loop ---------------------------------------------------------------

// runLoopSized is runLoopDoc on a screen of the given height: the list of settings is
// longer than 24 rows, so on the usual test screen its first lines scroll out of view.
func runLoopSized(t *testing.T, docJSON string, rows int, script []scheduledEvent) (string, []string) {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(docJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	tty := newFakeTTY(100, rows, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), drv.submitted
}

// runLoopDefault is the loop on the default scene, the one that reads texts.json.
func runLoopDefault(t *testing.T, w, h int, script []scheduledEvent) (string, []string) {
	t.Helper()
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(w, h, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), drv.submitted
}

func TestLoopSettingsListShowsThemAndSendsNothing(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	out, got := runLoopSized(t, factorySobria, 60, append(enterLine("/settings list"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("/settings list reached the model: %q", got)
	}
	f := lastFrame(out)
	plain := stripANSI(f)
	// Every setting is on screen: the list is not cut by the cap on a note's length.
	for _, k := range settingsKeys {
		if !strings.Contains(plain, k) {
			t.Errorf("the list on screen lacks %q:\n%s", k, plain)
		}
	}
	for _, want := range []string{"autocompact", "hidden parts"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the list on screen lacks %q", want)
		}
	}
}

func TestTheListFitsWhatANoteMayHold(t *testing.T) {
	list := settingsListing(nil, autoCompactSetting{}, map[string]bool{"status": true})
	if got := cleanNoteText(list); got != list {
		t.Errorf("a note would cut the list (%d lines, %d chars; cap %d lines, %d chars)",
			strings.Count(list, "\n")+1, len([]rune(list)), localNoteLines, localNoteMax)
	}
}

// A change made with /settings is written to texts.json (the default scene is the one
// whose changes are kept) and a fresh run of the loop behaves by it.
func TestLoopASettingChangeIsKeptInTextsJSON(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, append(enterLine("/settings scroll.page_overlap 9"), exitKeys()...))
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("the change reached the model: %q", drv.submitted)
	}
	b, err := os.ReadFile(filepath.Join(cfg, "texts.json"))
	if err != nil || !strings.Contains(string(b), "scroll.page_overlap") || !strings.Contains(string(b), `"9"`) {
		t.Fatalf("texts.json does not hold the change: %v %s", err, b)
	}
	if !strings.Contains(stripANSI(tty.output()), "scroll.page_overlap is now") {
		t.Errorf("the change was not confirmed on screen")
	}
	// What the next run reads back is the user's value.
	setTexts(t, "scroll.page_overlap", "9")
	if got := scrollPage(24); got != 24-9 {
		t.Errorf("a page after the change is %d rows; want %d", got, 24-9)
	}
}

// input.max_rows reaches the renderer: a cap of 3 shows at most 3 rows of a long line.
func TestLoopInputMaxRowsLimitsTheInputBox(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	if err := os.WriteFile(filepath.Join(cfg, "texts.json"), []byte(`{"input.max_rows":"3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	script := []scheduledEvent{at(10, pasteEvent(longLine()))}
	out, _ := runLoopDefault(t, 80, 24, append(script, exitKeys()...))
	f := rawFrameWith(out, "w200")
	rows := 0
	for _, l := range strings.Split(f, "\n") {
		if strings.Contains(l, "┃ w") {
			rows++
		}
	}
	if rows == 0 || rows > 3 {
		t.Errorf("with input.max_rows=3 the input drew %d rows of the line:\n%s", rows, f)
	}
}

func TestLoopABadSettingIsAWarningAndNothingIsKept(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	out, got := runLoopKeysOutput(t, append(enterLine("/settings scroll.wheel_step 999"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("reached the model: %q", got)
	}
	if !strings.Contains(stripANSI(out), "whole number from 1 to 50") {
		t.Errorf("the refusal was not shown:\n%s", lastFrame(out))
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, "texts.json")); strings.Contains(string(b), "wheel_step") {
		t.Errorf("a refused value was written: %s", b)
	}
}

func TestLoopTheAutocompactMenuStillOpensOnBareSettings(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	if _, open := settingsMenuOpen("/settings "); !open {
		t.Error("the menu must still open on '/settings '")
	}
	if _, open := settingsMenuOpen("/settings list"); open {
		t.Error("the menu must step aside for '/settings list'")
	}
}

func TestTheListSaysWhichPartsAreHiddenAndWhenNone(t *testing.T) {
	none := strings.Join(strings.Fields(settingsListing(nil, autoCompactSetting{}, nil)), " ")
	if !strings.Contains(none, "hidden parts = none") {
		t.Errorf("no hidden parts must read 'none':\n%s", none)
	}
	some := strings.Join(strings.Fields(settingsListing(nil, autoCompactSetting{}, map[string]bool{"status": true, "info": true, "x": false})), " ")
	if !strings.Contains(some, "hidden parts = info, status ") {
		t.Errorf("hidden parts must be listed sorted, without the shown ones:\n%s", some)
	}
}

// The autocompact choice made through the surface is written to settings.json, not only held in memory.
func TestLoopAutocompactThroughTheSurfaceIsSavedInSettingsJSON(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	_, got := runLoopDefault(t, 100, 30, append(enterLine("/settings autocompact 65"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("reached the model: %q", got)
	}
	got2 := loadAutoCompact(settingsPath())
	if !got2.On || got2.Percent != 65 {
		t.Errorf("settings.json holds %+v; want on at 65", got2)
	}
}
