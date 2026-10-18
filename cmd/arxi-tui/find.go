package main

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// Ctrl+F: look for a word in the chat.
//
// Dogfooding (ADR-0003): nothing here draws. The bar is the prompt's own `suffix` node in the
// scene (bound to host.find, styled with the find.bar token), the hits are washed with the
// find.match / find.current tokens, and the words are in the text registry. What the host owns
// is the small view state a scene cannot hold - the word, which hit is current - the same
// way it owns the scroll offset and the paste hint.
//
// Keys, while the bar is open:
//
//	letters / backspace   edit the word (the chat jumps to the newest hit as you type)
//	enter, up             the previous (older) hit
//	down                  the next (newer) hit
//	esc, ctrl+f           close the bar (the chat stays where it is)
//
// Every other key (page up/down, the wheel, ctrl+up...) keeps its usual job. Ctrl+C is never
// touched. A shortcut of the user's own on ctrl+f wins, and a scene whose prompt has no find
// bar leaves ctrl+f alone: a bar nobody can see must not swallow the keyboard.

// findMaxWord bounds the word, so a pasted document cannot become the search.
const findMaxWord = 200

func findKeys(add func(key, def, role string, paragraph bool)) {
	add("find.label", "find", "the word on the find bar (ctrl+f) that comes before the hit count and what you are looking for", false)
}

// finder is the find bar's state. The loop owns one.
type finder struct {
	open  bool
	word  string
	back  int  // 0: the newest hit, 1: the one before... (the renderer clamps it and hands it back)
	total int  // rows that hold the word, as the last frame counted them
	jump  bool // the next frame lands the chat on the current hit
}

// label is what the scene's suffix shows: "find 2/5: word ", or "find: " with no word. The
// trailing space is where the caret sits, one cell past the word.
func (f *finder) label() string {
	if !f.open {
		return ""
	}
	name := uiText("find.label")
	if f.word == "" {
		return name + ": "
	}
	cur := f.total - f.back
	if f.total == 0 {
		cur = 0
	}
	return name + " " + strconv.Itoa(cur) + "/" + strconv.Itoa(f.total) + ": " + f.word + " "
}

// close puts the bar away. The word is kept: ctrl+f again looks for it again.
func (f *finder) close() { f.open = false }

// begin opens the bar. A word kept from the last time is looked for at once.
func (f *finder) begin() {
	f.open = true
	f.back = 0
	f.jump = f.word != ""
}

// edited is every change of the word: the newest hit is the first one shown.
func (f *finder) edited() {
	f.back = 0
	f.jump = f.word != ""
}

// owns says whether a key is the finder's: ctrl+f when it can open, and the bar's own keys
// while it is open. canOpen is false when the scene has no bar or the user bound the chord.
func (f *finder) owns(k term.Key, canOpen bool) bool {
	if isCtrlF(k) {
		return canOpen || f.open
	}
	if !f.open {
		return false
	}
	switch k.Type {
	case term.KeyEscape, term.KeyEnter, term.KeyUp, term.KeyDown, term.KeyBackspace:
		// a modified arrow or enter is something else (ctrl+up scrolls, shift+enter is a new line)
		return k.Mod == 0
	case term.KeyRunes:
		return k.Mod&(term.ModCtrl|term.ModAlt) == 0 && len(k.Runes) > 0
	}
	return false
}

// key applies a key the finder owns.
func (f *finder) key(k term.Key) {
	switch {
	case isCtrlF(k):
		if f.open {
			f.close()
		} else {
			f.begin()
		}
	case k.Type == term.KeyEscape:
		f.close()
	case k.Type == term.KeyEnter, k.Type == term.KeyUp:
		f.back++
		f.jump = true
	case k.Type == term.KeyDown:
		if f.back > 0 {
			f.back--
		}
		f.jump = true
	case k.Type == term.KeyBackspace:
		if rs := []rune(f.word); len(rs) > 0 {
			f.word = string(rs[:len(rs)-1])
			f.edited()
		}
	case k.Type == term.KeyRunes:
		f.add(string(k.Runes))
	}
}

// add appends typed or pasted text to the word, on one line.
func (f *finder) add(s string) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < ' ' || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return
	}
	if rs := []rune(f.word + s); len(rs) <= findMaxWord {
		f.word = string(rs)
		f.edited()
	}
}

func isCtrlF(k term.Key) bool { return ctrlLetter(k, 'f') }

// hasFindBar says whether the scene on display gives the prompt a find bar: an input bound to
// user.input whose suffix is bound to host.find.
func hasFindBar(doc *scene.Document) bool {
	if doc == nil {
		return false
	}
	in := findUserInput(doc.Root)
	return in != nil && in.Suffix != nil && in.Suffix.Bind == "host.find"
}

// findBarSuffix is the prompt's find bar, as the built-in scene writes it.
const findBarSuffix = `{"type":"text","bind":"host.find","style":{"style":"find.bar"}}`

// withFindBar gives a saved interface the find bar it was saved without. A scene kept before
// ctrl+f existed has a prompt with no suffix, so ctrl+f would do nothing at all; the bar is
// invisible while closed, so adding it changes nothing a user could see. Only an input bound to
// user.input that has no suffix of its own gets it: a scene that already draws something
// there, or has no prompt, is left exactly as it is. The returned document keeps the name
// (the saved file), so it is still saved as the user's.
func withFindBar(doc *scene.Document) *scene.Document {
	if doc == nil || hasFindBar(doc) {
		return doc
	}
	in := findUserInput(doc.Root)
	if in == nil || in.Suffix != nil {
		return doc
	}
	var tree any
	if json.Unmarshal(doc.Source(), &tree) != nil {
		return doc
	}
	done := false
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if !done && t["type"] == "input" && t["bind"] == "user.input" && t["suffix"] == nil {
				var bar any
				_ = json.Unmarshal([]byte(findBarSuffix), &bar)
				t["suffix"] = bar
				done = true
				return
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(tree)
	if !done {
		return doc
	}
	out, err := json.Marshal(tree)
	if err != nil {
		return doc
	}
	next, err := scene.ParseNamed(doc.Name(), out)
	if err != nil || next.Validate() != nil || !hasFindBar(next) {
		return doc
	}
	return next
}

// noFindBarNotice is what ctrl+f says when the scene on display has no find bar.
const noFindBarNotice = "ctrl+f needs the find bar, and this interface has none: /ui reset restores the built-in one, or ask the chat to add a suffix bound to host.find to the prompt"
