package patch

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file implements the `remove` verb: `/ui remove <id>`. Until it existed the only
// way to take a node out of the document was to rewrite the whole scene, which is how an
// agent asked to "replace the banner" ended up pasting two screens of JSON, tripping the
// validator several times and giving up. `hide` is not a substitute: it only filters the
// node out of the current session and leaves it in the document.
//
// `remove` deletes the node and everything under it. Like `move` it works on the
// generic tree so every key it does not touch survives, and the result goes through the
// same parse and validation as every other verb. What it refuses:
//
//   - the scene root, which has no parent list to leave;
//   - a node that holds the input bar (or is it): the user could not type any more, the
//     same rule the host applies to a whole new scene;
//   - an id that does not exist, or that two nodes share (the id must name one node).

func (c Command) applyRemove(name string, src []byte) (Result, error) {
	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: scene is not valid JSON: %w", name, err)
	}
	var ids []string
	unaddressable, count := 0, 0
	forEachNode(tree, func(n map[string]any) {
		id, _ := n["id"].(string)
		if id != "" {
			ids = append(ids, id)
		} else {
			unaddressable++
		}
		if id == c.Target {
			count++
		}
	})
	if count == 0 {
		return Result{}, unknownTargetError(name, c.Target, ids, unaddressable)
	}
	if count > 1 {
		return Result{}, fmt.Errorf("%s: id %q is carried by %d nodes in this scene, so `remove %s` names no single node; ids are addresses and must be unique (docs/ADDRESSING.md §3)", name, c.Target, count, c.Target)
	}
	holdsInput := false
	forEachNode(findNode(tree, matchID(c.Target)), func(n map[string]any) {
		if t, _ := n["type"].(string); t == "input" {
			holdsInput = true
		}
	})
	if holdsInput {
		return Result{}, fmt.Errorf("%s: %q is, or holds, the input bar; removing it would leave nowhere to type. Remove the other nodes around it instead", name, c.Target)
	}
	if _, ok := detachNode(tree, matchID(c.Target)); !ok {
		return Result{}, fmt.Errorf("%s: node %q is the scene root, or sits in a single-node slot (prefix/suffix/row_template), so it is not in a children list and cannot be removed; remove its siblings or change it with `set`", name, c.Target)
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
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}
	diff, err := DiffSource(src, out)
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not diff the patched scene: %w", name, err)
	}
	return Result{Doc: doc, Source: out, Summary: c.summary(), Diff: diff}, nil
}
