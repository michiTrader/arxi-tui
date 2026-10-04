package main

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// noLiveCoreNotice is the one sentence for "this process has no arxi core".
const noLiveCoreNotice = "providers need a live arxi core; set ARXI_BIN to an arxi binary and restart " +
	"so the TUI can connect to a core that stores providers, keys and models"

// stripLeadingVerb removes a leading "/word" or "word" prefix from the line and
// reports whether that prefix was the one asked for.
func stripLeadingVerb(line, verb string) (rest string, matched bool) {
	body := strings.TrimSpace(line)
	for _, p := range []string{"/" + verb, verb} {
		if body == p {
			return "", true
		}
		if strings.HasPrefix(body, p+" ") {
			return strings.TrimSpace(body[len(p):]), true
		}
	}
	return "", false
}

// hubCommand recognises the commands that open the provider hub. Everything about
// providers lives in one place, so the old spellings (/login, /model, /provider add
// ...) all open it; /models and /model land on the model picker. Arguments are
// ignored on purpose: an API key typed after a command would be echoed and kept in
// the input buffer, so keys are only ever typed into the hub's masked field.
func hubCommand(line string) (open hubOpen, ok bool) {
	body := strings.TrimSpace(line)
	if !strings.HasPrefix(body, "/") {
		return 0, false
	}
	name := strings.TrimPrefix(body, "/")
	if i := strings.IndexAny(name, " \t"); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "provider", "providers", "login":
		return hubOpenProviders, true
	case "model", "models":
		return hubOpenModels, true
	}
	return 0, false
}

// menuHostCommand resolves the slash menu's highlighted row to the line a host
// command should run, so a menu pick and a typed command take the same path.
func menuHostCommand(input string, sel int) (line string, ok bool) {
	if !strings.HasPrefix(input, "/") {
		return "", false
	}
	matches := fold.FilterSlashMatches(strings.TrimPrefix(input, "/"))
	if len(matches) == 0 {
		return "", false
	}
	if sel < 0 || sel >= len(matches) {
		sel = len(matches) - 1
	}
	name := matches[sel].Name
	if _, isHub := hubCommand("/" + name); isHub {
		return "/" + name, true
	}
	return "", false
}

// clearCommand reports whether Enter on this line (or on the highlighted menu row)
// is `/clear`. It takes no arguments: `/clear now` is not a clear, so a typo can
// never wipe a conversation by accident.
func clearCommand(input string, sel int) bool {
	body := strings.TrimSpace(input)
	if body == "/clear" {
		return true
	}
	if !strings.HasPrefix(body, "/") || strings.ContainsAny(body, " \t") {
		return false
	}
	matches := fold.FilterSlashMatches(strings.TrimPrefix(body, "/"))
	if len(matches) == 0 {
		return false
	}
	if sel < 0 || sel >= len(matches) {
		sel = len(matches) - 1
	}
	return matches[sel].Name == "clear"
}
