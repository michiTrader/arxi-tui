package main

import (
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/term"
)

func bigPaste(lines int) string {
	var b []string
	for i := 1; i <= lines; i++ {
		b = append(b, "row "+string(rune('a'+i%26)))
	}
	return strings.Join(b, "\n")
}

// setTexts puts the user's words on screen for the test and takes them off after it.
func setTexts(t *testing.T, pairs ...string) {
	t.Helper()
	u := userTexts{}
	for i := 0; i+1 < len(pairs); i += 2 {
		u[pairs[i]] = pairs[i+1]
	}
	setActiveTexts(u)
	t.Cleanup(func() { setActiveTexts(nil) })
}

func TestOnlyABigPasteIsFolded(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	for _, c := range []struct {
		text string
		want bool
	}{
		{bigPaste(5), false}, // exactly the limit: shown whole
		{bigPaste(6), true},  // one over
		{"short", false},
		{strings.Repeat("x", pasteFoldChars), false},
		{strings.Repeat("x", pasteFoldChars+1), true},
	} {
		if got := shouldFold(c.text); got != c.want {
			t.Errorf("shouldFold(%d lines, %d chars) = %v, want %v", strings.Count(c.text, "\n")+1, len(c.text), got, c.want)
		}
	}
}

func TestTheFoldThresholdsAreTheUsersToChange(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	setTexts(t, "paste.fold_lines", "2")
	if !shouldFold(bigPaste(3)) || shouldFold(bigPaste(2)) {
		t.Error("paste.fold_lines 2 did not move the limit to 2 lines")
	}
	setTexts(t, "paste.fold_lines", "0")
	if shouldFold(bigPaste(50)) {
		t.Error("paste.fold_lines 0 must turn the line test off")
	}
	if !shouldFold(strings.Repeat("x", pasteFoldChars+1)) {
		t.Error("the size test is independent of the line test")
	}
	setTexts(t, "paste.fold_lines", "0", "paste.fold_chars", "0")
	if shouldFold(strings.Repeat("x", 100000)) {
		t.Error("with both tests off a paste is always shown whole")
	}
	if validChatLook("paste.fold_lines", "-1") == nil || validChatLook("paste.fold_lines", "abc") == nil {
		t.Error("a negative or non numeric limit must be refused when written")
	}
}

// A limit that is not a usable number (a hand edited file can hold one) falls back to the
// factory limit instead of turning the test off or folding everything.
func TestAnUnusableLimitFallsBackToTheFactoryOne(t *testing.T) {
	setTexts(t, "paste.fold_lines", "-1", "paste.fold_chars", "abc")
	if shouldFold(bigPaste(5)) || !shouldFold(bigPaste(6)) {
		t.Error("a negative line limit must act as the factory limit (5)")
	}
	if shouldFold(strings.Repeat("x", pasteFoldChars)) || !shouldFold(strings.Repeat("x", pasteFoldChars+1)) {
		t.Error("a non numeric size limit must act as the factory limit")
	}
}

func TestAMarkerNamesTheNumberAndTheSize(t *testing.T) {
	var p pasteStore
	if got := p.fold(bigPaste(68)); got != "[Pasted text #1 +68 lines]" {
		t.Errorf("first marker = %q", got)
	}
	if got := p.fold(strings.Repeat("y", 2000)); got != "[Pasted text #2 2000 chars]" {
		t.Errorf("a one line paste is sized in chars: %q", got)
	}
	if got := p.fold("one\ntwo"); got != "[Pasted text #3 +2 lines]" {
		t.Errorf("two lines are counted in lines, not chars: %q", got)
	}
	p.reset()
	if got := p.fold(bigPaste(7)); got != "[Pasted text #1 +7 lines]" {
		t.Errorf("after a send the numbering starts again: %q", got)
	}
}

func TestExpandPutsTheRealTextBackAndLeavesTheRest(t *testing.T) {
	var p pasteStore
	a, b := bigPaste(8), strings.Repeat("z", 30)
	ma, mb := p.fold(a), p.fold(b)
	in := "see " + ma + " and " + mb + " ok [Pasted text #9 +1 lines]"
	want := "see " + a + " and " + b + " ok [Pasted text #9 +1 lines]"
	if got := p.expand(in); got != want {
		t.Errorf("expand = %q, want %q", got, want)
	}
	if !p.has(in) || p.has("plain") {
		t.Error("has must say whether a known marker is in the line")
	}
}

func TestPastingTheSameTextAgainOpensTheMarkerInPlace(t *testing.T) {
	var p pasteStore
	a := bigPaste(8)
	m := p.fold(a)
	in := "pre " + m + " post"
	caret := len([]rune(in)) // at the end
	out, nc, ok := p.unfold(in, caret, a)
	if !ok || out != "pre "+a+" post" {
		t.Fatalf("unfold = %q %v", out, ok)
	}
	if nc != len([]rune(out)) {
		t.Errorf("a caret after the marker keeps its place after the text: %d, want %d", nc, len([]rune(out)))
	}
	// caret inside the marker lands at the end of the opened text
	if _, nc, _ := p.unfold(in, 4+3, a); nc != 4+len([]rune(a)) {
		t.Errorf("a caret inside the marker goes to the end of the text, got %d", nc)
	}
	// caret before the marker does not move
	if _, nc, _ := p.unfold(in, 2, a); nc != 2 {
		t.Errorf("a caret before the marker must stay, got %d", nc)
	}
	// another text is not this marker
	if _, _, ok := p.unfold(in, caret, bigPaste(9)); ok {
		t.Error("a different text must not open the marker")
	}
	// with no marker in the line there is nothing to open
	if _, _, ok := p.unfold("no marker here", 3, a); ok {
		t.Error("unfold without the marker in the line must say no")
	}
	// the earliest of two equal markers opens first
	m2 := p.fold(a)
	two := m2 + " " + m
	out, _, _ = p.unfold(two, 0, a)
	if !strings.HasPrefix(out, a+" [Pasted") {
		t.Errorf("the first marker in the line opens first: %q", out)
	}
}

func TestBackspaceAndDeleteTakeTheWholeMarker(t *testing.T) {
	var p pasteStore
	m := p.fold(bigPaste(8))
	in := "ab" + m + "cd"
	end := 2 + len([]rune(m))
	bs := term.Key{Type: term.KeyBackspace}
	del := term.Key{Type: term.KeyDelete}
	if out, c, ok := p.dropWhole(in, end, bs); !ok || out != "abcd" || c != 2 {
		t.Errorf("backspace after the marker = %q %d %v", out, c, ok)
	}
	if out, c, ok := p.dropWhole(in, 2, del); !ok || out != "abcd" || c != 2 {
		t.Errorf("delete before the marker = %q %d %v", out, c, ok)
	}
	if _, _, ok := p.dropWhole(in, 2, bs); ok {
		t.Error("backspace BEFORE the marker is an ordinary backspace")
	}
	if _, _, ok := p.dropWhole(in, end, del); ok {
		t.Error("delete AFTER the marker is an ordinary delete")
	}
	if _, _, ok := p.dropWhole(in, end-1, bs); ok {
		t.Error("backspace inside the marker is not a whole-marker drop")
	}
	if _, _, ok := p.dropWhole(in, end, term.Key{Type: term.KeyBackspace, Mod: term.ModAlt}); ok {
		t.Error("alt+backspace keeps its own meaning")
	}
	if _, _, ok := p.dropWhole(in, end, term.Key{Type: term.KeyBackspace, Mod: term.ModCtrl}); ok {
		t.Error("ctrl+backspace keeps its own meaning")
	}
	// the same marker twice in a line: the second one is found too
	two := m + "-" + m
	if out, _, ok := p.dropWhole(two, len([]rune(two)), bs); !ok || out != m+"-" {
		t.Errorf("backspace after the second copy = %q %v", out, ok)
	}
	if _, _, ok := p.dropWhole(in, end, term.Key{Type: term.KeyLeft}); ok {
		t.Error("only backspace and delete drop a marker")
	}
}

// ---- through the real loop ----

func pasteLoop(t *testing.T, script []scheduledEvent) (frames string, submitted []string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	out, sub := runLoopDoc(t, factorySobria, append(script, exitKeys()...))
	return stripANSI(out), sub
}

func at(ms int, ev term.Event) scheduledEvent {
	return scheduledEvent{time.Duration(ms) * time.Millisecond, ev}
}

func TestLoopABigPasteShowsAMarkerAndSendsTheRealText(t *testing.T) {
	big := bigPaste(68)
	frames, sub := pasteLoop(t, []scheduledEvent{
		at(10, pasteEvent(big)),
		at(150, enterEvent()),
	})
	if !strings.Contains(frames, "[Pasted text #1 +68 lines]") {
		t.Errorf("the bar never showed the marker")
	}
	if !strings.Contains(frames, pasteHint) {
		t.Errorf("the info bar never said %q", pasteHint)
	}
	if len(sub) != 1 || sub[0] != big {
		t.Fatalf("sent %q; want the real 68 lines, not the marker", sub)
	}
}

func TestLoopPastingAgainOpensItAndTheHintGoes(t *testing.T) {
	big := bigPaste(12)
	frames, sub := pasteLoop(t, []scheduledEvent{
		at(10, pasteEvent(big)),
		at(150, pasteEvent(big)),
		at(300, enterEvent()),
	})
	if !strings.Contains(frames, "[Pasted text #1 +12 lines]") {
		t.Fatal("no marker was shown")
	}
	if len(sub) != 1 || sub[0] != big {
		t.Fatalf("pasting twice must open the marker (one copy), sent %q", sub)
	}
	// after the second paste the last frames must not say the hint any more
	last := frames[strings.LastIndex(frames, "row"):]
	if strings.Contains(last, pasteHint) {
		t.Errorf("the hint stayed after the marker was opened")
	}
}

func TestLoopTypingAroundAMarkerAndBackspaceOverIt(t *testing.T) {
	big := bigPaste(9)
	script := append(typed("A "), at(50, pasteEvent(big)))
	script = append(script, typed(" B")...)
	script = append(script, at(50, enterEvent()))
	_, sub := pasteLoop(t, script)
	if len(sub) != 1 || sub[0] != "A "+big+" B" {
		t.Fatalf("text around a marker: sent %q", sub)
	}

	// paste, backspace: the whole marker goes and nothing is sent for it
	_, sub = pasteLoop(t, []scheduledEvent{
		at(10, pasteEvent(big)),
		at(100, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}),
		at(100, enterEvent()),
	})
	if len(sub) != 0 {
		t.Errorf("a marker that was backspaced away still sent %q", sub)
	}
}

func TestLoopASmallPasteAndASlashPasteAreNotFolded(t *testing.T) {
	small := bigPaste(3)
	frames, sub := pasteLoop(t, []scheduledEvent{at(10, pasteEvent(small)), at(100, enterEvent())})
	if strings.Contains(frames, "Pasted text") || len(sub) != 1 || sub[0] != small {
		t.Errorf("a small paste must stay whole: sent %q", sub)
	}
	// a paste into a "/" line is for the command, never folded
	frames, _ = pasteLoop(t, []scheduledEvent{at(10, keyEventScript('/')), at(60, pasteEvent(bigPaste(9)))})
	if strings.Contains(frames, "Pasted text") {
		t.Errorf("a paste after / was folded")
	}
}

func TestLoopNumberingStartsAgainAfterASend(t *testing.T) {
	big := bigPaste(9)
	frames, sub := pasteLoop(t, []scheduledEvent{
		at(10, pasteEvent(big)), at(100, enterEvent()),
		at(100, pasteEvent(bigPaste(10))), at(100, enterEvent()),
	})
	if len(sub) != 2 {
		t.Fatalf("sent %d, want 2: %q", len(sub), sub)
	}
	if strings.Contains(frames, "#2") {
		t.Errorf("the second line's paste was numbered #2: numbering must restart after a send")
	}
}

func keyEventScript(r rune) term.Event { return keyEvent(r) }

// shift+Enter is a line break inside the text, not a send: it must not open the marker. Pasting
// the same text again afterwards proves the marker was still there (it opens, one copy) rather
// than already opened (a second copy).
func TestLoopANewlineGestureKeepsTheMarkerFolded(t *testing.T) {
	big := bigPaste(9)
	shiftEnter := term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEnter, Mod: term.ModShift}}
	_, sub := pasteLoop(t, []scheduledEvent{
		at(10, pasteEvent(big)),
		at(100, shiftEnter),
		at(100, pasteEvent(big)),
		at(150, enterEvent()),
	})
	if len(sub) != 1 || sub[0] != big {
		t.Fatalf("after shift+enter the marker must still be folded, so a second paste opens it: sent %q", sub)
	}
}
