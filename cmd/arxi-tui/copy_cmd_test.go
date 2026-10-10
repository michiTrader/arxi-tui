package main

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

func convo() []fold.ChatLine {
	return []fold.ChatLine{
		{Role: "user", Text: "one?"},
		{Role: "assistant", Text: "first answer\nsecond line"},
		{Role: "tool", Text: "ignored"},
		{Role: "user", Text: "two?"},
		{Role: "assistant", Text: "second answer"},
		{Role: "user", Text: "three?"},
		{Role: "assistant", Text: "third answer"},
	}
}

func TestCopyMenuRowsAreLastAllThenNumbers(t *testing.T) {
	rows := copyMenuData(convo())
	var refs []string
	for _, r := range rows {
		refs = append(refs, r.Ref)
	}
	if got := strings.Join(refs, ","); got != "last,all,2,3" {
		t.Fatalf("rows = %s, want last,all,2,3", got)
	}
	if rows[0].Provider != "third answer" || rows[2].Provider != "second answer" || rows[3].Provider != "first answer" {
		t.Errorf("previews = %q / %q / %q (first line only, newest first)", rows[0].Provider, rows[2].Provider, rows[3].Provider)
	}
}

func TestCopyMenuWithoutAnswersSaysSoAndPicksNothing(t *testing.T) {
	rows := copyMenuData([]fold.ChatLine{{Role: "user", Text: "hi"}})
	if len(rows) != 1 || rows[0].Ref != "" {
		t.Fatalf("rows = %+v; the single row must carry no reference", rows)
	}
	if _, _, err := copyText(nil, "last"); err == nil {
		t.Error("copying with no answer must say so")
	}
}

func TestCopyTextLastNumberAndAll(t *testing.T) {
	h := convo()
	if got, _, _ := copyText(h, "last"); got != "third answer" {
		t.Errorf("last = %q", got)
	}
	if got, _, _ := copyText(h, "3"); got != "first answer\nsecond line" {
		t.Errorf("3 = %q", got)
	}
	all, _, _ := copyText(h, "all")
	for _, want := range []string{"You:\none?", "Assistant:\nfirst answer", "Assistant:\nthird answer"} {
		if !strings.Contains(all, want) {
			t.Errorf("all lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "ignored") {
		t.Error("tool lines do not belong in the copied conversation")
	}
	if _, _, err := copyText(h, "9"); err == nil {
		t.Error("an answer that does not exist must be refused")
	}
}

func TestCopySeveralComesOldestFirst(t *testing.T) {
	got, what, err := copyText(convo(), "1,3,2")
	if err != nil {
		t.Fatal(err)
	}
	want := "first answer\nsecond line\n\nsecond answer\n\nthird answer"
	if got != want || what != "3 answers" {
		t.Errorf("got %q (%s)", got, what)
	}
}

func runesKey(s string) term.Key { return term.Key{Type: term.KeyRunes, Runes: []rune(s)} }

func TestCopyMenuTabMarksAndEnterCopiesTheMarked(t *testing.T) {
	var c copyMenu
	c.setHistory(convo())
	tab, enter := term.Key{Type: term.KeyTab}, term.Key{Type: term.KeyEnter}
	down := term.Key{Type: term.KeyDown}

	input := copyPrefix
	input, _, _ = copyMenuKey(&c, input, len(input), down) // on "all"
	input, _, _ = copyMenuKey(&c, input, len(input), down) // on "2"
	copyMenuKey(&c, input, len(input), tab)
	rows, _ := c.view("")
	if !rows[2].Current || rows[0].Current {
		t.Fatalf("the ✓ must show on the marked row only: %+v", rows)
	}
	input, _, _ = copyMenuKey(&c, input, len(input), down) // on "3"
	copyMenuKey(&c, input, len(input), tab)
	_, _, pick := copyMenuKey(&c, input, len(input), enter)
	got, _, err := copyText(convo(), pick)
	if err != nil || got != "first answer\nsecond line\n\nsecond answer" {
		t.Errorf("pick %q -> %q, %v", pick, got, err)
	}
}

func TestCopyMenuEnterWithoutMarksCopiesTheHighlightedRow(t *testing.T) {
	var c copyMenu
	c.setHistory(convo())
	_, _, pick := copyMenuKey(&c, copyPrefix, len(copyPrefix), term.Key{Type: term.KeyEnter})
	if pick != "last" {
		t.Errorf("pick = %q, want last", pick)
	}
}

func TestCopyTypedNumberPicksThatExactRow(t *testing.T) {
	var c copyMenu
	c.setHistory(convo())
	_, _, pick := copyMenuKey(&c, copyPrefix+"3", len(copyPrefix)+1, term.Key{Type: term.KeyEnter})
	if pick != "3" {
		t.Errorf("/copy 3 picked %q", pick)
	}
}

func TestOSC52CarriesTheTextInBase64AndWrapsForTmux(t *testing.T) {
	plain := osc52("héllo", false)
	if !strings.HasPrefix(plain, "\x1b]52;c;") || !strings.HasSuffix(plain, "\a") {
		t.Fatalf("plain = %q", plain)
	}
	b64 := strings.TrimSuffix(strings.TrimPrefix(plain, "\x1b]52;c;"), "\a")
	if dec, err := base64.StdEncoding.DecodeString(b64); err != nil || string(dec) != "héllo" {
		t.Errorf("payload = %q, %v", dec, err)
	}
	wrapped := osc52("x", true)
	if !strings.HasPrefix(wrapped, "\x1bPtmux;\x1b\x1b]52;c;") || !strings.HasSuffix(wrapped, "\x1b\\") {
		t.Errorf("tmux = %q", wrapped)
	}
}

func TestClipHelpersFollowThePlatformAndWhatIsInstalled(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, x := range names {
				if x == n {
					return n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	if h := clipHelpers("darwin", has("pbcopy")); len(h) != 1 || h[0].argv[0] != "pbcopy" {
		t.Errorf("darwin = %+v", h)
	}
	if h := clipHelpers("linux", has("xclip", "termux-clipboard-set")); len(h) != 2 || h[0].argv[0] != "termux-clipboard-set" {
		t.Errorf("linux = %+v (Termux first)", h)
	}
	if h := clipHelpers("linux", has()); len(h) != 0 {
		t.Errorf("nothing installed must give no helper: %+v", h)
	}
}

func TestWindowsClipGetsUTF16WithAByteOrderMark(t *testing.T) {
	h := clipHelpers("windows", func(string) (string, error) { return "clip", nil })[0]
	b := h.input("añ😀")
	if b[0] != 0xff || b[1] != 0xfe {
		t.Fatalf("no BOM: % x", b[:2])
	}
	var u []uint16
	for i := 2; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	if got := string(utf16.Decode(u)); got != "añ😀" {
		t.Errorf("round trip = %q", got)
	}
}

func TestCopyIsInTheSlashMenuAndOpensItsOwnMenu(t *testing.T) {
	found := false
	for _, m := range fold.FilterSlashMatches("copy") {
		found = found || m.Name == "copy"
	}
	if !found {
		t.Fatal("/copy is not listed in the slash menu")
	}
	if !copyCommand("/copy", 0, fold.SlashAll) {
		t.Error("a bare /copy must open the menu")
	}
	if copyCommand("/copy 3", 0, fold.SlashAll) {
		t.Error("/copy 3 is already inside the menu")
	}
}

func TestCopyPickedReportsWhatWasCopied(t *testing.T) {
	var sink strings.Builder
	t.Setenv("PATH", "") // no clipboard program: only the terminal path runs
	msg := copyPicked(&sink, convo(), "last")
	if !strings.Contains(msg, "copied 1 answer") {
		t.Errorf("notice = %q", msg)
	}
	if !strings.Contains(sink.String(), "\x1b]52;c;") {
		t.Errorf("nothing was sent to the terminal: %q", sink.String())
	}
	if msg := copyPicked(&sink, nil, "last"); !strings.Contains(msg, "nothing to copy") {
		t.Errorf("empty = %q", msg)
	}
}
