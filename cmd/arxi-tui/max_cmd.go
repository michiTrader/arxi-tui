package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the host-level grammar for `/max [pane]`, the command that writes
// the host-owned `ui.max` interface state (BINDS.md §4.3, Q21). It is the write
// half of Scene 10 DASHBOARD: the engine already reads ui.max through ui.max.none
// (the grid gate) and the ui.max.is.<id> family (each maximized pane), and this
// turns a press of a pane's `cmd:/max <pane>` button — or the same line typed —
// into the state those gates read.
//
// Like the plugin and provider verbs it is host-owned, not a patch: ui.max is a
// view-state cursor the host holds across frames (the uiFocus model, press.go),
// not a document mutation, so the pure patch surface is the wrong home. One pure
// parser serves both the on_press path (dispatchPress) and the typed path (the
// loop's dispatch chain), so a button and a keystroke cannot diverge on what
// `/max chat` means — the same single-source discipline uiCommandKey keeps.

// parseMax recognizes `/max [pane]` (and the bare `max [pane]` the on_press
// surface produces once the cmd: prefix is stripped).
//
// pane is the logical pane id to maximize, matched by the scene's
// `ui.max.is.<pane>` gates; the empty string means **restore** — clear ui.max so
// the grid returns. `/max` with no argument is therefore the restore gesture the
// maximized view's `cmd:/max` button uses, not a mistake.
//
// matched reports whether the line IS a `/max` invocation. A matched line with
// more than one argument returns matched=true with a located refusal, so the
// host names the mistake rather than letting it fall through to the slash menu's
// silent no-match or a warning that it is unknown.
//
// The pane id is NOT validated against the scene's node ids, and that is a
// decision rather than an omission: ui.max carries a *logical* pane name the
// author chose for the `ui.max.is.<pane>` gate (Scene 10 uses "chat", not the
// node id "pane-chat"), so the host has no node to check it against. A typo sets
// a pane no gate matches, which the grid-hidden/no-pane-shown frame makes
// visible — a scene-authoring error, not one the host can catch here.
func parseMax(line string) (pane string, matched bool, err error) {
	rest, ok := stripLeadingVerb(line, "max")
	if !ok {
		return "", false, nil
	}
	fields := strings.Fields(rest)
	switch len(fields) {
	case 0:
		// `/max` alone restores: clear ui.max, the grid returns.
		return "", true, nil
	case 1:
		return fields[0], true, nil
	default:
		return "", true, fmt.Errorf("/max takes at most one pane id: /max <pane> to maximize, or /max alone to restore")
	}
}

// pickUsage is what /pick says when it is misused.
const pickUsage = "/pick <group> <name> chooses a tab or option; /pick <group> clears the group's choice"

// applyPick runs `/pick <group> <name>` (or the bare `pick ...` an on_press yields) on the
// choices the host holds. matched reports whether the line IS a /pick; notice is the refusal
// to show, or "". It is the one door for a typed line and a pressed button, as parseMax is for
// /max, so the two cannot differ.
//
// Each group has a selector of its own (ui.pick.is.<group>.<name>), which is what lets two
// tab sets live on one screen: /max has a single slot, so a second tab set (or a maximized
// pane beside a tab set) would overwrite the first's choice.
func applyPick(picks map[string]string, line string) (matched bool, notice string) {
	rest, ok := stripLeadingVerb(line, "pick")
	if !ok {
		return false, ""
	}
	if picks == nil {
		return true, "/pick: this surface holds no choices to write"
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 || len(fields) > 2 {
		return true, pickUsage
	}
	if !scene.ValidPickWord(fields[0]) {
		return true, fmt.Sprintf("/pick: %q is not a group name (use a-z, 0-9, '_' and '-'). %s", fields[0], pickUsage)
	}
	if len(fields) == 1 {
		delete(picks, fields[0])
		return true, ""
	}
	if !scene.ValidPickWord(fields[1]) {
		return true, fmt.Sprintf("/pick: %q is not a choice name (use a-z, 0-9, '_' and '-'). %s", fields[1], pickUsage)
	}
	picks[fields[0]] = fields[1]
	return true, ""
}

// applyPickOnEnter is applyPick for the typed path: it acts only on Enter, so a line still
// being typed is left alone.
func applyPickOnEnter(k term.Key, picks map[string]string, input string) (bool, string) {
	if k.Type != term.KeyEnter {
		return false, ""
	}
	return applyPick(picks, input)
}
