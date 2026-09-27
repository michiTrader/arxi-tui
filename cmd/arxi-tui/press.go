package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the host half of H8: it turns a keypress into a press of a
// pressable node's on_press action (BINDS.md §4.8). The scene layer parses and
// validates the action grammar (scene.ParseAction); here the loop chooses which
// node is focused, and dispatches the focused node's action when Enter is hit.
//
// # Why focus lives in the loop, not the fold
//
// ui.focus is host-owned view state (BINDS.md §4.3), held across frames like the
// input buffer and ui.hidden: the fold is rebuilt from the log each frame and
// would forget a cursor kept in State, so the loop owns `uiFocus` and re-attaches
// it every repaint. The engine already reads state.UIFocus for focus_glow, so a
// moved cursor lights the glow with no new engine code.

// pressableIDs returns the ids of nodes carrying an on_press action, in document
// order — the Tab ring Q19 signs as "scene order". A node with no id is skipped
// (ui.focus names an id, so an id-less node cannot be a focus target), and a
// hidden node's whole subtree is skipped (ui.hidden, D3): focus must not land on
// something not on screen.
//
// row_template subtrees ARE walked, for the whole-document containment
// invariant every node walker in this codebase holds to (a branch no walker
// visits is a branch where a bind is never refused). But a template node
// contributes its raw id at most: a template is instantiated per element at
// render time, and pressing an instantiated row (Scene 9's cmd:/agent
// {row.id}) needs the per-element {row.field} interpolation the host does not
// yet resolve — deferred with the rest of the row-click work E4 parked on H8.
// So in practice a template rarely carries a focusable id today, and walking it
// is a no-op for focus while keeping the walker complete.
func pressableIDs(doc *scene.Document, hidden map[string]bool) []string {
	var out []string
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil {
			return
		}
		if n.ID != "" && hidden[n.ID] {
			return // the node and its subtree are hidden; skip both
		}
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
		if n.RowTemplate != nil {
			walk(n.RowTemplate)
		}
	}
	if doc != nil {
		walk(doc.Root)
	}
	return out
}

// findPressable returns the node with the given id if it carries an on_press,
// searching the same subtree pressableIDs walks. It is what Enter-dispatch uses
// to recover the focused button's action, and what a focus: target is resolved
// against, so the two agree on what "a node in the scene" means.
func findPressable(doc *scene.Document, id string) *scene.Node {
	if doc == nil || id == "" {
		return nil
	}
	var found *scene.Node
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil || found != nil {
			return
		}
		if n.ID == id {
			found = n
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
			walk(n.RowTemplate)
		}
	}
	walk(doc.Root)
	return found
}

// advanceFocus moves the focus cursor one step through the ring, treating the
// empty string as the input's home slot (BINDS.md §4.8: focus defaults to the
// input at boot, and Tab returns to it after the last pressable node). The ring
// is [input, ids[0], …, ids[n-1]] and wraps, so Tab from the last node lands on
// the input and Shift-Tab from the input lands on the last node — the typing
// flow is always one Tab away, Q19's concern.
func advanceFocus(ids []string, current string, forward bool) string {
	// The ring as positions: -1 is the input home, 0..len-1 index ids.
	pos := -1
	for i, id := range ids {
		if id == current {
			pos = i
			break
		}
	}
	size := len(ids) + 1 // the ids plus the input home
	// Shift by one, mapping the input home (-1) to the last slot of a 0-based ring.
	idx := pos + 1 // 0 == input home, 1..len == ids
	if forward {
		idx = (idx + 1) % size
	} else {
		idx = (idx - 1 + size) % size
	}
	if idx == 0 {
		return "" // back to the input home
	}
	return ids[idx-1]
}

// pluginActionRouter is the seam press dispatch reaches a behavioral plugin
// through (I4). It is satisfied by *supervisor.Registry, which the loop holds and
// I5 populates behind the consent gate; press.go depends on the small interface
// rather than the concrete registry so the arch seam stays clean and a test can
// route to a fake. SendAction resolves the plugin by id, applies the granted
// capability gate, and writes the `action` frame; it reports (never crashes) when
// the plugin is unmounted, ungranted, or down (§I-E, §I-G).
type pluginActionRouter interface {
	SendAction(pluginID, action string, args map[string]string) error
}

// focusKey handles the two keys H8 adds to the loop: Tab/Shift-Tab move the focus
// cursor over the pressable nodes, and Enter dispatches the focused node's
// on_press. It returns handled=false for every other key and for Enter while the
// input holds focus (uiFocus == ""), so the caller's ordinary paths — the slash
// menu, caret motion, and typeKey's submit — are untouched.
//
// It sits after the slash branch in the dispatch chain: while the buffer starts
// with "/", Tab and Enter belong to the menu, so focusKey is never reached then.
// Ctrl-C never reaches here either, so the escape hatch stays uncapturable
// (invariant 6) whatever a button's action names.
func focusKey(k term.Key, input, uiFocus string, doc **scene.Document, notice *string, hidden map[string]bool, fetch patch.Fetcher, applyTokens func(*patch.PluginTokens), actions pluginActionRouter, ctx context.Context, drv Driver) (bool, string, string) {
	switch {
	case k.Type == term.KeyTab:
		ids := pressableIDs(*doc, hidden)
		if len(ids) == 0 {
			return false, input, uiFocus // nothing pressable; Tab is not ours
		}
		forward := k.Mod&term.ModShift == 0
		return true, input, advanceFocus(ids, uiFocus, forward)
	case k.Type == term.KeyEnter && uiFocus != "":
		node := findPressable(*doc, uiFocus)
		if node == nil || node.OnPress == "" {
			// Focus points at a node that is not pressable (a focus: target that
			// is not a button, say). Enter is not ours; let it submit as usual.
			return false, input, uiFocus
		}
		newFocus := dispatchPress(node.OnPress, uiFocus, doc, notice, hidden, fetch, applyTokens, actions, ctx, drv)
		// Clear the input the way the command paths do: a press is a submitted
		// action, not text left in the buffer.
		return true, "", newFocus
	}
	return false, input, uiFocus
}

// dispatchPress routes a parsed on_press action to its host effect (BINDS.md
// §4.8) and returns the focus cursor the loop should hold afterwards. It is the
// single dispatcher for the closed prefix set:
//
//   - focus: sets the cursor to the named node, resolved against the live scene;
//     a target naming no node leaves focus unchanged and reports it (never a
//     crash), the declarative twin of cmd:/focus.
//   - cmd: runs the command line exactly as a typed one: a /ui line goes through
//     the same uiCommandKey the typed surface uses (so id prefixing, invariant-3
//     re-validation and the token layers are reused, not reimplemented), and any
//     other slash line is submitted as a prompt — the Phase-0 contract typeKey
//     already honours, so a button and a keystroke cannot diverge on what a
//     command means.
//   - ext: routes the press to a behavioral plugin subprocess (I4). The plugin id
//     and action name were split by ParseAction; the router resolves the live
//     plugin, applies its granted capability gate, and writes the `action` frame.
//     A plugin that is unmounted, ungranted, or down is reported (never a crash),
//     the §I-G placeholder-not-crash rule on the input side.
//   - answer: is recognised (the vocabulary is signed) but not yet actioned:
//     answering an inbox item needs the behavioral driver channel Block I builds,
//     so the press reports the deferral rather than dropping silently.
func dispatchPress(onPress, currentFocus string, doc **scene.Document, notice *string, hidden map[string]bool, fetch patch.Fetcher, applyTokens func(*patch.PluginTokens), actions pluginActionRouter, ctx context.Context, drv Driver) string {
	action, err := scene.ParseAction(onPress)
	if err != nil {
		// The document validated at load, so a malformed action should be
		// unreachable here; report it rather than panic if one slips through.
		*notice = err.Error()
		return currentFocus
	}
	switch action.Kind {
	case scene.ActionFocus:
		if findNodeByID(*doc, action.Arg) == nil {
			*notice = fmt.Sprintf("focus: no node with id %q in the current scene", action.Arg)
			return currentFocus
		}
		return action.Arg
	case scene.ActionCmd:
		enter := term.Key{Type: term.KeyEnter}
		if handled, _ := uiCommandKey(action.Arg, enter, doc, notice, hidden, fetch, applyTokens); handled {
			return currentFocus
		}
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(action.Arg), "/"))
		if line != "" {
			_ = drv.SubmitPrompt(ctx, line)
		}
		return currentFocus
	case scene.ActionExt:
		if actions == nil {
			// No plugin host is attached (the router is only nil in a context that
			// mounts no plugins). Report rather than drop, so an ext: press is never
			// a silent no-op.
			*notice = fmt.Sprintf("ext:%s:%s cannot be routed: no plugin host is attached", action.PluginID, action.Arg)
			return currentFocus
		}
		// A plain ext:<id>:<action> press carries no args: the {row.field} values
		// §I-E resolves against the row scope ride on the same template-row
		// dispatch H8 parked (a template row's on_press is not yet substituted per
		// element at press time), so the resolved args arrive here once that lands.
		if err := actions.SendAction(action.PluginID, action.Arg, nil); err != nil {
			*notice = err.Error()
		}
		return currentFocus
	case scene.ActionAnswer:
		*notice = fmt.Sprintf("answer:%s recognised, but answering an inbox item needs the behavioral driver channel (Block I); the button is not yet actioned", action.Arg)
		return currentFocus
	}
	return currentFocus
}

// findNodeByID searches the whole scene tree for a node with the given id,
// including nodes that carry no on_press. A focus: target may name any node (a
// pane to glow, not only a button), so its resolution is broader than
// findPressable's.
func findNodeByID(doc *scene.Document, id string) *scene.Node {
	if doc == nil || id == "" {
		return nil
	}
	var found *scene.Node
	var walk func(n *scene.Node)
	walk = func(n *scene.Node) {
		if n == nil || found != nil {
			return
		}
		if n.ID == id {
			found = n
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
			walk(n.RowTemplate)
		}
	}
	walk(doc.Root)
	return found
}
