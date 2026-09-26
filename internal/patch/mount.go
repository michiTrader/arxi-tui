package patch

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file implements H3: composing a loaded plugin manifest's scene fragments
// into a host document, and its inverse. It lives in internal/patch, not in
// internal/ext, on purpose. DESIGN-BLOCK-H.md H-B.1 makes the call explicit —
// "mounting is `/ui add node`, from the host side instead of the user side" —
// so the placement engine H3 needs is the one add.go already built (insert /
// insertSibling over the generic map tree), and rebuilding it in ext would be a
// second reader of docs/ADDRESSING.md's `where` grammar, the exact drift the one
// shared parseWhere exists to prevent. ext stays the pure loader (it validates a
// manifest and imports no UI package); patch, which already owns the write path,
// imports ext to place what ext produced. The dependency points patch -> ext,
// never the reverse, so ext keeps its arch seam.
//
// The one addressing rule Block H adds that D2 did not need is fragment-id
// prefixing (H-B.3): D2 never composed two authors' trees, so it could leave ids
// raw; a plugin ecosystem cannot, because two independent plugins both using
// "overlay" as an id would collide the moment both are mounted. The loader
// rewrites every mounted id to `<plugin-id>/<id>` before the uniqueness invariant
// runs, so N strangers' trees compose without coordination — and a surviving
// collision is then a plugin colliding with itself, named as the author's bug.

// overlayAnchors is the closed set of `where` values that mean "a new overlay
// child of root at that anchor" (DESIGN-BLOCK-H.md H-B / ADDRESSING.md §4) rather
// than a D2 position relative to an existing id. It is exactly the anchor
// vocabulary the engine's overlay layout recognises (internal/engine/render.go
// renderStack's anchor switch): the set is closed because an anchor is code on
// the render side, so a `where` naming one the engine cannot place would validate
// here and float nowhere. Keeping the two in step is why this mirrors the
// engine's set rather than inventing a parallel one.
var overlayAnchors = map[string]bool{
	"top-right": true,
	"top-left":  true,
	"top":       true,
	"bottom":    true,
	"full":      true,
}

// Mount composes every fragment a declarative manifest mounts into the host
// scene, returning the patched document exactly as a /ui edit does — a Result the
// caller can draw, persist and diff. It is the H3 half of the plugin path; H6
// wires it behind `/ui plugin add <url>` (fetch, validate, mount).
//
// The manifest is validated first even though the load path already did: Mount is
// the single point where foreign fragments enter the host tree, so it enforces
// H2's guarantees on its own rather than trusting a caller to have run them —
// a behavioral manifest, or one with an invalid fragment, is refused here before
// a byte of it reaches the document (the same "an invariant enforced by
// assumption is enforced nowhere" argument patch.go makes for re-validation).
func Mount(hostName string, hostSrc []byte, m *ext.Manifest) (Result, error) {
	if m == nil {
		return Result{}, fmt.Errorf("%s: cannot mount a nil manifest", hostName)
	}
	if err := m.Validate(); err != nil {
		return Result{}, err
	}

	var tree any
	if err := json.Unmarshal(hostSrc, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: host scene is not valid JSON: %w", hostName, err)
	}

	prefix := m.ID + "/"
	for i, mnt := range m.Mounts {
		var frag map[string]any
		if err := json.Unmarshal(mnt.Fragment, &frag); err != nil {
			return Result{}, fmt.Errorf("%s: plugin %q mount %d fragment is not a JSON object: %w", hostName, m.ID, i, err)
		}
		// Prefix before placement: the ids the uniqueness check and every future
		// address see are the composed-tree ids, so the rewrite must be done
		// before the fragment joins the tree, not after.
		prefixFragmentIDs(frag, prefix)
		if err := mountFragment(hostName, m.ID, tree, mnt.Where, frag); err != nil {
			return Result{}, err
		}
	}

	// The id-uniqueness invariant (ADDRESSING.md §3), run over the composed tree
	// after prefixing. The prefix removes cross-plugin collisions by construction,
	// so a duplicate that survives is the plugin declaring the same raw id twice
	// in its own fragments — the author's bug, named as such.
	if dup := firstDuplicateID(tree); dup != "" {
		return Result{}, fmt.Errorf("%s: plugin %q mounts more than one node with id %q; ids are addresses and must be unique (docs/ADDRESSING.md §3), and because every mounted id is %q-prefixed a surviving collision means the plugin declared the same raw id twice in its own fragments", hostName, m.ID, dup, prefix)
	}

	out, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not re-serialise the mounted scene: %w", hostName, err)
	}

	doc, err := scene.ParseNamed(hostName, out)
	if err != nil {
		return Result{}, err
	}
	if err := doc.RefuseEmpty(); err != nil {
		return Result{}, err
	}
	// Invariant 3: the composed document is held to the same validator the boot
	// path uses. A plugin cannot buy its fragments past it by arriving through a
	// mount rather than through the user's own document.
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}

	diff, err := DiffSource(hostSrc, out)
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not diff the mounted scene: %w", hostName, err)
	}
	return Result{
		Doc:     doc,
		Source:  out,
		Summary: fmt.Sprintf("mounted plugin %q (%d fragment(s))", m.ID, len(m.Mounts)),
		Diff:    diff,
	}, nil
}

// mountFragment places one already-prefixed fragment at the position its `where`
// names. An overlay anchor (H-B / ADDRESSING.md §4) is the one form D2 does not
// have — it names no existing id, so it cannot go through parseWhere — and it
// means "a new top-level child of root", which is how a plugin overlay floats
// without referencing a host node. Every other form is a D2 position, resolved by
// the same parseWhere and insert `add` and `move` use, so a plugin author and a
// /ui user cannot drift on what `below <id>` or `into <id> top` mean.
func mountFragment(hostName, pluginID string, tree any, where string, frag map[string]any) error {
	if overlayAnchors[trimSpace(where)] {
		return appendToRoot(hostName, pluginID, tree, frag)
	}

	w, target, rem, err := parseWhere(where)
	if err != nil {
		// parseWhere returns bare errors so each caller can name itself; an
		// overlay anchor was already handled above, so an unknown where here is a
		// genuinely unusable location for a mount, not a missed anchor.
		return fmt.Errorf("%s: plugin %q mount %w (or an overlay anchor: %s)", hostName, pluginID, err, overlayAnchorList())
	}
	if extra := trimSpace(rem); extra != "" {
		return fmt.Errorf("%s: plugin %q mount where %q has unexpected trailing input %q; a mount's where is a single position, not a where plus a fragment (the fragment is the mount's own field)", hostName, pluginID, where, extra)
	}

	// insert reads Where as the resolved form and Target as the anchor id — the
	// same fields add fills — so the placement is shared verbatim. The collision
	// inputs it needs are recomputed from the current tree, because an earlier
	// mount in this same call may have grown it.
	var ids []string
	unaddressable, inputs := 0, 0
	forEachNode(tree, func(n map[string]any) {
		if id, _ := n["id"].(string); id != "" {
			ids = append(ids, id)
		} else {
			unaddressable++
		}
		if t, _ := n["type"].(string); t == "input" {
			inputs++
		}
	})
	c := Command{Verb: "add", Where: w, Target: target}
	return c.insert(hostName, tree, frag, ids, unaddressable, inputs)
}

// appendToRoot adds a fragment as the last top-level child of the scene's root,
// the placement an overlay-anchor `where` resolves to. The fragment carries its
// own `anchor` (Scene 6's ticker fragment is an overlay anchored top-right), so
// this only has to put it at the top level where the stack's positioning pass
// will float it; it does not synthesise or rewrite the anchor.
func appendToRoot(hostName, pluginID string, tree any, frag map[string]any) error {
	obj, ok := tree.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: cannot mount plugin %q at an overlay anchor: the host scene is not a JSON object", hostName, pluginID)
	}
	root, ok := obj["root"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s: cannot mount plugin %q at an overlay anchor: the host scene has no root object to attach a top-level overlay to", hostName, pluginID)
	}
	ch, _ := root["children"].([]any)
	root["children"] = append(append([]any{}, ch...), any(frag))
	return nil
}

// prefixFragmentIDs rewrites every node id in a fragment to `<prefix><id>` (H-B.3).
// It visits every node the tree carries, not only the children branch, for the
// reason forEachNode does: prefix/suffix/row_template carry nodes too, and a
// prefixer that missed one would leave an unprefixed id free to collide.
func prefixFragmentIDs(frag map[string]any, prefix string) {
	forEachNode(frag, func(n map[string]any) {
		if id, _ := n["id"].(string); id != "" {
			n["id"] = prefix + id
		}
	})
}

// firstDuplicateID returns the first non-empty id carried by more than one node
// in the tree, or "" when every id is unique. It is the read side of
// ADDRESSING.md §3, run here rather than in the scene validator because the
// invariant is still enforced at the write boundary (F1/F2/F3), and a mount is a
// write.
func firstDuplicateID(tree any) string {
	seen := map[string]bool{}
	dup := ""
	forEachNode(tree, func(n map[string]any) {
		if dup != "" {
			return
		}
		id, _ := n["id"].(string)
		if id == "" {
			return
		}
		if seen[id] {
			dup = id
			return
		}
		seen[id] = true
	})
	return dup
}

// overlayAnchorList renders the closed overlay-anchor set for an error message,
// sorted so the message is stable across runs.
func overlayAnchorList() string {
	out := make([]string, 0, len(overlayAnchors))
	for a := range overlayAnchors {
		out = append(out, a)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Unmount is Mount's inverse: it drops every node whose id begins `<plugin-id>/`,
// returning the host document as it was before the plugin was mounted. It is the
// symmetry DESIGN-BLOCK-H.md fork 4 requires — a mount with no unmount is a
// one-way door — and the property that makes the id namespacing testable: mount
// then unmount canonicalises back to the original document, because prefixing
// makes "the plugin's nodes" a decidable set that names no host node.
//
// It removes only nodes, not tokens: H3 mounts fragments, and the token merge is
// H4's, so the token half of unmount lands with it. Removal names no host node
// (a host id never begins `<plugin-id>/`, since `/` is not in the id grammar), so
// it cannot orphan one — the same property that lets `show *` clear ui.hidden
// without naming an id (F3).
func Unmount(hostName string, hostSrc []byte, pluginID string) (Result, error) {
	if pluginID == "" {
		return Result{}, fmt.Errorf("%s: unmount needs a plugin id; it names the `<plugin-id>/` namespace to remove", hostName)
	}
	prefix := pluginID + "/"

	var tree any
	if err := json.Unmarshal(hostSrc, &tree); err != nil {
		return Result{}, fmt.Errorf("%s: host scene is not valid JSON: %w", hostName, err)
	}

	removed := removeByIDPrefix(tree, prefix)
	if removed == 0 {
		return Result{}, fmt.Errorf("%s: no node carries an id beginning %q, so plugin %q is not mounted here and has nothing to unmount", hostName, prefix, pluginID)
	}

	out, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not re-serialise the unmounted scene: %w", hostName, err)
	}

	doc, err := scene.ParseNamed(hostName, out)
	if err != nil {
		return Result{}, err
	}
	if err := doc.RefuseEmpty(); err != nil {
		return Result{}, err
	}
	if err := doc.Validate(); err != nil {
		return Result{}, err
	}

	diff, err := DiffSource(hostSrc, out)
	if err != nil {
		return Result{}, fmt.Errorf("%s: could not diff the unmounted scene: %w", hostName, err)
	}
	return Result{
		Doc:     doc,
		Source:  out,
		Summary: fmt.Sprintf("unmounted plugin %q (%d node(s))", pluginID, removed),
		Diff:    diff,
	}, nil
}

// removeByIDPrefix drops every node whose id begins prefix from the children list
// that holds it, returning the count removed. A removed node takes its whole
// subtree with it — the subtree's ids all share the prefix, so there is nothing
// to recurse into — which is why a dropped child is not descended.
//
// It filters children lists specifically, because a mounted node only ever lands
// in one: appendToRoot appends to root.children, and insert splices into a
// children array. It still recurses through the other node-bearing branches so a
// fragment mounted `into` a nested container is reached, without the traversal
// needing to know which branch that container sits under.
func removeByIDPrefix(v any, prefix string) int {
	count := 0
	switch t := v.(type) {
	case map[string]any:
		if ch, ok := t["children"].([]any); ok {
			next := make([]any, 0, len(ch))
			for _, c := range ch {
				if cm, ok := c.(map[string]any); ok {
					if id, _ := cm["id"].(string); strings.HasPrefix(id, prefix) {
						count++
						continue
					}
				}
				next = append(next, c)
				count += removeByIDPrefix(c, prefix)
			}
			t["children"] = next
		}
		for k, child := range t {
			if k == "children" {
				continue
			}
			count += removeByIDPrefix(child, prefix)
		}
	case []any:
		for _, c := range t {
			count += removeByIDPrefix(c, prefix)
		}
	}
	return count
}
