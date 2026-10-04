package ui

import "strings"

// The glyphs the transcript draws its structure with. They are constants here and not a
// user-visible table: the theme owns colour, and a shape change belongs in a release.
const (
	glyphBullet   = "• "
	glyphGutter   = "│ "
	glyphQuoteBar = "▏ "
)

// Markdown, rendered incrementally. The hard requirement is that a half-arrived
// document draws correctly: scenario 01 has a delta that opens a ```go fence and
// a later delta that closes it, so any renderer that needs a complete document
// before it can produce output shows nothing for 150 ms and then jumps. This one
// runs the state machine over whatever text has arrived and stops where the text
// stops. An unclosed fence simply means the block is still code.
//
// It is deliberately small: headings, emphasis, code spans, links, fenced code,
// lists, quotes and tables. That is what an assistant actually emits. Tables live in
// table.go, because they are the one construct whose layout is a fact about several
// rows at once rather than about the row being drawn.

// RenderMarkdown lays text out in width columns.
func RenderMarkdown(text string, width int, base string) []Line {
	if width <= 0 {
		return nil
	}
	gutter := Line{{Text: glyphGutter, Style: "markdown.code.fence"}}
	raw := strings.Split(text, "\n")
	if n := len(raw); n > 1 && raw[n-1] == "" {
		raw = raw[:n-1] // a trailing newline is a terminator, not a blank line
	}
	var out []Line
	inFence, lang := false, ""
	// hang is the column the open list item's text starts at, carried to the next row so
	// that an assistant wrapping its own list — which is how a long item arrives, indented
	// under the words rather than under the marker — draws as one item instead of an item
	// followed by a paragraph. It is cleared on entry to every row and set again only by a
	// list item and by a row continuing one, which is what keeps the paragraph after a
	// list out of the list.
	hang := 0
	for i := 0; i < len(raw); i++ {
		trimmed := strings.TrimRight(raw[i], " ")
		open := hang
		hang = 0
		// A table is the one construct that spans rows, so it is the one case that has to
		// look past the row it is on — and the answer decides how far the loop jumps, which
		// is why it is counted out here rather than inside the switch. Not inside a fence:
		// there a row of pipes is code and nothing else.
		rows := 0
		if !inFence && isTableRow(trimmed) {
			rows = tableRows(raw[i:])
		}
		switch {
		case strings.HasPrefix(strings.TrimSpace(trimmed), "```"):
			// The info string is kept, not discarded: it is the only place a fence
			// says what language it holds, and the highlighter needs the answer on
			// every row that follows.
			inFence, lang = !inFence, ""
			if inFence {
				lang = strings.TrimPrefix(strings.TrimSpace(trimmed), "```")
			}
		case inFence:
			body := expandTabs(trimmed, 4)
			// Squeezed past the width of the screen the gutter gives way rather than the
			// code, the same rule a quote's bars follow: a row of structure with no room
			// for the text it introduces says nothing at all, and dropping the row instead
			// loses a line of somebody's program with no sign that it was ever there.
			gt := gutter
			if gt.Width() >= width {
				gt = Line{}
			}
			for _, w := range HardWrapSpans(Highlight(body, lang, "markdown.codeblock"), width-gt.Width()) {
				out = append(out, join(gt, w))
			}
		case rows > 0:
			out = append(out, renderTable(raw[i:i+rows], width, base)...)
			i += rows - 1
		case trimmed == "":
			out = append(out, Line{})
		case isHeading(trimmed):
			out = append(out, WrapText(headingText(trimmed), "markdown.heading", width, nil)...)
		case isQuote(trimmed):
			out = append(out, renderQuote(trimmed, width, base)...)
		case isBullet(trimmed):
			indent, body := bulletParts(trimmed)
			head := Line{{Text: indent + glyphBullet, Style: "markdown.bullet"}}
			hang = head.Width()
			out = append(out, listItem(head, body, width, base)...)
		case isOrdered(trimmed):
			indent, marker, body := orderedParts(trimmed)
			head := Line{{Text: indent + marker, Style: "markdown.bullet"}}
			hang = head.Width()
			out = append(out, listItem(head, body, width, base)...)
		// An indented row under an open item continues it: the source indent is structure
		// rather than text, so it is dropped and the row is redrawn at the item's own
		// column. Two spaces are enough to ask for this — an assistant does not always
		// count them out to the width of its marker — but one is not, because a single
		// space in front of a sentence is a typo far more often than it is a continuation.
		// A row with no indent at all is deliberately left alone: read as a lazy
		// continuation it would swallow the paragraph that follows every list.
		case open > 0 && strings.HasPrefix(trimmed, "  "):
			hang = open
			cont := Line{{Text: strings.Repeat(" ", open)}}
			out = append(out, listItem(cont, strings.TrimLeft(trimmed, " "), width, base)...)
		default:
			out = append(out, WrapSpans(inlineSpans(trimmed, base), width, nil)...)
		}
	}
	return out
}

func isHeading(s string) bool {
	t := strings.TrimLeft(s, "#")
	return len(t) < len(s) && len(s)-len(t) <= 6 && strings.HasPrefix(t, " ")
}

func headingText(s string) string {
	return strings.TrimSpace(strings.TrimLeft(s, "#"))
}

func isBullet(s string) bool {
	t := strings.TrimLeft(s, " ")
	return strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ")
}

func bulletParts(s string) (indent, body string) {
	t := strings.TrimLeft(s, " ")
	return s[:len(s)-len(t)], t[2:]
}

// isOrdered reports whether the line opens a numbered list item. The space after the
// delimiter is what does the work: without it "1.5x faster" and a sentence ending in
// "…shipped in 2026." would both become lists, and a number that is prose never has
// a space behind its dot.
func isOrdered(s string) bool { return orderedMark(strings.TrimLeft(s, " ")) > 0 }

// orderedMark returns the length of the "1. " that opens a numbered item, or 0. Both
// delimiters markdown allows are accepted, because an assistant writes either.
func orderedMark(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
		return 0
	}
	return i + 2
}

// orderedParts splits a numbered item into its indentation, the marker as it will be
// drawn — the number, its delimiter and the one space behind them — and the text. The
// number is kept as the author wrote it rather than counted here: a renderer that
// renumbers has to remember what came before, and this one is a state machine over a
// document that may still be arriving.
func orderedParts(s string) (indent, marker, body string) {
	t := strings.TrimLeft(s, " ")
	n := orderedMark(t)
	return s[:len(s)-len(t)], t[:n], t[n:]
}

// listItem draws one item of either kind of list: the marker, then the text wrapped
// into what is left and indented under itself, so the second line of an item lines up
// with the first word rather than with the marker. The only difference between a
// bullet and a number is what the head says, which is why both go through here — and a
// row continuing an item from the source passes a head of plain spaces, which is the
// same drawing with nothing written in the gutter.
func listItem(head Line, body string, width int, base string) []Line {
	// A marker that leaves no room for the item gives way to it, the same way a quote's
	// bars and a fence's gutter do. Kept, it would take the whole row and the text would
	// be wrapped into nothing and silently dropped.
	if head.Width() >= width {
		return WrapSpans(inlineSpans(body, base), width, nil)
	}
	cont := Line{{Text: strings.Repeat(" ", head.Width())}}
	var out []Line
	for i, w := range WrapSpans(inlineSpans(body, base), width-head.Width(), nil) {
		prefix := head
		if i > 0 {
			prefix = cont
		}
		out = append(out, join(prefix, w))
	}
	return out
}

func isQuote(s string) bool { return strings.HasPrefix(strings.TrimLeft(s, " "), ">") }

// quoteParts counts the levels of nesting and returns what is quoted. One space after
// each marker belongs to the marker, which is what makes "> > a" two levels rather
// than one level holding "> a".
func quoteParts(s string) (depth int, body string) {
	t := strings.TrimLeft(s, " ")
	for strings.HasPrefix(t, ">") {
		depth++
		t = strings.TrimPrefix(t[1:], " ")
	}
	return depth, t
}

// renderQuote draws a blockquote with one bar per level of nesting, on every row it
// occupies rather than only the first. That is the difference between a quote and a
// list: a list's marker introduces an item, but a quote's bar says the row is still
// somebody else's words, so a wrapped line that dropped it would read as the
// assistant's own. The body is inline markdown only — a quote holding a fence or a
// table is not something an assistant emits, and supporting it would mean running the
// whole state machine again one level down.
func renderQuote(s string, width int, base string) []Line {
	depth, body := quoteParts(s)
	bars := Line{}
	for i := 0; i < depth; i++ {
		bars = append(bars, Span{Text: glyphQuoteBar, Style: "markdown.quote.bar"})
	}
	// Nested past the width of the screen, the bars have to give way rather than the
	// words: a row of bars with no room for text says nothing at all.
	for len(bars) > 0 && bars.Width() >= width {
		bars = bars[:len(bars)-1]
	}
	var out []Line
	for _, w := range WrapSpans(inlineSpans(body, "markdown.quote"), width-bars.Width(), nil) {
		out = append(out, join(bars, w))
	}
	return out
}

// inlineSpans splits a line into styled runs: strong, emphasis, code spans and links.
// Runs are found left to right, so the first marker in the line is the one that wins
// and a backtick before an asterisk keeps `*Store` a code span rather than the start
// of an italic. An unterminated marker is left as literal text, which is exactly what
// a half-arrived delta looks like: the alternative, italicising the rest of the
// paragraph until the closing asterisk arrives, makes the transcript flicker as it
// streams.
func inlineSpans(s, base string) []Span {
	var out []Span
	add := func(text, style string) {
		if text != "" {
			out = append(out, Span{Text: text, Style: style})
		}
	}
	plain := 0
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				add(s[plain:i], base)
				add(s[i+2:i+2+end], "markdown.strong")
				i += 4 + end
				plain = i
				continue
			}
			// Both asterisks of an unterminated ** are literal, which is why this case
			// steps over the pair. Left to the fallthrough, the second one would open an
			// emphasis nobody wrote as soon as another asterisk turned up on the line.
			i += 2
			continue
		case s[i] == '*':
			if end := emphasisEnd(s, i); end > 0 {
				add(s[plain:i], base)
				add(s[i+1:end], "markdown.emphasis")
				i = end + 1
				plain = i
				continue
			}
		case s[i] == '`':
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				add(s[plain:i], base)
				add(s[i+1:i+1+end], "markdown.code")
				i += 2 + end
				plain = i
				continue
			}
		case s[i] == '[':
			if text, url, n := linkParts(s[i:]); n > 0 {
				add(s[plain:i], base)
				add(text, "markdown.link")
				// The url is drawn after the text because a terminal that cannot make the
				// text clickable would otherwise show a link to nowhere. When the text is
				// already the url, repeating it says nothing.
				if url != text {
					add(" ("+url+")", "markdown.link.url")
				}
				i += n
				plain = i
				continue
			}
		}
		i++
	}
	add(s[plain:], base)
	return out
}

// emphasisEnd finds the asterisk that closes an emphasis run opened at i, or -1 if the
// line holds none. The content may not begin or end with a space, and that one rule is
// what keeps arithmetic and globs out of italics: "2 * 3 * 4", "rm *.go", "*args and
// *kwargs" and "./**/*.go" all put a space on one side of the run they would otherwise
// open. A closing asterisk up against a word does emphasise mid-word — "w*h*2"
// italicises the h — which is what CommonMark does with it too.
func emphasisEnd(s string, i int) int {
	if i+1 >= len(s) || s[i+1] == ' ' {
		return -1
	}
	end := strings.IndexByte(s[i+1:], '*')
	if end < 0 {
		return -1
	}
	end += i + 1
	if s[end-1] == ' ' {
		return -1
	}
	return end
}

// linkParts reads a [text](url) at the start of s and returns how many bytes it took.
// The parenthesis has to open immediately after the bracket closes, which is what
// leaves "- [ ] a task" and a footnote marker like [1] alone, and neither half may be
// empty, because a link to nowhere is text somebody bracketed. A closing parenthesis
// inside the url ends it early; a url that carries one is rare enough that counting
// nesting would cost more than it saves.
func linkParts(s string) (text, url string, n int) {
	end := strings.IndexByte(s, ']')
	if end < 2 || !strings.HasPrefix(s[end+1:], "(") {
		return "", "", 0
	}
	shut := strings.IndexByte(s[end+2:], ')')
	if shut <= 0 {
		return "", "", 0
	}
	return s[1:end], s[end+2 : end+2+shut], end + 3 + shut
}

// join glues a prefix to a wrapped line. The trim is what keeps a marker with no text
// yet from leaving a blank in the last cell it occupies.
func join(prefix, tail Line) Line {
	return append(append(Line{}, prefix...), tail...).TrimRight()
}

// pad returns n spaces as a single unstyled span.
func pad(n int) Span { return Span{Text: strings.Repeat(" ", max(0, n))} }
