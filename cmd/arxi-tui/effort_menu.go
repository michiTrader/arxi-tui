package main

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is /effort: the thinking level the next chat request asks the model for.
// It is a host setting like the chat model: it survives /clear, shows in the status
// bar and rides along with every chat.send. The menu is the model menu's twin, with a
// fixed list instead of one read from the core.

// effortPrefix is what the input must start with for the effort menu to open.
const effortPrefix = "/effort "

// effortAuto is the level that sends nothing: the model decides how much to think.
const effortAuto = "auto"

// effortLevels are the choices, in the order the menu lists them, each with the line
// shown beside it.
var effortLevels = []struct{ name, hint string }{
	{effortAuto, "let the model decide"},
	{"minimal", "answer fast, barely think"},
	{"low", "a little thinking"},
	{"medium", "balanced"},
	{"high", "think as long as it helps"},
}

// effortMenuOpen reports whether the buffer opens the effort menu, and the filter.
func effortMenuOpen(input string) (filter string, open bool) {
	return menuOpen(effortPrefix, input)
}

// effortMenuData builds the menu rows, marking the level in use. The highlight starts
// on it.
func effortMenuData(current string) []fold.ModelMatch {
	var out []fold.ModelMatch
	for _, l := range effortLevels {
		out = append(out, fold.ModelMatch{Ref: l.name, Name: l.name, Provider: l.hint, Current: l.name == current})
	}
	return out
}

// effortCommand reports whether Enter on this line (or on the highlighted command-menu
// row) is `/effort` with nothing after it: the menu opens instead of the word going to
// the chat.
func effortCommand(input string, sel int, cat string) bool {
	return menuCommand("effort", input, sel, cat)
}

// setEffortLevel validates a typed level. It accepts the menu's names in any case.
func setEffortLevel(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, l := range effortLevels {
		if l.name == s {
			return s, true
		}
	}
	return "", false
}

// effortSetter is the optional capability a Driver has when its chat turns can carry
// a thinking level (serveDriver). The mock has no model to ask.
type effortSetter interface {
	SetEffort(level string)
}
