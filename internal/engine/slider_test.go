package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// renderSliderSpan renders a single slider in isolation and returns its one
// styled span. Like renderSwitchSpan it goes through RenderFrame, not
// renderSlider directly, so the focus glow this file checks — applied at the
// renderNode chokepoint — is exercised on the real path rather than bypassed.
// preview, when non-nil, is installed as the preview substitution table so a
// numeric value can be fed to the slider's bind without depending on a fold
// field that happens to project a fraction.
func renderSliderSpan(t *testing.T, src string, state fold.State, preview map[string]string) (text, style string) {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := &Renderer{Width: 40, Height: 4, PreviewMocks: preview}
	for _, line := range r.RenderFrame(doc, state).Live {
		for _, span := range line {
			if strings.TrimSpace(span.Text) != "" {
				return span.Text, span.Style
			}
		}
	}
	t.Fatalf("slider rendered no non-blank span from %q", src)
	return "", ""
}

// sliderTrack is the whole picture, and its geometry is where an off-by-one
// hides. This pins the three positions that must never drift: 0 puts the knob on
// the first cell, 1 on the last, and 0.5 in the middle of an odd track. The
// remedy each failure prints names the arithmetic, because a knob one cell off
// its value is a slider that lies about the level it shows.
func TestSliderTrackPlacesTheKnobByFraction(t *testing.T) {
	cases := []struct {
		name     string
		fraction float64
		width    int
		want     string
	}{
		{"zero pins the knob to the first cell", 0, 5, "●────"},
		{"one pins the knob to the last cell", 1, 5, "────●"},
		{"half centres the knob on an odd track", 0.5, 5, "──●──"},
		{"below zero clamps to the first cell", -0.3, 5, "●────"},
		{"above one clamps to the last cell", 1.7, 5, "────●"},
		{"a width of one collapses to the knob alone", 0.5, 1, "●"},
		{"a zero width draws nothing", 0.5, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sliderTrack(c.fraction, c.width)
			if got != c.want {
				t.Errorf("sliderTrack(%v, %d) = %q, want %q.\n"+
					"consequence: the knob sits at the wrong cell, so the slider shows a level the\n"+
					"bound value does not hold — the one thing a value control must get right.\n"+
					"remedy: place the knob at round(fraction*(width-1)) with fraction clamped to [0,1].",
					c.fraction, c.width, got, c.want)
			}
		})
	}
}

// parseFraction is the slider's gate between "a value to draw" and "a string to
// show verbatim", and it is untested code inside a test's blast radius if left
// unpinned. The rejected column is the load-bearing one: each entry is a value
// that must NOT snap the knob to zero, because a knob at zero for a bind that
// never resolved reads as a real reading of nil.
func TestParseFractionAcceptsNumbersAndRejectsTheRest(t *testing.T) {
	accepted := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"1", 1},
		{"0.5", 0.5},
		{"  0.25  ", 0.25},
		{"1.5", 1.5}, // accepted here; the clamp to [0,1] is sliderTrack's job, not the parser's
	}
	for _, c := range accepted {
		got, ok := parseFraction(c.in)
		if !ok || got != c.want {
			t.Errorf("parseFraction(%q) = (%v, %v), want (%v, true); a numeric value the slider could\n"+
				"draw was rejected, so a real level falls through to the verbatim path and no knob is shown.",
				c.in, got, ok, c.want)
		}
	}

	rejected := []string{
		"",    // an empty value, not a level
		"[…]", // the unresolved-bind placeholder
		"on",  // a scalar pointed at the wrong field
		"NaN", // no place on a [0,1] track
		"Inf", // ditto
		"1,5", // a decimal comma ParseFloat does not accept
	}
	for _, in := range rejected {
		if got, ok := parseFraction(in); ok {
			t.Errorf("parseFraction(%q) = (%v, true), but this value is not a level the slider can draw.\n"+
				"consequence: a non-numeric bind snaps the knob to a computed cell, so \"waiting\" or\n"+
				"\"misconfigured\" masquerades as a real value instead of showing verbatim.\n"+
				"remedy: reject anything strconv.ParseFloat refuses, plus NaN and infinities.", in, got)
		}
	}
}

// A slider shows a value, and that value comes from its bind through the same
// resolveBindRow chokepoint every bound node uses. This test protects that the
// knob tracks the bound number: a mid value centres the knob, a high value pushes
// it right, and the two are distinguishable — a slider frozen at one position is
// not a value control.
func TestSliderKnobTracksItsBind(t *testing.T) {
	src := `{"root":{"type":"slider","id":"s","bind":"settings.level"}}`

	mid, _ := renderSliderSpan(t, src, fold.State{}, map[string]string{"settings.level": "0.5"})
	high, _ := renderSliderSpan(t, src, fold.State{}, map[string]string{"settings.level": "1"})

	if !strings.Contains(mid, "●") {
		t.Errorf("a slider bound to 0.5 drew %q with no knob; the value cannot be read off a track\n"+
			"that shows no position.", mid)
	}
	if mid == high {
		t.Errorf("a slider drew the same span %q for 0.5 and 1.0; the knob does not track its bind,\n"+
			"so the control shows the same level for two different values.\n"+
			"remedy: resolve n.Bind through resolveBindRow and place the knob by the parsed fraction.", mid)
	}
	// The high value must reach the last cell: a value control that cannot show
	// its maximum understates every reading near the top.
	if !strings.HasSuffix(strings.TrimRight(high, " "), "●") {
		t.Errorf("a slider bound to 1.0 drew %q, whose knob is not at the right end; a full value that\n"+
			"does not reach the end of the track reads as less than full.", high)
	}
}

// A bind that does not resolve to a number draws verbatim rather than snapping
// the knob to zero — the placeholder-not-crash rule (§I-G) and the exact choice
// renderSparkline makes for a non-series bind. Without it, an unmounted plugin's
// "[…]" or a scalar pointed at the wrong field would read as a real value pinned
// to the left of the track.
func TestSliderDrawsNonNumericBindVerbatim(t *testing.T) {
	src := `{"root":{"type":"slider","id":"s","bind":"settings.level"}}`
	got, _ := renderSliderSpan(t, src, fold.State{}, map[string]string{"settings.level": "medium"})
	if strings.Contains(got, "●") {
		t.Errorf("a slider bound to the non-numeric %q drew a knob (%q); a value that is not a number\n"+
			"was placed on the track as if it were one.\n"+
			"remedy: draw the resolved string verbatim when parseFraction rejects it.", "medium", got)
	}
	if !strings.Contains(got, "medium") {
		t.Errorf("a slider bound to %q drew %q, dropping the value; the misconfiguration is now invisible\n"+
			"instead of on screen where it can be fixed.", "medium", got)
	}
}

// The optional text label is drawn before the track, so a lone slider node can
// name its own setting rather than always needing a sibling text node. A
// row_template that composes the label elsewhere leaves text empty and gets the
// bare track — the mirror of renderSwitch's label rule.
func TestSliderDrawsLabelBeforeTrack(t *testing.T) {
	labelled, _ := renderSliderSpan(t,
		`{"root":{"type":"slider","id":"s","text":"Volume","bind":"settings.level"}}`,
		fold.State{}, map[string]string{"settings.level": "0.5"})
	if !strings.Contains(labelled, "Volume") {
		t.Errorf("a slider with text \"Volume\" drew %q, dropping its label; a labelled slider that shows\n"+
			"only a track is a setting the user cannot name.", labelled)
	}
	if strings.Index(labelled, "Volume") > strings.Index(labelled, "●") {
		t.Errorf("a slider drew its knob before its label (%q); the label reads as belonging to the next\n"+
			"row rather than this control.", labelled)
	}
}

// The focus glow reaches a slider for the same reason it reaches a switch:
// renderSlider reads n.Style, and the glow arrives as a rewritten n.Style at the
// renderNode chokepoint. A settings screen Tabs between slider, switch and input
// rows, so a slider that dropped the glow would leave the user unable to see which
// row an adjust key will act on.
func TestSliderHonoursItsFocusGlow(t *testing.T) {
	src := `{"root":{"type":"slider","id":"s","bind":"settings.level","focus_glow":{"style":"glow"}}}`
	preview := map[string]string{"settings.level": "0.5"}

	_, unfocused := renderSliderSpan(t, src, fold.State{}, preview)
	if unfocused == "glow" {
		t.Fatalf("an unfocused slider already wore its focus_glow token %q; the glow is keyed to ui.focus\n"+
			"and must not apply when nothing is focused.", unfocused)
	}

	_, focused := renderSliderSpan(t, src, fold.State{UIFocus: "s"}, preview)
	if focused != "glow" {
		t.Errorf("a focused slider's span carried style %q, not the focus_glow token \"glow\".\n"+
			"consequence: the slider drops the glow the chokepoint writes into n.Style, so a focused\n"+
			"settings control looks identical to an unfocused one.\n"+
			"remedy: apply styleName(n.Style) to the slider's span.", focused)
	}
}
