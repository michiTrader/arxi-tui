package engine

import (
	"sort"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// The style axis, which the three bind guards do not touch.
//
// Those guards ask whether a bind's *value* reaches the frame. This one asks
// about the other half of what a scene node declares: the token it is drawn
// under. A value that arrives under the wrong token is on screen and wrong,
// and every projection guard in this package passes it, because the text is
// there — only the styling is somebody else's.
//
// # What was measured before anything was written
//
// A throwaway probe declared `{"style": "WITNESSTOKEN"}` on a text child and
// rendered it inside every container type in renderNode's switch, with and
// without a border. Eight of the nine combinations that actually draw the
// child kept its token. One did not:
//
//	box       bordered=false  childTokenKept=true
//	box       bordered=true   childTokenKept=false   <-- here
//	overlay   bordered=false  childTokenKept=true
//	overlay   bordered=true   childTokenKept=true
//	row       bordered=false  childTokenKept=true
//	row       bordered=true   childTokenKept=true
//	stack     bordered=false  childTokenKept=true
//	stack     bordered=true   childTokenKept=true
//
// renderBox's content loop flattened each child line with `l.Text()` and
// re-emitted it as one span under `styleName(n.Style)` — the *box's* token, or
// none. Everything the children said about themselves was discarded at the
// border.
//
// # This is a repeat, not a new class
//
// padLine carries the rule in writing, and it was written from a bug that had
// already shipped: *"Flattening the line into its first span (the previous
// approach) is how a dim menu row came out undimmed: the padding is chrome,
// and chrome must not restyle the content it fills around."* wrapWithBorder —
// the overlay's bordered path — was fixed to honour it, and its comment says
// so explicitly: *"span by span: a bordered overlay keeps each span's own
// token instead of welding the row into the box's style (the same weld padLine
// removed from the borderless path)."*
//
// The same weld survived in renderBox. Two bordered drawing paths, one fixed
// and commented, one not, and nothing in the tree compared them. That is why
// this guard sweeps the matrix rather than testing the box: a rule honoured in
// three places out of four is not a rule, it is a coincidence, and the fourth
// place is found by enumeration or not at all.
//
// # It is reachable from a shipped scene, not just from a constructed one
//
// MAXIMUM's Tasks panel is a bordered box around a list. renderList emits its
// empty state as `{Text: "no tasks", Style: "dim"}`; the golden records what
// reached the screen:
//
//	«border:│»no tasks          «border:│»
//
// Bare. The dim token was minted by the renderer, validated by nothing, and
// deleted by the box one call later. The same fate awaited every token any
// child of any bordered box declared, including tokens the validator had
// checked against the theme and accepted.
//
// # The direction of the check
//
// It asks only that a declared child token survives the container. It does not
// ask that containers never style anything: a box with its own `style` still
// paints the padding it adds, which is chrome styling chrome and is what
// MAXIMUM's banner relies on. The distinction this guard draws is between a
// container styling its own chrome and a container overwriting its content's
// declarations — the same line padLine draws.
//
// # What it deliberately does not check
//
// Whether the token is the *right* one. A child asking for "warn" and getting
// "warn" passes here even if the theme maps warn to something illegible, and a
// scene whose author picked the wrong token is not this guard's business. The
// goldens own appearance. This owns the property that a declaration survives
// the trip to the frame at all.

// childStyleWitness is the token the probe child declares. It is deliberately
// not a token any theme defines: this test renders rather than validates, so
// an undefined token is fine here, and a name no factory theme could mint
// means a surviving witness cannot be one the engine produced on its own.
const childStyleWitness = "CHILDSTYLEWITNESS"

// childTextWitness marks the child's content so the sweep can tell "the token
// was dropped" apart from "the child was never drawn". Without it a container
// that renders nothing at all would look identical to one that welds, and the
// guard would report a defect in a node type that simply has no children —
// a false alarm shaped like the real thing, which is how a guard earns being
// ignored.
const childTextWitness = "CHILDTEXTWITNESS"

// containerShapes returns the node types to sweep, read from renderNode's own
// switch rather than listed here. Same reasoning as the composite bind guard:
// a hand-written list is a second inventory, and the sweep must be as wide as
// the engine is. A container added to renderNode is covered from the moment it
// exists, and a leaf type simply never draws the child and drops out below.
func containerShapes(t *testing.T) []string {
	t.Helper()
	return nodeTypesInRenderNode(t)
}

func TestAContainerDoesNotRestyleItsChildrensContent(t *testing.T) {
	shapes := containerShapes(t)

	// The floor on the sweep. If the parse stops finding renderNode's cases,
	// no container is rendered, no child is drawn, every combination drops
	// out of the loop below, and this file reports success having measured
	// nothing.
	if len(shapes) < 5 {
		t.Fatalf("found only %d node types in renderNode's switch (%v); the sweep is reading the wrong dispatch\n"+
			"consequence: with no container types, no child is ever drawn and this guard passes vacuously.",
			len(shapes), shapes)
	}

	child := func() []*scene.Node {
		return []*scene.Node{{
			Type:  "text",
			Text:  childTextWitness,
			Style: map[string]string{"style": childStyleWitness},
		}}
	}

	var welded []string
	drawn := 0

	for _, shape := range shapes {
		for _, bordered := range []bool{false, true} {
			for _, containerStyled := range []bool{false, true} {
				n := &scene.Node{Type: shape, Children: child()}
				if bordered {
					n.BorderRaw = []byte(`"single"`)
				}
				if containerStyled {
					// The container declares a token of its own. This is
					// the case that matters: a container with nothing to
					// say cannot overwrite anything, so a sweep without
					// this arm would miss the weld entirely on any path
					// that substitutes the parent's token.
					n.Style = map[string]string{"style": "banner"}
				}

				r := &Renderer{Width: 60, Height: 10}
				frame := r.renderNode(n, fold.State{}, 10)

				var keptToken, drewText bool
				for _, line := range frame.Live {
					for _, span := range line {
						if span.Style == childStyleWitness {
							keptToken = true
						}
						if strings.Contains(span.Text, childTextWitness) {
							drewText = true
						}
					}
				}

				if !drewText {
					// Not a container, or one that does not draw children
					// of this shape. Nothing was welded because nothing
					// was carried.
					continue
				}
				drawn++

				if !keptToken {
					welded = append(welded, describeWeld(shape, bordered, containerStyled, became(frame)))
				}
			}
		}
	}

	sort.Strings(welded)

	if len(welded) > 0 {
		t.Errorf("%d container shape(s) discard the style token their child declared:\n  %s\n\n"+
			"consequence: the child's content reaches the screen under the wrong token, so every\n"+
			"projection guard in this package passes it — they ask whether the value arrived, and it\n"+
			"did. Only the styling is gone, and a validated, theme-checked token silently becomes the\n"+
			"container's or none. Reachable from a shipped scene: MAXIMUM's Tasks panel is a bordered\n"+
			"box around a list whose empty state is minted `dim`, and the styled golden records it\n"+
			"bare.\n"+
			"remedy: carry the child's spans through instead of flattening the line into one span.\n"+
			"padLine and wrapWithBorder already do this and say why; the rule is that chrome may style\n"+
			"chrome and may not restyle the content it wraps.",
			len(welded), strings.Join(welded, "\n  "))
	}

	// The same floor the bind guards carry. If scene.Node stopped accepting a
	// style map, or the witness child stopped rendering, every combination
	// would hit the `!drewText` continue above and this guard would pass
	// having compared nothing.
	if drawn == 0 {
		t.Fatal("no container drew the witness child, so this guard's passing means nothing\n" +
			"consequence: a green result here would certify a styling surface nobody measured.\n" +
			"remedy: confirm renderNode still dispatches container types to renderers that render\n" +
			"n.Children, and that a text node still carries Style through to its span.")
	}

	t.Logf("swept %d container/border/style combination(s) that drew the child, across %d node type(s)",
		drawn, len(shapes))
}

// became reports the token the child's content is actually drawn under, so a
// failure can say what replaced the declaration rather than only that it is
// gone. "Replaced by the container's token" and "replaced by nothing" are
// different bugs with different fixes, and leaving the reader to re-derive
// which one they have is the kind of vague failure that gets a guard skipped.
func became(frame ui.Frame) string {
	for _, line := range frame.Live {
		for _, span := range line {
			if strings.Contains(span.Text, childTextWitness) {
				if span.Style == "" {
					return "no token at all"
				}
				return "the token " + span.Style
			}
		}
	}
	return "an unlocatable span"
}

// describeWeld renders the failing combination into a line a reader can act on.
func describeWeld(shape string, bordered, containerStyled bool, got string) string {
	desc := shape
	if bordered {
		desc += " (bordered"
	} else {
		desc += " (borderless"
	}
	if containerStyled {
		desc += ", container declares its own token)"
	} else {
		desc += ", container declares no token)"
	}
	return desc + " — child's token became " + got
}
