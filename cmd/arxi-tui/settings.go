package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is /settings: the few host settings that are not a command of their own.
// Today that is autocompact, the choice to compact the conversation without being
// asked once it fills a percentage of the model's context window (compact.go).
//
// It is OFF until the user turns it on. Compacting spends a request to the model and
// replaces the older conversation with a summary, so it is not something to do to a
// user who never asked for it.
//
// The choice is kept in settings.json in the settings folder. A broken file is no
// settings at all: it must never stop the program from starting.

// settingsPrefix is what the input must start with for the settings menu to open.
const settingsPrefix = "/settings "

// settingOff is the menu row, and the pick, that switches autocompact off.
const settingOff = "autocompact:off"

// autoCompactPercents are the thresholds the menu offers.
var autoCompactPercents = []int{50, 60, 70, 80, 90}

type settingsFile struct {
	AutoCompact struct {
		On      bool `json:"on"`
		Percent int  `json:"percent"`
	} `json:"autocompact"`
}

func settingsPath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "settings.json")
}

// loadAutoCompact reads the saved choice. Missing or unreadable is the default: off,
// at the default percentage.
func loadAutoCompact(path string) autoCompactSetting {
	out := autoCompactSetting{Percent: defaultAutoCompactPercent}
	if path == "" {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f settingsFile
	if json.Unmarshal(b, &f) != nil {
		return out
	}
	out.On = f.AutoCompact.On
	if f.AutoCompact.Percent > 0 {
		out.Percent = clampPercent(f.AutoCompact.Percent)
	}
	return out
}

// saveAutoCompact writes the choice, best effort: failing to remember is not worth an
// error on screen. It returns the failure so the caller may mention it.
func saveAutoCompact(path string, s autoCompactSetting) error {
	if path == "" {
		return nil
	}
	var f settingsFile
	f.AutoCompact.On, f.AutoCompact.Percent = s.On, clampPercent(s.Percent)
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func settingRef(p int) string { return "autocompact:" + strconv.Itoa(p) }

// settingsMenuData is the menu rows: off, then each percentage. The row in force is
// marked, and the highlight starts on it.
func settingsMenuData(s autoCompactSetting, window int) []fold.ModelMatch {
	var out []fold.ModelMatch
	hint := "never compact on its own; /compact still works"
	out = append(out, fold.ModelMatch{Ref: settingOff, Name: "autocompact off", Provider: hint, Current: !s.On})
	for _, p := range autoCompactPercents {
		h := "compact when the context reaches " + strconv.Itoa(p) + "% of the window"
		if window > 0 {
			h += " (~" + humanTokens(uint64(window)*uint64(p)/100) + " tokens)"
		} else {
			h += " (this model's window is unknown: set it when adding the model, or this does nothing)"
		}
		out = append(out, fold.ModelMatch{Ref: settingRef(p), Name: fmt.Sprintf("autocompact at %d%%", p), Provider: h,
			Current: s.On && clampPercent(s.Percent) == p})
	}
	return out
}

// settingsAfterPick is the setting in force once the user picks a row. Switching off
// keeps the percentage, so switching on again lands where it was left.
func settingsAfterPick(cur autoCompactSetting, pick string) autoCompactSetting {
	if pick == settingOff {
		cur.On = false
		return cur
	}
	if rest, ok := strings.CutPrefix(pick, "autocompact:"); ok {
		if p, err := strconv.Atoi(rest); err == nil {
			return autoCompactSetting{On: true, Percent: clampPercent(p)}
		}
	}
	return cur
}

func settingsMenuOpen(input string) (string, bool) { return menuOpen(settingsPrefix, input) }

func settingsCommand(input string, sel int, cat string) bool {
	return menuCommand("settings", input, sel, cat)
}

// settingsNotice is the line shown after a pick.
func settingsNotice(s autoCompactSetting, window int) string {
	if !s.On {
		return "autocompact off"
	}
	n := fmt.Sprintf("autocompact on at %d%% of the context window", clampPercent(s.Percent))
	if window <= 0 {
		n += ". This model's window is not known, so nothing will compact until it is set (add the model with a context window, or let the provider report it)"
	}
	return n
}

// windowOf is the context window of the model a reference names ("provider/id"), 0
// when the core did not say.
func (d hubData) windowOf(ref string) int {
	for _, m := range d.models {
		if modelRef(m) == ref {
			return m.ContextWindow
		}
	}
	return 0
}
