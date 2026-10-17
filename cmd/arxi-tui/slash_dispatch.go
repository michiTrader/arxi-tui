package main

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// slashKind says what a submitted line is, before anything acts on it.
type slashKind int

const (
	// slashNone: not a slash line. An ordinary prompt.
	slashNone slashKind = iota
	// slashBare: just "/" (with optional spaces). Nothing to run or warn about.
	slashBare
	// slashKnown: a command the host runs (factory, alias or user-defined).
	slashKnown
	// slashPlanned: listed in the menu but with no behaviour behind it yet.
	slashPlanned
	// slashUnknown: not a command at all.
	slashUnknown
)

// slashClass is the verdict of classifySlash.
type slashClass struct {
	Kind slashKind
	// Name is the word after "/", up to the first whitespace, exactly as typed
	// (case-sensitive: /Help is not /help).
	Name string
	// Args is the rest of the line, trimmed.
	Args string
}

// plannedCommands are registered in the menu but have no host behaviour yet.
// Saying so is the honest answer: neither "unknown" (the menu lists it) nor a
// prompt to the model (the user typed a command, not a question). A command
// leaves this set when it gets a handler, and the test that ties this set to
// fold.Commands keeps the answer from naming something the menu does not list.
var plannedCommands = map[string]bool{
	"surface": true,
}

// commandAliases are spellings the host accepts that the menu does not list
// under their own row (hubCommand routes them to the provider hub).
var commandAliases = map[string]bool{
	"providers": true,
	"login":     true,
}

// classifySlash decides what a submitted line is. It is pure: it reads the
// line, the factory registry and the names of the user's commands, and touches
// nothing else, so every path that submits a line (Enter on the input, a menu
// pick, a pressed button) can ask the same question and get the same answer.
//
// A line is a slash line only when its first non-space character is "/" and
// its first word has no further "/" (a line that starts with a path is text).
// The text after the verb is never interpreted here: classification says what
// the verb is, not what to do with the arguments.
func classifySlash(line string, userCommands []string) slashClass {
	body := strings.TrimSpace(line)
	if !strings.HasPrefix(body, "/") {
		return slashClass{Kind: slashNone}
	}
	body = strings.TrimSpace(body[1:])
	if body == "" {
		return slashClass{Kind: slashBare}
	}
	name, args := body, ""
	if i := strings.IndexFunc(body, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }); i >= 0 {
		name, args = body[:i], strings.TrimSpace(body[i:])
	}
	if isPathWord(name) {
		return slashClass{Kind: slashNone}
	}
	c := slashClass{Name: name, Args: args}
	switch {
	case plannedCommands[name]:
		c.Kind = slashPlanned
	case commandAliases[name], isFactoryCommand(name), contains(userCommands, name):
		c.Kind = slashKnown
	default:
		c.Kind = slashUnknown
	}
	return c
}

// isPathWord reports whether the first word after the leading "/" has another
// "/" inside it: "/mnt/c/Users/x" or "/usr/bin/ls". No command name contains a
// slash, so such a line is a path the user is talking about, not a command,
// and it goes to the model as ordinary text. A single word ("/nonsense") stays
// an unknown command: that is a typo the user wants to hear about.
func isPathWord(name string) bool {
	return strings.Contains(name, "/")
}

// slashMenuOpen reports whether the typed input is the start of a command, so
// the slash menu owns the keys. It asks classifySlash, so the menu and the
// submit logic cannot disagree: a line that starts with a path never opens the
// menu, and Enter on it sends it.
func slashMenuOpen(input string) bool {
	return strings.HasPrefix(input, "/") && classifySlash(input, nil).Kind != slashNone
}

func isFactoryCommand(name string) bool {
	for _, c := range fold.Commands {
		if c.Name == name {
			return true
		}
	}
	return false
}

// warning is the local message for a line that must not reach the model, or ""
// when the line needs none. Each says what happened (nothing was sent) and
// what to do next. The name is cleaned because it is the user's own text.
func (c slashClass) warning() string {
	name := cleanWarnText(c.Name)
	switch c.Kind {
	case slashUnknown:
		return "Unknown command /" + name + ". Nothing was sent. Type /help to see the commands, or /send <text> to send this line to the model."
	case slashPlanned:
		return "/" + name + " is not available yet. Nothing was sent."
	}
	return ""
}

// refusesEnter reports whether Enter on this line must be answered with a local
// warning instead of reaching the menu or the model. A planned command always
// is (the user typed its whole name). An unknown one is only when the menu has
// no row for what was typed: with rows, Enter is a menu pick (typing "/mod" and
// Enter chooses the highlighted command), and that pick is the menu's business.
func (c slashClass) refusesEnter(line string) bool {
	switch c.Kind {
	case slashPlanned:
		return true
	case slashUnknown:
		typed := strings.TrimPrefix(strings.TrimSpace(line), "/")
		return len(fold.FilterSlashMatches(typed)) == 0
	}
	return false
}

// userCommandNames lists the names of the commands a behaviour layer adds.
func userCommandNames(b behaviour) []string {
	out := make([]string, 0, len(b.Commands))
	for _, c := range b.Commands {
		out = append(out, c.Name)
	}
	return out
}
