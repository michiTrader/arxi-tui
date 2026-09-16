package scene

import (
	"fmt"
	"strings"
)

// signedBinds is the closed inventory of bind strings a scene may address.
// Every name here is a signed row in docs/BINDS.md Section 4. A scene that
// references a bind not in this set fails validation with a file:line error
// (BINDS.md §4.5 exit criterion).
//
// These are the host-owned fields — the run-state half (§4.1/4.2) and the
// view-state half (§4.3). The plugin namespace (<plugin-id>.*) is open by
// design (§4.4) and is validated separately against a plugin's declared
// streams.
var signedBinds = map[string]bool{
	// §4.1 Run state
	"chat.history":            true,
	"thinking.text":           true,
	"agent.working":           true,
	"agent.mode":              true,
	"model.name":              true,
	"usage.in":                true,
	"usage.out":               true,
	"usage.delta":             true,
	"session.tokens_used":     true,
	"session.new_milestone":   true,
	"team.members":            true,
	"todos.count":             true,
	"run.quiescent.diagnosis": true,

	// §4.2 Agent blocked / remedy
	"agent.blocked.blocked_ref": true,
	"agent.blocked.blocked_on":  true,
	"agent.blocked.actor":       true,

	// §4.3 View state
	"slash.active":  true,
	"slash.typed":   true,
	"slash.matches": true,
	"ui.focus":      true,
	"ui.max":        true,
	"ui.surface":    true,

	// §2 Bootstrap set
	"user.input":           true,
	"user.input.submitted": true,
	"host.escape.armed":    true,
	"host.scene.error":     true,
}

// isValidHostBind reports whether a bind name is a signed host-owned field.
// Plugin binds (anything starting with a dotted namespace prefix that is not
// in the host inventory) are accepted as open by design.
func isValidHostBind(name string) bool {
	if signedBinds[name] {
		return true
	}
	// Plugin namespace: <plugin-id>.<field> where plugin-id is not a known
	// host namespace prefix. Host namespaces are the dotted prefixes above.
	// Any bind whose prefix is not a host namespace is treated as a plugin
	// bind and accepted — the gate (ADR-0003) validates it at mount time.
	hostPrefixes := []string{
		"chat.", "thinking.", "agent.", "model.", "usage.",
		"session.", "team.", "todos.", "run.",
		"slash.", "ui.", "user.", "host.",
	}
	for _, p := range hostPrefixes {
		if strings.HasPrefix(name, p) {
			// It starts with a host namespace but is not a signed field —
			// that is an error (a typo like "agent.workin" or an invented
			// sub-field like "agent.foo").
			return false
		}
	}
	// Falls through to a plugin namespace — accepted as open.
	return true
}

// ValidationError is a bind/when validation failure. It carries the field
// path and the offending bind name so the caller can report file:line.
type ValidationError struct {
	Field string // "bind" or "when"
	Bind  string
	Node  string // the node's id, or its type if unset
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("scene: unsigned bind %q in field %q on node %q — every bind must resolve to a signed row in docs/BINDS.md §4.5", e.Bind, e.Field, e.Node)
}

// Validate walks every node and checks that all bind/when strings resolve to
// a signed row. It returns the first validation error with enough context for
// a file:line address (BINDS.md §4.5 exit criterion).
func (d *Document) Validate() error {
	if d == nil || d.Root == nil {
		return fmt.Errorf("scene: document has no root node")
	}
	return d.Root.validate()
}

func (n *Node) validate() error {
	if n.Type == "" {
		return fmt.Errorf("scene: node at %s has no type", nodePath(n))
	}

	// Check bind.
	if n.Bind != "" && !isValidHostBind(n.Bind) {
		return ValidationError{Field: "bind", Bind: n.Bind, Node: nodeID(n)}
	}

	// Check when.
	if n.When != "" && !isValidHostBind(n.When) {
		return ValidationError{Field: "when", Bind: n.When, Node: nodeID(n)}
	}

	// Check prefix node's binds.
	if p := n.PrefixNode(); p != nil {
		if p.Bind != "" && !isValidHostBind(p.Bind) {
			return ValidationError{Field: "bind", Bind: p.Bind, Node: nodeID(p)}
		}
	}

	// Check suffix node's binds.
	if n.Suffix != nil {
		if n.Suffix.Bind != "" && !isValidHostBind(n.Suffix.Bind) {
			return ValidationError{Field: "bind", Bind: n.Suffix.Bind, Node: nodeID(n.Suffix)}
		}
	}

	// Check row_template's binds.
	if n.RowTemplate != nil {
		if n.RowTemplate.Bind != "" && !isValidHostBind(n.RowTemplate.Bind) {
			return ValidationError{Field: "bind", Bind: n.RowTemplate.Bind, Node: nodeID(n.RowTemplate)}
		}
	}

	// Recurse into children.
	for _, c := range n.Children {
		if err := c.validate(); err != nil {
			return err
		}
	}

	return nil
}

func nodeID(n *Node) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Type
}

func nodePath(n *Node) string {
	if n.ID != "" {
		return n.ID
	}
	return n.Type
}
