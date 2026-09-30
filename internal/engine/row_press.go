package engine

import (
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// RowPress is one pressable node inside one instantiated row of a row_template,
// with its {row.field} interpolation already resolved against that row's scope.
//
// It is the unit the (still deferred) focus-ring wiring consumes. A template
// expands to N rows at render time; a row's subtree may carry several on_press
// nodes (Scene 8's approve/reject/reply buttons), and the same authored id
// repeats across every row, so a pressable row target is identified by the pair
// (RowIndex, NodeID) -- never by node id alone, which is not unique under a
// template. That pair is exactly what the loop must key ui.focus on for a row
// press, which is why this type carries both rather than a single synthetic id
// string: the id scheme is the loop's to choose, and pinning one here would
// commit the format before the consumer that owns it exists.
type RowPress struct {
	// RowIndex is the 0-based position of the instantiated row, in the order
	// rowScopesFor yields (the array element order the fold holds).
	RowIndex int
	// NodeID is the id of the pressable node within the row_template subtree, the
	// raw authored id shared by every row's instance of that node, so it is
	// meaningful only paired with RowIndex.
	NodeID string
	// OnPress is the node's on_press with every {row.<field>} resolved against
	// this row's scope, ready for scene.ParseAction. The braces are gone: a
	// consumer dispatches this string as-is and never re-runs the interpolation.
	OnPress string
}

// RowPresses enumerates every pressable node in every instantiated row of a
// row_template, resolving each on_press against its row scope. It is the pure
// half of the row-click work E4/H8 parked: rowScopesFor already builds the
// per-element scopes and scene.ExpandRowInterpolation already resolves one
// action, so this composes the two into the flat, row-major list the focus ring
// will Tab over and the dispatcher will look a pressed (row, node) up in. It is
// landed ahead of that impure wiring per the block pattern, so the resolution is
// network-free and testable before a focus cursor or a live press exists.
//
// A node with no row_template has no rows and yields nothing. Within a row the
// nodes are walked in document order (prefix, suffix, children, then a nested
// template), the same order pressableIDs walks a static subtree, so the row ring
// reads in the order the row is drawn.
//
// The whole on_press string is expanded, not just the parsed argument: a
// validated on_press can only carry braces in its argument region, because
// ParseAction cuts the prefix on the first colon and a brace before it is an
// unknown prefix the validator already refused. Expanding the verbatim string
// therefore equals expanding the argument, and it keeps ParseAction the single
// reader of the grammar -- reconstructing "cmd:"+arg here would be a second
// writer of the prefix vocabulary, the drift the one-parser design exists to
// prevent.
//
// An on_press that interpolates a field the row scope lacks is returned as an
// error, not skipped: ExpandRowInterpolation refuses it because the validator
// already accepted the field against the element schema, so a miss is a
// scope/schema drift, and a silently dropped row press is a button that looks
// present and does nothing -- the silent-drop shape row_template exists to
// avoid.
func RowPresses(n *scene.Node, state fold.State) ([]RowPress, error) {
	if n == nil || n.RowTemplate == nil {
		return nil, nil
	}
	var out []RowPress
	for i, row := range rowScopesFor(n.Bind, state) {
		var walkErr error
		var walk func(m *scene.Node)
		walk = func(m *scene.Node) {
			if m == nil || walkErr != nil {
				return
			}
			if m.OnPress != "" && m.ID != "" {
				expanded, err := scene.ExpandRowInterpolation(m.OnPress, row)
				if err != nil {
					walkErr = err
					return
				}
				out = append(out, RowPress{RowIndex: i, NodeID: m.ID, OnPress: expanded})
			}
			if p := m.PrefixNode(); p != nil {
				walk(p)
			}
			if m.Suffix != nil {
				walk(m.Suffix)
			}
			for _, c := range m.Children {
				walk(c)
			}
			if m.RowTemplate != nil {
				walk(m.RowTemplate)
			}
		}
		walk(n.RowTemplate)
		if walkErr != nil {
			return nil, walkErr
		}
	}
	return out, nil
}
