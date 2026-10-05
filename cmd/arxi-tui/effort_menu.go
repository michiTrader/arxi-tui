package main

import (
	"strings"

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

type effortLevel struct{ name, hint string }

var (
	levelMinimal = effortLevel{"minimal", "answer fast, barely think"}
	levelLow     = effortLevel{"low", "a little thinking"}
	levelMedium  = effortLevel{"medium", "balanced"}
	levelHigh    = effortLevel{"high", "think as long as it helps"}
)

// effortLevelsFor lists the levels that mean something for a model reference
// ("provider/model"), in menu order. What a model accepts depends on the model and on
// the wire the provider speaks, and the core only has one way to carry a level
// (`reasoning_effort`, the OpenAI-style field), so the answer is by family:
//
//   - OpenAI's reasoning models (gpt-5 and later, the o-series) take all four.
//   - Claude models think through a token budget, which this core does not send, so
//     there is nothing to choose: no levels.
//   - Everything else that speaks the OpenAI wire (DeepSeek, gateways, local models)
//     takes low / medium / high; "minimal" is an OpenAI-only word.
//
// An empty reference (no model chosen yet) gets the common three.
func effortLevelsFor(modelRef string) []effortLevel {
	m := strings.ToLower(modelRef)
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	switch {
	case strings.Contains(m, "claude"):
		return nil
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		return []effortLevel{levelMinimal, levelLow, levelMedium, levelHigh}
	}
	return []effortLevel{levelLow, levelMedium, levelHigh}
}

// effortAllowed reports whether level is one of the levels the model takes.
func effortAllowed(modelRef, level string) bool {
	for _, l := range effortLevelsFor(modelRef) {
		if l.name == level {
			return true
		}
	}
	return false
}

// effortMenuOpen reports whether the buffer opens the effort menu, and the filter.
func effortMenuOpen(input string) (filter string, open bool) {
	return menuOpen(effortPrefix, input)
}

// effortMenuData builds the menu rows for a model, marking the level in use. The
// highlight starts on it. A model with no levels gets a single row that says so; it
// carries no reference, so picking it changes nothing.
func effortMenuData(modelRef, current string) []fold.ModelMatch {
	levels := effortLevelsFor(modelRef)
	if len(levels) == 0 {
		return []fold.ModelMatch{{Name: "no levels", Provider: "this model does not take a thinking level"}}
	}
	var out []fold.ModelMatch
	for _, l := range levels {
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
