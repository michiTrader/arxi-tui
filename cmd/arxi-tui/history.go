package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// inputHistory is what the up and down arrows walk: the lines the user has sent, oldest
// first. It follows the sibling project's editor — the line being typed is stashed on the
// way out and given back on the way down, because losing a half-written prompt to an
// accidental arrow key is unforgivable — and, like a shell, it survives the session.
type inputHistory struct {
	lines []string
	idx   int    // len(lines) means "editing a fresh line"
	stash string // the line being typed when the walk began
	path  string // "" keeps the history in memory only
	// edits holds what the user changed in a recalled line before stepping off it, by
	// the line's index. A recalled line that was edited is the user's work like the
	// stash is: walking away from it (an arrow pressed by mistake on the last row)
	// must not throw the edit away, so stepping back lands on the edit, not on the
	// original. Cleared when a line is sent.
	edits map[int]string
}

// historyMax is how many lines are kept, in memory and on disk.
const historyMax = 500

// historyEnv names the directory the history file lives in, for tests and for people who
// keep their configuration somewhere else.
const historyEnv = "ARXI_HISTORY_DIR"

// historyPath is where the history is kept: next to the keys and the providers, in
// ~/.arxi. Empty when there is none to be found, which leaves the
// history in memory.
func historyPath() string {
	dir := os.Getenv(historyEnv)
	if dir == "" {
		dir = configDir()
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "history")
}

// loadHistory reads the history file. A missing or damaged file is an empty history, never
// an error: a convenience must not stop the program from starting.
func loadHistory(path string) *inputHistory {
	h := &inputHistory{path: path}
	if path != "" {
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
			for sc.Scan() {
				var s string
				if json.Unmarshal(sc.Bytes(), &s) == nil && strings.TrimSpace(s) != "" {
					h.lines = append(h.lines, s)
				}
			}
			f.Close()
		}
	}
	if len(h.lines) > historyMax {
		h.lines = h.lines[len(h.lines)-historyMax:]
	}
	h.idx = len(h.lines)
	return h
}

// Add records a sent line. An empty line and a repeat of the last one are not recorded, and
// either way the walk starts again from the newest end.
func (h *inputHistory) Add(line string) {
	// A draft that was being typed when an older line was recalled and sent instead
	// is not lost with the walk: it goes into the history just before the sent line,
	// so the up arrow still finds it.
	draft := h.stash
	h.idx, h.stash, h.edits = len(h.lines), "", nil
	if strings.TrimSpace(draft) != "" {
		h.record(draft)
		h.idx = len(h.lines)
	}
	if strings.TrimSpace(line) == "" {
		return
	}
	h.record(line)
	h.idx = len(h.lines)
}

// record appends a line to the history and the file, unless it repeats the last one.
func (h *inputHistory) record(line string) {
	if n := len(h.lines); n > 0 && h.lines[n-1] == line {
		return
	}
	h.lines = append(h.lines, line)
	trimmed := len(h.lines) > historyMax
	if trimmed {
		h.lines = h.lines[len(h.lines)-historyMax:]
	}
	h.save(line, trimmed)
}

// keepEdit remembers the edit made to the recalled line the walk is on, when there is one.
func (h *inputHistory) keepEdit(current string) {
	if h.idx >= len(h.lines) {
		return
	}
	if h.edits == nil {
		h.edits = map[int]string{}
	}
	h.edits[h.idx] = current
}

// at is the line the walk shows at i: the user's edit of it when there is one.
func (h *inputHistory) at(i int) string {
	if e, ok := h.edits[i]; ok {
		return e
	}
	return h.lines[i]
}

// save appends one line to the file, or rewrites it whole when the oldest lines dropped off.
// It is best effort for the same reason loading is.
func (h *inputHistory) save(line string, rewrite bool) {
	if h.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return
	}
	if rewrite {
		var b strings.Builder
		for _, l := range h.lines {
			enc, _ := json.Marshal(l)
			b.Write(enc)
			b.WriteByte('\n')
		}
		_ = os.WriteFile(h.path, []byte(b.String()), 0o600)
		return
	}
	// 0600: what people type to a model can be as private as what they type to a shell.
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	enc, _ := json.Marshal(line)
	_, _ = f.Write(append(enc, '\n'))
}

// Browsing reports whether the line on screen came from the history walk.
func (h *inputHistory) Browsing() bool { return h.idx < len(h.lines) }

// Recalled reports whether the line on screen is exactly the one the walk is on. A recalled
// line that starts with "/" (a "/model vy" that was sent once) looks like the beginning of a
// menu, and the arrows must keep walking the history instead of moving that menu's highlight;
// the moment the user edits it, it is theirs again and the menu gets its keys.
func (h *inputHistory) Recalled(input string) bool {
	return h.idx < len(h.lines) && input == h.lines[h.idx]
}

// Older steps back. current is the line being edited; it is stashed when the walk starts.
// ok is false when there is nothing older.
func (h *inputHistory) Older(current string) (string, bool) {
	if h.idx == 0 || len(h.lines) == 0 {
		return "", false
	}
	if h.idx == len(h.lines) {
		h.stash = current
	} else {
		h.keepEdit(current)
	}
	h.idx--
	return h.at(h.idx), true
}

// Newer steps forward, ending on the stashed line. ok is false when the walk is not on.
func (h *inputHistory) Newer(current string) (string, bool) {
	if h.idx >= len(h.lines) {
		return "", false
	}
	h.keepEdit(current)
	h.idx++
	if h.idx == len(h.lines) {
		s := h.stash
		h.stash = ""
		return s, true
	}
	return h.at(h.idx), true
}

// historyStep is the direction a key asks for: -1 older, +1 newer, 0 not a history key.
// The arrows are the ones a hand reaches for; ctrl+p and ctrl+n are readline's names for
// the same two steps, and they work where the arrows are taken (the slash menu's).
func historyStep(k term.Key) int {
	switch {
	case k.Type == term.KeyUp && k.Mod == 0:
		return -1
	case k.Type == term.KeyDown && k.Mod == 0:
		return 1
	case k.Type == term.KeyRunes && k.Mod == term.ModCtrl && len(k.Runes) == 1:
		switch k.Runes[0] {
		case 'p':
			return -1
		case 'n':
			return 1
		}
	}
	return 0
}
