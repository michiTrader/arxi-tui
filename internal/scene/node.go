package scene

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// Node is one element in a scene document. A scene is a tree of Nodes, each
// declaring its type, a stable id, optional bindings and styles.
type Node struct {
	ID          string            `json:"id,omitempty"`
	Type        string            `json:"type"`
	Bind        string            `json:"bind,omitempty"`
	When        string            `json:"when,omitempty"`
	Placeholder string            `json:"placeholder,omitempty"`
	Text        string            `json:"text,omitempty"`
	Children    []*Node           `json:"children,omitempty"`
	Style       map[string]string `json:"style,omitempty"`
	Grow        *int              `json:"grow,omitempty"`
	Weight      *int              `json:"weight,omitempty"`

	// Prefix is either a string (for the input node's prompt glyph) or a
	// child node (for the marquee's styled prefix). Uses json.RawMessage so
	// the same field accepts both shapes without a custom unmarshaller.
	PrefixRaw json.RawMessage `json:"prefix,omitempty"`
	// Suffix is a child node that renders after the main text (used by the
	// thinking marquee's usage delta suffix).
	Suffix      *Node    `json:"suffix,omitempty"`
	Anchor      string   `json:"anchor,omitempty"`
	FilterBy    string   `json:"filter_by,omitempty"`
	Count       bool     `json:"count,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	RowTemplate *Node    `json:"row_template,omitempty"`
	// Border renders a box-drawing frame around the node's content. It accepts
	// either a string ("single", "double", "ascii") for shape-only, or an object
	// { "shape": "single", "style": "warn" } for shape + style (Scene 3 tokens
	// overlay). Uses json.RawMessage so both forms unmarshal without a custom
	// UnmarshalJSON on the whole Node.
	BorderRaw json.RawMessage `json:"border,omitempty"`
	// Title renders a title at the top-left inside the border.
	Title string `json:"title,omitempty"`
	// OnPress names the action a press dispatches ("cmd:/max chat",
	// "answer:approve", "focus:<node>"). SCENES.md calls it universal and
	// four of the eleven scenes write it; nothing dispatches it yet.
	//
	// The field exists in order to be refused, not to be read, and that is
	// the whole reason it was added. Without it the key was not part of the
	// parser's vocabulary at all, so encoding/json discarded it in silence:
	// a scene declaring on_press parsed, validated and rendered, and the
	// property was gone before any layer could have an opinion. That is a
	// silent drop one step earlier than the four this package already paid
	// for — earlier because there was no field for the unrendered-field
	// audit to enumerate, so it reported full coverage precisely *because*
	// the property was missing. Declaring it puts the key back inside the
	// vocabulary, where unrenderedFields can refuse it with an address.
	OnPress string `json:"on_press,omitempty"`
	// Scroll is Scene 4's marquee animation, `{ "speed": <cells/tick>,
	// "pause_when": "<bind>" }`. It graduated from refused-raw to read-struct
	// in G2: the render semantics are signed (SCENES.md Scene 4, G-B) and the
	// host clock that drives it is signed (ADR-0005), so the shape is now read
	// rather than merely refused.
	//
	// A struct rather than json.RawMessage, unlike the raw fields above, and for
	// FocusGlow's stated reason: the engine reads `{speed, pause_when}` now, so
	// leaving it raw would mean parsing it at the render site, and a shape parsed
	// where it is used is a shape with no single definition. It was raw only
	// while nothing read it.
	//
	// The axis it rides is horizontal offset, and only a marquee draws that axis
	// (SCENES.md Scene 4). validate.go refuses a scroll on any other node type
	// with an address, the same way a row.* bind outside a template is refused:
	// a prop on the wrong node type is a scene defect, not a silent no-op. An
	// absent scroll (nil) is the ordinary no-op — most nodes do not scroll.
	//
	// The `anim:"1"` tag puts it on the progress audit's animation axis, beside
	// FocusGlow. It is a marker rather than a name convention because a name-based
	// rule silently captures an unrelated field added later.
	Scroll *Scroll `json:"scroll,omitempty" anim:"1"`
	// Reveal is Scene 4's typewriter animation (G3), `{ "anim": "<token>" }`.
	// It graduated from a parsed-and-warned key to a read struct the same way
	// scroll did in G2: the render semantics are signed (SCENES.md Scene 4, G-B
	// — the character-count axis) and the host clock that drives it is signed
	// (ADR-0005), so the shape is now read rather than warned about.
	//
	// A struct rather than json.RawMessage, for Scroll's and FocusGlow's stated
	// reason: the engine reads `{anim}` now, so leaving it raw would mean parsing
	// it at the render site, and a shape parsed where it is used is a shape with
	// no single definition.
	//
	// The axis it rides is character count — a growing prefix of the node's own
	// text — and only a text node draws that axis (SCENES.md Scene 4). validate.go
	// refuses a reveal on any other node type with an address, the same way scroll
	// is refused off a marquee and a row.* bind outside a template is: a prop on
	// the wrong node type is a scene defect, not a silent no-op. An absent reveal
	// (nil) is the ordinary no-op — most text nodes do not reveal.
	//
	// Anim names a timing token (Q8); empty means anim.default. A named token
	// absent from the active theme is refused at load by ValidateTokens (the same
	// net a style token gets), so a typewriter naming a duration the theme never
	// declared is a load error with an address, not a node that silently never
	// animates.
	//
	// The `anim:"1"` tag puts it on the progress audit's animation axis, beside
	// Scroll and FocusGlow — a marker rather than a name convention because a
	// name-based rule silently captures an unrelated field added later.
	Reveal *Reveal `json:"reveal,omitempty" anim:"1"`
	// MinWidth is the minimum content width an overlay will accept before
	// its content wraps. The overlay never shrinks below this (Q7).
	MinWidth *int `json:"min_width,omitempty"`

	// FocusGlow is Scene 4's focus emphasis: `{ "style": "<token>" }`. When
	// this node's id equals `ui.focus`, the engine renders its content under
	// the named token instead of the node's ordinary style.
	//
	// It was the first Scene 4 property that could land before the host clock,
	// and the asymmetry with the other four was a scope decision, not an
	// oversight. focus_glow's input is the focused node's id: `ui.focus` is
	// signed in BINDS.md, maintained by the fold and already projected, so
	// the property is expressible with no new vocabulary and no clock.
	// transition, reveal and enter still need the host clock and their own
	// render semantics; scroll was the same until G2 signed those (SCENES.md
	// Scene 4, G-B) and built the clock (ADR-0005), so scroll now reads its
	// struct above and this list is down to the three that remain warnings.
	//
	// A struct rather than json.RawMessage: the shape is being read now, so
	// leaving it raw would mean parsing it at the render site, and a shape
	// parsed where it is used is a shape with no single definition. Scroll
	// followed it out of the raw fields for the same reason once the engine
	// began reading it.
	//
	// The `anim:"1"` tag is what puts it on the progress audit's animation
	// axis. It is a marker rather than a name convention because a
	// name-based rule silently captures an unrelated field added later.
	FocusGlow *FocusGlow `json:"focus_glow,omitempty" anim:"1"`
}

// declaredUnrenderedFields returns the json names of the fields this node
// actually sets, so the validator can ask which of them the engine cannot yet
// draw.
//
// It re-serialises the node rather than testing each field by hand, and that
// is the whole point. A hand-written switch would be a second inventory of
// Node's fields, maintained beside the struct and the unrenderedFields map,
// and this package has already paid twice for exactly that shape: the signed
// bind map drifted from BINDS.md in both directions, and the audit's remedy
// drifted from the refusal it promised. A field added to Node tomorrow shows
// up here for free, because every optional field carries omitempty — so a key
// present in the output is a key the document set.
//
// The converse does not hold, and the comment used to imply it did. omitempty
// drops a key written with its type's zero value, so `"on_press": ""` is
// absent from the output and this function cannot report it: what it answers
// is "which fields does this node *hold a value for*", not "which keys did
// the author write". The gap was invisible for a raw field —
// `"scroll": null` kept its bytes and was refused, back when scroll was raw —
// and it is open for every ordinary one, which is the worst shape for it to
// have, because a raw and an ordinary key sat side by side in unrenderedFields
// and behaved differently. refuseUnrendered therefore unions this list with the
// parser's declaredKeys, which is the exact record; see the reasoning there.
// This function is deliberately left answering the value question, because that
// is the one a hand-built Document — with no source text, and so no
// declaredKeys — can still answer.
//
// The subtrees are cleared before marshalling. The walk visits every node
// itself, so serialising whole subtrees at each step would make the pass
// quadratic and would also report a child's field as the parent's.
//
// # Why clearing and restoring are one list rather than two
//
// They used to be two, written four lines apart: an assignment per branch
// blanking it, and an `if x != nil` per branch putting its name back. Two
// hand-written statements of one fact — which fields the shallow clone drops
// — and that is the shape this package has watched drift six times. These two
// had already drifted, in the tree, with the whole suite green: `children` was
// cleared and never restored, so no node has ever reported declaring it.
//
// The consequence is not a missing key in a diagnostic. It is that the remedy
// both field audits print — "add it to scene.unrenderedFields so the validator
// refuses it with an address" — is a **no-op for `children`**, and would be a
// no-op for any node-bearing branch a contributor cleared here without
// restoring. Measured, each line from a run, on a `Footer *Node` added to Node
// and cleared in the clone the way the paragraph above reasons a contributor
// would:
//
//	the audit fires, printing "add it to scene.unrenderedFields"
//	following that remedy  -> the entry sits in the map, inert
//	the next guard fires, printing "add a fixture for it"
//	following that remedy  -> "the validator accepted it anyway", whose own
//	                          remedy names refuseUnrendered — which is already
//	                          correct and passes its own test
//
// Three remedies deep, each the correct action for the message shown, and the
// last one points at working code. That is the printed remedy as attack
// surface, one turn on from the composite this package recorded last: not a
// single guard whose advice is inert, but a *chain* of them, each handing the
// author the next plausible place to look, and none naming this function.
//
// So the branch list is derived from the struct once: a field whose type can
// carry a node is cleared, and the same walk records its json name, so the
// restore cannot fall behind the clear. A branch added to Node tomorrow is
// handled by both halves or by neither.
func (n *Node) declaredUnrenderedFields() []string {
	shallow := *n
	sv := reflect.ValueOf(&shallow).Elem()

	branches := subtreeBranches()
	declared := make([]string, 0, len(branches))
	for _, b := range branches {
		field := sv.Field(b.index)
		if !field.IsZero() {
			declared = append(declared, b.jsonName)
		}
		field.SetZero()
	}

	encoded, err := json.Marshal(&shallow)
	if err != nil {
		return nil
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		return nil
	}

	// The cleared branches are still declared by the original node, and one
	// of them (row_template) is unrenderedFields' first entry. They are
	// restored by name rather than by value: what matters is presence.
	out := make([]string, 0, len(keys)+len(declared))
	for key := range keys {
		out = append(out, key)
	}
	out = append(out, declared...)
	// Sorted so a node declaring two unrendered fields refuses the same one
	// every run; an address that moves between runs is not an address.
	sort.Strings(out)
	return out
}

// subtreeBranch is one field of Node the shallow clone must drop: a branch
// carrying another node, or the raw JSON one may be decoded from.
type subtreeBranch struct {
	index    int
	jsonName string
}

// subtreeBranches derives, from Node itself, the fields declaredUnrenderedFields
// clears — and therefore the ones it must restore by name.
//
// # What counts as a subtree
//
// A field whose type is built out of Node (through pointers, slices, arrays or
// maps, keys included), or a json.RawMessage. The second is not generality for
// its own sake: `prefix` is raw precisely because it is polymorphic between a
// string and a node, so the type cannot answer the question — which is the
// same fact rawBranchAccessors exists to record for the sibling audit.
//
// # Why reflection over the type rather than a written list
//
// nested_branch_audit_test.go's fieldTypeCarriesNode makes this same
// derivation and records why at length: `*Node` and `[]*Node` are the two
// spellings the format happens to use today, while `map[string]*Node` for
// named slots and `[][]*Node` for a grid are each how a format grows. That
// audit asks the shape; this function has to agree with it, and two
// derivations only agree permanently when they ask the same question of the
// same type.
var subtreeBranches = sync.OnceValue(func() []subtreeBranch {
	t := reflect.TypeOf(Node{})
	raw := reflect.TypeOf(json.RawMessage(nil))

	var out []subtreeBranch
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if f.Type != raw && !typeBuildsOnNode(f.Type, map[reflect.Type]bool{}) {
			continue
		}
		out = append(out, subtreeBranch{index: i, jsonName: name})
	}
	return out
})

// typeBuildsOnNode reports whether a type can hold a Node, whatever shape it
// is built out of. The seen set is what keeps Node's own recursive branches
// from spinning: Children is []*Node, and *Node reaches Node again.
func typeBuildsOnNode(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true

	if t == reflect.TypeOf(Node{}) {
		return true
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return typeBuildsOnNode(t.Elem(), seen)
	case reflect.Map:
		return typeBuildsOnNode(t.Key(), seen) || typeBuildsOnNode(t.Elem(), seen)
	default:
		return false
	}
}

// PrefixNode decodes PrefixRaw as a child Node (for marquee prefix).
// Returns nil if the prefix is a string or absent.
func (n *Node) PrefixNode() *Node {
	if len(n.PrefixRaw) == 0 {
		return nil
	}
	// A string prefix (like "┃ " for input) starts with a quote.
	if len(n.PrefixRaw) > 0 && n.PrefixRaw[0] == '"' {
		return nil
	}
	var p Node
	if err := json.Unmarshal(n.PrefixRaw, &p); err != nil {
		return nil
	}
	return &p
}

// PrefixText decodes PrefixRaw as a string (for input node).
// Returns "" if the prefix is a node or absent.
func (n *Node) PrefixText() string {
	if len(n.PrefixRaw) == 0 {
		return ""
	}
	if len(n.PrefixRaw) > 0 && n.PrefixRaw[0] == '"' {
		var s string
		if err := json.Unmarshal(n.PrefixRaw, &s); err != nil {
			return ""
		}
		return s
	}
	return ""
}

// HasBorder reports whether the node declares any border at all.
func (n *Node) HasBorder() bool {
	return len(n.BorderRaw) > 0
}

// borderObject is the object form of a border: { "shape": …, "style": … }.
//
// It is one named type rather than the two anonymous structs the accessors
// used to declare inline, and that is load-bearing rather than tidiness. The
// vocabulary check reports a border key it does not recognise, so it has to
// know what a border may contain, and the only truthful answer is "whatever
// the code that reads a border reads". With the shape spelled out separately
// inside each accessor, the two statements were free to diverge, and an
// injection measured the divergence: teaching BorderStyleName one extra key
// left a document using that key correct, honoured by the renderer, and
// *warned about* by the guard, with the whole suite green. A false alarm on a
// working construction, produced by the guard disagreeing with the code it
// claims to describe — and a false alarm is how a guard loses its reader.
//
// That is the same drift this package has watched four times, arriving one
// level in from where it was last closed. Deriving a vocabulary by reflection
// is only worth something if it reflects the thing that actually does the
// reading; reflecting a copy of it just moves the copy. One type, read by
// both accessors and by borderVocabulary(), makes the disagreement
// unrepresentable rather than merely tested for.
type borderObject struct {
	Shape string `json:"shape"`
	Style string `json:"style"`
}

// FocusGlow is the object form of Scene 4's focus_glow: `{ "style": <token> }`.
//
// It is exported because the engine reads it, and it is one named type for
// borderObject's reason — the vocabulary check for `focus_glow.*` is derived
// from this struct, so the keys the guard accepts and the keys the renderer
// reads cannot drift apart. An anonymous struct at the render site would
// reintroduce exactly the divergence R19h measured: a guard reflecting a copy
// warned about a document the renderer honoured, with the suite green.
//
// One field, deliberately. The natural second field is a duration, and a
// duration needs the `[anim]` timing token Q8 specifies. That token is now
// signed and implemented (D4; TOKENS.md's `anim` section, parsed and validated
// in internal/theme) — but a glow that is on or off still needs no clock, so
// this stays one field. The other Scene 4 props are the ones that consume a
// timing token, and they wait on a host clock and on their own render semantics
// being signed, not on the token, which exists.
type FocusGlow struct {
	Style string `json:"style"`
}

// Scroll is the object form of Scene 4's scroll (G2): the marquee's horizontal
// motion, `{ "speed": <cells/tick>, "pause_when": "<bind>" }`.
//
// One named type, read by the engine and inventoried by the validator, for the
// reason borderObject and FocusGlow are named types: the shape the renderer
// reads and the shape the validator checks are the same declaration, so they
// cannot drift. Parsing `{speed, pause_when}` at the render site instead would
// reintroduce exactly the divergence a single type makes unrepresentable.
//
// Speed is cells advanced per tick; the tick *rate* is the anim.marquee token's
// fps (D4), so cadence lives in one place and speed only says how far each tick
// moves. A non-positive speed is refused at validation, not clamped: a zero
// speed marquee ticks forever without moving, which is a defect worth naming.
//
// PauseWhen names a bind (BINDS.md §4.6 truthiness); while it is truthy the
// offset holds, so a scene can freeze the marquee when a pane is unfocused or
// the agent is idle without the renderer inventing a pause policy. Empty means
// never pause. It is validated as a signed bind, the same net a `when` gets.
type Scroll struct {
	Speed     int    `json:"speed"`
	PauseWhen string `json:"pause_when,omitempty"`
}

// Reveal is the object form of Scene 4's reveal (G3): the typewriter's growing
// prefix, `{ "anim": "<token>" }`.
//
// One named type, read by the engine and inventoried by the validator, for the
// reason Scroll/FocusGlow/borderObject are named types: the shape the renderer
// reads and the shape the validator checks are the same declaration, so they
// cannot drift.
//
// Anim names a timing token whose duration_ms and curve the host clock measures
// elapsed time against (ADR-0005); the eased fraction of that duration is the
// share of the content the renderer draws. Empty means anim.default (Q8 — every
// animated prop uses the default token unless it names one), and a named token
// the active theme does not define is refused at load (ValidateTokens), the same
// net an undefined style token gets.
type Reveal struct {
	Anim string `json:"anim,omitempty"`
}

// border decodes the object form. It reports false for the string form and
// for a malformed one, neither of which carries keys to speak of.
func (n *Node) border() (borderObject, bool) {
	if len(n.BorderRaw) == 0 || n.BorderRaw[0] == '"' {
		return borderObject{}, false
	}
	var obj borderObject
	if err := json.Unmarshal(n.BorderRaw, &obj); err != nil {
		return borderObject{}, false
	}
	return obj, true
}

// BorderShape returns the border shape ("single", "double", "ascii") or ""
// if no border is set. Both string and object forms are accepted.
func (n *Node) BorderShape() string {
	if len(n.BorderRaw) == 0 {
		return ""
	}
	// String form: "single", "double", "ascii"
	if n.BorderRaw[0] == '"' {
		var s string
		if err := json.Unmarshal(n.BorderRaw, &s); err != nil {
			return ""
		}
		return s
	}
	obj, ok := n.border()
	if !ok {
		return ""
	}
	return obj.Shape
}

// BorderStyleName returns the border style token (e.g. "warn") or "" if unset.
// Only the object form carries a style; a bare string border has no style.
func (n *Node) BorderStyleName() string {
	obj, ok := n.border()
	if !ok {
		return ""
	}
	return obj.Style
}
