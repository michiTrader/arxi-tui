package ui

import (
	"strings"
	"testing"
)

// The parser/printer half of a style has no golden file and no recording behind it: it is
// a grammar, and what a grammar needs is a test that reads back everything it writes. The
// round-trip below is most of that. The tables after it are the cases a round-trip cannot
// reach — the spellings a person writes that the printer never produces, and the mistakes.

// TestStyleRoundTripsTheDefaultTheme is the strongest cheap test available here: every
// value the default theme holds, printed and read back, must be the same style. It covers
// indexed colours, RGB, every attribute the theme uses and the zero style without naming
// any of them, so a token added to the default theme is covered on the day it is added.
// The default theme is not yet shipped in this Phase 0 copy of the substrate (that is
// Fase 1's vocabulary decision), so the test exercises the exhaustive sample below; when
// the default theme lands, the sample is replaced by iterating it, unchanged in intent.
func TestStyleRoundTripsTheDefaultTheme(t *testing.T) {
	cases := map[string]Style{
		"":                       {},
		"bold":                   {Attrs: AttrBold},
		"dim italic":             {Attrs: AttrDim | AttrItalic},
		"underline":              {Attrs: AttrUnderline},
		"reverse":                {Attrs: AttrReverse},
		"strike":                 {Attrs: AttrStrike},
		"fg=#c8d3f5 bg=8 bold":   {FG: MustHex("#c8d3f5"), BG: Idx(Bright + Black), Attrs: AttrBold},
		"bg=#1c5a38":             {BG: MustHex("#1c5a38")},
		"fg=red bg=bright-blue":  {FG: Idx(Red), BG: Idx(Bright + Blue)},
		"fg=bright-white":        {FG: Idx(Bright + White)},
		"bg=bright-black":        {BG: Idx(Bright + Black)},
	}
	for _, want := range cases {
		text := want.String()
		got, err := ParseStyle(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("%q read back as %+v, want %+v", text, got, want)
		}
	}
}

// TestZeroStyleIsTheEmptyString is the sentence the default theme leans on for prompt.band:
// a token set to "" in a scene draws the way it did before anybody themed it. Both
// directions matter, because the printer's half is what the default-theme listing shows
// for a token nobody styled.
func TestZeroStyleIsTheEmptyString(t *testing.T) {
	if got := (Style{}).String(); got != "" {
		t.Errorf("the zero style prints as %q, want empty", got)
	}
	got, err := ParseStyle("")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Errorf(`ParseStyle("") = %+v, want the zero style`, got)
	}
}

// TestEveryPaletteIndexRoundTrips walks all 256 of them because the printer has three
// branches — a name, a bright- name and a number — and the boundaries between them, 7 to 8
// and 15 to 16, are exactly where an off-by-one lives.
func TestEveryPaletteIndexRoundTrips(t *testing.T) {
	for n := 0; n < 256; n++ {
		want := Idx(uint8(n))
		text := want.String()
		got, err := ParseColor(text)
		if err != nil {
			t.Errorf("index %d printed as %q: %v", n, text, err)
			continue
		}
		if got != want {
			t.Errorf("index %d printed as %q and read back as %+v", n, text, got)
		}
	}
	// Three of the spellings outright, because a printer that gave every index back as a
	// number would pass the loop above with all sixteen names broken.
	for _, c := range []struct {
		n    uint8
		want string
	}{{7, "white"}, {8, "bright-black"}, {16, "16"}} {
		if got := Idx(c.n).String(); got != c.want {
			t.Errorf("index %d prints as %q, want %q", c.n, got, c.want)
		}
	}
}

// TestParseColorAccepts is the table of spellings a person writes. The printer produces only
// one of each, so a round-trip test never reaches the second column of this one.
func TestParseColorAccepts(t *testing.T) {
	for text, want := range map[string]Color{
		"#2c2c31": {Kind: ColorRGB, R: 0x2c, G: 0x2c, B: 0x31},
		"2c2c31":  {Kind: ColorRGB, R: 0x2c, G: 0x2c, B: 0x31},
		"#2C2C31": {Kind: ColorRGB, R: 0x2c, G: 0x2c, B: 0x31},
		// Six hex digits are a colour even when all six are zeros, which is the rule that
		// keeps 000000 black instead of index 0: nobody writes an index with leading zeros.
		"000000": {Kind: ColorRGB},
		"0":      Idx(0),
		"8":      Idx(Bright + Black),
		"255":    Idx(255),
		// yellow is six characters long and would be read as hex if the length test stood
		// alone. It is not six hex digits, so it never reaches that branch.
		"yellow":       Idx(Yellow),
		"black":        Idx(Black),
		"bright-black": Idx(Bright + Black),
		"bright-white": Idx(Bright + White),
		"  red  ":      Idx(Red),
	} {
		got, err := ParseColor(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %+v, want %+v", text, got, want)
		}
	}
}

func TestParseColorRejects(t *testing.T) {
	for _, text := range []string{
		"",
		"   ",
		"#fff", // three digits is the css spelling, not this one
		"#gggggg",
		"orange", // a real colour with no palette entry
		"256",    // one past the palette
		"-1",
		"bright-", // the prefix with nothing after it
		"bright-orange",
		"bright-bright-red",
	} {
		if c, err := ParseColor(text); err == nil {
			t.Errorf("%q was accepted as %+v", text, c)
		}
	}
}

// TestParseStyleAccepts covers the freedoms the grammar gives on purpose: any order, any
// spacing, and either kind of colour on either side.
func TestParseStyleAccepts(t *testing.T) {
	for text, want := range map[string]Style{
		"fg=#c8d3f5 bg=8 bold": {FG: MustHex("#c8d3f5"), BG: Idx(Bright + Black), Attrs: AttrBold},
		"bg=#1c5a38":           {BG: MustHex("#1c5a38")},
		"dim italic":           {Attrs: AttrDim | AttrItalic},
		// The printer's order is the table's order, and a person writing the line by hand
		// will not reproduce it. Extra spaces come free with strings.Fields.
		"italic   dim":                  {Attrs: AttrDim | AttrItalic},
		"bold fg=bright-blue underline": {FG: Idx(Bright + Blue), Attrs: AttrBold | AttrUnderline},
		"strike reverse":                {Attrs: AttrReverse | AttrStrike},
		"bg=red fg=white":               {FG: Idx(White), BG: Idx(Red)},
	} {
		got, err := ParseStyle(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %+v, want %+v", text, got, want)
		}
	}
}

func TestParseStyleRejects(t *testing.T) {
	for _, text := range []string{
		// Two colours for one span is a line half-replaced by an edit every time, and
		// silently keeping one of them is how a config comes to say what nobody wrote.
		"fg=red fg=blue",
		"bg=red bg=blue",
		"fg=red fg=red", // the same twice is still a mistake, and a cheaper one to make
		"bright",        // the prefix of an attribute is not one
		"boldish",
		"fg", // a key with no colour after it
		"fg=",
		"fg=nope",
		"colour=red", // fg and bg are the only two
		"fgred",
		"fg=red extra=1",
	} {
		if s, err := ParseStyle(text); err == nil {
			t.Errorf("%q was accepted as %q", text, s.String())
		}
	}
}

// TestEveryAttributeHasAName is the drift guard between the bit constants and the words a
// token may name. An attribute added to the constants and forgotten in attrNames is
// something the default theme can hold and the parser cannot say, and nothing else in the
// suite would notice — the default theme should use all attributes, so even the round-trip
// above would pass if one were missing.
func TestEveryAttributeHasAName(t *testing.T) {
	var all Attr
	for a := Attr(1); a != 0 && a <= AttrStrike; a <<= 1 {
		name, ok := nameOfAttr(a)
		if !ok {
			t.Errorf("attribute bit %#x has no name in attrNames", a)
			continue
		}
		got, err := ParseStyle(name)
		if err != nil || got.Attrs != a {
			t.Errorf("%q = %+v (%v); want the single attribute %#x", name, got, err, a)
		}
		all |= a
	}
		// And all of them at once print in the table's order, which is the line a reader
		// copies out of the default theme listing and edits.
	if got, want := (Style{Attrs: all}).String(), strings.ReplaceAll(attrList(), ", ", " "); got != want {
		t.Errorf("every attribute prints as %q, want %q", got, want)
	}
}

func nameOfAttr(a Attr) (string, bool) {
	for _, d := range attrNames {
		if d.Attr == a {
			return d.Name, true
		}
	}
	return "", false
}

// TestStyleSyntaxNamesEveryAttribute keeps the one line of help a theme reader gets from
// drifting away from the parser that answers it. It is the legend the default-theme listing
// prints above the token table, and the three colour spellings are in it because a reader who
// knows only #rrggbb will never discover that bright-black is a word.
func TestStyleSyntaxNamesEveryAttribute(t *testing.T) {
	syntax := StyleSyntax()
	for _, d := range attrNames {
		if !strings.Contains(syntax, d.Name) {
			t.Errorf("the style legend does not name %s: %q", d.Name, syntax)
		}
	}
	for _, want := range []string{"fg=", "bg=", "#rrggbb", "0-255", "bright-black"} {
		if !strings.Contains(syntax, want) {
			t.Errorf("the style legend does not mention %s: %q", want, syntax)
		}
	}
}
