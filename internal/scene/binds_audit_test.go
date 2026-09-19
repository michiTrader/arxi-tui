package scene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The SCENES <-> BINDS audit. AGENTS.md names this test a Phase-0 decision that
// must be ported rather than invented: it is the direct descendant of
// arxi-sim's TestEveryDeclaredKeyIsDrawn, and it enforces the §4.5 exit
// criterion from both ends.
//
// Two failure modes it exists to catch, both of which had already happened:
//
//  1. The runtime inventory in validate.go disagrees with the signed document.
//     A scene author reads docs/BINDS.md, uses a bind it signs, and the
//     validator refuses it — the document is then a lie, and worse, Phase 2's
//     eval corpus measures the repair loop (order, patch, file:line, retry),
//     so a validator that rejects signed vocabulary trains the model away from
//     the product's own contract.
//
//  2. A bind the goldens use that no row signs. That is the reverse leak:
//     vocabulary entering through the engine instead of through a signature,
//     which is exactly the closed-vocabulary-by-accident failure this project
//     was built to avoid.

// bindRowPattern matches the first cell of a markdown table row in BINDS.md,
// which by the document's own convention is the bind name in backticks.
var bindRowPattern = regexp.MustCompile("^\\|\\s*`([^`]+)`")

// signedRowsFromDocument parses docs/BINDS.md and returns every bind it signs.
// The document is the contract; this function is the only thing that reads it,
// so the test compares the shipped map against the signature rather than
// against a second hand-maintained copy.
func signedRowsFromDocument(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "BINDS.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v — the signed bind inventory is the contract this test audits; without it the validator cannot be checked against anything", path, err)
	}
	signed := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		m := bindRowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		// The plugin namespace is open by construction (ADR-0003): its row
		// documents a shape, not a name the validator can enumerate.
		if strings.HasPrefix(name, "<") {
			continue
		}
		// Table header separators and prose rows are not bind names. Every
		// bind is a dotted path, which is the cheapest reliable filter.
		if !strings.Contains(name, ".") {
			continue
		}
		signed[name] = true
	}
	if len(signed) == 0 {
		t.Fatalf("parsed zero bind rows from %s — the audit would vacuously pass and stop protecting the §4.5 exit criterion; the table format likely changed and bindRowPattern must follow it", path)
	}
	return signed
}

// TestSignedInventoryMatchesDocument is the drift guard. The validator ships a
// Go map because arxi is one static binary and cannot read docs/ off the user's
// disk; that duplication is only safe while this test holds it to the document.
func TestSignedInventoryMatchesDocument(t *testing.T) {
	signed := signedRowsFromDocument(t)

	var unsignedInCode []string
	for bind := range signedBinds {
		if !signed[bind] {
			unsignedInCode = append(unsignedInCode, bind)
		}
	}
	var missingFromCode []string
	for bind := range signed {
		if !signedBinds[bind] {
			missingFromCode = append(missingFromCode, bind)
		}
	}
	sort.Strings(unsignedInCode)
	sort.Strings(missingFromCode)

	if len(unsignedInCode) > 0 {
		t.Errorf("validate.go accepts %d bind(s) that docs/BINDS.md signs nowhere: %v\n"+
			"consequence: a scene may reference vocabulary the product never committed to, which is how a closed vocabulary grows by accident instead of by signature.\n"+
			"remedy: either add a signed row to docs/BINDS.md §4 (naming its source events, update timing and empty-state) or delete the entry from signedBinds.",
			len(unsignedInCode), unsignedInCode)
	}
	if len(missingFromCode) > 0 {
		t.Errorf("docs/BINDS.md signs %d bind(s) the validator rejects: %v\n"+
			"consequence: the document promises vocabulary the engine refuses at load time, so a scene written from the spec fails validation — and Phase 2's eval corpus would teach the model to avoid the binds the product documents.\n"+
			"remedy: add each to signedBinds in internal/scene/validate.go, or retire the row from the document with its own golden mutation.",
			len(missingFromCode), missingFromCode)
	}
}

// goldenScenes is every scene document the repository pins. The audit runs over
// all of them, not a subset: MAXIMUM.json was previously absent from the
// validation test, so the richest scene — the only one binding agent.todos and
// session.tokens_used — was the one scene never checked against the inventory.
var goldenScenes = []string{"RAW.json", "SOARIA.json", "MAXIMUM.json"}

// TestEveryGoldenBindIsSigned is the forward half of the §4.5 exit criterion:
// every bind/when string in every golden scene resolves to a signed row.
func TestEveryGoldenBindIsSigned(t *testing.T) {
	signed := signedRowsFromDocument(t)
	for _, name := range goldenScenes {
		path := filepath.Join("..", "..", "testdata", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		doc, err := ParseDocument(data)
		if err != nil {
			t.Fatalf("%s: ParseDocument: %v", name, err)
		}
		if err := doc.Validate(); err != nil {
			t.Errorf("%s: Validate: %v\n"+
				"consequence: a pinned golden scene no longer loads, so the default interface is unreachable.\n"+
				"remedy: correct the scene, or sign the bind in docs/BINDS.md §4 if the scene is right.", name, err)
		}
		for _, bind := range collectBinds(doc.Root) {
			if !signed[bind] {
				t.Errorf("%s references bind %q, which docs/BINDS.md signs nowhere\n"+
					"consequence: the goldens would pin vocabulary that entered through the engine rather than through a signature (§4.5: \"No bind is invented by the engine implementation\").\n"+
					"remedy: sign the bind with a row in §4 naming its source events, update timing and empty-state.", name, bind)
			}
		}
	}
}

// TestSignedBindUnusedByAnyGoldenWarns is the reverse half, and it is a warning
// by design (AGENTS.md: "a signed row no scene uses is a warning"). A signed
// bind with no scene exercising it is not a defect — the inventory was frozen
// before Phase 1's goldens on purpose, and phases still to come bind the rest.
// It is logged so the unexercised surface is a known number rather than a
// discovery made when a later phase first binds one of these and finds the fold
// never computed it.
func TestSignedBindUnusedByAnyGoldenWarns(t *testing.T) {
	signed := signedRowsFromDocument(t)
	used := make(map[string]bool)
	for _, name := range goldenScenes {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		doc, err := ParseDocument(data)
		if err != nil {
			t.Fatalf("%s: ParseDocument: %v", name, err)
		}
		for _, bind := range collectBinds(doc.Root) {
			used[bind] = true
		}
	}
	var unused []string
	for bind := range signed {
		if !used[bind] {
			unused = append(unused, bind)
		}
	}
	sort.Strings(unused)
	t.Logf("%d of %d signed binds are not exercised by any golden scene: %v",
		len(unused), len(signed), unused)
}

// collectBinds walks a scene subtree and returns every bind path it addresses,
// from both `bind` fields and the leading path of a `when` condition. It mirrors
// validateBinds' traversal — children, prefix, suffix and row template — so a
// bind hidden in a template cannot escape the audit.
func collectBinds(n *Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	if n.Bind != "" {
		out = append(out, n.Bind)
	}
	if n.When != "" {
		if parts := strings.Fields(n.When); len(parts) > 0 && parts[0] != "" {
			out = append(out, parts[0])
		}
	}
	for _, c := range n.Children {
		out = append(out, collectBinds(c)...)
	}
	out = append(out, collectBinds(n.PrefixNode())...)
	out = append(out, collectBinds(n.Suffix)...)
	out = append(out, collectBinds(n.RowTemplate)...)
	return out
}

// TestSignedBindsExportsExactlyWhatTheValidatorEnforces guards the accessor
// Phase 2's runner reads to build the model's vocabulary list.
//
// The failure this catches is quiet and expensive. If SignedBinds ever reports
// a bind the validator rejects, the runner tells the model that bind is
// available, the model uses it, and the engine refuses the patch — recorded as
// the model failing the case. If it omits a bind the validator accepts, the
// model never reaches for it and the corpus scores a vocabulary gap the
// product does not have. Either way the eval reports a model deficiency that
// is really a prompt defect, which is the hardest class of result to
// disbelieve, because the numbers look like evidence.
//
// Comparing against signedBinds rather than the document is deliberate: the
// document is already audited above, and what the runner must not diverge from
// is the thing that actually refuses patches.
func TestSignedBindsExportsExactlyWhatTheValidatorEnforces(t *testing.T) {
	exported := SignedBinds()

	inExport := make(map[string]bool, len(exported))
	for _, b := range exported {
		inExport[b] = true
	}

	var missing []string
	for bind := range signedBinds {
		if !inExport[bind] {
			missing = append(missing, bind)
		}
	}
	var extra []string
	for _, bind := range exported {
		if !signedBinds[bind] {
			extra = append(extra, bind)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 {
		t.Errorf("SignedBinds omits %d bind(s) the validator accepts: %v\n"+
			"consequence: Phase 2's runner builds the model's vocabulary from this list, so an omitted bind is one the model is told does not exist; it will avoid signed vocabulary and the corpus will record that as a model failure rather than a prompt defect.\n"+
			"remedy: SignedBinds must enumerate signedBinds in full.", len(missing), missing)
	}
	if len(extra) > 0 {
		t.Errorf("SignedBinds reports %d bind(s) the validator rejects: %v\n"+
			"consequence: the runner would offer the model vocabulary the engine refuses at load time, producing refusals the case never predicted and scoring them against the model.\n"+
			"remedy: SignedBinds must not add names of its own.", len(extra), extra)
	}

	// Sorted order is part of the contract, not a convenience: the list goes
	// into a model prompt, and an unstable order would change the prompt
	// between runs, making turns-to-convergence irreproducible for reasons
	// that have nothing to do with the model.
	if !sort.StringsAreSorted(exported) {
		t.Errorf("SignedBinds returned an unsorted list: %v\n"+
			"consequence: the inventory is rendered into the model prompt, so an unstable order silently changes the input between runs and makes a score difference unattributable.\n"+
			"remedy: sort before returning.", exported)
	}

	// The accessor must not hand out a window into the validator's state.
	if len(exported) > 0 {
		exported[0] = "mutated.by.caller"
		again := SignedBinds()
		if again[0] == "mutated.by.caller" {
			t.Errorf("a caller mutated the inventory through the slice SignedBinds returned\n" +
				"consequence: any consumer could silently widen or narrow the vocabulary the validator enforces for the rest of the process.\n" +
				"remedy: build a fresh slice on each call.")
		}
	}
}
