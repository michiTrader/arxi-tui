package scene

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// The parser's vocabulary, and what happens to a key outside it.
//
// This is the fifth instance of one defect class and the first one caught at
// the layer where the class actually lives. The previous four were each a
// single field: a style key the validator accepted and styleName() dropped, a
// border token both drawing paths ignored, `row_template` walked by everything
// and drawn by nobody, and then `on_press`/`scroll` — universal in SCENES.md,
// never declared on Node, so encoding/json discarded them without a word.
//
// Fixing the fourth pair by declaring the fields was correct and too narrow.
// encoding/json ignores *every* key it does not recognise, so declaring two of
// them bought exactly two keys. Measured on the tree as it stood after that
// fix, with the whole suite green:
//
//	focus_glow  parsed, validated clean, drew nothing
//	transition  parsed, validated clean, drew nothing
//	reveal      parsed, validated clean, drew nothing
//	enter       parsed, validated clean, drew nothing
//	shine       parsed, validated clean, drew nothing
//	tab         parsed, validated clean, drew nothing
//
// All six are named by SCENES.md — the first four by Scene 4's animation
// paragraph, `shine` by Scene 11, `tab: false` by Q19 — and a seventh probe,
// `totally_invented_key`, behaved identically to all of them. That is the
// tell: the format had no way to distinguish a property it documents from a
// string nobody has ever typed before, because it was not looking at keys at
// all.
//
// The worst case is not the documented gap, though. It is the typo:
//
//	{ "root": { "type": "stack", "chidlren": [ …two nodes… ] } }
//
// parses, validates clean, and renders an empty stack. One transposed pair of
// letters silently deletes the entire subtree, and every layer reports
// success. `{ "scene": { … } }` — the whole tree under the wrong top-level key
// — was also accepted, with a nil root and no complaint.
//
// So the remedy is not another field, it is a vocabulary. nodeVocabulary is
// derived from Node's own json tags by reflection, which is the point: a list
// written by hand would be a second inventory of Node's fields, and this
// package has already watched three hand-maintained inventories drift (the
// signed bind map from BINDS.md in both directions, the unrendered-field map
// from the refusal it advertised, and the universals audit from the engine it
// claimed to check). A field added to Node tomorrow joins the vocabulary for
// free, and one deleted leaves it the same way.
//
// Why a warning rather than a refusal. PLAN.md signs "unknown-but-parseable is
// a warning, missing state is a placeholder", and the engine already honours
// it for node *types*: `button`, `switch`, `slider` and `sparkline` are
// documented, unimplemented, and each draws [[UNKNOWN NODE TYPE]] rather than
// failing the load, so a document written for a later version keeps booting.
// Refusing an unknown *property* would give the format two opposite answers
// for its two kinds of unknown construction, and would break the forward
// compatibility the plan calls a standing risk. A warning closes the asymmetry
// in the other direction: the document still loads, and the author is told
// what the engine did not understand, with an address.
//
// The three outcomes a key may now have are the same three a universal
// property has under the engine audit — honoured, refused with an address, or
// warned about with an address. What no key may do any more is vanish.

// Warning is a non-fatal finding about a document: it loaded, it will render,
// and something in it did not mean what its author probably thought.
//
// It is a distinct type from Error rather than an Error with a severity flag,
// because the two travel different paths and the difference is load-bearing.
// An Error replaces the document with the last good scene (invariant 3); a
// Warning lets the document through. A single type with a flag is a type whose
// callers decide the severity at each call site, and that is how a warning
// becomes silent: one caller forgets to check the flag and the finding is
// gone. The compiler cannot make that mistake with two types.
type Warning struct {
	Loc Loc
	Msg string
}

func (w Warning) String() string {
	return fmt.Sprintf("%s: %s", w.Loc.String(), w.Msg)
}

// RefuseEmpty rejects a document that parsed into no tree at all.
//
// It is a refusal where an unknown property is a warning, and the asymmetry is
// deliberate rather than an inconsistency. PLAN.md's forward-compatibility
// rule protects constructions a *later* engine might understand: an unknown
// property could be Scene 4's `reveal` arriving before Phase 4 builds it, so
// the document still loads and the author is told what was skipped. A document
// with no root is not that. There is no version of this format under which a
// treeless scene draws something, so accepting it cannot be forward
// compatibility — it can only hide a mistake, and the mistake it hides is a
// whole interface that vanished.
//
// Measured before it was written: `{ "scene": { … } }` — the entire tree under
// one wrong top-level key — parsed, validated clean, and rendered nothing.
// Refusing it is also what makes invariant 3 do its job, because the raw-scene
// fallback only fires when the load path is told something went wrong.
//
// It is separate from Validate rather than folded into it because Validate is
// called on hand-built documents throughout the suite and in future patch
// code, where a nil root means "nothing to check" rather than "the author lost
// their scene". The load path knows it read bytes off a disk; Validate does
// not.
func (d *Document) RefuseEmpty() error {
	if d == nil {
		return &Error{Msg: "no scene document at all"}
	}
	if d.Root != nil {
		return nil
	}
	return &Error{
		Loc: Loc{File: d.file},
		Msg: "the document declares no \"root\" node, so there is no scene to draw; " +
			"a tree under any other top-level key is discarded by the parser in silence",
	}
}

// nodeVocabulary is every json key Node declares, derived from the struct.
//
// Computed once per call rather than cached in a package var on purpose: the
// cost is a reflect walk over ~20 fields on documents that already cost a JSON
// parse, and a package-level var would be initialised before any test could
// perturb the struct. The injection that proves this guard works needs to be
// able to reason about the vocabulary as a function of Node, not as a snapshot
// taken at init.
func nodeVocabulary() map[string]bool {
	vocab := make(map[string]bool)
	t := reflect.TypeOf(Node{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			vocab[name] = true
		}
	}
	return vocab
}

// Vocabulary returns the json keys a node may declare, sorted.
//
// Exported because the engine's universals audit asks the same question from
// the other side of the boundary, and two packages computing it separately is
// the drift this file exists to stop.
func Vocabulary() []string {
	vocab := nodeVocabulary()
	out := make([]string, 0, len(vocab))
	for key := range vocab {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Warnings reports every key in the document that the parser does not
// recognise, addressed to the node that declared it.
//
// It reads the keys recorded by the parse rather than re-deriving them from
// the tree, because by the time there is a tree the evidence is gone: an
// unknown key's whole signature is that it is absent from the parsed Node.
// This is the same reason nodeOffsets is a second pass over the source — the
// token stream is the only place the information exists.
//
// A Document built by hand (no source bytes) has no recorded keys and warns
// about nothing, which is correct: there was no text for a key to be
// misspelled in.
func (d *Document) Warnings() []Warning {
	if d == nil || d.Root == nil {
		return nil
	}
	vocab := nodeVocabulary()
	var out []Warning
	d.collectWarnings(d.Root, nodePathRoot, vocab, &out)
	return out
}

// collectWarnings walks the tree the same way validateBinds does, and that
// parallel is deliberate: the walk visits node paths, and only a node path may
// be asked for its keys. Consulting every object in the source instead would
// report `style`'s token names and `border`'s `shape`/`style` keys as unknown
// node properties, which is a wall of false alarms — and a guard that cries
// wolf is a guard that gets deleted.
func (d *Document) collectWarnings(n *Node, path string, vocab map[string]bool, out *[]Warning) {
	for _, key := range d.declaredKeys[path] {
		if vocab[key] {
			continue
		}
		*out = append(*out, Warning{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("node type %q declares %q, which is not a property this engine "+
				"knows; it was ignored. If it is a typo the node lost whatever it named "+
				"(a misspelled \"children\" silently drops the whole subtree); if it is "+
				"from a later version of the format, this engine cannot draw it",
				n.Type, key),
		})
	}

	for i, child := range n.Children {
		d.collectWarnings(child, childPath(path, i), vocab, out)
	}
	if prefix := n.PrefixNode(); prefix != nil {
		d.collectWarnings(prefix, prefixPath(path), vocab, out)
	}
	if n.Suffix != nil {
		d.collectWarnings(n.Suffix, suffixPath(path), vocab, out)
	}
	if n.RowTemplate != nil {
		d.collectWarnings(n.RowTemplate, templatePath(path), vocab, out)
	}
}
