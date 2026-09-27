package main

import (
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the keypress half of the I5 consent prompt: it turns a single key
// pressed while the consent screen (ext.ConsentScene) is up into the
// supervisor.ConsentAnswer that supervisor.Mount's Prompt callback must return.
// It is kept a pure function of the key and the declared set, with no loop state,
// for the same reason focusKey's grammar lives apart from the select loop: the
// mapping from a keypress to a grant is the security-load-bearing decision, and a
// pure function is the one shape a counterfactual can pin exactly.

// consentAnswerForKey maps a keypress against the consent screen to the user's
// answer. It returns decided=false for any key that is NOT one of the screen's
// three choices, which is the load-bearing default: while the modal is up the
// user is not typing into chat, and an unrelated key must leave the prompt
// standing rather than be read as an answer — "no answer is not a yes" (Q15).
//
// The three answers match exactly what ext.ConsentScene draws: [y] grants the
// declared powers for this session, [r] grants them and asks the gate to
// remember, [n] rejects. Escape rejects too, because a consent screen the user
// dismisses without choosing must not spawn the process — the safe reading of
// "get me out of here" is no, not yes. The match is case-insensitive so a user
// with caps lock is not silently ignored into a non-answer.
//
// Granting the WHOLE declared set (not a subset) is deliberate and matches the
// screen: ConsentScene shows the requested capabilities as a single list with one
// y/n/r prompt, offering no per-capability toggle, so the answer is all-or-nothing.
// A declared set that is empty grants an empty set — a powerless plugin the user
// approved, which supervisor.ConsentAnswer keeps distinct from a rejection (the
// first spawns, the second does not).
//
// Ctrl-C is not this function's concern: the loop checks the panic gesture before
// any per-mode key handling (invariant 6), so the escape hatch is never routed
// here and can never be captured by a consent prompt.
func consentAnswerForKey(k term.Key, declared []string) (answer supervisor.ConsentAnswer, decided bool) {
	if k.Type == term.KeyEscape {
		return supervisor.ConsentAnswer{Rejected: true}, true
	}
	if k.Type != term.KeyRunes || len(k.Runes) != 1 {
		return supervisor.ConsentAnswer{}, false
	}
	switch unicode.ToLower(k.Runes[0]) {
	case 'y':
		return supervisor.ConsentAnswer{Granted: append([]string(nil), declared...)}, true
	case 'r':
		return supervisor.ConsentAnswer{Granted: append([]string(nil), declared...), Remember: true}, true
	case 'n':
		return supervisor.ConsentAnswer{Rejected: true}, true
	default:
		return supervisor.ConsentAnswer{}, false
	}
}
