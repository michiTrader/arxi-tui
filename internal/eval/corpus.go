// Package eval loads the Phase 2 eval corpus and holds it to the engine.
//
// The corpus is data (docs/EVAL.md), which is what lets it exist before the
// patch surface it will eventually grade. This package is the half that runs
// today: it replays every refusal a case claims against the real validator, so
// the corpus cannot drift from the diagnostics it was written against. PLAN.md
// asks for one harness rather than two — the same pinned scenes feed the
// hostile property tests and the corpus — and this is that seam.
package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Case is one corpus entry: an order in the user's words, the scene it starts
// from, the refusals the repair loop is expected to walk through, and the end
// state that counts as converged.
type Case struct {
	ID    string `json:"id"`
	Order string `json:"order"`
	Base  string `json:"base"`
	// Rationale names the decision the case protects. EVAL.md makes it
	// mandatory: a case that cannot say what it defends is coverage theatre,
	// and LoadAll refuses one that omits it.
	Rationale string `json:"rationale"`
	// Attempts is the repair loop in order: each entry must be refused, with
	// the reason and address the engine actually produces.
	Attempts []Attempt `json:"attempts"`
	// Convergence is the accepted end state.
	Convergence Convergence `json:"convergence"`

	// Path is where the case was loaded from, so a failure can address it.
	Path string `json:"-"`
}

// Attempt is one refused patch on the way to convergence.
type Attempt struct {
	// Note says what miss this attempt represents. It is the sentence a
	// reviewer reads to decide whether the attempt is plausible or invented.
	Note string `json:"note"`
	// Document is a complete scene document, not a patch operation: the
	// operation vocabulary is Phase 2's design work, and a corpus written in
	// its terms would freeze it before the exercise meant to inform it.
	Document json.RawMessage `json:"document"`
	Refused  Refusal         `json:"expect_refused"`
}

// Refusal is what the engine must say about an attempt. It pins a substring of
// the reason and the line, not the whole message: pinning the exact text would
// make every wording improvement a corpus-wide rewrite and the corpus would
// start voting against clearer errors, while pinning nothing would let the
// diagnostic rot into "invalid scene".
//
// Line is counted against the attempt's own document fragment, not the case
// file that contains it (EVAL.md, "What line is counted against"): what Phase 2
// grades is a document the model produced, so the address the corpus pins has
// to be an address into that document.
type Refusal struct {
	Reason string `json:"reason"`
	Line   int    `json:"line"`
	// Kind selects which validator must produce the refusal. Empty (the
	// default) means the bind/parse path; "token" means ValidateTokens.
	//
	// The distinction is not bookkeeping: the two report through different
	// types — bind and syntax refusals are *scene.Error, token refusals are
	// scene.TokenError — so a corpus that only ever ran the bind path would
	// record a scene as "refused" while the engine loads it happily, and the
	// token diagnostics would have no coverage here at all.
	Kind string `json:"kind,omitempty"`
}

// Refusal kinds.
const (
	KindBind  = ""      // the bind/parse validator (the default)
	KindToken = "token" // ValidateTokens against the active theme
)

// Convergence is the end state a case accepts.
type Convergence struct {
	Document json.RawMessage `json:"document"`
	// MustBind names the fields the converged document has to address, so a
	// document that validates by deleting the feature the order asked for is
	// not mistaken for success. This is the check that keeps "it validates"
	// from being the whole bar.
	MustBind []string `json:"must_bind"`
}

// LoadAll reads every case in dir. It refuses a malformed or under-specified
// case rather than skipping it: a corpus that silently drops cases reports a
// pass rate over a set nobody can enumerate.
func LoadAll(dir string) ([]Case, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("%s: no corpus cases found; the eval corpus is Phase 2's gate and an empty corpus passes vacuously", dir)
	}

	cases := make([]Case, 0, len(names))
	seen := make(map[string]string, len(names))
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var c Case
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		c.Path = name

		if c.ID == "" {
			return nil, fmt.Errorf("%s: case has no id", name)
		}
		if prev, dup := seen[c.ID]; dup {
			return nil, fmt.Errorf("%s: duplicate case id %q (also in %s); ids are how a score is attributed to a case", name, c.ID, prev)
		}
		seen[c.ID] = name
		if c.Order == "" {
			return nil, fmt.Errorf("%s: case %q has no order; the order is the input being graded", name, c.ID)
		}
		if c.Base == "" {
			return nil, fmt.Errorf("%s: case %q names no base scene", name, c.ID)
		}
		if c.Rationale == "" {
			return nil, fmt.Errorf("%s: case %q has no rationale; EVAL.md requires each case to name the decision it protects, because a case that cannot say what it defends will be deleted by the first person who finds it inconvenient", name, c.ID)
		}
		if len(c.Convergence.Document) == 0 {
			return nil, fmt.Errorf("%s: case %q has no convergence document; without an accepted end state the case can only measure failure", name, c.ID)
		}
		if len(c.Convergence.MustBind) == 0 {
			return nil, fmt.Errorf("%s: case %q lists no must_bind fields; a converged document that binds nothing would pass by deleting what the order asked for", name, c.ID)
		}
		for i, a := range c.Attempts {
			if len(a.Document) == 0 {
				return nil, fmt.Errorf("%s: case %q attempt %d has no document", name, c.ID, i)
			}
			if a.Refused.Reason == "" {
				return nil, fmt.Errorf("%s: case %q attempt %d expects a refusal with no reason; an attempt that does not say why it fails cannot detect a diagnostic that rotted into \"invalid scene\"", name, c.ID, i)
			}
			if a.Note == "" {
				return nil, fmt.Errorf("%s: case %q attempt %d has no note; the note is what a reviewer reads to judge whether the miss is plausible or invented", name, c.ID, i)
			}
			switch a.Refused.Kind {
			case KindBind, KindToken:
			default:
				return nil, fmt.Errorf("%s: case %q attempt %d has unknown refusal kind %q; a kind the harness does not recognise would silently run the wrong validator", name, c.ID, i, a.Refused.Kind)
			}
		}
		cases = append(cases, c)
	}
	return cases, nil
}
