package patch_test

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/patch"
)

// TestTheSlashMenuAdvertisesTheVerbsThatExist holds the command registry's
// description of /ui to the surface that implements it.
//
// The registry read "Mutate the scene (add, move, style, plugin)" while the
// surface implements two verbs. That is the accepted-but-not-drawn class this
// repo has now paid for six times, arriving one layer further out than
// before: not a field the validator accepts and the engine ignores, but a
// *capability the chrome advertises and the surface does not have*. It is
// worse in this position than in a document, because the slash menu is the
// only place a user learns what they may type, and they read it at the moment
// of use — so the interface itself invites them into a refusal.
//
// # Why this test lives in package patch_test
//
// The natural home is internal/fold, beside the registry. It cannot go there:
// fold would have to import the patch surface, and a fold that imports the
// mutation layer stops being the pure, host-owned fold ADR-0002 requires —
// the architectural boundary AGENTS.md lists first. An external test package
// here can import both without either production package importing the other,
// so the invariant is checked without the dependency that would break it.
//
// # Why it checks the verbs are named rather than pinning the exact string
//
// Pinning the description would make every wording change a test failure with
// no defect behind it, and a guard that cries wolf is a guard that gets
// deleted. The property that matters is narrower and permanent: every verb
// the surface implements is named in the menu, and no verb the menu names is
// absent from the surface. Both directions, because the drift that motivated
// this was the second one and a one-way check would have missed it.
func TestTheSlashMenuAdvertisesTheVerbsThatExist(t *testing.T) {
	var entry fold.SlashMatch
	for _, c := range fold.Commands {
		if c.Name == "ui" {
			entry = c
			break
		}
	}
	if entry.Name == "" {
		t.Fatal("the command registry has no `ui` entry.\nConsequence: the mutation surface is unreachable from the slash menu, so the only way to discover it is to read the source.\nRemedy: add it to fold.Commands.")
	}

	for _, verb := range patch.Verbs() {
		if !strings.Contains(entry.Description, verb) {
			t.Errorf("the slash menu does not name the implemented verb %q.\n  description: %q\nConsequence: the menu is where a user learns what they may type; a verb that works and is never advertised is a feature nobody finds.\nRemedy: name every patch.Verbs() entry in the description.", verb, entry.Description)
		}
	}

	// The other direction, which is the one that actually drifted. Any word
	// inside the parentheses is read as a verb by the person reading the
	// menu, so every one of them must be real.
	open := strings.Index(entry.Description, "(")
	close := strings.LastIndex(entry.Description, ")")
	if open < 0 || close < open {
		t.Fatalf("the `ui` description must list its verbs in parentheses so this guard can read them.\n  description: %q\nConsequence: without a machine-readable list the menu can drift from the surface again, which is the defect this test exists for.\nRemedy: keep the form \"Mutate the scene (verb, verb)\".", entry.Description)
	}
	implemented := map[string]bool{}
	for _, verb := range patch.Verbs() {
		implemented[verb] = true
	}
	for _, word := range strings.Split(entry.Description[open+1:close], ",") {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if !implemented[word] {
			t.Errorf("the slash menu advertises %q, which the surface does not implement.\n  implemented: %v\nConsequence: the menu is read at the moment of use, so it walks the user into a refusal the interface invited — the accepted-but-not-drawn class, one layer out from the validator.\nRemedy: remove the verb from the description, or implement it; do not describe a plan in the menu.", word, patch.Verbs())
		}
	}
}
