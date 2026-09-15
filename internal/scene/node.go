package scene

// Node is one element in a scene document. A scene is a tree of Nodes, each
// declaring its type, a stable id, optional bindings and styles.
type Node struct {
	ID        string            `json:"id,omitempty"`
	Type      string            `json:"type"`
	Bind      string            `json:"bind,omitempty"`
	When      string            `json:"when,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	Text      string            `json:"text,omitempty"`
	Children  []*Node           `json:"children,omitempty"`
	Style     map[string]string `json:"style,omitempty"`
	Grow      *int              `json:"grow,omitempty"`
	Weight    *int              `json:"weight,omitempty"`
	Prefix    *Node             `json:"prefix,omitempty"` // simplified for now
}
