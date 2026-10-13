package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

func TestAutoCompactDefaultsToOff(t *testing.T) {
	got := loadAutoCompact(filepath.Join(t.TempDir(), "nothing.json"))
	if got.On || got.Percent != defaultAutoCompactPercent {
		t.Fatalf("no file: %+v, want off at %d%%", got, defaultAutoCompactPercent)
	}
	if got := loadAutoCompact(""); got.On {
		t.Fatalf("no settings folder: %+v, want off", got)
	}
}

func TestAutoCompactSurvivesARestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "settings.json")
	if err := saveAutoCompact(p, autoCompactSetting{On: true, Percent: 70}); err != nil {
		t.Fatal(err)
	}
	if got := loadAutoCompact(p); !got.On || got.Percent != 70 {
		t.Fatalf("reloaded %+v, want on at 70", got)
	}
}

func TestAutoCompactIgnoresABrokenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte("{not json"), 0o644)
	if got := loadAutoCompact(p); got.On {
		t.Fatalf("broken file turned autocompact on: %+v", got)
	}
	// A hand-edited absurd percentage is pulled into range, not trusted.
	os.WriteFile(p, []byte(`{"autocompact":{"on":true,"percent":9999}}`), 0o644)
	if got := loadAutoCompact(p); got.Percent != maxAutoCompactPercent {
		t.Fatalf("percent %d, want it clamped to %d", got.Percent, maxAutoCompactPercent)
	}
}

func TestSettingsPickChangesTheSetting(t *testing.T) {
	cur := autoCompactSetting{Percent: 80}
	on := settingsAfterPick(cur, settingRef(60))
	if !on.On || on.Percent != 60 {
		t.Fatalf("picked 60: %+v", on)
	}
	off := settingsAfterPick(on, settingOff)
	if off.On || off.Percent != 60 {
		t.Fatalf("picked off: %+v, want off and the percentage kept", off)
	}
	if got := settingsAfterPick(on, "nonsense"); got != on {
		t.Fatalf("an unknown pick changed the setting: %+v", got)
	}
}

func TestSettingsMenuMarksTheRowInForce(t *testing.T) {
	cur := func(rows []fold.ModelMatch) string {
		for _, r := range rows {
			if r.Current {
				return r.Ref
			}
		}
		return ""
	}
	if got := cur(settingsMenuData(autoCompactSetting{Percent: 80}, 1_000_000)); got != settingOff {
		t.Errorf("off: current row %q", got)
	}
	if got := cur(settingsMenuData(autoCompactSetting{On: true, Percent: 70}, 1_000_000)); got != settingRef(70) {
		t.Errorf("on at 70: current row %q", got)
	}
}

func TestSettingsMenuTellsWhenTheWindowIsUnknown(t *testing.T) {
	known := settingsMenuData(autoCompactSetting{}, 1_000_000)[4].Provider // 80%
	if !strings.Contains(known, "800k") {
		t.Errorf("a 1M window at 80%% should say 800k tokens: %q", known)
	}
	unknown := settingsMenuData(autoCompactSetting{}, 0)[4].Provider
	if !strings.Contains(unknown, "unknown") {
		t.Errorf("no window must be said, or the setting silently does nothing: %q", unknown)
	}
	if n := settingsNotice(autoCompactSetting{On: true, Percent: 80}, 0); !strings.Contains(n, "not known") {
		t.Errorf("turning it on with no window must warn: %q", n)
	}
}

func TestSettingsIsAListedCommand(t *testing.T) {
	for _, c := range fold.Commands {
		if c.Name == "settings" {
			if !settingsCommand("/settings", 0, "All") {
				t.Error("`/settings` alone must open the menu")
			}
			return
		}
	}
	t.Fatal("settings is not in the command registry")
}

func TestHubDataFindsTheWindowOfTheDefault(t *testing.T) {
	d := hubData{models: []driver.ModelRow{
		{Provider: "a", ID: "x", ContextWindow: 200_000},
		{Provider: "b", ID: "y", ContextWindow: 1_000_000},
		{Provider: "b", ID: "z"},
	}}
	if got := d.windowOf("b/y"); got != 1_000_000 {
		t.Errorf("b/y window %d", got)
	}
	if got := d.windowOf("b/z"); got != 0 {
		t.Errorf("a model with no window must give 0, got %d", got)
	}
	if got := d.windowOf("nobody/none"); got != 0 {
		t.Errorf("unknown model gave %d", got)
	}
}

func TestStatusUsage(t *testing.T) {
	off := autoCompactSetting{Percent: 80}
	on := autoCompactSetting{On: true, Percent: 80}
	cases := []struct {
		name         string
		ctx, in, out uint64
		window       int
		ac           autoCompactSetting
		want         string
	}{
		{"fresh screen, off: nothing", 0, 0, 0, 1_000_000, off, ""},
		{"fresh screen, on: says so", 0, 0, 0, 1_000_000, on, "autocompact 80%"},
		{"percentage of a known window", 37_000, 929_000, 13_000, 1_000_000, on, "ctx 37k 3% · ↑929k ↓13k · autocompact 80%"},
		{"unknown window: no invented percentage", 37_000, 929_000, 13_000, 0, off, "ctx 37k · ↑929k ↓13k · autocompact off"},
		{"something held never reads 0%", 100, 100, 10, 1_000_000, off, "ctx 100 1% · ↑100 ↓10 · autocompact off"},
	}
	for _, c := range cases {
		if got := statusUsage(c.ctx, c.in, c.out, c.window, c.ac); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func evs(types ...string) []fold.Event {
	var out []fold.Event
	for i, ty := range types {
		e := fold.Event{Type: ty, Seq: int64(i + 1), Actor: "assistant"}
		if ty == "llm.response" {
			e.Payload = map[string]any{"text": "ok", "tokens_in": float64(10), "tokens_out": float64(5), "context_tokens": float64(900_000)}
		}
		out = append(out, e)
	}
	return out
}

func TestAutoCompactDue(t *testing.T) {
	hist := longHistory(10, 1000)
	on := autoCompactSetting{On: true, Percent: 80}
	turn := evs("run.prompt", "llm.response", "agent.turn_done")
	if !autoCompactDue(turn, on, 1_000_000, hist) {
		t.Fatal("90% of the window at an 80% setting must compact")
	}
	if autoCompactDue(turn, autoCompactSetting{Percent: 80}, 1_000_000, hist) {
		t.Error("autocompact off must never compact")
	}
	if autoCompactDue(turn, on, 0, hist) {
		t.Error("an unknown window must not compact: the percentage means nothing")
	}
	if autoCompactDue(turn, on, 2_000_000, hist) {
		t.Error("45% of the window is under the 80% setting")
	}
	if autoCompactDue(evs("run.prompt", "llm.response"), on, 1_000_000, hist) {
		t.Error("the turn is not over yet: nothing to do mid-turn")
	}
	if autoCompactDue(turn, on, 1_000_000, longHistory(2, 100)) {
		t.Error("a short history is not worth a model call, however full the window")
	}
}

// The turn_done that ends a compaction must not start another one, or a summary that
// still sits above the threshold would compact forever.
func TestAutoCompactDoesNotChainOnItsOwnTurnDone(t *testing.T) {
	on := autoCompactSetting{On: true, Percent: 80}
	after := evs("run.prompt", "llm.response", "agent.turn_done", "agent.activated", "chat.compacted", "chat.note", "agent.turn_done")
	if autoCompactDue(after, on, 1_000_000, longHistory(10, 1000)) {
		t.Fatal("a compaction's own turn_done triggered another compaction")
	}
	// ...but the next real answer can trigger one again.
	next := append(after, evs("run.prompt", "llm.response", "agent.turn_done")...)
	if !autoCompactDue(next, on, 1_000_000, longHistory(10, 1000)) {
		t.Fatal("a new answer after a compaction should be able to compact again")
	}
}

// Off is a choice too: it must still be off after a restart, with its percentage.
func TestAutoCompactOffIsRemembered(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	if err := saveAutoCompact(p, autoCompactSetting{On: false, Percent: 60}); err != nil {
		t.Fatal(err)
	}
	if got := loadAutoCompact(p); got.On || got.Percent != 60 {
		t.Fatalf("reloaded %+v, want off at 60", got)
	}
}
