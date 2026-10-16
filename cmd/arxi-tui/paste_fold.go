package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// A big paste does not belong in the input bar: sixty-eight lines of log would push what the
// reader is typing off the screen. A paste over the threshold is kept aside and the bar holds a
// short marker in its place, "[Pasted text #1 +68 lines]". Three rules keep that honest:
//
//   - What is SENT is the real text: Enter swaps every marker for what it stands for, so the
//     model never reads the marker. The history keeps the real text too.
//   - Pasting the same text AGAIN unfolds the marker in the bar (the info bar says so while a
//     marker is there), so the reader can look at or edit what was pasted. A different text is
//     a paste of its own, folded or not by the same rule.
//   - Backspace right after a marker (or Delete right before it) removes the whole marker, not
//     one letter of it: half a marker is neither the text nor a clean marker.
//
// The store belongs to the loop, like the buffer and the caret; it empties when a line is sent.

// pasteFoldLines / pasteFoldChars are the factory thresholds: a paste folds when it has MORE
// lines than the first or MORE characters than the second. Either can be changed with
// /ui text paste.fold_lines and /ui text paste.fold_chars; 0 turns that test off, and with
// both off a paste is always shown whole.
const (
	pasteFoldLines = 5
	pasteFoldChars = 1500
)

// pasteHint is what the info bar says while the bar holds a folded paste.
const pasteHint = "paste again to expand"

func pasteKeys(add func(key, def, role string, paragraph bool)) {
	add("paste.fold_lines", strconv.Itoa(pasteFoldLines), "a paste with more lines than this is shown in the input as [Pasted text #1 +N lines] (0: never by lines)", false)
	add("paste.fold_chars", strconv.Itoa(pasteFoldChars), "a paste with more characters than this is shown in the input as [Pasted text #1 N chars] (0: never by size)", false)
	add("paste.hint", pasteHint, "what the info bar says while the input holds a folded paste", false)
}

type pasteStore struct {
	next int               // the number the next marker gets (starts at 1)
	text map[string]string // marker -> what it stands for
}

func (p *pasteStore) has(input string) bool {
	for tok := range p.text {
		if strings.Contains(input, tok) {
			return true
		}
	}
	return false
}

// shouldFold says whether a paste is big enough to be kept aside, by the active thresholds.
func shouldFold(text string) bool {
	// Each limit is read with its key spelled out, so the check that every declared text key
	// is read somewhere (TestEveryTextKeyUsedByTheHostIsDeclared) can see it.
	whole := func(raw string, def int) int {
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || v < 0 {
			return def
		}
		return v
	}
	if n := whole(uiText("paste.fold_lines"), pasteFoldLines); n > 0 && strings.Count(text, "\n")+1 > n {
		return true
	}
	if n := whole(uiText("paste.fold_chars"), pasteFoldChars); n > 0 && len([]rune(text)) > n {
		return true
	}
	return false
}

// fold keeps text aside and returns the marker that stands for it.
func (p *pasteStore) fold(text string) string {
	if p.text == nil {
		p.text = map[string]string{}
	}
	if p.next < 1 {
		p.next = 1
	}
	var tok string
	if lines := strings.Count(text, "\n") + 1; lines > 1 {
		tok = fmt.Sprintf("[Pasted text #%d +%d lines]", p.next, lines)
	} else {
		tok = fmt.Sprintf("[Pasted text #%d %d chars]", p.next, len([]rune(text)))
	}
	p.next++
	p.text[tok] = text
	return tok
}

// expand puts the real text back in place of every marker the line holds.
func (p *pasteStore) expand(input string) string {
	for tok, text := range p.text {
		input = strings.ReplaceAll(input, tok, text)
	}
	return input
}

// reset forgets every marker: the line was sent, the next paste is #1 again.
func (p *pasteStore) reset() { p.text, p.next = nil, 0 }

// unfold answers a paste of text that a marker in the line already stands for: the marker is
// replaced by the text, in place, and the caret keeps its place relative to the text around it.
func (p *pasteStore) unfold(input string, caret int, text string) (string, int, bool) {
	rs := []rune(input)
	best, bestAt := "", -1
	for tok, held := range p.text {
		if held != text {
			continue
		}
		if at := runeIndex(rs, []rune(tok)); at >= 0 && (bestAt < 0 || at < bestAt) {
			best, bestAt = tok, at
		}
	}
	if bestAt < 0 {
		return input, caret, false
	}
	tr, hr := []rune(best), []rune(p.text[best])
	out := append(append(append([]rune{}, rs[:bestAt]...), hr...), rs[bestAt+len(tr):]...)
	switch {
	case caret >= bestAt+len(tr):
		caret += len(hr) - len(tr)
	case caret > bestAt:
		caret = bestAt + len(hr)
	}
	// The entry stays: ctrl+z can bring the marker back, and a marker must still mean its text.
	return string(out), caret, true
}

// dropWhole makes Backspace/Delete treat a marker as one character: Backspace with the caret
// right after a marker, or Delete with it right before one, removes all of it.
func (p *pasteStore) dropWhole(input string, caret int, k term.Key) (string, int, bool) {
	if k.Mod&(term.ModCtrl|term.ModAlt) != 0 || (k.Type != term.KeyBackspace && k.Type != term.KeyDelete) {
		return input, caret, false
	}
	rs := []rune(input)
	for tok := range p.text {
		tr := []rune(tok)
		at := runeIndex(rs, tr)
		for at >= 0 {
			end := at + len(tr)
			if (k.Type == term.KeyBackspace && caret == end) || (k.Type == term.KeyDelete && caret == at) {
				return string(append(append([]rune{}, rs[:at]...), rs[end:]...)), at, true
			}
			next := runeIndex(rs[end:], tr)
			if next < 0 {
				break
			}
			at = end + next
		}
	}
	return input, caret, false
}

func runeIndex(s, sub []rune) int {
	if len(sub) == 0 || len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// sentPaste remembers, for the one line that was just sent, what the bar showed (markers
// and all) and what was really sent. The chat draws the first, so a long paste reads in
// the conversation the way it read in the bar; the model, the history and the session
// file keep the second. Pasting the same text twice had unfolded the marker in the bar
// before Enter, so then the two are equal and nothing is replaced: the chat always equals
// what the bar showed.
type sentPaste struct{ shown, real string }

// remember is called when Enter is about to send: shown is the line as the bar holds it,
// real the same line with every marker swapped for its text (equal when there was nothing
// to hide, and then drawing "shown" changes nothing).
func (s *sentPaste) remember(shown, real string) {
	*s = sentPaste{shown: strings.TrimSpace(shown), real: strings.TrimSpace(real)}
}

// forChat returns the event the chat should draw. Only the run.prompt that carries exactly
// the remembered real text is changed (on a copy: the original goes on to the session
// file untouched). The memory is spent by the first prompt that comes, whichever it is.
func (s *sentPaste) forChat(e fold.Event) fold.Event {
	if s.real == "" || e.Type != "run.prompt" {
		return e
	}
	text, _ := e.Payload["text"].(string)
	mine := strings.TrimSpace(text) == s.real
	shown := s.shown
	// The memory is for the very next line the chat shows. Spent either way, so it can
	// never rewrite a later line that happens to hold the same words.
	*s = sentPaste{}
	if !mine {
		return e
	}
	payload := make(map[string]any, len(e.Payload))
	for k, v := range e.Payload {
		payload[k] = v
	}
	payload["text"] = shown
	e.Payload = payload
	return e
}
