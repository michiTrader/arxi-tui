package eval

import (
	"errors"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// This file is the judge: the one place that decides whether a scene document
// is refused, and whether a refusal matches what a case claimed.
//
// It is package code rather than test code for a reason the corpus depends on.
// The corpus test and the model runner both have to answer exactly the same
// question — "what does the engine say about this document?" — and they answer
// it about documents from different sources: the test asks about the documents
// a human wrote into the case file, the runner asks about the documents the
// model produced. If those two asked through separate copies of this logic,
// the corpus test could be green while the runner scored the identical
// document differently, and the corpus' central promise (that every refusal it
// records is one the engine really produces) would hold only for the half
// nobody is grading. Sharing the judge is what makes the recorded attempts a
// prediction of the runner's behaviour rather than a parallel story about it.
//
// The grader is deliberately ignorant of models, prompts and turns. It maps a
// document to a verdict and nothing else, so the runner can be tested with a
// scripted model and still be exercising the real scoring path.

// Verdict is what the engine said about one document.
//
// Accepted and Addressed are separate booleans rather than an error value
// because the corpus cares about three independent facts, and collapsing them
// loses the one Phase 2 is about: a refusal with no address still refuses, but
// it hands the model nothing to repair from, and invariant 4 calls that a bug.
type Verdict struct {
	// Accepted reports that the engine raised nothing at all.
	Accepted bool
	// Message is the engine's refusal text, empty when accepted.
	Message string
	// Line is the line the refusal addressed, 0 when unaddressed.
	Line int
	// Addressed reports whether the refusal carried a file:line at all.
	// A refusal that is not addressed is the failure invariant 4 names, and
	// the runner has to be able to say so rather than silently scoring the
	// model on a repair it was given no address for.
	Addressed bool
}

// Grade runs the validator that kind selects and reports what it said.
//
// parseErr is threaded in rather than re-derived because the caller already
// had to parse to get a document, and a document that fails to parse never
// reaches either validator — so a parse refusal is the verdict regardless of
// which validator the case named.
//
// The two validators are dispatched here rather than at the call site because
// they differ in shape as well as in type: Validate returns the first error
// and stops, ValidateTokens collects every offender. The corpus wants the same
// three facts either way, and a caller that had to know the difference would
// be a caller that could get it wrong.
func Grade(doc *scene.Document, parseErr error, kind string) Verdict {
	if parseErr != nil {
		return verdictFrom(parseErr)
	}

	switch kind {
	case KindToken:
		errs := scene.ValidateTokens(doc, theme.SOBRIA())
		if len(errs) == 0 {
			return Verdict{Accepted: true}
		}
		// The first offender is the verdict: it is the one a repair turn
		// will be pointed at, so scoring against a later one would grade
		// the model on an address it was never shown.
		first := errs[0]
		return Verdict{
			Message:   first.Error(),
			Line:      first.Loc.Line,
			Addressed: first.Loc.Line > 0,
		}
	default:
		err := doc.Validate()
		if err == nil {
			return Verdict{Accepted: true}
		}
		return verdictFrom(err)
	}
}

// GradeBoth runs both validators and reports the first refusal either one
// raises, bind path first.
//
// The runner needs this and the corpus test does not, and the difference is
// the whole reason it exists as its own function. A case declares which
// validator its attempt is aimed at, because the case author knows what they
// wrote. A model does not: handed an order about greying a footer it may
// equally return a document with an unsigned bind, and grading that document
// only through the token validator would call it converged while the engine
// refuses to load it. Scoring a case as passed on a document the product
// rejects is the worst failure this harness could have, because it is
// indistinguishable from success in the results.
//
// Bind before token matches the engine's own order: Document.Validate is what
// load runs first, so the refusal the user would actually see is the bind one.
func GradeBoth(doc *scene.Document, parseErr error) Verdict {
	if parseErr != nil {
		return verdictFrom(parseErr)
	}
	if v := Grade(doc, nil, KindBind); !v.Accepted {
		return v
	}
	return Grade(doc, nil, KindToken)
}

// verdictFrom turns an engine error into a verdict, preserving the address if
// the error carries one anywhere in its chain.
//
// errors.As rather than a type assertion is the load-bearing detail: the parse
// path wraps, and the whole point of the addressing work was that a %w between
// the caller and the *scene.Error must not cost the position. A plain
// assertion here would silently report Addressed=false for exactly the
// wrapped-error case that motivated the work.
func verdictFrom(err error) Verdict {
	var sceneErr *scene.Error
	if errors.As(err, &sceneErr) {
		return Verdict{
			Message:   sceneErr.Error(),
			Line:      sceneErr.Loc.Line,
			Addressed: sceneErr.Loc.Line > 0,
		}
	}
	return Verdict{Message: err.Error()}
}

// Matches reports whether a verdict is the refusal this Refusal describes.
//
// Reason is a substring test and Line an equality test, which is the pairing
// EVAL.md argues for: pinning the whole message would make every wording
// improvement a corpus-wide rewrite and the corpus would start voting against
// clearer errors, while pinning nothing would let the diagnostic rot into
// "invalid scene".
//
// Line == 0 in a case means the case does not pin an address. That is not the
// same as pinning "unaddressed", and the distinction matters: a case that
// omits the line should not start failing when the engine learns to address
// that refusal, because gaining an address is an improvement.
func (r Refusal) Matches(v Verdict) bool {
	if v.Accepted {
		return false
	}
	if !strings.Contains(v.Message, r.Reason) {
		return false
	}
	if r.Line > 0 && (!v.Addressed || v.Line != r.Line) {
		return false
	}
	return true
}

// CollectBinds returns every bind path a document addresses, in document
// order.
//
// It walks the same arms the validator does — children, prefix, suffix,
// row_template — because a must_bind field satisfied only inside a row
// template is still satisfied, and SCENES.md Q10 makes templates the place
// relative binds live. A walker that skipped templates would fail a converged
// document for binding the field in the one place the format says it belongs.
//
// This is a local walker rather than an export from internal/scene on purpose:
// the corpus is a consumer of the scene package, and widening that package's
// API for a consumer's convenience is how an internal detail becomes a
// contract nobody meant to sign. SignedBinds is exported because the model
// genuinely needs the vocabulary; this walk is the corpus' own business.
func CollectBinds(doc *scene.Document) []string {
	if doc == nil {
		return nil
	}
	var out []string
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil {
			return
		}
		if n.Bind != "" {
			out = append(out, n.Bind)
		}
		if n.When != "" {
			if parts := strings.Fields(n.When); len(parts) > 0 && parts[0] != "" {
				out = append(out, parts[0])
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
		walk(n.PrefixNode())
		walk(n.Suffix)
		walk(n.RowTemplate)
	}
	walk(doc.Root)
	return out
}

// Converged reports whether a document is the accepted end state for this
// case: it must survive both validators, and it must still bind every field
// must_bind names.
//
// The second half is what stops the cheapest way to pass an eval. Deleting the
// node the order asked for also makes a scene validate, and a harness that
// scored "it loads" would record that deletion as a success — teaching the
// next reader that the model can do a job it just refused to do.
func (c Case) Converged(doc *scene.Document, parseErr error) (bool, Verdict, []string) {
	v := GradeBoth(doc, parseErr)
	if !v.Accepted {
		return false, v, nil
	}

	bound := make(map[string]bool)
	for _, b := range CollectBinds(doc) {
		bound[b] = true
	}
	var missing []string
	for _, want := range c.Convergence.MustBind {
		if !bound[want] {
			missing = append(missing, want)
		}
	}
	return len(missing) == 0, v, missing
}
