package patch

import (
	"encoding/json"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file implements the `add` verb: `/ui add node <where> <fragment>`. It is
// the first /ui verb whose argument is a *position* in the tree rather than a
// node's own id, so it is the first to speak the write-path addressing
// vocabulary docs/ADDRESSING.md signs (D2). The read-path BINDS.md is
// deliberately not involved — insertion never reads bind state, it names a
// place.
//
// `add` is also the half of the add/move pair that ships first, for one
// structural reason: a fragment is a brand-new subtree, so it cannot already be
// an ancestor of its insertion point, so `add` has no cycle to refuse. `move`
// does, and that refusal is the piece of the write path this verb gets to skip.

// containerTypes are the node types that take children. `into` is refused
// against anything else, because a non-container has no children slot to append
// to and silently growing one would invent structure the author did not write.
var containerTypes = map[string]bool{
	"stack":   true,
	"row":     true,
	"box":     true,
	"overlay": true,
}

func containerType(t string) bool { return containerTypes[t] }

// cutField peels the first whitespace-delimited token off s and returns it with
// the untrimmed remainder. It is used instead of strings.Fields for `add`
// because the fragment is the remainder and must keep its exact bytes: a text
// value with runs of spaces would be collapsed by a re-join of Fields.
func cutField(s string) (field, rest string) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	s = s[i:]
	for j := 0; j < len(s); j++ {
		if s[j] == ' ' || s[j] == '\t' {
			return s[:j], s[j+1:]
		}
	}
	return s, ""
}

// forEachNode visits every node object (a map carrying a "type") in a generic
// JSON tree, descending through every value and array element. Like walk in
// patch.go it does not privilege the "children" branch: prefix, suffix and
// row_template all carry nodes, the count has grown once per phase, and a
// reader that knew their names would fall behind the format silently.
func forEachNode(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		if _, isNode := t["type"]; isNode {
			fn(t)
		}
		for _, child := range t {
			forEachNode(child, fn)
		}
	case []any:
		for _, child := range t {
			forEachNode(child, fn)
		}
	}
}

// parseWhere reads a where clause off the front of rest and returns the
// resolved form ("above"/"below"/"into"/"into_top"/"below_input"/"above_input"),
// the anchor id (empty for the two semantic anchors), and the untrimmed
// remainder after the clause. It is the single reader of docs/ADDRESSING.md's
// `where` grammar, shared by `add` (whose remainder is the fragment) and `move`
// (whose remainder must be empty), so the two verbs cannot drift in how a
// position is named — a divergence here would let `/ui add node into x top` and
// `/ui move y into x top` disagree about what `top` means.
//
// The errors are returned bare (no verb prefix) so each caller can name itself;
// they keep the substrings the grammar sweep pins ("needs a where clause",
// "unknown where", "needs a node id").
func parseWhere(rest string) (where, target, rem string, err error) {
	where, r := cutField(rest)
	switch where {
	case "below_input", "above_input":
		// The semantic anchors name the input node's role, not an id, so they
		// consume no further token and leave the anchor empty.
		return where, "", r, nil
	case "above", "below", "into":
		id, r2 := cutField(r)
		if id == "" {
			return "", "", "", fmt.Errorf("%s needs a node id: the where clause is `%s <id>`", where, where)
		}
		rem = r2
		if where == "into" {
			// `into <id> top` inserts as the first child; `into <id>` as the
			// last. The trailing "top" is optional, so it is only consumed when
			// it is actually present — a remainder that happens to start with a
			// bare word (a fragment, for `add`) is left untouched.
			if peek, r3 := cutField(rem); peek == "top" {
				where, rem = "into_top", r3
			}
		}
		return where, id, rem, nil
	case "":
		return "", "", "", fmt.Errorf("needs a where clause: one of above <id>, below <id>, into <id> [top], below_input, above_input")
	default:
		return "", "", "", fmt.Errorf("unknown where %q; expected one of above <id>, below <id>, into <id> [top], below_input, above_input", where)
	}
}

// parseAdd reads "add node <where> <fragment>" out of the raw command body.
//
// The body is the raw string (with the /ui prefix already peeled) rather than a
// token slice, so the fragment — everything after the where clause — is taken
// verbatim. Each refusal states the full grammar, because a command surface
// that rejects an input without showing the shape it wanted costs the user (or
// the repair loop) a turn spent guessing.
func parseAdd(body string) (Command, error) {
	_, rest := cutField(body) // drop the "add" verb token
	kw, rest := cutField(rest)
	if kw != "node" {
		return Command{}, fmt.Errorf("/ui add expects: /ui add node <where> <fragment>, where <where> is one of: above <id>, below <id>, into <id> [top], below_input, above_input")
	}

	where, target, rest, err := parseWhere(rest)
	if err != nil {
		return Command{}, fmt.Errorf("/ui add node %w", err)
	}

	fragment := trimSpace(rest)
	if fragment == "" {
		return Command{}, fmt.Errorf("/ui add node needs a JSON fragment to insert, e.g. /ui add node %s {\"type\":\"text\",\"text\":\"hello\"}", where)
	}
	return Command{Verb: "add", Where: where, Target: target, Fragment: fragment}, nil
}

// parseMove reads "move <id> <where>" out of the raw command body. `move` has
// no trailing fragment, so its remainder after the where clause must be empty;
// unexpected trailing input is refused rather than silently ignored, matching
// the surface's rule that an unclaimed token is never treated as chat.
func parseMove(body string) (Command, error) {
	_, rest := cutField(body) // drop the "move" verb token
	id, rest := cutField(rest)
	if id == "" {
		return Command{}, fmt.Errorf("/ui move expects: /ui move <id> <where>, where <where> is one of: above <id>, below <id>, into <id> [top], below_input, above_input")
	}

	where, target, rest, err := parseWhere(rest)
	if err != nil {
		return Command{}, fmt.Errorf("/ui move %s %w", id, err)
	}
	if extra := trimSpace(rest); extra != "" {
		return Command{}, fmt.Errorf("/ui move takes only <id> <where>; unexpected trailing input %q (a JSON fragment goes with /ui add, not /ui move)", extra)
	}
	return Command{Verb: "move", Subject: id, Where: where, Target: target}, nil
}

// trimSpace trims ASCII spaces and tabs from both ends without pulling in a
// package for it, matching cutField's whitespace class exactly.
func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// applyAdd performs the source-to-source insertion and re-validates the result.
//
// The edit walks the same generic map tree the other verbs do, for the same
// reason (patch.go's package comment): scene.Node drops any key it does not
// declare, so editing through the parsed tree would silently erase every
// unknown property — including a fragment's own — on the way past.
func (c Command) applyAdd(name string, src []byte) (Result, error) {
	// Parse the fragment before touching the tree: a fragment that is not a
	// JSON object, or that declares no type, is refused up front, so a bad
	// fragment never leaves the document half-edited.
	var frag map[string]any
	if err := json.Unmarshal([]byte(c.Fragment), &frag); err != nil {
		return Result{}, fmt.Errorf("%s: the fragment for /ui add is not a JSON object: %w", name, err)
	}
	if _, ok := frag["type"]; !ok {
		return Result{}, fmt.Errorf("%s: the fragment for /ui add declares no \"type\"; a scene node must have one", name)
	}

	var tree any
	if err := json.Unmarshal(src, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: scene is not valid JSON: %w", name, err)
	}

	// Collect the ids already in the document so a fragment that reuses one is
	// refused by attribution — the problem named is the fragment, not the
	// scene the user already had.
	existing := map[string]bool{}
	var ids []string
	unaddressable := 0
	inputs := 0
	forEachNode(tree, func(n map[string]any) {
		if id, _ := n["id"].(string); id != "" {
			existing[id] = true
			ids = append(ids, id)
		} else {
			unaddressable++
		}
		if t, _ := n["type"].(string); t == "input" {
			inputs++
		}
	})

	// A fragment that reuses an existing id, or repeats one within itself,
	// makes that id ambiguous — the load-time invariant docs/ADDRESSING.md §3
	// forbids, because every <id> form and every future `move` treats an id as
	// an address. Refused here rather than after re-validation, so the message
	// blames the fragment instead of some distant node.
	if dup := duplicateFragmentID(frag, existing); dup != "" {
		return Result{}, fmt.Errorf("%s: the fragment declares id %q, which is already used in this scene; ids are addresses and must be unique (docs/ADDRESSING.md §3), or every above/below/into that names it — and a later /ui move — becomes ambiguous", name, dup)
	}

	if err := c.insert(name, tree, frag, ids, unaddressable, inputs); err != nil {
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
	// Invariant 3: the inserted fragment is held to the same validator the boot
	// path uses. A node cannot buy its way past it by arriving through /ui.
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}

	diff, err := DiffSource(src, out)
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not diff the patched scene: %w", name, err)
	}
	return Result{Doc: doc, Source: out, Summary: c.summary(), Diff: diff}, nil
}

// insert resolves the where clause against the tree and splices the fragment
// in, mutating the generic tree in place. It refuses, with an address, every
// case docs/ADDRESSING.md §2 names: an unknown or ambiguous id, an `into` onto
// a non-container, and a semantic anchor with no single input node.
func (c Command) insert(name string, tree any, frag map[string]any, ids []string, unaddressable, inputs int) error {
	switch c.Where {
	case "below_input", "above_input":
		if inputs == 0 {
			return fmt.Errorf("%s: %s addresses the node the user types into, but this scene declares no input node (docs/ADDRESSING.md §2)", name, c.Where)
		}
		if inputs > 1 {
			return fmt.Errorf("%s: %s is ambiguous: this scene has %d input nodes, so \"the input\" names no single position (docs/ADDRESSING.md §2)", name, c.Where, inputs)
		}
		after := c.Where == "below_input"
		if insertSibling(tree, isInput, after, frag) {
			return nil
		}
		return errNoSiblingSlot(name, "the input node")

	case "above", "below":
		if err := c.requireUniqueID(name, tree, ids, unaddressable); err != nil {
			return err
		}
		after := c.Where == "below"
		if insertSibling(tree, matchID(c.Target), after, frag) {
			return nil
		}
		return errNoSiblingSlot(name, fmt.Sprintf("node %q", c.Target))

	case "into", "into_top":
		if err := c.requireUniqueID(name, tree, ids, unaddressable); err != nil {
			return err
		}
		target := findNode(tree, matchID(c.Target))
		t, _ := target["type"].(string)
		if !containerType(t) {
			return fmt.Errorf("%s: node %q has type %q, and only containers (stack, row, box, overlay) take children with `into`", name, c.Target, t)
		}
		ch, _ := target["children"].([]any)
		if c.Where == "into_top" {
			target["children"] = append([]any{any(frag)}, ch...)
		} else {
			target["children"] = append(append([]any{}, ch...), any(frag))
		}
		return nil

	default:
		return fmt.Errorf("%s: /ui add node: unknown where %q", name, c.Where)
	}
}

// errNoSiblingSlot explains an anchor that resolved to a node with no sibling
// list to insert into — the scene root, or a node occupying a single-node slot
// (prefix/suffix/row_template). The distinction matters: the anchor was found,
// so this is not an "unknown id"; the position it names simply cannot hold a
// sibling, and the remedy is `into` on a container.
func errNoSiblingSlot(name, anchor string) error {
	return fmt.Errorf("%s: %s is not inside a children list (it is the scene root, or sits in a single-node slot like prefix/suffix), so it has no siblings to add beside; use `into <container-id>` instead", name, anchor)
}

// requireUniqueID refuses an <id> anchor that names no node, or more than one.
// A duplicate is the §3 invariant surfacing on the read side of the write path:
// two nodes with one id make every above/below/into that names it ambiguous.
func (c Command) requireUniqueID(name string, tree any, ids []string, unaddressable int) error {
	count := 0
	forEachNode(tree, func(n map[string]any) {
		if id, _ := n["id"].(string); id == c.Target {
			count++
		}
	})
	if count == 0 {
		return unknownTargetError(name, c.Target, ids, unaddressable)
	}
	if count > 1 {
		return fmt.Errorf("%s: id %q is carried by %d nodes in this scene, so `%s %s` names no single position; ids are addresses and must be unique (docs/ADDRESSING.md §3)", name, c.Target, count, c.Where, c.Target)
	}
	return nil
}

// insertSibling splices frag into the children list that contains the first
// node matching match, before it or after it. It returns false when no children
// list contains a match — the anchor is the root or sits in a single-node slot.
//
// It inspects a map's own "children" array first, then recurses into every
// value, so a sibling list nested anywhere (under an overlay, a box, a template)
// is reached without the traversal knowing the branch names.
func insertSibling(v any, match func(map[string]any) bool, after bool, frag map[string]any) bool {
	switch t := v.(type) {
	case map[string]any:
		if ch, ok := t["children"].([]any); ok {
			for i, c := range ch {
				cm, ok := c.(map[string]any)
				if !ok || !match(cm) {
					continue
				}
				idx := i
				if after {
					idx = i + 1
				}
				next := make([]any, 0, len(ch)+1)
				next = append(next, ch[:idx]...)
				next = append(next, any(frag))
				next = append(next, ch[idx:]...)
				t["children"] = next
				return true
			}
		}
		for _, child := range t {
			if insertSibling(child, match, after, frag) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if insertSibling(child, match, after, frag) {
				return true
			}
		}
	}
	return false
}

// findNode returns the first node matching match, or nil.
func findNode(v any, match func(map[string]any) bool) map[string]any {
	var found map[string]any
	forEachNode(v, func(n map[string]any) {
		if found == nil && match(n) {
			found = n
		}
	})
	return found
}

func matchID(id string) func(map[string]any) bool {
	return func(n map[string]any) bool {
		got, _ := n["id"].(string)
		return got == id
	}
}

func isInput(n map[string]any) bool {
	t, _ := n["type"].(string)
	return t == "input"
}

// duplicateFragmentID returns the first id the fragment declares that collides
// with an existing document id or with another id in the fragment itself, or ""
// if the fragment introduces no clash.
func duplicateFragmentID(frag map[string]any, existing map[string]bool) string {
	seen := map[string]bool{}
	dup := ""
	forEachNode(frag, func(n map[string]any) {
		if dup != "" {
			return
		}
		id, _ := n["id"].(string)
		if id == "" {
			return
		}
		if existing[id] || seen[id] {
			dup = id
			return
		}
		seen[id] = true
	})
	return dup
}

// addSummary states the insertion in the user's vocabulary, so the change-diff
// view can show what was done before it is trusted — a diff of re-indented JSON
// is not a description of a change.
func (c Command) addSummary() string {
	kind := ""
	var frag map[string]any
	if json.Unmarshal([]byte(c.Fragment), &frag) == nil {
		if t, _ := frag["type"].(string); t != "" {
			kind = fmt.Sprintf("%q ", t)
		}
	}
	return fmt.Sprintf("added a %snode %s", kind, c.whereWords())
}

// whereWords renders the resolved where clause as a human phrase.
func (c Command) whereWords() string {
	switch c.Where {
	case "above":
		return fmt.Sprintf("above %q", c.Target)
	case "below":
		return fmt.Sprintf("below %q", c.Target)
	case "into":
		return fmt.Sprintf("into %q", c.Target)
	case "into_top":
		return fmt.Sprintf("at the top of %q", c.Target)
	case "below_input":
		return "below the input"
	case "above_input":
		return "above the input"
	default:
		return c.Where
	}
}
