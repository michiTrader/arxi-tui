package scene

import "encoding/json"

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
