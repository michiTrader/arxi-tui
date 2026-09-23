package patch

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file implements the `move` verb: `/ui move <id> <where>`. It is `add`'s
// sibling and reuses everything `add` already built — the write-path `where`
// vocabulary (parseWhere), the resolver that splices a node into a children
// list or a container (insert / insertSibling), and the source-to-source
// discipline that keeps a node's undeclared keys alive across the edit.
//
// The one thing `move` adds is the refusal `add` gets to skip. `add` inserts a
// brand-new subtree, which cannot already contain its own insertion point, so
// it can never form a cycle. `move` relocates an existing node, so it can be
// asked to place a node inside itself — `/ui move menu into cmds` where cmds is
// a descendant of menu — which would detach a subtree and reattach it below one
// of its own descendants, an unrepresentable tree. That is docs/ADDRESSING.md
// §2.4, and it is checked here before anything is detached.

// applyMove relocates the node carrying c.Subject to the position c.Where names,
// then re-parses and re-validates the result like every other verb.
//
// The edit rides the generic map tree for the reason patch.go's package comment
// argues: scene.Node drops any key it does not declare, so moving a node through
// the typed tree would strip every undeclared property off it — a plugin
// fragment's own fields, a future phase's fields — on the way past. A `move`
// that quietly rewrote the node it relocated would be the worst possible bug in
// a surface whose whole promise is that the user's document is theirs.
func (c Command) applyMove(name string, src []byte) (Result, error) {
	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: scene is not valid JSON: %w", name, err)
	}

	// One pass collects everything the refusals need: the ids that exist (for
	// the "which ids are there" listing), the count of anonymous nodes, the
	// number of input nodes (for the semantic anchors), and how many nodes carry
	// the subject id (the move target must resolve to exactly one).
	var ids []string
	unaddressable := 0
	inputs := 0
	subjectCount := 0
	forEachNode(tree, func(n map[string]any) {
		id, _ := n["id"].(string)
		if id != "" {
			ids = append(ids, id)
		} else {
			unaddressable++
		}
		if id == c.Subject {
			subjectCount++
		}
		if t, _ := n["type"].(string); t == "input" {
			inputs++
		}
	})

	if subjectCount == 0 {
		return Result{}, unknownTargetError(name, c.Subject, ids, unaddressable)
	}
	if subjectCount > 1 {
		// The §3 uniqueness invariant surfacing on the read side of `move`: two
		// nodes with one id make "move that node" name no single node, exactly
		// as they make every above/below/into ambiguous.
		return Result{}, fmt.Errorf("%s: id %q is carried by %d nodes in this scene, so `move %s` names no single node; ids are addresses and must be unique (docs/ADDRESSING.md §3)", name, c.Subject, subjectCount, c.Subject)
	}

	// The subject's subtree is what the cycle refusal is about. It is computed
	// against the whole tree (forEachNode descends prefix/suffix/row_template
	// too, not only children), so an anchor buried in a single-node slot of the
	// subject is still caught — a chokepoint that only looked at children would
	// miss it.
	subject := findNode(tree, matchID(c.Subject))
	subtreeIDs := map[string]bool{}
	subtreeHasInput := false
	forEachNode(subject, func(n map[string]any) {
		if id, _ := n["id"].(string); id != "" {
			subtreeIDs[id] = true
		}
		if t, _ := n["type"].(string); t == "input" {
			subtreeHasInput = true
		}
	})

	if err := c.refuseCycle(name, subtreeIDs, subtreeHasInput); err != nil {
		return Result{}, err
	}

	// Detach the subject from its current children list, then splice it in at
	// the resolved where. The order is safe: the cycle refusal above guarantees
	// the anchor is not inside the subject, so detaching the subject never
	// removes the anchor the reinsertion is about to look for.
	detached, ok := detachNode(tree, matchID(c.Subject))
	if !ok {
		return Result{}, fmt.Errorf("%s: node %q is the scene root, or sits in a single-node slot (prefix/suffix/row_template), so it is not in a children list and has no place to be moved from; `move` relocates a node that has siblings", name, c.Subject)
	}

	// insert reads c.Target as the anchor of the where clause and c.Where as the
	// resolved form — the same fields `add` fills — so the placement logic is
	// shared verbatim. The detached node plays the role `add` gives the fragment.
	if err := c.insert(name, tree, detached, ids, unaddressable, inputs); err != nil {
		return Result{}, err
	}

	out, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not re-serialise the patched scene: %w", name, err)
	}

	doc, err := scene.ParseNamed(name, out)
	if err != nil {
		return Result{}, err
	}
	if err := doc.RefuseEmpty(); err != nil {
		return Result{}, err
	}
	// Invariant 3: the moved document is held to the same validator the boot
	// path uses. A move cannot buy its way past it.
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}

	diff, err := DiffSource(src, out)
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not diff the patched scene: %w", name, err)
	}
	return Result{Doc: doc, Source: out, Summary: c.summary(), Diff: diff}, nil
}

// refuseCycle enforces docs/ADDRESSING.md §2.4: a node cannot become its own
// descendant. For the id anchors it is a set membership — the anchor is in the
// subject's subtree (which includes the subject itself, so `move x into x` is
// caught here too). For the semantic anchors the anchor has no id, so the test
// is whether the single input node lives inside the subject's subtree.
func (c Command) refuseCycle(name string, subtreeIDs map[string]bool, subtreeHasInput bool) error {
	switch c.Where {
	case "above", "below", "into", "into_top":
		if subtreeIDs[c.Target] {
			reason := fmt.Sprintf("a descendant of %q", c.Subject)
			if c.Target == c.Subject {
				reason = "the node being moved itself"
			}
			return fmt.Errorf("%s: cannot move %q %s %q: the anchor is %s, and a node cannot become its own descendant (docs/ADDRESSING.md §2.4)", name, c.Subject, c.whereVerb(), c.Target, reason)
		}
	case "below_input", "above_input":
		if subtreeHasInput {
			return fmt.Errorf("%s: cannot move %q %s: the input node is inside %q's own subtree, so the move would place %q beside a descendant of itself (docs/ADDRESSING.md §2.4)", name, c.Subject, c.Where, c.Subject, c.Subject)
		}
	}
	return nil
}

// detachNode removes the first node matching match from the children list that
// holds it, returning the removed node. It returns (nil, false) when the match
// is in no children list — the scene root, or a node occupying a single-node
// slot — which the caller turns into a refusal, because a node with no parent
// list has no position to be moved out of.
//
// It mirrors insertSibling's traversal: a map's own "children" array first, then
// every value, so a node nested anywhere is reached without the walker knowing
// the branch names.
func detachNode(v any, match func(map[string]any) bool) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		if ch, ok := t["children"].([]any); ok {
			for i, c := range ch {
				cm, ok := c.(map[string]any)
				if !ok || !match(cm) {
					continue
				}
				next := make([]any, 0, len(ch)-1)
				next = append(next, ch[:i]...)
				next = append(next, ch[i+1:]...)
				t["children"] = next
				return cm, true
			}
		}
		for _, child := range t {
			if n, ok := detachNode(child, match); ok {
				return n, true
			}
		}
	case []any:
		for _, child := range t {
			if n, ok := detachNode(child, match); ok {
				return n, true
			}
		}
	}
	return nil, false
}

// whereVerb renders the resolved where form as the keyword the user typed, so
// the cycle refusal names the move in the user's own words. into_top is the one
// form whose stored spelling is not what was typed.
func (c Command) whereVerb() string {
	if c.Where == "into_top" {
		return "into (top of)"
	}
	return c.Where
}

// moveSummary states the relocation in the user's vocabulary for the change-diff
// view, the same role addSummary plays for `add`: a diff of re-indented JSON is
// not a description of a change.
func (c Command) moveSummary() string {
	return fmt.Sprintf("moved %q %s", c.Subject, c.whereWords())
}
