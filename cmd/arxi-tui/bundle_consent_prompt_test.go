package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// These tests pin bundleAnswerForKey, the security-load-bearing keypress→answer
// mapping of the one bundle consent screen. The concern is the same as the
// single-plugin consentAnswerForKey's: y/r grant, n/Esc reject, and every other key
// leaves the prompt standing so a stray keystroke is never read as consent.

func TestBundleAnswerGrantSessionOnly(t *testing.T) {
	answer, decided := bundleAnswerForKey(runeKey('y'))
	if !decided {
		t.Fatal("'y' must be a decided answer; without it the bundle screen would swallow the one grant key and stand forever")
	}
	if answer.Rejected {
		t.Fatal("'y' is a grant, not a rejection; a rejection would refuse the bundle the user just approved")
	}
	if answer.Remember {
		t.Fatal("'y' grants for this session only; setting Remember would persist a grant the user did not ask to keep ('r' is the remember key)")
	}
}

func TestBundleAnswerGrantAndRemember(t *testing.T) {
	answer, decided := bundleAnswerForKey(runeKey('r'))
	if !decided {
		t.Fatal("'r' must be a decided answer; without it the remember key would leave the prompt standing")
	}
	if answer.Rejected {
		t.Fatal("'r' is a grant-and-remember, not a rejection")
	}
	if !answer.Remember {
		t.Fatal("'r' must ask the gate to remember each per-plugin grant; without Remember the bundle re-prompts every install")
	}
}

func TestBundleAnswerReject(t *testing.T) {
	answer, decided := bundleAnswerForKey(runeKey('n'))
	if !decided {
		t.Fatal("'n' must be a decided answer; without it the reject key would leave the prompt standing")
	}
	if !answer.Rejected {
		t.Fatal("'n' must reject the bundle; a bundle is all-or-nothing, so a non-rejection would grant plugins the user said no to")
	}
	if answer.Remember {
		t.Fatal("a rejection remembers nothing; Remember on a rejection is meaningless and a later install must re-prompt")
	}
}

func TestBundleAnswerEscapeRejects(t *testing.T) {
	answer, decided := bundleAnswerForKey(term.Key{Type: term.KeyEscape})
	if !decided {
		t.Fatal("Escape must be a decided answer; a screen the user dismisses must resolve, not hang")
	}
	if !answer.Rejected {
		t.Fatal("Escape must reject: the safe reading of 'get me out of here' is no, not a grant of every bundle plugin")
	}
}

func TestBundleAnswerIsCaseInsensitive(t *testing.T) {
	for _, r := range []rune{'Y', 'R', 'N'} {
		if _, decided := bundleAnswerForKey(runeKey(r)); !decided {
			t.Fatalf("%q must be decided; a caps-lock user pressing an uppercase choice must be heard, not read as a non-answer", r)
		}
	}
}

func TestBundleAnswerUnrelatedKeyLeavesPromptStanding(t *testing.T) {
	// The counterfactual for the whole file: any key that is not a choice must NOT
	// decide, so the modal keeps the screen up rather than composing (or refusing)
	// the bundle off a keystroke meant for something else. A granting default branch
	// — the mistake this guards — would read 'x' as a yes.
	for _, k := range []term.Key{
		runeKey('x'),
		runeKey('1'),
		runeKey(' '),
		{Type: term.KeyEnter},
		{Type: term.KeyTab},
		{Type: term.KeyBackspace},
	} {
		if answer, decided := bundleAnswerForKey(k); decided {
			t.Fatalf("key %+v was read as a decided answer %+v; an unrelated key must leave the bundle prompt standing (no answer is not a yes, Q15)", k, answer)
		}
	}
}
