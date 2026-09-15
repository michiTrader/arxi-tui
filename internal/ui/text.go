package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Text layout. Everything here works on spans rather than on an ANSI string,
// because the renderer must be able to wrap a line without knowing what a colour
// looks like. That is the same reason a Span carries a style name.

type token struct {
	text  string
	style string
	space bool
	brk   bool // a newline in the text: end the line here, whatever the width says
}

// tokenize also sanitizes, and that is not a convenience — it is where the promise
// "a Line is one terminal row" is kept. Text arrives from a streaming delta, a tool
// summary or a path, and any of those can carry a newline the wrapper never chose;
// a raw one reaches the terminal, drops the cursor a row nobody counted, and the
// next frame's walk up lands one row low. Nothing else can see it either: a newline
// scores zero columns, so neither Overflow nor the trailing-space rule notices.
func tokenize(spans []Span) []token {
	var out []token
	for _, sp := range spans {
		for i, seg := range strings.Split(sp.Text, "\n") {
			if i > 0 {
				out = append(out, token{brk: true})
			}
			for _, chunk := range splitKeepSpaces(stripControls(seg)) {
				out = append(out, token{text: chunk, style: sp.Style, space: chunk[0] == ' '})
			}
		}
	}
	// A trailing newline is a terminator, not a blank line — the same rule
	// RenderMarkdown states for a whole document. Every streaming part ends with
	// one, so keeping it would grow a phantom blank row under every paragraph.
	for len(out) > 0 && out[len(out)-1].brk {
		out = out[:len(out)-1]
	}
	return out
}

func splitKeepSpaces(s string) []string {
	var out []string
	start, inSpace := 0, false
	for i := 0; i < len(s); i++ {
		isSpace := s[i] == ' '
		if i > start && isSpace != inSpace {
			out = append(out, s[start:i])
			start = i
		}
		inSpace = isSpace
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// stripControls makes a string fit to put in a Line. Tabs become spaces and every
// other control byte is dropped, because a byte that moves the cursor by itself is a
// lie about how many rows and columns the frame occupies — and the frame's row count
// is what inline mode walks up. Newlines are gone by the time this is called: the
// caller splits on them, which is the one control character that means something we
// can honour. The tab stops are counted from the start of the text rather than from
// the terminal column, the same approximation a fenced code block already makes.
func stripControls(s string) string {
	s = expandTabs(s, 4)
	if strings.IndexFunc(s, isControl) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// WrapSpans lays spans out in width columns, breaking on spaces. cont is prefixed
// to every line after the first, which is how a bullet's text stays aligned under
// itself. A word longer than the width is split rather than allowed to overflow:
// an overflowing line corrupts inline mode.
//
// A newline in the text breaks the line too. It has to: this is the wrapper every
// streaming part, notice, prompt and tool summary goes through, and all of those
// carry newlines the layout did not choose. See tokenize for what happens to the
// control characters that do not mean anything a Line can hold.
//
// Spaces are held back rather than appended as they arrive. A space that turns out
// to be where the line broke is the break itself, not ink, and drawing it leaves a
// trailing blank that fills the last cell of the row — which is how a terminal is
// talked into wrapping a row we thought we owned — and that a text editor or a
// linter silently strips out of a golden file.
//
// Leading spaces are the exception, and only where the text chose the break: the
// indentation under a `--- FAIL:` header is what tells a reader that the line below
// belongs to it, and a wrapper that eats it turns a subtest into a package. So a
// space is dropped at the start of a line the width broke and kept at the start of
// one a newline broke, and an indent with no room left for a word gives way to the
// word. A space *between* two words is never dropped, however close to the edge it
// falls: there it is the break, and deleting it would join two words into one.
func WrapSpans(spans []Span, width int, cont Line) []Line {
	if width <= 0 {
		return nil
	}
	contW := cont.Width()
	var lines []Line
	cur, curW := Line{}, 0
	var pend []Span
	pendW := 0
	// keepIndent says the current line begins where the text says it does, so the
	// spaces arriving now are the author's indentation rather than the space some
	// break was made at. True at the very start and after a newline; false after a
	// width break, where the space is the break itself.
	keepIndent := true
	// ink says something has been written on this line besides its indentation. It is
	// what tells the two kinds of space that reach the last column apart: an indent
	// that wide has to give way, but a space between two words that wide is simply
	// where the line breaks.
	ink := false
	flush := func() {
		lines = append(lines, cur)
		cur, curW = append(Line{}, cont...), contW
		pend, pendW = nil, 0
		keepIndent = false
		ink = false
	}
	// hardBreak ends the line the text asked to end rather than the one the width
	// chose. Held-back spaces are dropped for the same reason they are at a width
	// break — a space before a break is not ink — and the line is trimmed, so a
	// paragraph break under a hanging indent becomes an empty row instead of a row
	// made of padding, which would be a trailing blank in the last cell.
	hardBreak := func() {
		cur = cur.TrimRight()
		flush()
		keepIndent = true
	}
	commit := func(sp Span, w int) {
		cur = append(append(cur, pend...), sp)
		curW += pendW + w
		pend, pendW = nil, 0
		ink = true
	}
	for _, tk := range tokenize(spans) {
		if tk.brk {
			hardBreak()
			continue
		}
		w := ansi.StringWidth(tk.text)
		if tk.space {
			if curW == contW && !keepIndent {
				continue // never start a line with padding the width chose
			}
			// An indent that leaves no room for a word is not an indent. Held back it
			// would take the whole row, and the word after it would be truncated to
			// nothing and dropped, which is a worse lie than losing the spaces. Only an
			// indent, though: a space between two words that reaches the last column is
			// where the line breaks, and dropped there it lets the next word up onto a
			// row that no longer has the space in front of it — which glues two of
			// somebody's words into one and is invisible to every check we have, since
			// the row neither overflows nor ends in a blank.
			if !ink && curW+pendW+w >= width {
				continue
			}
			pend = append(pend, Span{Text: tk.text, Style: tk.style})
			pendW += w
			continue
		}
		for w > 0 {
			if curW+pendW+w <= width {
				commit(Span{Text: tk.text, Style: tk.style}, w)
				break
			}
			if curW > contW || (len(lines) == 0 && curW > 0) {
				flush()
				continue
			}
			// The word alone does not fit: cut it at the edge and carry the rest.
			head := ansi.Truncate(tk.text, width-curW-pendW, "")
			if head == "" {
				break // nothing fits at all; give up rather than loop forever
			}
			commit(Span{Text: head, Style: tk.style}, ansi.StringWidth(head))
			tk.text = tk.text[len(head):]
			w = ansi.StringWidth(tk.text)
			flush()
		}
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

// WrapText is WrapSpans for a single style.
func WrapText(text, style string, width int, cont Line) []Line {
	return WrapSpans([]Span{{Text: text, Style: style}}, width, cont)
}

// HardWrap breaks at the edge without looking for spaces, which is what code and
// preformatted output want.
func HardWrap(text, style string, width int, cont Line) []Line {
	if width <= 0 {
		return nil
	}
	var lines []Line
	prefix, contW := Line{}, 0
	for text != "" {
		room := width - contW
		if room <= 0 {
			break
		}
		head := ansi.Truncate(text, room, "")
		if head == "" {
			break
		}
		lines = append(lines, append(append(Line{}, prefix...), Span{Text: head, Style: style}))
		text = text[len(head):]
		prefix, contW = cont, cont.Width()
	}
	if len(lines) == 0 {
		lines = append(lines, Line{})
	}
	return lines
}

// HardWrapSpans is HardWrap for text that is already coloured. Neither wrapper
// above fits a diff row on its own: WrapSpans breaks on spaces, which is wrong for
// code, and HardWrap takes a single style, which is wrong for anything the
// highlighter has been through. Continuation is left to the caller, because a diff
// row's continuation is not a fixed prefix — it is a blank gutter and a repeated
// sign, and both need the row's wash.
//
// Cuts are made with ansi.Truncate rather than by byte count, so a wide grapheme is
// never split down the middle and never pushes a row one cell over the width.
func HardWrapSpans(spans []Span, width int) []Line {
	if width <= 0 {
		return nil
	}
	var out []Line
	cur, curW := Line{}, 0
	flush := func() {
		out = append(out, cur)
		cur, curW = Line{}, 0
	}
	for _, sp := range spans {
		text := sp.Text
		for text != "" {
			room := width - curW
			head := ""
			if room > 0 {
				head = ansi.Truncate(text, room, "")
			}
			if head == "" {
				if curW == 0 {
					return out // one grapheme is wider than the whole row
				}
				flush()
				continue
			}
			cur = append(cur, Span{Text: head, Style: sp.Style, Fill: sp.Fill})
			curW += ansi.StringWidth(head)
			text = text[len(head):]
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// expandTabs replaces tabs with spaces up to the next stop. A raw tab in a line
// makes the terminal jump to its own tab stop, which is never where we put the
// cursor.
func expandTabs(s string, stop int) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := stop - col%stop
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
}
