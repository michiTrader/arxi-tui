package scene

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TokenError records a reference to an undefined token in a scene.
type TokenError struct {
	Token    string // The token name that was not found in the theme
	NodeType string // The type of node that referenced it
}

func (e TokenError) Error() string {
	return fmt.Sprintf("node type %q references undefined token %q", e.NodeType, e.Token)
}

// Validate checks that every bind and when string referenced in the scene is
// in the signed inventory (docs/BINDS.md §4.5). This is the exit criterion:
// an unsigned bind is a load-time error. Returns the first validation error
// encountered, or nil if the document is valid.
func (d *Document) Validate() error {
	if d.Root == nil {
		return nil
	}
	return validateBinds(d.Root)
}

// signedBinds is the §4.5 inventory: every bind a scene may reference. A bind
// not in this list fails validation at load time.
//
// This map is the *runtime* inventory and it is deliberately a Go literal
// rather than the parsed document: arxi ships as one static binary, so the
// validator cannot depend on docs/BINDS.md existing on the user's disk. The
// document stays the contract, and TestSignedInventoryMatchesDocument is what
// keeps the two identical — a row added to the doc without a line here (or the
// reverse) fails the suite. That test exists because this map had already
// drifted from the signed document in both directions: it carried four binds
// signed nowhere (agent.status, banner.text, session.tokens) while rejecting
// eighteen that BINDS.md §4 signs and internal/fold already computes
// (slash.selected, team.members, ui.focus, usage.in/out, …). Drift in this
// direction is the expensive one: Phase 2's eval corpus measures the repair
// loop — order, patch, file:line error, retry — so a validator that rejects a
// signed bind teaches the model to avoid the vocabulary the product documents.
var signedBinds = map[string]bool{
	// §4.1 run state
	"chat.history":            true,
	"thinking.text":           true,
	"agent.working":           true,
	"agent.mode":              true,
	"agent.todos":             true,
	"model.name":              true,
	"usage.in":                true,
	"usage.out":               true,
	"usage.delta":             true,
	"session.tokens_used":     true,
	"session.new_milestone":   true,
	"team.members":            true,
	"todos.count":             true,
	"run.quiescent.diagnosis": true,

	// §4.2 agent blocked / remedy surface
	"agent.blocked.blocked_ref": true,
	"agent.blocked.blocked_on":  true,
	"agent.blocked.actor":       true,

	// §4.3 view state (arxi-tui's own contract)
	"slash.active":   true,
	"slash.typed":    true,
	"slash.matches":  true,
	"slash.selected": true,
	"slash.hint":     true,
	"status.active":  true,
	"ui.focus":       true,
	"ui.max":         true,
	"ui.surface":     true,

	// §2 bootstrap set — host survival state the raw scene may display
	"user.input":           true,
	"user.input.submitted": true,
	"host.escape.armed":    true,
	"host.scene.error":     true,
}

func validateBinds(n *Node) error {
	// Check this node's bind.
	if n.Bind != "" && !signedBinds[n.Bind] {
		return fmt.Errorf("unsigned bind %q in node type %q; every bind must appear in BINDS.md §4.5", n.Bind, n.Type)
	}

	// Check when conditions (they reference the same namespace).
	if n.When != "" {
		// When conditions may use operators like "!=", extract the bind path.
		parts := strings.Fields(n.When)
		if len(parts) > 0 {
			bindPath := parts[0]
			if !signedBinds[bindPath] && bindPath != "" {
				return fmt.Errorf("unsigned bind %q in when condition of node type %q; every bind must appear in BINDS.md §4.5", bindPath, n.Type)
			}
		}
	}

	// Recurse into children.
	for _, child := range n.Children {
		if err := validateBinds(child); err != nil {
			return err
		}
	}

	// Recurse into prefix/suffix/template.
	if prefix := n.PrefixNode(); prefix != nil {
		if err := validateBinds(prefix); err != nil {
			return err
		}
	}
	if n.Suffix != nil {
		if err := validateBinds(n.Suffix); err != nil {
			return err
		}
	}
	if n.RowTemplate != nil {
		if err := validateBinds(n.RowTemplate); err != nil {
			return err
		}
	}

	return nil
}

// ValidateTokens checks that every token referenced in the scene is defined in
// the theme. It walks the entire tree and collects all undefined token
// references, so the caller sees the full scope of the problem in one pass.
// Nodes without a style.token attribute are skipped — an absent token is not an error.
func ValidateTokens(doc *Document, thm *theme.Theme) []TokenError {
	var errs []TokenError
	if doc.Root != nil {
		collectTokenErrors(doc.Root, thm, &errs)
	}
	return errs
}

func collectTokenErrors(n *Node, thm *theme.Theme, errs *[]TokenError) {
	// Check if this node declares a token in its style map.
	if tokenName, ok := n.Style["token"]; ok && tokenName != "" {
		if !thm.Has(tokenName) {
			*errs = append(*errs, TokenError{
				Token:    tokenName,
				NodeType: n.Type,
			})
		}
	}

	// Check border style token if present.
	if borderStyle := n.BorderStyleName(); borderStyle != "" {
		if !thm.Has(borderStyle) {
			*errs = append(*errs, TokenError{
				Token:    borderStyle,
				NodeType: n.Type + " border",
			})
		}
	}

	// Recurse into children for nested structures (box nodes, overlays).
	for _, child := range n.Children {
		collectTokenErrors(child, thm, errs)
	}

	// Recurse into prefix/suffix nodes.
	if prefix := n.PrefixNode(); prefix != nil {
		collectTokenErrors(prefix, thm, errs)
	}
	if n.Suffix != nil {
		collectTokenErrors(n.Suffix, thm, errs)
	}
	if n.RowTemplate != nil {
		collectTokenErrors(n.RowTemplate, thm, errs)
	}
}
