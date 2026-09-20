package scene

import (
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
