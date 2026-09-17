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

// signedBinds is the §4.5 inventory: every bind the golden scenes may reference.
// A bind not in this list fails validation. This list is the contract between
// the scene format and the fold implementation.
var signedBinds = map[string]bool{
	"agent.mode":          true,
	"agent.status":        true,
	"agent.todos":         true,
	"agent.working":       true,
	"banner.text":         true,
	"chat.history":        true,
	"model.name":          true,
	"session.tokens":      true,
	"session.tokens_used": true,
	"slash.active":        true,
	"slash.matches":       true,
	"thinking.text":       true,
	"usage.delta":         true,
	"user.input":          true,
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
