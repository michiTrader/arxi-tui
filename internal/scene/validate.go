package scene

import (
	"fmt"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TokenError records a reference to an undefined token in a scene. LESSONS.md
// carries this rule over from arxi-sim's theme audit with the polarity
// inverted — the vocabulary is open, so it is the *reference* that is checked —
// but the error discipline is unchanged: "a referenced-but-undefined token is
// an error with file:line, and an unused token is a warning".
type TokenError struct {
	Token    string // The token name that was not found in the theme
	NodeType string // The type of node that referenced it
	Loc      Loc    // Where the reference is, for the file:line rule
}

func (e TokenError) Error() string {
	msg := fmt.Sprintf("node type %q references undefined token %q", e.NodeType, e.Token)
	if e.Loc == (Loc{}) {
		return msg
	}
	return e.Loc.String() + ": " + msg
}

// Validate checks that every bind and when string referenced in the scene is
// in the signed inventory (docs/BINDS.md §4.5). This is the exit criterion:
// an unsigned bind is a load-time error. Returns the first validation error
// encountered, or nil if the document is valid.
//
// §4.5 specifies the refusal exactly — "a file:line error pointing at the
// offending node" — so the walk carries each node's access path and the error
// resolves it to a position through the document's address book.
func (d *Document) Validate() error {
	if d == nil || d.Root == nil {
		return nil
	}
	return d.validateBinds(d.Root, nodePathRoot)
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

// SignedBinds returns the §4.5 inventory: every bind path a scene may
// reference, sorted. The slice is freshly built on each call, so a caller
// cannot mutate the validator's inventory by holding onto it.
//
// This exists for one reason that is worth stating, because the obvious
// alternative is cheaper and wrong. Phase 2's repair loop has to tell the model
// which binds exist; without that, the first turn of every case is spent
// guessing the vocabulary, and the corpus measures recall of an undocumented
// list instead of the repair loop PLAN.md asked it to measure. The cheap
// alternative is to write the list out again in the runner's prompt — which
// would be the *fourth* copy of the inventory (docs/BINDS.md §4, signedBinds
// here, and the two audit directions in binds_audit_test.go). That map has
// already drifted from the document in both directions once, and the guard
// test only exists because it did. A hand-copied prompt list would drift the
// same way, silently, and its failure mode is the expensive one: the model is
// told a signed bind does not exist, avoids it, and the corpus records that as
// the model's failure rather than the prompt's.
//
// Exporting the inventory rather than the map keeps the validator the single
// source: there is no second list to keep in step, so there is no third drift
// guard to write.
func SignedBinds() []string {
	out := make([]string, 0, len(signedBinds))
	for bind := range signedBinds {
		out = append(out, bind)
	}
	sort.Strings(out)
	return out
}

// validateBinds walks a subtree, carrying the node's access path so a refusal
// can name where it happened. The path is threaded as an argument rather than
// stored on Node because the tree is also built by hand and by future patch
// code, and a position field would then be a field that is sometimes a lie.
func (d *Document) validateBinds(n *Node, path string) error {
	// Check this node's bind.
	if n.Bind != "" && !signedBinds[n.Bind] {
		return &Error{
			Loc: d.locOf(path),
			Msg: fmt.Sprintf("unsigned bind %q in node type %q; every bind must appear in BINDS.md §4.5", n.Bind, n.Type),
		}
	}

	// Check when conditions (they reference the same namespace).
	if n.When != "" {
		// When conditions may use operators like "!=", extract the bind path.
		parts := strings.Fields(n.When)
		if len(parts) > 0 {
			bindPath := parts[0]
			if !signedBinds[bindPath] && bindPath != "" {
				return &Error{
					Loc: d.locOf(path),
					Msg: fmt.Sprintf("unsigned bind %q in when condition of node type %q; every bind must appear in BINDS.md §4.5", bindPath, n.Type),
				}
			}
		}
	}

	// Recurse into children.
	for i, child := range n.Children {
		if err := d.validateBinds(child, childPath(path, i)); err != nil {
			return err
		}
	}

	// Recurse into prefix/suffix/template.
	if prefix := n.PrefixNode(); prefix != nil {
		if err := d.validateBinds(prefix, prefixPath(path)); err != nil {
			return err
		}
	}
	if n.Suffix != nil {
		if err := d.validateBinds(n.Suffix, suffixPath(path)); err != nil {
			return err
		}
	}
	if n.RowTemplate != nil {
		if err := d.validateBinds(n.RowTemplate, templatePath(path)); err != nil {
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
	if doc != nil && doc.Root != nil {
		doc.collectTokenErrors(doc.Root, nodePathRoot, thm, &errs)
	}
	return errs
}

func (d *Document) collectTokenErrors(n *Node, path string, thm *theme.Theme, errs *[]TokenError) {
	// Check if this node declares a token in its style map.
	if tokenName, ok := n.Style["token"]; ok && tokenName != "" {
		if !thm.Has(tokenName) {
			*errs = append(*errs, TokenError{
				Token:    tokenName,
				NodeType: n.Type,
				Loc:      d.locOf(path),
			})
		}
	}

	// Check border style token if present.
	if borderStyle := n.BorderStyleName(); borderStyle != "" {
		if !thm.Has(borderStyle) {
			*errs = append(*errs, TokenError{
				Token:    borderStyle,
				NodeType: n.Type + " border",
				Loc:      d.locOf(path),
			})
		}
	}

	// Recurse into children for nested structures (box nodes, overlays).
	for i, child := range n.Children {
		d.collectTokenErrors(child, childPath(path, i), thm, errs)
	}

	// Recurse into prefix/suffix nodes.
	if prefix := n.PrefixNode(); prefix != nil {
		d.collectTokenErrors(prefix, prefixPath(path), thm, errs)
	}
	if n.Suffix != nil {
		d.collectTokenErrors(n.Suffix, suffixPath(path), thm, errs)
	}
	if n.RowTemplate != nil {
		d.collectTokenErrors(n.RowTemplate, templatePath(path), thm, errs)
	}
}
