package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file is the `inspect` option of ui_preview: why each node is drawn or hidden, and
// the order Tab visits things.
//
// A picture says what is on screen, not why. When a row the model just added does not
// appear, the cause is a `when` that is false in this state, or a parent that is hidden;
// without the reason the model edits the wrong thing. The report asks the renderer's own
// predicate (engine.NodeHidden), so it cannot disagree with the picture, and it does not
// list what is inside a hidden node, because the renderer does not draw it either.

// inspectMaxRows bounds the report: it is read by a model and paid for in tokens.
const inspectMaxRows = 120

func inspectText(doc *scene.Document, state fold.State) string {
	var b strings.Builder
	b.WriteString("NODES (id type; HIDDEN says why; bind = value now):\n")
	rows := 0
	var walk func(n *scene.Node, depth int)
	walk = func(n *scene.Node, depth int) {
		if n == nil {
			return
		}
		if rows >= inspectMaxRows {
			if rows == inspectMaxRows {
				fmt.Fprintf(&b, "  ... (more than %d nodes; the rest is not listed)\n", inspectMaxRows)
				rows++
			}
			return
		}
		rows++
		name := n.ID
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(&b, "%s%s %s", strings.Repeat("  ", depth+1), name, n.Type)
		hidden := engine.NodeHidden(n, state)
		if hidden {
			switch {
			case n.ID != "" && state.UIHidden[n.ID]:
				b.WriteString("  HIDDEN (hidden with /ui hide)")
			default:
				fmt.Fprintf(&b, "  HIDDEN (when %q is false: %s)", n.When, quoteShort(engine.BindValue(n.When, state)))
			}
		} else if n.When != "" {
			fmt.Fprintf(&b, "  drawn (when %q is true)", n.When)
		}
		if n.Bind != "" {
			fmt.Fprintf(&b, "  bind %s = %s", n.Bind, quoteShort(engine.BindValue(n.Bind, state)))
		}
		if n.Type == "button" && n.Text != "" {
			fmt.Fprintf(&b, "  %s", quoteShort(n.Text))
		}
		if n.OnPress != "" {
			fmt.Fprintf(&b, "  on_press %s", n.OnPress)
		}
		b.WriteString("\n")
		if hidden {
			return
		}
		if p := n.PrefixNode(); p != nil {
			walk(p, depth+1)
		}
		if n.Suffix != nil {
			walk(n.Suffix, depth+1)
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	if doc != nil {
		walk(doc.Root, 0)
	}
	ring, err := focusRing(doc, state, state.UIHidden)
	switch {
	case err != nil:
		fmt.Fprintf(&b, "TAB ORDER: cannot be built: %v\n", err)
	case len(ring) == 0:
		b.WriteString("TAB ORDER: input only (no pressable node is drawn)\n")
	default:
		names := make([]string, len(ring))
		for i, id := range ring {
			names[i] = strings.ReplaceAll(id, rowFocusMarker, "#")
		}
		fmt.Fprintf(&b, "TAB ORDER: input, %s, then the input again\n", strings.Join(names, ", "))
	}
	return b.String()
}

func quoteShort(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		s = string(r[:40]) + "…"
	}
	return fmt.Sprintf("%q", s)
}
