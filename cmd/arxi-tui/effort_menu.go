package main

import (
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is /effort: the thinking level the next chat request asks the model for.
// It is a host setting like the chat model: it survives /clear, shows in the status
// bar when set and rides along with every chat.send. The menu is the model menu's
// twin, with a list built from the model in use instead of one read from the core.
//
// There is no "auto" level. Not choosing one is the state where nothing is sent and
// the bar shows nothing; choosing the level already in use again goes back to it.

// effortPrefix is what the input must start with for the effort menu to open.
const effortPrefix = "/effort "

// effortHints is the one-line meaning of each level word, in the menu.
var effortHints = map[string]string{
	"off":     "no thinking, answer straight away",
	"on":      "think, as deep as the model sees fit",
	"minimal": "answer fast, barely think",
	"low":     "a little thinking",
	"medium":  "balanced",
	"high":    "think as long as it helps",
	"xhigh":   "very deep thinking, slower",
	"max":     "the deepest thinking, slowest",
}

// defaultEffortLevels are what a model the core knows nothing about is offered: the
// three depths every OpenAI-style server understands.
var defaultEffortLevels = []string{"low", "medium", "high"}

// effortLevelsFor is the list of levels the chat model takes. What a model takes
// depends on the model (some have `max`, some lack `medium`, some only switch
// thinking off and on), so the core owns the table and sends each model's list with
// `model list`; the menu shows exactly that. known is false when the core did not say
// (an older core, or no model chosen yet), and the common three are offered then.
func effortLevelsFor(levels []string, known bool) []string {
	if !known {
		return defaultEffortLevels
	}
	return levels
}

// effortAllowed reports whether level is one of the levels the model takes.
func effortAllowed(levels []string, known bool, level string) bool {
	for _, l := range effortLevelsFor(levels, known) {
		if l == level {
			return true
		}
	}
	return false
}

// effortLabel is how the status bar names a level: the two switch words read as
// what they do.
func effortLabel(level string) string {
	switch level {
	case "off":
		return "thinking off"
	case "on":
		return "thinking on"
	}
	return level
}

// effortMenuData builds the menu rows for the chat model's levels, marking the level
// in use. The highlight starts on it. A model with no levels gets a single row that
// says so; it carries no reference, so picking it changes nothing.
func effortMenuData(levels []string, known bool, current string) []fold.ModelMatch {
	levels = effortLevelsFor(levels, known)
	if len(levels) == 0 {
		return []fold.ModelMatch{{Name: "no levels", Provider: "this model does not take a thinking level"}}
	}
	var out []fold.ModelMatch
	for _, l := range levels {
		out = append(out, fold.ModelMatch{Ref: l, Name: l, Provider: effortHints[l], Current: l == current})
	}
	return out
}

// effortMenuOpen reports whether the buffer opens the effort menu, and the filter.
func effortMenuOpen(input string) (filter string, open bool) {
	return menuOpen(effortPrefix, input)
}

// effortCommand reports whether Enter on this line (or on the highlighted command-menu
// row) is `/effort` with nothing after it: the menu opens instead of the word going to
// the chat.
func effortCommand(input string, sel int, cat string) bool {
	return menuCommand("effort", input, sel, cat)
}

// effortAfterPick is the level in force once the user picks `pick` while `current` is
// set: picking the level in use again clears it.
func effortAfterPick(current, pick string) string {
	if pick == current {
		return ""
	}
	return pick
}

// effortSetter is the optional capability a Driver has when its chat turns can carry
// a thinking level (serveDriver). The mock has no model to ask.
type effortSetter interface {
	SetEffort(level string)
}
