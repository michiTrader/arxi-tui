package main

import (
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file is the first impure increment of the row-click focus work the three
// pure halves (scene.ExpandRowInterpolation PR #134, engine.RowPresses PR #135,
// rowFocusKey PR #136) were landed beneath. focusRing widens pressableIDs into
// the mixed Tab ring a template's instantiated rows belong in: an ordinary
// pressable node still contributes its raw id, but a row_template contributes
// one synthetic rowFocusKey per pressable node of each instantiated row instead
// of its raw subtree ids.
//
// # Why this replaces pressableIDs' walk into the template subtree
//
// pressableIDs walks a row_template subtree and adds any on_press node's raw id
// as an ordinary focus target. That was always a placeholder its own comment
// flagged: a template's authored id repeats across every instantiated row, so a
// raw id names all rows at once and Enter cannot know which the user meant.
// engine.RowPresses records the same decision — a row target is (RowIndex,
// NodeID), never the id alone. focusRing therefore stops the raw-id walk at the
// template boundary and enumerates RowPresses instead, so each instantiated row
// gets its own ring slot keyed by a rowFocusKey the dispatcher can decode back
// to the exact (node, row) whose expanded on_press to fire.
//
// # Why the state parameter, and why an error return
//
// The row scopes come from the fold: a template expands against the array its
// bind names, which lives in State. So unlike pressableIDs — a pure walk of the
// document's static shape — the ring cannot be built without the folded state.
// And a row whose on_press interpolates a {row.<field>} the scope lacks is a
// scope/schema drift RowPresses refuses rather than expanding to the wrong
// address; focusRing propagates that refusal rather than dropping the row,
// because a silently omitted ring slot is a button the user can see but never
// Tab to — the same silent-wrong-frame this repo holds worse than an error.
func focusRing(doc *scene.Document, state fold.State, hidden map[string]bool) ([]string, error) {
	var out []string
	var walkErr error
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil || walkErr != nil {
			return
		}
		if n.ID != "" && hidden[n.ID] {
			return // the node and its subtree are hidden; skip both (D3)
		}
		// The node's own raw id is an ordinary target: a row_template container
		// is a static node that may itself carry an on_press, distinct from the
		// per-row targets its template instantiates below.
		if n.OnPress != "" && n.ID != "" {
			out = append(out, n.ID)
		}
		if p := n.PrefixNode(); p != nil {
			walk(p)
		}
		if n.Suffix != nil {
			walk(n.Suffix)
		}
		for _, c := range n.Children {
			walk(c)
		}
		// The template subtree is NOT walked as static ids: its pressable nodes
		// are per-row, so they enter the ring through RowPresses as rowFocusKeys,
		// row-major and in the document order the row is drawn. This is the walk
		// pressableIDs left as a no-op placeholder, now that the pure halves exist.
		if n.RowTemplate != nil {
			presses, err := engine.RowPresses(n, state)
			if err != nil {
				walkErr = err
				return
			}
			for _, rp := range presses {
				out = append(out, rowFocusKey(rp.NodeID, rp.RowIndex))
			}
		}
	}
	if doc != nil {
		walk(doc.Root)
	}
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

// rowPressOnPress recovers the already-expanded on_press of the pressable node
// (nodeID, rowIndex) names, by re-running engine.RowPresses over the document
// and matching the pair a rowFocusKey decoded to. It is what Enter-dispatch
// calls once parseRowFocusKey reports focus names a row target, the row-side
// twin of findPressable: focusRing put the key into the ring, and this reads the
// action back out of the same enumeration so the ring and the dispatch agree on
// what a row target means.
//
// The first matching pair wins, which is the order focusRing appended the keys
// in, so a document whose distinct templates happen to reuse an id and row index
// resolves to the same slot the ring offered — the ring and the lookup never
// disagree even where the (id, row) pair is not globally unique. The expanded
// on_press is returned verbatim for scene.ParseAction; its {row.<field>} braces
// are already resolved (RowPresses' contract), so the caller dispatches it as-is
// and never re-runs the interpolation.
func rowPressOnPress(doc *scene.Document, state fold.State, nodeID string, rowIndex int) (onPress string, found bool, err error) {
	var out string
	var ok bool
	var walkErr error
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil || ok || walkErr != nil {
			return
		}
		if p := n.PrefixNode(); p != nil {
			walk(p)
		}
		if n.Suffix != nil {
			walk(n.Suffix)
		}
		for _, c := range n.Children {
			walk(c)
		}
		if n.RowTemplate != nil {
			presses, e := engine.RowPresses(n, state)
			if e != nil {
				walkErr = e
				return
			}
			for _, rp := range presses {
				if rp.NodeID == nodeID && rp.RowIndex == rowIndex {
					out, ok = rp.OnPress, true
					return
				}
			}
		}
	}
	if doc != nil {
		walk(doc.Root)
	}
	if walkErr != nil {
		return "", false, walkErr
	}
	return out, ok, nil
}
