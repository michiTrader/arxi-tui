package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Attr is a text attribute set.
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrUnderline
	AttrReverse
	AttrStrike
)

// ColorKind distinguishes "leave it to the terminal" from the two ways of asking
// for a colour.
type ColorKind uint8

const (
	ColorNone ColorKind = iota
	ColorIndex
	ColorRGB
)

// Color is a colour request. Indexed colours are the default the factory scene
// should reach for on purpose: index 1 is whatever red the user picked in their own
// terminal scheme, which is almost always the red they want, and it is the only
// kind that survives on a terminal without truecolor.
type Color struct {
	Kind ColorKind
	R    uint8 // the palette index when Kind is ColorIndex
	G    uint8
	B    uint8
}

// Idx returns one of the terminal's own palette entries (0-255).
func Idx(n uint8) Color { return Color{Kind: ColorIndex, R: n} }

// Hex parses "#rrggbb" or "rrggbb". A malformed value returns an error rather
// than a silently wrong colour, so a bad theme file is reported to the user.
func Hex(s string) (Color, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 6 {
		return Color{}, fmt.Errorf("colour %q: want #rrggbb", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return Color{}, fmt.Errorf("colour %q: %w", s, err)
	}
	return Color{Kind: ColorRGB, R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
}

// MustHex is Hex for literals inside a compiled-in theme.
func MustHex(s string) Color {
	c, err := Hex(s)
	if err != nil {
		panic(err)
	}
	return c
}

// Style is everything a theme can say about one span.
type Style struct {
	FG    Color
	BG    Color
	Attrs Attr
}

// Has reports whether every attribute in a is set.
func (s Style) Has(a Attr) bool { return s.Attrs&a == a }

// IsZero reports whether the style asks for nothing.
func (s Style) IsZero() bool {
	return s.FG.Kind == ColorNone && s.BG.Kind == ColorNone && s.Attrs == 0
}

// Over lays s on top of under, and it is the whole reason a span can carry two
// theme keys. A colour set by s wins; a colour s leaves alone falls through. That
// asymmetry is the useful part: a diff's added-line wash names only a background,
// so a syntax-coloured token laid over it keeps its own foreground and inherits the
// band, which is how a green comment can sit on a blue row.
//
// Attributes are unioned rather than replaced. Dim is what a gutter asks for and
// bold is what a keyword asks for, and neither is trying to say the other is wrong.
func (s Style) Over(under Style) Style {
	out := under
	if s.FG.Kind != ColorNone {
		out.FG = s.FG
	}
	if s.BG.Kind != ColorNone {
		out.BG = s.BG
	}
	out.Attrs |= s.Attrs
	return out
}

// Terminal palette indices, named so the theme table reads like intent.
const (
	Black   = 0
	Red     = 1
	Green   = 2
	Yellow  = 3
	Blue    = 4
	Magenta = 5
	Cyan    = 6
	White   = 7
	Bright  = 8 // add to any of the above for the bright variant
)

// Everything below is the config file's half of a style: the spelling a user writes and
// the same spelling printed back. It is here rather than in a config package because a
// vocabulary and its parser rot apart the moment they live in different files — an
// attribute added to the constants above and forgotten here is a word the theme knows and
// the file cannot say.

// attrNames is every attribute a style value may name, in the order String prints them.
// A declared table, for the same reason the key decoder's tables are declared: the set
// is closed, the error message for a word outside it is built from the table itself, and
// a new attribute is one row rather than a parser, a printer and a help string kept in
// step by hand.
var attrNames = []struct {
	Name string
	Attr Attr
}{
	{"bold", AttrBold},
	{"dim", AttrDim},
	{"italic", AttrItalic},
	{"underline", AttrUnderline},
	{"reverse", AttrReverse},
	{"strike", AttrStrike},
}

// colorNames is the palette in index order. A "bright-" in front of any of them adds
// Bright, so eight words cover the sixteen colours a terminal scheme actually lets its
// user choose; everything from 16 up is a number, because it has no name to give.
var colorNames = [8]string{"black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"}

func parseAttr(name string) (Attr, bool) {
	for _, a := range attrNames {
		if a.Name == name {
			return a.Attr, true
		}
	}
	return 0, false
}

func attrList() string {
	out := make([]string, len(attrNames))
	for i, a := range attrNames {
		out[i] = a.Name
	}
	return strings.Join(out, ", ")
}

// ParseColor reads the three ways a config may name a colour: six hex digits with or
// without a leading #, a palette index from 0 to 255, or a palette name like red or
// bright-black.
//
// Six hex digits mean a colour and one to three digits mean an index, which is the rule
// that makes "000000" black rather than index 0 — leading zeros are how a hex colour
// looks, and nobody writes an index that way. The name check comes last and is what saves
// "yellow", which is six characters long and would otherwise be read as hex; it is not
// six hex digits, so it never reaches that branch.
func ParseColor(s string) (Color, error) {
	v := strings.TrimSpace(s)
	switch {
	case v == "":
		return Color{}, errors.New("no colour")
	case strings.HasPrefix(v, "#"), len(v) == 6 && isHexDigits(v):
		return Hex(v)
	}
	if n, err := strconv.ParseUint(v, 10, 8); err == nil {
		return Idx(uint8(n)), nil
	}
	name, bright := strings.CutPrefix(v, "bright-")
	for i, n := range colorNames {
		if n == name {
			if bright {
				i += Bright
			}
			return Idx(uint8(i)), nil
		}
	}
	return Color{}, fmt.Errorf("colour %q: want #rrggbb, an index 0-255, or a name like bright-black", s)
}

func isHexDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
			return false
		}
	}
	return true
}

// String is the spelling ParseColor reads back, so that a style table can print the
// default theme as the lines a config would write to change it. A colour nobody asked for
// prints as nothing at all, and a caller has to notice that rather than print it: it is
// not a colour, it is the absence of one.
func (c Color) String() string {
	switch c.Kind {
	case ColorRGB:
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	case ColorIndex:
		if c.R < 2*Bright {
			if c.R >= Bright {
				return "bright-" + colorNames[c.R-Bright]
			}
			return colorNames[c.R]
		}
		return strconv.Itoa(int(c.R))
	}
	return ""
}

// ParseStyle reads a [styles] value: fg= and bg= with a colour each, and any of the
// attribute words, in any order, separated by spaces.
//
//	fg=#c8d3f5 bg=8 bold
//	bg=#1c5a38
//	dim italic
//
// The empty string is the zero style, and the factory theme depends on that: a token set
// to "" in a scene draws the way it did before anybody themed it, which is how a reader
// takes a wash away from behind their own prompt. ParseStyle is the sentence that makes
// it true, because the parser — not a doc line — is what a token value passes through.
//
// A repeated fg= or bg= is an error rather than last-one-wins. Two colours for one span is
// a mistake every time — usually a line half-replaced by an edit — and silently picking one
// of them is how a config comes to say something its author did not write.
func ParseStyle(s string) (Style, error) {
	var out Style
	var haveFG, haveBG bool
	for _, field := range strings.Fields(s) {
		key, val, isPair := strings.Cut(field, "=")
		if !isPair {
			a, ok := parseAttr(field)
			if !ok {
				return Style{}, fmt.Errorf("%q is not fg=, bg= or an attribute; the attributes are %s", field, attrList())
			}
			out.Attrs |= a
			continue
		}
		c, err := ParseColor(val)
		if err != nil {
			return Style{}, fmt.Errorf("%s: %w", key, err)
		}
		switch key {
		case "fg":
			if haveFG {
				return Style{}, errors.New("fg is given twice")
			}
			out.FG, haveFG = c, true
		case "bg":
			if haveBG {
				return Style{}, errors.New("bg is given twice")
			}
			out.BG, haveBG = c, true
		default:
			return Style{}, fmt.Errorf("%q: want fg= or bg=", key)
		}
	}
	return out, nil
}

// String prints a style as the value ParseStyle reads, and the pair of them round-trips
// every value the default theme holds — which is a test, and is most of why this method
// exists. The other reason is that the factory theme can then show what each token is set
// to instead of only naming it, so changing a colour is copying a line and editing it
// rather than guessing at a syntax from a doc string.
//
// The zero style prints as the empty string, which is the same thing ParseStyle reads it
// back from.
func (s Style) String() string {
	out := make([]string, 0, 3)
	if s.FG.Kind != ColorNone {
		out = append(out, "fg="+s.FG.String())
	}
	if s.BG.Kind != ColorNone {
		out = append(out, "bg="+s.BG.String())
	}
	for _, a := range attrNames {
		if s.Attrs&a.Attr != 0 {
			out = append(out, a.Name)
		}
	}
	return strings.Join(out, " ")
}

// StyleSyntax is the legend the default-theme listing prints above the token table. It
// lives next to the parser so that the help and the grammar cannot drift: the attribute
// list in it is the table, not a copy of the table.
func StyleSyntax() string {
	return "fg=<colour> bg=<colour> and any of " + attrList() +
		"; a colour is #rrggbb, an index 0-255, or a name like bright-black"
}
