package scene

import (
	"fmt"
	"sort"
	"strings"
)

// signedNodeTypes is the v0 primitive vocabulary docs/SCENES.md settles: every
// node type a scene may legally declare, whether or not this engine builds it
// yet.
//
// # Why this list exists at all
//
// The renderer draws a placeholder for any type it has no case for, and until
// the previous commit that placeholder was a constant string — so it was the
// engine's whole answer to two very different documents. Measured on the tree
// with the suite green:
//
//	{"type": "button"} -> parses, validates clean, 0 warnings, placeholder
//	{"type": "buton"}  -> parses, validates clean, 0 warnings, placeholder
//
// `button` is a settled primitive whose renderer is Phase 3 work, and the
// placeholder is PLAN.md's forward-compatibility rule doing its job. `buton`
// is a mistake, and every layer told its author the document was fine.
//
// Naming the type in the frame was the cheap half and is already done. This is
// the other half. LESSONS.md states the question that separates the two cases,
// and states it as a general rule rather than a case: *could a later engine be
// right about this?* An unknown property may be Scene 4's `reveal` arriving
// before Phase 4 builds it, so it warns and loads. A document with no root is
// refused, because no version of this format draws a treeless scene. A node
// type needs the same question asked, and asking it requires knowing which
// types the format has signed — which is a fact about the format, not about
// the renderer. That is why this lives here and not in internal/engine: a list
// of planned types inside the renderer would be the engine inventing format,
// the objection that keeps row_template and on_press refused.
//
// # Why a written list rather than a derived one
//
// Every other inventory in this package is derived by reflection, and the
// comments in vocabulary.go say why at length: four hand-maintained
// inventories, three drifted. This one cannot be derived, and the reason is
// exactly what makes it useful. The vocabulary must include types that exist
// in *no* Go construct — `button` has no field, no case clause, no struct tag
// to reflect over. Its only existence is in the document. Deriving the list
// from the engine's case labels would make it say "the types we built", which
// is the question the renderer can already answer and not the one being asked.
//
// So it is written, and the drift risk is answered the way signedBinds answers
// it: an audit reads docs/SCENES.md and fails if the two disagree in either
// direction, plus a third direction checking that every type the renderer
// draws is signed. That test is the reason this list is allowed to be
// hand-written.
var signedNodeTypes = map[string]bool{
	// Containers
	"stack":   true,
	"row":     true,
	"box":     true,
	"overlay": true,

	// Content
	"text":      true,
	"markdown":  true,
	"input":     true,
	"spinner":   true,
	"marquee":   true,
	"list":      true,
	"button":    true,
	"switch":    true,
	"slider":    true,
	"sparkline": true,
	"rule":      true,
}

// SignedNodeTypes returns the settled v0 primitive vocabulary, sorted.
//
// Freshly built per call so a caller cannot mutate the inventory by holding
// onto it — the same reason SignedBinds copies, and the same reason
// NodeVocabularyForAudit copies: a test that can edit the contract it checks is
// an escape hatch that ends up a blindfold.
func SignedNodeTypes() []string {
	out := make([]string, 0, len(signedNodeTypes))
	for typ := range signedNodeTypes {
		out = append(out, typ)
	}
	sort.Strings(out)
	return out
}

// IsSignedNodeType reports whether the format has settled on a node type,
// regardless of whether this engine renders it yet.
//
// The two questions are deliberately separate and are asked by different
// layers. "Do I have a case for this?" is the renderer's, and its answer is a
// placeholder. "Could a later version of this engine be right about it?" is
// the format's, and its answer decides whether the author is looking at a
// scheduled gap or at their own typo.
func IsSignedNodeType(typ string) bool {
	return signedNodeTypes[typ]
}

// nearestSignedType returns the signed type within edit distance 2 of the
// given one, or "" when nothing is close enough.
//
// Distance 2 rather than 1 because the transposition that motivated this whole
// change was itself a distance-2 edit: the default scene's goldens were pinned
// under SOARIA for SOBRIA, and 44 references inherited it. A threshold that
// misses the exact class of typo already paid for is a threshold chosen for
// the wrong reason.
//
// It returns nothing rather than a guess when two candidates tie or none is
// close. A suggestion is only worth printing when it is almost certainly
// right; a wrong one sends the author to edit a line that was correct, which
// is worse than the silence it replaced.
func nearestSignedType(typ string) string {
	const maxDistance = 2
	best, bestDist, tied := "", maxDistance+1, false
	for _, candidate := range SignedNodeTypes() {
		d := editDistance(typ, candidate)
		switch {
		case d < bestDist:
			best, bestDist, tied = candidate, d, false
		case d == bestDist:
			tied = true
		}
	}
	if bestDist > maxDistance || tied {
		return ""
	}
	return best
}

// editDistance is Levenshtein distance, counting a transposition as the two
// edits it literally is.
//
// Written out rather than pulled from a dependency because it is twenty lines
// and this module's go.mod is deliberately four direct requirements; a new
// dependency for a suggestion string is a supply-chain surface the feature
// does not earn.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// nodeIsDispatched reports whether the node at this path is one the renderer
// looks up by type.
//
// Not every *Node in the tree is. `prefix` and `suffix` are Nodes structurally
// — they carry text, bind and style — but the marquee reads their fields
// directly and never calls renderNode on them, so they are text-bearing
// fragments rather than nodes with a type. The shipped SOBRIA and MAXIMUM
// scenes both omit the type on exactly these, and renderMarquee's own comment
// says so: "an untyped node with Text set (the sobria prefix omits the type)".
//
// This function exists because the first draft of the unsigned-type warning
// did not have it, and the repository's own guard caught the result
// immediately: TestTheShippedScenesWarnAboutNothing failed on three nodes in
// the two factory scenes. That is the false-alarm direction, which
// borderVocabulary's comment identifies as the one that gets a guard switched
// off — a check that fires on the product's own shipped documents teaches
// people to ignore it long before it ever finds a real defect.
//
// The axis is the position in the tree rather than a property of the node,
// which is the correction this engine has now made twice: `when` used to be
// honoured per container, so the same node hid under a `row` and drew under a
// `stack`. A placeholder is owed by whatever renderNode was asked to draw, so
// asking "does the renderer dispatch on this?" is asking the question the
// placeholder answers.
func nodeIsDispatched(path string) bool {
	// row_template is included deliberately even though nothing renders it
	// yet: it is refused by unrenderedFields with an address, so a document
	// carrying one never reaches a frame, and when Phase 3 builds it the
	// rows will go through renderNode like every other node. Excluding it
	// would be a guard that quietly stops covering a construction on the
	// day that construction starts working.
	return !strings.HasSuffix(path, ".prefix") && !strings.HasSuffix(path, ".suffix")
}

// nestedFormReaders records, for each nested position, which owning node types
// read which *shape* of it. It is the inventory behind warnDroppedNestedForm.
//
// # The defect it exists for
//
// Every nested branch — `prefix`, `suffix`, `children` — is walked by the
// validator on every node: the binds inside it are checked against BINDS.md,
// its tokens are checked against the theme, its keys are checked against the
// node vocabulary. Each is *composed* by a strict subset of the owners.
// Measured, with the whole suite green:
//
//	{"type":"input",  "prefix":{"type":"text","text":"X"}} -> clean, X never drawn
//	{"type":"marquee","prefix":"X "}                        -> clean, X never drawn
//	{"type":"text",   "prefix":{"type":"text","text":"X"}} -> clean, X never drawn
//	{"type":"text",   "suffix":{"type":"text","text":"X"}} -> clean, X never drawn
//	{"type":"text",   "children":[{"type":"text","text":"X"}]} -> clean, X never drawn
//
// Each is the silent drop this package has now paid for five times, and the
// symptom is the usual one: the document is well-formed, every layer reports
// success, and the screen is missing what the author wrote.
//
// # Why each previous fix could not reach the next one
//
// One defect, five recurrences, and the axis is the whole story. `when` was
// once honoured per *container*; then per *node type*; the remedy both times
// was to move the work into renderNode, which withFocusGlow documents as
// making "some node types obey and others do not" unrepresentable — true, and
// quantified over node types. The third recurrence was a nested *position*,
// which no widening of a type switch reaches, and was closed by asking
// hiddenByWhen inside renderMarquee — quantified over one owner. The fourth
// was a nested *shape*: `prefix` is polymorphic, renderInput calls PrefixText
// and renderMarquee calls PrefixNode, so handing either owner the other shape
// lands on a reader that does not exist.
//
// The fourth fix — this map as it was first written — was quantified over the
// *branch*. It enumerated `prefix` and `suffix` because those were the two
// branches the defect had been found in, and `children` is a nested branch by
// exactly the same argument: a child of an owner that does not compose one is
// parsed, validated, warned about for its own misspellings, and never drawn.
// Eleven of the fifteen signed types drop it, and it is the most expensive row
// in the table because the construction is an entire subtree rather than one
// span, and because `children` is the branch an author is most likely to write
// by analogy from the shipped scenes.
//
// A chokepoint is only a chokepoint for the traffic that goes through it; a
// sweep is only a sweep over the axes it varies; and an inventory is only an
// inventory of the keys it enumerates. Hence the shape of the fix: the branch
// stops being a hardcoded pair and becomes a key of this map, so adding a
// nested branch to Node means adding a row here or being caught by the audit.
//
// # Why a warning rather than a refusal
//
// LESSONS.md's question: could a later engine be right about this? Yes, by
// construction — a `text` with a prefix span, or an input with a styled
// node prefix, are both things this format could grow, and most of the rows
// above are constructions the shipped scenes write in their *other* pairing.
// Refusing would break PLAN.md's forward-compatibility rule for a document
// that names no misspelling. A warning keeps the rule the vocabulary path
// already follows for unknown keys and unsigned types: the document loads, and
// the author is told what the engine did not draw, with an address. What the
// construction may not do any more is vanish.
//
// The inventory is written here rather than derived, for signedNodeTypes'
// reason: the fact being recorded is which *shape* an owner reads, and a shape
// is not a Go declaration to reflect over — PrefixText and PrefixNode have the
// same signature shape and differ only in which branch of the raw JSON they
// decode, and `n.Children` is read by four render functions that share no
// signature at all. Written inventories in this package drift, so this one is
// pinned by an audit in internal/engine that renders every shape under every
// signed owner type and fails if a pairing this map calls silent in fact
// draws, or if a pairing it omits does not. That audit measures the real
// renderer, not a mirror of it.
var nestedFormReaders = map[string]map[string]string{
	// prefix, node shape: read by renderMarquee via PrefixNode().
	"prefix.node": {"marquee": "renderMarquee reads PrefixNode()"},
	// prefix, string shape: read by renderInput via PrefixText().
	"prefix.string": {"input": "renderInput reads PrefixText()"},
	// suffix is always a node, and only the marquee composes one.
	"suffix.node": {"marquee": "renderMarquee reads n.Suffix"},
	// row_template is always a node, and only the list composes one: renderList
	// instantiates it once per element of the array its bind names (D1). Before
	// D1 it was refused rather than read, so it had no owner here; now it does,
	// and this row keeps the drop-warning path from taking its silent early
	// return for a template.
	"row_template.node": {"list": "renderList instantiates n.RowTemplate per row via renderRowTemplate"},
	// children is always an array, and four owners compose it. The box and
	// the overlay are containers with a frame; the row and the stack are the
	// two layout primitives. Every other signed type draws from its own
	// bind and text, and a child handed to one of them is not laid out
	// anywhere.
	"children.array": {
		"box":     "renderBox lays n.Children out as an inner stack",
		"overlay": "renderOverlay renders each child into its column",
		"row":     "renderHorizontal divides the width between n.Children",
		"stack":   "renderStack divides the height between n.Children",
	},
}

// nestedFormLabel names the shape a nested position was written in, in the
// vocabulary nestedFormReaders is keyed by.
//
// It reads the raw JSON rather than trusting the branch name because `prefix`
// is the one branch with two live shapes, and which one an owner reads is the
// fact this whole path turns on. The other branches have a single shape each,
// named in the key so the map is keyed uniformly: a `<branch>.<shape>` string
// is what makes the branch a varied axis instead of a hardcoded pair.
func nestedFormLabel(branch string, raw []byte) string {
	switch branch {
	case "prefix":
		if len(raw) > 0 && raw[0] == '"' {
			return "prefix.string"
		}
		return "prefix.node"
	case "children":
		return "children.array"
	default:
		return "suffix.node"
	}
}

// nestedShapeNoun renders a form's shape as the noun the warning uses. It is
// derived from the form key rather than passed in, so a form added to
// nestedFormReaders cannot be described by a stale literal at the call site.
func nestedShapeNoun(form string) string {
	switch {
	case strings.HasSuffix(form, ".string"):
		return "a string"
	case strings.HasSuffix(form, ".array"):
		return "a list of child nodes"
	default:
		return "a node"
	}
}

// NestedFormReadersForAudit exposes the inventory to the engine-side audit
// that pins it against the renderer.
//
// Copied rather than returned, for NodeVocabularyForAudit's reason: a test
// that can edit the contract it checks is an escape hatch that becomes a
// blindfold.
func NestedFormReadersForAudit() map[string]map[string]string {
	out := make(map[string]map[string]string, len(nestedFormReaders))
	for form, owners := range nestedFormReaders {
		inner := make(map[string]string, len(owners))
		for owner, why := range owners {
			inner[owner] = why
		}
		out[form] = inner
	}
	return out
}

// warnUnsignedType reports a `type` value outside the signed v0 vocabulary.
//
// It is a warning rather than a refusal, and the asymmetry is the one
// RefuseEmpty argues for itself, resolved the other way. The question is
// LESSONS.md's: could a later engine be right about this? For a type the
// format has signed, yes by construction, so it must load silently. For an
// unsigned one, a plugin fragment or a v1 primitive may well introduce it, and
// refusing would break exactly the forward compatibility PLAN.md calls a
// standing risk. A refusal here would also be the harsher answer to the
// *cheaper* mistake: a misspelled type costs one node, while a misspelled
// `children` — already only a warning — silently deletes an entire subtree.
//
// The message names the closest signed type when there is one. That is the
// six letters the author was hunting for, and printing them is the whole
// point: this repository has now paid five times for a layer that knew the
// answer and did not say it.
func (d *Document) warnUnsignedType(n *Node, path string, out *[]Warning) {
	// A missing type is not an unsigned type. It is reported separately
	// rather than folded in because a node with no type is a node that
	// cannot draw at all, and saying `unsigned type ""` would describe it
	// wrongly — it reads as a quoting bug and sends the author looking in
	// the engine rather than at their own document.
	if n.Type == "" {
		*out = append(*out, Warning{
			Loc: d.locOf(path),
			Msg: "a node declares no \"type\", so the engine has nothing to draw for it and " +
				"renders a placeholder; every node needs a type from the signed vocabulary (" +
				strings.Join(SignedNodeTypes(), ", ") + ")",
		})
		return
	}
	if signedNodeTypes[n.Type] {
		return
	}

	msg := fmt.Sprintf("node type %q is not in the signed v0 vocabulary, so this engine draws a "+
		"placeholder where the node should be", n.Type)
	if near := nearestSignedType(n.Type); near != "" {
		msg += fmt.Sprintf("; did you mean %q?", near)
	} else {
		msg += fmt.Sprintf("; the settled types are %s", strings.Join(SignedNodeTypes(), ", "))
	}
	*out = append(*out, Warning{Loc: d.locOf(path), Msg: msg})
}
