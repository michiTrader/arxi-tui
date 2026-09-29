package main

import (
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the keypress half of the bundle consent prompt (J4), the sibling of
// consent_prompt.go's consentAnswerForKey: it turns a single key pressed while the
// one bundle consent screen (ext.BundleConsentScene) is up into the ext.BundleAnswer
// the fan-out (ext.GrantBundle) consumes. It is kept a pure function of the key, no
// loop state, for the same reason the single-plugin mapping is: the keypress→grant
// decision is security-load-bearing, and a pure function is the one shape a
// counterfactual pins exactly.
//
// # Why a separate function rather than reusing consentAnswerForKey
//
// A single-plugin answer (supervisor.ConsentAnswer) carries the granted capability
// subset, because supervisor.Mount re-checks that subset against the manifest's
// declared and closed sets. A bundle answer (ext.BundleAnswer) carries no capability
// list at all: a bundle is all-or-nothing (DESIGN-BLOCK-J.md — "a scene wired to a
// plugin the user rejected is a scene with dead binds"), so `y`/`r` grant every
// declared capability of every not-yet-remembered plugin and `n` grants nothing,
// and the per-plugin declared sets live in ext.GrantBundle's decisions, never in the
// answer. Mapping the single-plugin answer onto the bundle one and dropping its
// Granted field would say, in code, that the two answers are the same shape when the
// whole point of the bundle screen is that one keystroke fans out to N grants the
// user never itemised. The two mappings agree on the vocabulary (y/r/n/Esc) by
// design; keeping them separate is what lets each pin its own contract.

// bundleAnswerForKey maps a keypress against the bundle consent screen to the user's
// single answer. It returns decided=false for any key that is NOT one of the three
// choices, the same load-bearing default the single-plugin mapping keeps: while the
// screen is up the user is not typing into chat, and an unrelated key must leave the
// prompt standing rather than be read as an answer — "no answer is not a yes" (Q15).
//
// The three answers match exactly what ext.BundleConsentScene draws: [y] grants
// every not-yet-remembered plugin's declared powers for this session, [r] grants
// them and asks the gate to remember each per-plugin row, [n] rejects the whole
// bundle. Escape rejects too, because a screen the user dismisses without choosing
// must not compose the interface — the safe reading of "get me out of here" is no,
// not yes. The match is case-insensitive so a user with caps lock is not silently
// ignored into a non-answer.
//
// Ctrl-C is not this function's concern: the loop checks the panic gesture before
// any per-mode key handling (invariant 6), so the escape hatch is never routed here
// and can never be captured by a consent prompt.
func bundleAnswerForKey(k term.Key) (answer ext.BundleAnswer, decided bool) {
	if k.Type == term.KeyEscape {
		return ext.BundleAnswer{Rejected: true}, true
	}
	if k.Type != term.KeyRunes || len(k.Runes) != 1 {
		return ext.BundleAnswer{}, false
	}
	switch unicode.ToLower(k.Runes[0]) {
	case 'y':
		return ext.BundleAnswer{}, true
	case 'r':
		return ext.BundleAnswer{Remember: true}, true
	case 'n':
		return ext.BundleAnswer{Rejected: true}, true
	default:
		return ext.BundleAnswer{}, false
	}
}
