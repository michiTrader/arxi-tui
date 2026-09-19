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
	// Object form: { "shape": "single", "style": "warn" }
	var obj struct {
		Shape string `json:"shape"`
	}
	if err := json.Unmarshal(n.BorderRaw, &obj); err != nil {
		return ""
	}
	return obj.Shape
}

// BorderStyleName returns the border style token (e.g. "warn") or "" if unset.
// Only the object form carries a style; a bare string border has no style.
func (n *Node) BorderStyleName() string {
	if len(n.BorderRaw) == 0 || (len(n.BorderRaw) > 0 && n.BorderRaw[0] == '"') {
		return ""
	}
	var obj struct {
		Style string `json:"style"`
	}
	if err := json.Unmarshal(n.BorderRaw, &obj); err != nil {
		return ""
	}
	return obj.Style
}
