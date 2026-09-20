package scene

import (
	"encoding/json"
	"sort"
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
	// Scroll is Scene 4's animation property, `{ "speed": n, "pause_when":
	// <bind> }`. Same story and same remedy as OnPress: universal in
	// SCENES.md, implemented nowhere, and silently discarded until the key
	// was declared here.
	//
	// json.RawMessage rather than a struct because the shape belongs to the
	// animation clock Phase 4 designs; parsing it into fields now would
	// pin a format ahead of the phase meant to choose it, while refusing it
	// only needs to know the key was written.
	Scroll json.RawMessage `json:"scroll,omitempty"`
	// MinWidth is the minimum content width an overlay will accept before
	// its content wraps. The overlay never shrinks below this (Q7).
	MinWidth *int `json:"min_width,omitempty"`
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
// The clone is shallow: children, template, prefix and suffix are cleared
// before marshalling. The walk visits every node itself, so serialising whole
// subtrees at each step would make the pass quadratic and would also report a
// child's field as the parent's.
func (n *Node) declaredUnrenderedFields() []string {
	shallow := *n
	shallow.Children = nil
	shallow.RowTemplate = nil
	shallow.Suffix = nil
	shallow.PrefixRaw = nil
	shallow.BorderRaw = nil

	encoded, err := json.Marshal(&shallow)
	if err != nil {
		return nil
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		return nil
	}

	// The cleared fields are still declared by the original node, and one
	// of them (row_template) is the map's first entry. They are restored
	// by name rather than by value: what matters is presence.
	out := make([]string, 0, len(keys)+4)
	for key := range keys {
		out = append(out, key)
	}
	if n.RowTemplate != nil {
		out = append(out, "row_template")
	}
	if n.Suffix != nil {
		out = append(out, "suffix")
	}
	if len(n.PrefixRaw) > 0 {
		out = append(out, "prefix")
	}
	if len(n.BorderRaw) > 0 {
		out = append(out, "border")
	}
	// Sorted so a node declaring two unrendered fields refuses the same one
	// every run; an address that moves between runs is not an address.
	sort.Strings(out)
	return out
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
