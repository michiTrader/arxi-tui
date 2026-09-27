package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// runeKey builds a single-rune key event, the shape the decoder produces for an
// ordinary typed character.
func runeKey(r rune) term.Key {
	return term.Key{Type: term.KeyRunes, Runes: []rune{r}}
}

// declaredCaps is a fixed non-empty declared set for the answer tests: y and r
// must grant exactly it, so the tests can assert the granted subset is the whole
// declared list and nothing invented.
var declaredCaps = []string{"actions.register", "events.emit"}

// sameSet reports whether two capability slices hold the same elements in the
// same order. consentAnswerForKey copies the declared slice verbatim, so order is
// preserved and an exact comparison is the right assertion.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestConsentAnswerGrantsDeclaredSet pins that [y] grants the whole declared set
// for the session and does NOT remember it. If y granted a subset, an approved
// plugin would come up missing powers it asked for and the user said yes to; if it
// set Remember, a one-session yes would silently persist across restarts.
func TestConsentAnswerGrantsDeclaredSet(t *testing.T) {
	ans, decided := consentAnswerForKey(runeKey('y'), declaredCaps)
	if !decided {
		t.Fatal("[y] was not read as a decision; the primary grant key must resolve the prompt, or the user cannot approve a plugin at all.")
	}
	if ans.Rejected {
		t.Fatal("[y] produced a rejection; y grants, it does not reject.")
	}
	if ans.Remember {
		t.Error("[y] set Remember; only [r] persists a grant — a plain y is a session-only yes (Q15).")
	}
	if !sameSet(ans.Granted, declaredCaps) {
		t.Errorf("[y] granted %v; want the whole declared set %v.\n"+
			"consequence: an approved plugin spawns with the wrong powers — fewer than the user saw and agreed to, or more.\n"+
			"remedy: grant exactly the declared set the screen showed.", ans.Granted, declaredCaps)
	}
}

// TestConsentAnswerRemembersOnR pins [r]: grant the declared set AND remember it.
// The remember flag is the whole difference between r and y, and it is what the
// disk store persists — dropping it turns "grant and remember" into a plain grant
// that re-asks next launch.
func TestConsentAnswerRemembersOnR(t *testing.T) {
	ans, decided := consentAnswerForKey(runeKey('r'), declaredCaps)
	if !decided {
		t.Fatal("[r] was not read as a decision.")
	}
	if ans.Rejected {
		t.Fatal("[r] produced a rejection; r grants and remembers.")
	}
	if !ans.Remember {
		t.Errorf("[r] did not set Remember; the persist-across-restart promise (Q15) rides on this flag, so without it r is indistinguishable from y.")
	}
	if !sameSet(ans.Granted, declaredCaps) {
		t.Errorf("[r] granted %v; want the whole declared set %v.", ans.Granted, declaredCaps)
	}
}

// TestConsentAnswerRejectsOnNAndEsc pins that [n] and Escape both reject, and that
// a rejection carries no granted powers. Escape rejecting is the safe reading of a
// dismissed prompt: a screen the user closed without choosing must spawn nothing.
func TestConsentAnswerRejectsOnNAndEsc(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  term.Key
	}{
		{"n", runeKey('n')},
		{"escape", term.Key{Type: term.KeyEscape}},
	} {
		ans, decided := consentAnswerForKey(tc.key, declaredCaps)
		if !decided {
			t.Errorf("%s was not read as a decision; a reject key that leaves the prompt standing traps the user on the consent screen.", tc.name)
		}
		if !ans.Rejected {
			t.Errorf("%s did not reject; consequence: a plugin the user declined (or dismissed) still spawns (Q15).", tc.name)
		}
		if len(ans.Granted) != 0 {
			t.Errorf("%s carried granted powers %v; a rejection grants nothing.", tc.name, ans.Granted)
		}
	}
}

// TestConsentAnswerIsCaseInsensitive pins that capitals answer the same as
// lowercase. A user with caps lock pressing Y must not have it read as a
// non-answer that leaves the prompt hanging.
func TestConsentAnswerIsCaseInsensitive(t *testing.T) {
	for _, tc := range []struct {
		r        rune
		remember bool
	}{{'Y', false}, {'R', true}} {
		ans, decided := consentAnswerForKey(runeKey(tc.r), declaredCaps)
		if !decided || ans.Rejected {
			t.Errorf("%q was not read as a grant; the match must be case-insensitive.", string(tc.r))
			continue
		}
		if ans.Remember != tc.remember {
			t.Errorf("%q set Remember=%v; want %v (uppercase must mean the same as lowercase).", string(tc.r), ans.Remember, tc.remember)
		}
	}
	// N rejects the same as n.
	if ans, decided := consentAnswerForKey(runeKey('N'), declaredCaps); !decided || !ans.Rejected {
		t.Errorf("N did not reject like n (decided=%v rejected=%v).", decided, ans.Rejected)
	}
}

// TestConsentAnswerLeavesPromptStandingForOtherKeys is the "no answer is not a
// yes" property: any key that is not one of the three choices must return
// decided=false, so the modal stays up rather than an unrelated keystroke being
// read as consent. Enter is the dangerous one — it submits everywhere else in the
// loop — so it is tested explicitly alongside an ordinary letter.
func TestConsentAnswerLeavesPromptStandingForOtherKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  term.Key
	}{
		{"unrelated letter x", runeKey('x')},
		{"enter", term.Key{Type: term.KeyEnter}},
		{"tab", term.Key{Type: term.KeyTab}},
		{"space", runeKey(' ')},
	} {
		ans, decided := consentAnswerForKey(tc.key, declaredCaps)
		if decided {
			t.Errorf("%s was read as a decision (answer %+v); an unrelated key must leave the consent prompt standing, or a stray keystroke grants or dismisses a plugin the user never answered (Q15).", tc.name, ans)
		}
	}
}

// TestConsentAnswerGrantsEmptyForNoDeclaredPowers pins the powerless-plugin case:
// y on a manifest that declared nothing grants an empty set and is a DECISION, not
// a rejection. supervisor.ConsentAnswer keeps "run with no powers" (empty Granted,
// not Rejected) distinct from "do not run" (Rejected), and the first must spawn.
func TestConsentAnswerGrantsEmptyForNoDeclaredPowers(t *testing.T) {
	ans, decided := consentAnswerForKey(runeKey('y'), nil)
	if !decided {
		t.Fatal("[y] on a no-powers manifest was not a decision; the user must be able to approve a powerless plugin.")
	}
	if ans.Rejected {
		t.Error("[y] on a no-powers manifest rejected; an empty grant is still a yes — a powerless plugin the user approved must spawn, not be refused.")
	}
	if len(ans.Granted) != 0 {
		t.Errorf("[y] on a no-powers manifest granted %v; a manifest that declared nothing must be granted nothing.", ans.Granted)
	}
}
