package engine

import (
	"strings"
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/ui"
)

// Find in the chat (Ctrl+F). The renderer is the only place that knows the wrapped rows of
// the transcript, so the search lives here: the host hands over the word (ChatFind) and
// which hit is current (ChatFindBack), and gets back where to scroll (ChatFindStops). The
// hits are washed with two theme tokens, so a theme decides how they look.
const (
	// findMatchToken washes every hit; findCurrentToken the hits of the row the finder is on.
	findMatchToken   = "find.match"
	findCurrentToken = "find.current"
)

// rowRunes is the row's text, as typed and lower-cased (same length, rune for rune). ok is
// false for a row that carries a terminal escape: its cells are not its runes, so it is not
// searched.
func rowRunes(l ui.Line) (orig, lower []rune, ok bool) {
	t := l.Text()
	if strings.ContainsRune(t, 0x1b) {
		return nil, nil, false
	}
	orig = []rune(t)
	return orig, lowerRunes(t), true
}

// lowerRunes lower-cases rune by rune, so an index into the result is an index into the
// original (strings.ToLower may change the length of a text).
func lowerRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

// occurrences lists the [start, end) rune ranges of q in text, left to right, not
// overlapping.
func occurrences(text, q []rune) [][2]int {
	if len(q) == 0 {
		return nil
	}
	var out [][2]int
	for i := 0; i+len(q) <= len(text); {
		match := true
		for j := range q {
			if text[i+j] != q[j] {
				match = false
				break
			}
		}
		if match {
			out = append(out, [2]int{i, i + len(q)})
			i += len(q)
		} else {
			i++
		}
	}
	return out
}

// clampFind keeps "how many hits back from the newest" inside 0..n-1 (0 when there are none).
func clampFind(cur, n int) int {
	if n <= 0 || cur < 0 {
		return 0
	}
	if cur > n-1 {
		return n - 1
	}
	return cur
}

// findMatchRows is the index of every row that holds q (case-insensitive), top to bottom.
func findMatchRows(lines []ui.Line, q string) []int {
	qr := lowerRunes(q)
	if len(qr) == 0 {
		return nil
	}
	var out []int
	for i, l := range lines {
		_, low, ok := rowRunes(l)
		if ok && len(occurrences(low, qr)) > 0 {
			out = append(out, i)
		}
	}
	return out
}

// findStops is, for each matching row, the ChatScroll (distance from the tail) that puts
// that row in the middle of a window `budget` rows tall over `total` rows. A chat that fits
// has nothing to scroll: every stop is 0.
func findStops(matches []int, total, budget int) []int {
	if len(matches) == 0 {
		return nil
	}
	stops := make([]int, len(matches))
	max := total - budget
	if budget < 0 || max <= 0 {
		return stops
	}
	for i, m := range matches {
		start := m - budget/2
		if start < 0 {
			start = 0
		}
		if start > max {
			start = max
		}
		stops[i] = max - start
	}
	return stops
}

// highlightFind washes the hits in the matching rows. cur is the index (in matches) of the
// current row. Rows are copied before they are touched: the rows come out of the chat cache
// and are shared between frames.
func highlightFind(lines []ui.Line, matches []int, q string, cur int) []ui.Line {
	if len(matches) == 0 {
		return lines
	}
	qr := lowerRunes(q)
	out := make([]ui.Line, len(lines))
	copy(out, lines)
	for k, m := range matches {
		tok := findMatchToken
		if k == cur {
			tok = findCurrentToken
		}
		out[m] = washRow(lines[m], qr, tok)
	}
	return out
}

// washRow returns a copy of the row with every hit of q under Fill=token. The spans are
// split at the hit's edges; each part keeps its own Style, so the colour of a word is not
// lost (Style is drawn over Fill).
func washRow(l ui.Line, q []rune, token string) ui.Line {
	orig, low, ok := rowRunes(l)
	if !ok {
		return l
	}
	_ = orig
	hits := occurrences(low, q)
	if len(hits) == 0 {
		return l
	}
	inHit := func(i int) bool {
		for _, h := range hits {
			if i >= h[0] && i < h[1] {
				return true
			}
		}
		return false
	}
	var out ui.Line
	pos := 0
	for _, s := range l {
		rs := []rune(s.Text)
		var run []rune
		runHit := false
		flush := func() {
			if len(run) == 0 {
				return
			}
			sp := ui.Span{Text: string(run), Style: s.Style, Fill: s.Fill, Hit: s.Hit}
			if runHit {
				sp.Fill = token
			}
			out = append(out, sp)
			run = nil
		}
		for i, r := range rs {
			h := inHit(pos + i)
			if h != runHit {
				flush()
				runHit = h
			}
			run = append(run, r)
		}
		flush()
		pos += len(rs)
	}
	return out
}
