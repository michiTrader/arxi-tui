package engine

import (
	"testing"
)

// TestParseSeriesAcceptsNumericArraysAndRejectsEverythingElse pins the boundary
// between "draw a chart" and "draw the string verbatim". The store hands the
// resolver back a compact-JSON series ("[1,3,2]") for a `series` bind and the
// placeholder "[…]" for an unresolved one, and a scene author can point a
// sparkline at a scalar text field by mistake — the node must tell those apart,
// because the failure of confusing them is a chart of zeros standing in for a
// value that never arrived.
func TestParseSeriesAcceptsNumericArraysAndRejectsEverythingElse(t *testing.T) {
	ok := []struct {
		in   string
		want int
	}{
		{"[1,3,2]", 3},
		{"[1,2,3,4,5]", 5},
		{" [0, -1, 2] ", 3}, // surrounding space (defensive) and a negative sample
		{"[]", 0},           // resolved but empty: a valid series with no points
		{"[42]", 1},
	}
	for _, c := range ok {
		nums, got := parseSeries(c.in)
		if !got {
			t.Errorf("parseSeries(%q) rejected a numeric array; a series bind resolves to exactly\n"+
				"this compact-JSON form (store.renderValue), so rejecting it draws the raw string\n"+
				"where a chart belongs.", c.in)
			continue
		}
		if len(nums) != c.want {
			t.Errorf("parseSeries(%q) decoded %d values, want %d.", c.in, len(nums), c.want)
		}
	}

	notSeries := []string{
		"[…]",       // the unresolved-bind placeholder
		"$1.23",     // a scalar text value: a sparkline pointed at the wrong field
		"",          // nothing resolved
		"[1,2",      // malformed array
		`["a","b"]`, // a JSON array, but of strings, not numbers
		"3",         // a bare number is not a series
	}
	for _, in := range notSeries {
		if _, got := parseSeries(in); got {
			t.Errorf("parseSeries(%q) accepted a non-series as a chart.\n"+
				"consequence: a misconfigured or unresolved bind draws a zeroed or empty chart\n"+
				"instead of showing the string that names the problem, so \"waiting\" and\n"+
				"\"wrong field\" both masquerade as a flat line at zero.", in)
		}
	}
}

// TestSparklineScalesRelativeToTheSeries is the shape property: the minimum
// value sits on the lowest glyph and the maximum on the highest, with the
// in-between values ramping across the eight blocks. A sparkline that scaled
// against an absolute floor would flatten a series whose values are large but
// close, hiding exactly the movement the node exists to show.
func TestSparklineScalesRelativeToTheSeries(t *testing.T) {
	got := sparkline([]float64{0, 1, 2, 3, 4, 5, 6, 7}, 80)
	want := "▁▂▃▄▅▆▇█"
	if got != want {
		t.Fatalf("sparkline(0..7) = %q, want %q.\n"+
			"consequence: an eight-step ramp must land one value on each glyph; a mismatch means\n"+
			"the min..max scale is off and a real trend reads as the wrong shape.", got, want)
	}

	// The same shape at a large offset draws identically: the scale is relative,
	// so 1000..1007 is the same ramp as 0..7. This is the counterfactual for an
	// absolute-axis regression — under an absolute floor these would collapse to
	// eight identical tall bars and this equality would fail.
	offset := sparkline([]float64{1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007}, 80)
	if offset != want {
		t.Fatalf("sparkline(1000..1007) = %q, want %q; the scale is not relative to the series,\n"+
			"so a series of large-but-close values loses its shape.", offset, want)
	}
}

// TestSparklineEndpointsReachBothExtremes guards the rounding decision directly:
// the largest value must reach the tallest glyph and the smallest the shortest.
// Truncating instead of rounding biases every value one glyph low and the top
// of the series never reaches the tallest block, so a peak reads as a plateau.
func TestSparklineEndpointsReachBothExtremes(t *testing.T) {
	got := sparkline([]float64{2, 5, 9, 1, 7}, 80)
	// Index of the max (9) must be the tallest glyph; index of the min (1) the shortest.
	runes := []rune(got)
	if runes[2] != '█' {
		t.Errorf("the series maximum drew %q, not the tallest glyph %q (full chart %q);\n"+
			"a peak that does not reach the top reads as a plateau.", string(runes[2]), "█", got)
	}
	if runes[3] != '▁' {
		t.Errorf("the series minimum drew %q, not the shortest glyph %q (full chart %q);\n"+
			"a trough that does not reach the bottom hides the low.", string(runes[3]), "▁", got)
	}
}

// TestSparklineFlatSeriesDrawsLowest pins the max==min decision: a series with
// no variation has no shape to place on a min..max ramp (the span is zero and
// the ratio is undefined), so it draws on the lowest glyph. A mid glyph would
// read as "half of something" when the truth is "no change", and dividing by a
// zero span would produce NaN glyphs.
func TestSparklineFlatSeriesDrawsLowest(t *testing.T) {
	for _, in := range [][]float64{{5, 5, 5, 5}, {0, 0, 0}, {42}} {
		got := sparkline(in, 80)
		for _, r := range got {
			if r != '▁' {
				t.Fatalf("flat series %v drew %q; a flat series must draw the lowest glyph on every\n"+
					"cell so it cannot be mistaken for a varying one, and so a zero span never\n"+
					"divides into a NaN glyph.", in, got)
			}
		}
		if len([]rune(got)) != len(in) {
			t.Fatalf("flat series %v drew %d glyphs, want %d.", in, len([]rune(got)), len(in))
		}
	}
}

// TestSparklineClipsToTheLastWidthValues pins the overflow decision: a series
// longer than the pane shows its NEWEST values, because a live series (a
// plugin's price ticks, I3) grows at the end and the recent samples are the
// ones a watcher came for. Showing the oldest would freeze the chart on stale
// data as new points arrive.
func TestSparklineClipsToTheLastWidthValues(t *testing.T) {
	// Ten samples, width 3. The last three (8,9,10) span 8..10, so they map to
	// low/mid/high; had the first three (1,2,3) been drawn they would map the
	// same way, so the values alone do not prove which end was kept. Use a
	// series whose tail is flat-high and whose head varies to tell them apart.
	got := sparkline([]float64{1, 5, 2, 8, 3, 9, 4, 7, 7, 7}, 3)
	if n := len([]rune(got)); n != 3 {
		t.Fatalf("width 3 drew %d glyphs (%q), want 3; a longer series must be clipped to the pane.", n, got)
	}
	// The kept tail is {7,7,7}: flat, so every glyph is the lowest. If the head
	// {1,5,2} had been kept instead it would vary and not be all-lowest.
	if got != "▁▁▁" {
		t.Errorf("width-3 clip drew %q, want the flat tail %q.\n"+
			"consequence: the chart shows the oldest samples and freezes while new data arrives,\n"+
			"instead of scrolling to the newest the way a live series should.", got, "▁▁▁")
	}
}

// TestSparklineEmptyInputsDrawNothing guards the degenerate inputs the render
// path can hand this function: a resolved-but-empty series ("[]") and a
// zero-or-negative width. Both must produce no glyphs rather than panic on an
// empty slice index.
func TestSparklineEmptyInputsDrawNothing(t *testing.T) {
	if got := sparkline(nil, 80); got != "" {
		t.Errorf("sparkline(nil) = %q, want empty.", got)
	}
	if got := sparkline([]float64{}, 80); got != "" {
		t.Errorf("sparkline([]) = %q, want empty.", got)
	}
	if got := sparkline([]float64{1, 2, 3}, 0); got != "" {
		t.Errorf("sparkline(width 0) = %q, want empty.", got)
	}
}
