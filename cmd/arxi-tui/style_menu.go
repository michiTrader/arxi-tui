package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// This file is /style: how your own messages are drawn in the conversation. It is a
// view setting like the scroll position, so it never reaches the model or the log; it
// is kept in the settings folder so a choice made once is still there next time.
//
//	bar    a ┃ marker down the left edge of your message (the default)
//	band   the same marker over a shaded block that runs to the right edge
//	plain  no marker, just brighter text

// stylePrefix is what the input must start with for the style menu to open.
const stylePrefix = "/style "

type promptStyleOption struct{ name, hint string }

var promptStyles = []promptStyleOption{
	{engine.PromptBar, "a ┃ marker on every row of your message"},
	{engine.PromptBand, "your message on a shaded block"},
	{engine.PromptPlain, "no marker, only brighter text"},
}

// normalizePromptStyle maps anything unknown (a hand-edited file, an older version) to
// the default instead of failing: a cosmetic setting must never stop the program.
func normalizePromptStyle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, o := range promptStyles {
		if o.name == s {
			return s
		}
	}
	return engine.PromptBar
}

// stylePath is the file the choice is kept in, "" when there is nowhere to keep it.
func stylePath() string {
	d := configDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "style")
}

// loadPromptStyle reads the saved choice; a missing file is the default.
func loadPromptStyle(path string) string {
	if path == "" {
		return engine.PromptBar
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return engine.PromptBar
	}
	return normalizePromptStyle(string(b))
}

// savePromptStyle keeps the choice. The default removes the file, so "never chose" and
// "chose the default" are the same state on disk.
func savePromptStyle(path, style string) error {
	if path == "" {
		return errNoConfigDir
	}
	style = normalizePromptStyle(style)
	if style == engine.PromptBar {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(style+"\n"), 0o600)
}

// styleMenuOpen reports whether the buffer opens the style menu, and the filter.
func styleMenuOpen(input string) (filter string, open bool) {
	return menuOpen(stylePrefix, input)
}

// styleMenuData builds the menu rows, marking the style in use.
func styleMenuData(current string) []fold.ModelMatch {
	var out []fold.ModelMatch
	for _, o := range promptStyles {
		out = append(out, fold.ModelMatch{Ref: o.name, Name: o.name, Provider: uiText("style." + o.name), Current: o.name == current})
	}
	return out
}

// styleCommand reports whether Enter on this line (or on the highlighted command-menu
// row) is `/style` with nothing after it: the menu opens instead of the word going to
// the chat.
func styleCommand(input string, sel int, cat string) bool {
	return menuCommand("style", input, sel, cat)
}
