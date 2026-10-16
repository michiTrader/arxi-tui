package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// A long line in the bar is a window that follows the caret: the engine tests cover the
// window itself; these drive it with real keys through the loop (it was listed as "tested
// in the engine only"). One line under the paste-fold limits wraps into more rows than the
// window may take (a third of the screen), so nothing is folded into a marker here.
func longLine() string {
	var w []string
	for i := 1; i <= 200; i++ {
		w = append(w, fmt.Sprintf("w%03d", i))
	}
	return strings.Join(w, " ")
}

func inputWindowRun(t *testing.T, ups int) string {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	script := []scheduledEvent{at(10, pasteEvent(longLine()))}
	for i := 0; i < ups; i++ {
		script = append(script, at(10, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyUp}}))
	}
	out, _ := runLoopDoc(t, factorySobria, append(script, exitKeys()...))
	return out
}

func TestLoopALongLineShowsTheEndWhereTheCaretIs(t *testing.T) {
	out := inputWindowRun(t, 0)
	f := frameWith(out, "w200")
	if f == "" {
		t.Fatal("no frame showed the end of the line, where the caret is")
	}
	if strings.Contains(f, "w001 ") {
		t.Errorf("the window showed the start of the line while the caret is at the end:\n%s", f)
	}
	if !strings.Contains(f, "↑") {
		t.Errorf("rows are hidden above the window and nothing says so (no ↑ marker):\n%s", f)
	}
	if strings.Contains(f, "↓") {
		t.Errorf("a ↓ marker with nothing below the caret:\n%s", f)
	}
}

func TestLoopTheWindowFollowsTheCaretBackUpToTheStart(t *testing.T) {
	out := inputWindowRun(t, 40)
	// The last frame that still holds the line (ctrl+c at the end clears it).
	f := frameWith(out, "w001 ")
	if f == "" {
		t.Fatal("moving the caret up never brought the start of the line into view")
	}
	if strings.Contains(f, "w200") {
		t.Errorf("the caret is at the start but the window still shows the end:\n%s", f)
	}
	if !strings.Contains(f, "↓") {
		t.Errorf("rows are hidden below the window and nothing says so (no ↓ marker):\n%s", f)
	}
	if strings.Contains(f, "↑") {
		t.Errorf("an ↑ marker with nothing above the caret:\n%s", f)
	}
}

// rawFrameWith is the last frame, rows kept apart, that shows what.
func rawFrameWith(out, what string) string {
	frames := strings.Split(out, frameBegin)
	for i := len(frames) - 1; i >= 0; i-- {
		if f := stripANSI(frames[i]); strings.Contains(f, what) {
			return f
		}
	}
	return ""
}

func TestLoopTheInputWindowNeverTakesMoreThanAThirdOfTheScreen(t *testing.T) {
	f := rawFrameWith(inputWindowRun(t, 0), "w200")
	rows := 0
	for _, l := range strings.Split(f, "\n") {
		if strings.Contains(l, "┃ w") {
			rows++
		}
	}
	if rows == 0 || rows > 8 {
		t.Errorf("the input window drew %d rows of the line on a 24-row screen; want 1..8 (a third):\n%s", rows, f)
	}
}

// The window does not slide while the caret is still inside it: two rows up from the end
// the last words stay on screen. This is what the loop remembering where the window was
// (inputScroll) buys; a window recomputed from the caret alone would slide with it.
func TestLoopTheWindowStaysPutWhileTheCaretIsInsideIt(t *testing.T) {
	out := inputWindowRun(t, 2)
	// The last frame that still holds any of the line: the one after the two Up keys.
	f := frameWith(out, "w1")
	if f == "" || !strings.Contains(f, "w200") {
		t.Fatalf("two rows up from the end the window slid away from the last words:\n%s", f)
	}
	if !strings.Contains(f, "↑") {
		t.Errorf("rows are still hidden above the window:\n%s", f)
	}
}
