package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Tables are the one markdown construct that does not fit the line-at-a-time state
// machine in markdown.go. Every other case decides what a row is by looking at that
// row; a table is only a table once the delimiter row under its header has arrived,
// and its column widths are a fact about all of its rows at once. So it gets a file,
// and RenderMarkdown gets a case that hands it a slice of them.
//
// Two forms, chosen by arithmetic rather than by a flag. Where the columns fit there is
// a grid: the header, a rule under it, a vertical between neighbours, and no outer box
// — a box costs ten columns to repeat what the rule already says, and this interface
// draws a fenced code block with a bare gutter for the same reason. Where they do not
// fit the grid is abandoned rather than squeezed, and the test for that is whether every
// column can still hold its widest whole word: at twenty columns three cells would share
// fourteen, which is a stack of syllables and not a table, and at forty the file names in
// scenario 06 would be cut a letter short of their extension. Each row is drawn as its own
// small record instead. Scenario 02 is the recording that goes down that path and 06 the
// one that stays in the grid at a normal width, which is why both are in the corpus.

// minCol is the narrowest a column may be before the grid stops being worth drawing.
// Eight columns holds a short word or a count like "+9 -2"; below that every cell wraps
// into a column of fragments and the reader is left doing the table's work. It is the
// floor under the word rule in renderTable rather than the usual reason a table stacks:
// at twenty-four columns three cells would share eighteen, which no arithmetic about
// words can rescue, so the arithmetic is not consulted.
const minCol = 8

// cellAlign is what the colons in a delimiter row asked for.
type cellAlign int

const (
	alignLeft cellAlign = iota
	alignRight
	alignCenter
)

// tableRows counts the lines of the table at the head of lines, and returns zero when
// what starts there is not one.
//
// The delimiter row is required. That is markdown's own rule, and it is worth keeping
// for a second reason here: prose with a pipe in it stays prose. The cost is that a
// header whose delimiter has not arrived yet draws as text for one frame, which is the
// same honest flicker an unclosed fence already shows.
func tableRows(lines []string) int {
	if len(lines) < 2 || !isTableRow(lines[0]) || !isDelimiterRow(lines[1]) {
		return 0
	}
	n := 2
	for n < len(lines) && isTableRow(lines[n]) {
		n++
	}
	return n
}

// isTableRow wants the leading pipe. GitHub does not require it, but a renderer that
// does not either has to call "a | b" a table, and an assistant writes that in a
// sentence.
func isTableRow(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "|") }

// isDelimiterRow is the row of dashes: every cell ---, :--- or :---:, dashes required.
func isDelimiterRow(s string) bool {
	cells := tableCells(s)
	for _, c := range cells {
		c = strings.TrimSuffix(strings.TrimPrefix(c, ":"), ":")
		if c == "" || strings.Trim(c, "-") != "" {
			return false
		}
	}
	return len(cells) > 0
}

// tableCells splits a row on its unescaped pipes and drops the empty pieces the outer
// ones leave behind. A \| survives as a pipe inside the cell, because a table of shell
// snippets is exactly where somebody needs one.
func tableCells(row string) []string {
	s := strings.TrimSpace(row)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			cur.WriteByte('|')
			i++
		case s[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// tableAligns reads the colons off the delimiter row. A column with none is left, which
// is what an unqualified column gets everywhere else too.
func tableAligns(row string, n int) []cellAlign {
	cells := tableCells(row)
	out := make([]cellAlign, n)
	for i := range out {
		if i >= len(cells) {
			continue
		}
		left := strings.HasPrefix(cells[i], ":")
		right := strings.HasSuffix(cells[i], ":")
		switch {
		case left && right:
			out[i] = alignCenter
		case right:
			out[i] = alignRight
		}
	}
	return out
}

// fitCells squares a row off against the header's column count: markdown drops the
// cells a row has too many of and treats the ones it is missing as empty.
func fitCells(cells []string, n int) []string {
	out := make([]string, n)
	copy(out, cells)
	return out
}

// naturalWidths is how wide each column would like to be: the widest cell in it,
// measured after the inline markers are gone, since ** and ` are not drawn.
//
// The floor of one is not cosmetic. A column of empty cells would otherwise be zero
// columns wide, and WrapSpans answers a width of zero with no lines at all — the cell
// would vanish and take the row's height down with it.
func naturalWidths(head []string, body [][]string, n int, base string) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = 1
	}
	measure := func(cells []string) {
		for i, c := range cells {
			if i < n {
				out[i] = max(out[i], Line(inlineSpans(c, base)).Width())
			}
		}
	}
	measure(head)
	for _, r := range body {
		measure(r)
	}
	return out
}

// fitColumns spends budget on the columns. Where everything fits, every column keeps
// its natural width: a table of two short words stays two short words wide instead of
// being stretched across the terminal, which is most of what tidier means here.
//
// Where it does not fit, the narrow columns are paid in full and the wide ones share
// what is left over, level by level — so a table of one long column and two short ones
// spends the width on the long one instead of wrapping all three alike. It reports
// false when not even the floor can be met, which is the caller's cue to stop drawing
// a grid at all.
func fitColumns(natural []int, budget int) ([]int, bool) {
	n := len(natural)
	if n == 0 || budget < n*minCol {
		return nil, false
	}
	out := make([]int, n)
	total := 0
	for _, w := range natural {
		total += w
	}
	if total <= budget {
		copy(out, natural)
		return out, true
	}
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return natural[order[a]] < natural[order[b]] })
	left := budget
	for k, i := range order {
		share := left / (n - k)
		if natural[i] < share {
			share = natural[i]
		}
		out[i] = share
		left -= share
	}
	return out, true
}

// longestWords is the widest unbreakable run in each column. WrapSpans cuts a word that
// does not fit rather than letting it overflow, so a column narrower than this number is
// a column that splits words down the middle — "runshow_test.g" over "o" — and a grid of
// half words is harder to read than the records the fallback draws. It is the rule that
// usually decides which form a table takes; minCol is only the floor under it.
func longestWords(head []string, body [][]string, n int, base string) []int {
	out := make([]int, n)
	measure := func(cells []string) {
		for i, c := range cells {
			if i >= n {
				continue
			}
			// Measured on the drawn text, like the widths themselves: `max_output_bytes` is
			// sixteen columns of word and not eighteen.
			for _, word := range strings.Fields(Line(inlineSpans(c, base)).Text()) {
				out[i] = max(out[i], ansi.StringWidth(word))
			}
		}
	}
	measure(head)
	for _, r := range body {
		measure(r)
	}
	return out
}

// holdsWholeWords reports whether every column ended up wide enough for the widest word
// in it. It is evaluated after fitColumns has spent the budget, because a column's fate
// depends on what the other columns took.
func holdsWholeWords(cols, words []int) bool {
	for i, w := range words {
		if cols[i] < w {
			return false
		}
	}
	return true
}

// renderTable draws the table tableRows has already found: lines[0] is the header,
// lines[1] the delimiter, and everything after that is the body.
func renderTable(lines []string, width int, base string) []Line {
	head := tableCells(lines[0])
	n := len(head)
	aligns := tableAligns(lines[1], n)
	body := make([][]string, 0, len(lines))
	for _, ln := range lines[2:] {
		body = append(body, fitCells(tableCells(ln), n))
	}
	// The separator is measured and not assumed to be three columns wide: frame.v is
	// overridable, and what the columns get to divide is whatever is left after every
	// separator has been paid for.
	sep := Line{pad(1), {Text: "│", Style: "markdown.table.frame"}, pad(1)}
	cols, ok := fitColumns(naturalWidths(head, body, n, base), width-sep.Width()*(n-1))
	if !ok || !holdsWholeWords(cols, longestWords(head, body, n, base)) {
		return stackedTable(head, body, width, base)
	}
	out := make([]Line, 0, len(lines)+1)
	out = append(out, tableRow(fitCells(head, n), cols, aligns, sep, "markdown.table.header")...)
	out = append(out, tableRule(cols, sep.Width(), width))
	for _, r := range body {
		out = append(out, tableRow(r, cols, aligns, sep, base)...)
	}
	return out
}

// tableRow draws one row, which is as tall as its tallest cell. Each cell wraps inside
// its own column, so a sentence in the last one lengthens the row rather than pushing it
// off the frame.
//
// Every separator is drawn on every line of the row, including the lines a short cell has
// nothing left to say on. The vertical is the only thing telling the eye where a column
// ends, and a boundary that stops halfway down a wrapped row makes the cells either side
// of it read as one. The line is then trimmed, so a row whose right-hand cells are spent
// costs one vertical rather than a run of padding — a line may not end in a space, which
// is how a terminal is talked into wrapping a row we thought we owned.
func tableRow(cells []string, cols []int, aligns []cellAlign, sep Line, base string) []Line {
	wrapped := make([][]Line, len(cols))
	height := 1
	for i, w := range cols {
		wrapped[i] = WrapSpans(inlineSpans(cells[i], base), w, nil)
		height = max(height, len(wrapped[i]))
	}
	out := make([]Line, 0, height)
	for row := 0; row < height; row++ {
		var line Line
		for i := range cols {
			if i > 0 {
				line = append(line, sep...)
			}
			var cell Line
			if row < len(wrapped[i]) {
				cell = wrapped[i][row]
			}
			line = append(line, alignCell(cell, cols[i], aligns[i])...)
		}
		out = append(out, line.TrimRight())
	}
	return out
}

// alignCell pads a wrapped cell out to its column. The colons in a delimiter row are the
// whole reason a column of counts reads as a column: "+9 -2" over "+8" lines up on the
// digits or it does not line up at all.
func alignCell(cell Line, w int, a cellAlign) Line {
	gap := w - cell.Width()
	if gap <= 0 {
		return cell
	}
	out := make(Line, 0, len(cell)+2)
	switch a {
	case alignRight:
		out = append(out, pad(gap))
		out = append(out, cell...)
	case alignCenter:
		out = append(out, pad(gap/2))
		out = append(out, cell...)
		out = append(out, pad(gap-gap/2))
	default:
		out = append(out, cell...)
		out = append(out, pad(gap))
	}
	return out
}

// tableRule is the one line that makes the grid a grid: a run of frame.h under each
// column, crossed where a separator passes through it.
//
// It is built to the frame's width and then cut to it, because all three glyphs are
// overridable and a two-cell one inside a repeat is how a row ends up a column too wide.
func tableRule(cols []int, sepW, width int) Line {
	h, cross := "─", "┼"
	gap := max(0, sepW-ansi.StringWidth(cross))
	var b strings.Builder
	for i, w := range cols {
		if i > 0 {
			b.WriteString(fill(h, gap/2))
			b.WriteString(cross)
			b.WriteString(fill(h, gap-gap/2))
		}
		b.WriteString(fill(h, w))
	}
	return Line{{Text: ansi.Truncate(b.String(), width, ""), Style: "markdown.table.frame"}}
}

// fill repeats a glyph to a width. A glyph is a string and not a rune — an override may
// be "=" or "──" — so the run is measured and cut rather than counted.
func fill(glyph string, w int) string {
	gw := ansi.StringWidth(glyph)
	if w <= 0 || gw <= 0 {
		return ""
	}
	return ansi.Truncate(strings.Repeat(glyph, w/gw+1), w, "")
}

// stackedTable is the table with the grid taken away, for a window too narrow to hold
// one. Each body row becomes a small record of "header: value" lines under a hanging
// indent, blank-separated — the shape a table takes on a phone, and readable, which the
// grid at this width would not be. The headers keep their own style, so a value still
// says which column it came from.
//
// An empty cell is skipped rather than drawn as a label with nothing after it: the point
// of this form is that the record is short.
func stackedTable(head []string, body [][]string, width int, base string) []Line {
	var out []Line
	if len(body) == 0 {
		// A table whose body has not arrived yet. Its headers are all there is to say,
		// and saying them is better than holding the frame back until the first row lands.
		for _, h := range head {
			out = append(out, WrapText(h, "markdown.table.header", width, nil)...)
		}
		return out
	}
	// The hanging indent is capped rather than assumed to fit. WrapSpans measures a
	// continuation prefix against the same width as the text, so an indent as wide as the
	// frame would draw a row made of padding and overflow it by the whole indent.
	cont := Line{pad(min(2, max(0, width-1)))}
	for r, row := range body {
		if r > 0 {
			out = append(out, Line{})
		}
		for i, c := range row {
			if c == "" {
				continue
			}
			var spans []Span
			if i < len(head) && head[i] != "" {
				spans = append(spans, Span{Text: head[i] + ": ", Style: "markdown.table.header"})
			}
			spans = append(spans, inlineSpans(c, base)...)
			out = append(out, WrapSpans(spans, width, cont)...)
		}
	}
	return out
}
