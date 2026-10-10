package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is /copy: putting what the model answered on the clipboard without
// selecting it with the mouse (a drag-select also takes the frame's border).
//
// The menu `/copy ` lists, from the top: `last` (the newest answer), `all` (the whole
// conversation, each message labelled) and then the earlier answers numbered 2, 3, ...
// counting back from the newest, so the number is the same in the menu and in
// `/copy 3`. Enter copies the highlighted row; Tab marks several rows (a ✓ shows) and
// Enter then copies all the marked ones, oldest first.
//
// # How the text gets out
//
// The terminal is asked with OSC 52, which works over ssh and inside tmux, and a local
// clipboard program is run as well when there is one (termux-clipboard-set, pbcopy, the
// Wayland and X11 tools, clip on Windows). Neither reports back whether it worked: a
// terminal that ignores OSC 52 drops it silently, so the notice says what was sent, not
// that it arrived.

const copyPrefix = "/copy "

// copyMaxBytes caps what is sent through OSC 52: many terminals drop a longer sequence.
const copyMaxBytes = 100 * 1024

// copyCommand reports whether Enter on this line (or the highlighted row) is a bare
// `/copy`, which opens the menu.
func copyCommand(input string, sel int, cat string) bool {
	return menuCommand("copy", input, sel, cat)
}

func copyMenuOpen(input string) (filter string, open bool) { return menuOpen(copyPrefix, input) }

// copyMenu is the `/copy ` menu: the rows, and which of them are marked.
type copyMenu struct {
	modelMenu
	marked map[string]bool
}

// answers returns the assistant answers of a conversation, oldest first.
func answers(h []fold.ChatLine) []string {
	var out []string
	for _, l := range h {
		if l.Role == "assistant" && strings.TrimSpace(l.Text) != "" {
			out = append(out, l.Text)
		}
	}
	return out
}

// preview is the first line of a text, cut to fit a menu row.
func preview(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > 60 {
		s = string(r[:59]) + "…"
	}
	return s
}

// copyMenuData builds the rows for a conversation: last, all, then 2..n.
func copyMenuData(h []fold.ChatLine) []fold.ModelMatch {
	as := answers(h)
	if len(as) == 0 {
		return []fold.ModelMatch{{Name: "nothing to copy", Provider: "the model has not answered yet"}}
	}
	rows := []fold.ModelMatch{
		{Ref: "last", Name: "last", Provider: preview(as[len(as)-1])},
		{Ref: "all", Name: "all", Provider: "the whole conversation, " + plural(len(h), "message", "messages")},
	}
	for n := 2; n <= len(as); n++ {
		rows = append(rows, fold.ModelMatch{Ref: strconv.Itoa(n), Name: strconv.Itoa(n), Provider: preview(as[len(as)-n])})
	}
	return rows
}

// setHistory loads the rows for a conversation and forgets earlier marks.
func (c *copyMenu) setHistory(h []fold.ChatLine) {
	c.setRows(copyMenuData(h))
	c.marked = map[string]bool{}
}

// view is the filtered rows with the marked ones carrying the ✓.
func (c *copyMenu) view(filter string) ([]fold.ModelMatch, int) {
	rows, sel := c.modelMenu.view(filter)
	out := make([]fold.ModelMatch, len(rows))
	for i, r := range rows {
		out[i] = r
		out[i].Current = r.Ref != "" && c.marked[r.Ref]
	}
	return out, sel
}

// copyMenuKey applies one key while the menu is open. It returns the new buffer and
// caret and, when the user chose, the pick: "last", "all", a number, or several
// numbers joined by commas ("2,5") when rows were marked.
func copyMenuKey(c *copyMenu, input string, caret int, k term.Key) (next string, nextCaret int, pick string) {
	filter, _ := menuOpen(copyPrefix, input)
	rows, sel := c.view(filter)
	switch {
	case k.Type == term.KeyTab:
		if len(rows) > 0 && rows[sel].Ref != "" {
			if c.marked == nil {
				c.marked = map[string]bool{}
			}
			c.marked[rows[sel].Ref] = !c.marked[rows[sel].Ref]
			if !c.marked[rows[sel].Ref] {
				delete(c.marked, rows[sel].Ref)
			}
		}
		return input, caret, ""
	case navAction(k) == "pick":
		if len(c.marked) > 0 {
			var refs []string
			for ref := range c.marked {
				refs = append(refs, ref)
			}
			return "", 0, strings.Join(refs, ",")
		}
		// `/copy 3` typed whole: the exact row, whatever else the filter matches.
		for _, r := range rows {
			if r.Ref != "" && r.Ref == strings.TrimSpace(filter) {
				return "", 0, r.Ref
			}
		}
	}
	return choiceMenuKey(&c.modelMenu, copyPrefix, input, caret, k)
}

// copyText turns a pick into the text to copy and a short account of it.
func copyText(h []fold.ChatLine, pick string) (text, what string, err error) {
	as := answers(h)
	if len(as) == 0 {
		return "", "", errors.New("nothing to copy: the model has not answered yet")
	}
	if strings.Contains(","+pick+",", ",all,") { // "all" already holds every answer
		var parts []string
		for _, l := range h {
			var who string
			switch l.Role {
			case "user":
				who = "You"
			case "assistant":
				who = "Assistant"
			default:
				continue
			}
			if strings.TrimSpace(l.Text) == "" {
				continue
			}
			parts = append(parts, who+":\n"+l.Text)
		}
		return strings.Join(parts, "\n\n"), "the conversation", nil
	}
	// Numbers, oldest first so the pasted text reads in the order it was said.
	var nums []int
	for _, p := range strings.Split(pick, ",") {
		p = strings.TrimSpace(p)
		if p == "last" {
			p = "1"
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > len(as) {
			return "", "", fmt.Errorf("/copy: there is no answer %q (the conversation has %d)", p, len(as))
		}
		nums = append(nums, n)
	}
	for i := 0; i < len(nums); i++ { // sort descending: highest n is oldest
		for j := i + 1; j < len(nums); j++ {
			if nums[j] > nums[i] {
				nums[i], nums[j] = nums[j], nums[i]
			}
		}
	}
	var parts []string
	seen := map[int]bool{}
	for _, n := range nums {
		if !seen[n] {
			seen[n] = true
			parts = append(parts, as[len(as)-n])
		}
	}
	if len(parts) == 1 {
		return parts[0], "1 answer", nil
	}
	return strings.Join(parts, "\n\n"), plural(len(parts), "answer", "answers"), nil
}

// osc52 is the escape sequence that asks the terminal to set its clipboard. Inside tmux
// it is wrapped so tmux forwards it.
func osc52(text string, tmux bool) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if tmux {
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// clipHelper is a program that sets the system clipboard from its stdin.
type clipHelper struct {
	argv  []string
	utf16 bool // Windows clip reads UTF-16LE with a byte order mark
}

// clipHelpers lists the helpers worth trying on this system, first the ones found.
func clipHelpers(goos string, lookPath func(string) (string, error)) []clipHelper {
	var cands []clipHelper
	switch goos {
	case "windows":
		cands = []clipHelper{{argv: []string{"clip"}, utf16: true}}
	case "darwin":
		cands = []clipHelper{{argv: []string{"pbcopy"}}}
	default:
		cands = []clipHelper{
			{argv: []string{"termux-clipboard-set"}},
			{argv: []string{"wl-copy"}},
			{argv: []string{"xclip", "-selection", "clipboard"}},
			{argv: []string{"xsel", "--clipboard", "--input"}},
		}
	}
	var out []clipHelper
	for _, c := range cands {
		if _, err := lookPath(c.argv[0]); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// helperInput is the bytes a helper reads.
func (c clipHelper) input(text string) []byte {
	if !c.utf16 {
		return []byte(text)
	}
	u := utf16.Encode([]rune(text))
	b := []byte{0xff, 0xfe}
	for _, w := range u {
		b = append(b, byte(w), byte(w>>8))
	}
	return b
}

// run feeds the text to the helper.
func (c clipHelper) run(text string) error {
	cmd := exec.Command(c.argv[0], c.argv[1:]...)
	cmd.Stdin = bytes.NewReader(c.input(text))
	return cmd.Run()
}

// copyToClipboard sends text to the terminal (OSC 52) and to every helper that runs. It
// reports what it could not do; an OSC 52 write that the terminal drops is invisible.
func copyToClipboard(tty interface{ Write([]byte) (int, error) }, text string) (note string) {
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "�")
	}
	viaHelper := false
	for _, h := range clipHelpers(runtime.GOOS, exec.LookPath) {
		if h.run(text) == nil {
			viaHelper = true
			break
		}
	}
	if len(text) > copyMaxBytes {
		if viaHelper {
			return ""
		}
		return fmt.Sprintf("too long for the terminal clipboard (%d kB, the limit is %d kB) and no clipboard program was found",
			len(text)/1024, copyMaxBytes/1024)
	}
	if tty != nil {
		_, _ = tty.Write([]byte(osc52(text, os.Getenv("TMUX") != "")))
	}
	return ""
}

// copyPicked copies a pick and returns the notice to show.
func copyPicked(tty interface{ Write([]byte) (int, error) }, h []fold.ChatLine, pick string) string {
	text, what, err := copyText(h, pick)
	if err != nil {
		return err.Error()
	}
	if note := copyToClipboard(tty, text); note != "" {
		return "/copy: " + note
	}
	return fmt.Sprintf("copied %s (%d kB) — if nothing pastes, your terminal ignores OSC 52", what, (len(text)+1023)/1024)
}
