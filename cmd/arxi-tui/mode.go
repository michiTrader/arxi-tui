package main

import (
	"strings"
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is /mode: how much the agent may do on its own. The mode is a host
// setting like the model and the thinking level: it survives /clear, shows first in
// the status bar and is cycled with Shift+Tab.
//
// A mode is nothing but an answer to one question — "may the agent do this kind of
// thing without asking?" — for each kind of tool. The answers use the core's own
// three policies (allow / ask / deny, surface.Policy), so the tool loop can hand a
// mode's answer straight to the core's per-tool policy. The chat has no tools yet, so
// until the tool loop lands a mode changes only what the bar says; the table below is
// the part that is already settled and tested.

// modePrefix is what the input must start with for the mode menu to open.
const modePrefix = "/mode "

// Tool classes a mode decides on. Reading never changes anything; editing changes
// files; running executes a command, which can do anything the user can.
type toolClass string

const (
	classRead toolClass = "read"
	classEdit toolClass = "edit"
	classRun  toolClass = "run"
)

// The core's three policies, spelled as the core spells them.
const (
	policyAllow = "allow"
	policyAsk   = "ask"
	policyDeny  = "deny"
)

// agentMode is one choice of the menu.
type agentMode struct {
	name string
	hint string
	// read, edit and run are the policy for each tool class.
	read, edit, run string
}

// agentModes lists the modes in the order Shift+Tab walks them. The first is the
// default: the safest one that still lets the agent work.
var agentModes = []agentMode{
	{"ask", "asks before editing files or running commands", policyAllow, policyAsk, policyAsk},
	{"auto", "edits files on its own, asks before running commands", policyAllow, policyAllow, policyAsk},
	{"plan", "read-only: looks around and proposes, changes nothing", policyAllow, policyDeny, policyDeny},
	{"full access", "edits and runs commands without asking", policyAllow, policyAllow, policyAllow},
}

// defaultMode is the mode a session starts in.
const defaultMode = "ask"

// modeByName finds a mode by its name, ignoring case and surrounding space.
func modeByName(name string) (agentMode, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, m := range agentModes {
		if m.name == name {
			return m, true
		}
	}
	return agentMode{}, false
}

// policy is what this mode says about a tool class. An unknown class is denied: a
// kind of action nobody classified must not run unasked.
func (m agentMode) policy(c toolClass) string {
	switch c {
	case classRead:
		return m.read
	case classEdit:
		return m.edit
	case classRun:
		return m.run
	}
	return policyDeny
}

// nextMode is the mode after name in Shift+Tab order, wrapping at the end. An
// unknown name starts over from the default.
func nextMode(name string) string {
	for i, m := range agentModes {
		if m.name == name {
			return agentModes[(i+1)%len(agentModes)].name
		}
	}
	return defaultMode
}

// modeMenuOpen reports whether the buffer opens the mode menu, and the filter.
func modeMenuOpen(input string) (filter string, open bool) {
	return menuOpen(modePrefix, input)
}

// modeMenuData builds the menu rows, marking the mode in use.
func modeMenuData(current string) []fold.ModelMatch {
	var out []fold.ModelMatch
	for _, m := range agentModes {
		out = append(out, fold.ModelMatch{Ref: m.name, Name: m.name, Provider: m.hint, Current: m.name == current})
	}
	return out
}

// modeCommand reports whether Enter on this line (or on the highlighted command-menu
// row) is `/mode` with nothing after it: the menu opens instead of the word going to
// the chat.
func modeCommand(input string, sel int, cat string) bool {
	return menuCommand("mode", input, sel, cat)
}

// modeSetter is the optional capability a Driver has when the mode changes what its
// chat turns may do (serveDriver). The mock has no tools.
type modeSetter interface {
	SetMode(name string)
}

// approver is the optional capability a Driver has when a change can wait for the
// user: PendingApproval says one does, Decide answers it.
type approver interface {
	PendingApproval() bool
	Decide(allow bool) bool
}

// approvalKey maps a key pressed while a change waits: y allows it, n or Esc
// declines it. Anything else is not an answer, and "no answer" is never a yes.
func approvalKey(k term.Key) (allow, decided bool) {
	if k.Type == term.KeyEscape {
		return false, true
	}
	if k.Type != term.KeyRunes || len(k.Runes) != 1 {
		return false, false
	}
	switch unicode.ToLower(k.Runes[0]) {
	case 'y':
		return true, true
	case 'n':
		return false, true
	}
	return false, false
}
