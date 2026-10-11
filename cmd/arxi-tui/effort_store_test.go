package main

import (
	"path/filepath"
	"testing"
)

// A model with no pick starts on the preferred level, never on "off", and the fallbacks
// cover models that lack it. Counterfactual: the old code started every model on "".
func TestEveryModelStartsOnAThinkingLevelByDefault(t *testing.T) {
	cases := []struct {
		name   string
		levels []string
		prefer string
		want   string
	}{
		{"the usual three", []string{"low", "medium", "high"}, "medium", "medium"},
		{"claude", []string{"off", "low", "medium", "high", "max"}, "medium", "medium"},
		{"no medium", []string{"low", "high"}, "medium", "high"},
		{"only a switch", []string{"off", "on"}, "medium", "on"},
		{"only off", []string{"off"}, "medium", ""},
		{"takes none", nil, "medium", ""},
		{"user prefers high", []string{"low", "medium", "high"}, "high", "high"},
		{"user prefers a level the model lacks", []string{"low", "medium"}, "xhigh", "medium"},
		{"user prefers none", []string{"low", "medium"}, "none", ""},
		{"off is never a default", []string{"off", "low", "medium"}, "off", "medium"},
		{"deep only", []string{"xhigh", "max"}, "medium", "xhigh"},
	}
	for _, c := range cases {
		if got := defaultEffort(c.levels, c.prefer); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// What the user chose for a model survives a restart, per model, and "none" is a choice.
func TestTheChosenEffortSurvivesARestartPerModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effort.json")
	p := loadEffortPicks(path)
	p.remember("a/fast", "low")
	p.remember("b/deep", "high")
	p.remember("c/quiet", "")
	p.save(path)

	q := loadEffortPicks(path)
	lv := []string{"low", "medium", "high"}
	for ref, want := range map[string]string{"a/fast": "low", "b/deep": "high", "c/quiet": "", "d/new": "medium"} {
		got, applied := effortStart(q, ref, lv, true, "medium")
		if !applied || got != want {
			t.Errorf("%s: got %q (applied %v), want %q", ref, got, applied, want)
		}
	}
}

// A pick the model no longer takes falls back to the default; an unknown model is left alone.
func TestAStalePickFallsBackAndAnUnknownModelIsLeftAlone(t *testing.T) {
	p := effortPick{"a/m": "max"}
	if got, ok := effortStart(p, "a/m", []string{"low", "medium"}, true, "medium"); !ok || got != "medium" {
		t.Errorf("stale pick: got %q %v", got, ok)
	}
	if _, ok := effortStart(p, "a/m", nil, false, "medium"); ok {
		t.Error("the core did not say which levels the model takes; nothing may be applied")
	}
}

func TestABrokenEffortFileIsNoPicks(t *testing.T) {
	if len(loadEffortPicks(filepath.Join(t.TempDir(), "missing.json"))) != 0 {
		t.Error("a missing file must be no picks")
	}
	if len(loadEffortPicks("")) != 0 {
		t.Error("no path must be no picks")
	}
}
