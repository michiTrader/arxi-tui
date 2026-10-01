package main

import (
	"fmt"
	"strings"
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
// silent no-match or be submitted to the agent as a prompt.
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
