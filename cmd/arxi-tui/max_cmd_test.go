package main

import (
	"context"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// These tests pin the host-level grammar of `/max [pane]` and its dispatch, the
// write half of Scene 10 DASHBOARD. The parser is pure, so it is pinned directly;
// the press path is exercised through focusKey the way the other on_press tests
// are, against a real uiMax cursor so the test proves the host state moved rather
// than that a function was called.

// TestParseMaxReadsThePaneAndRestore pins the three shapes of the command: a
// pane to maximize, the bare restore, and a line that is not /max at all (so the
// dispatch chain lets it fall through to the next handler).
func TestParseMaxReadsThePaneAndRestore(t *testing.T) {
	pane, matched, err := parseMax("/max chat")
	if !matched || err != nil {
		t.Fatalf("/max chat refused: matched=%v err=%v", matched, err)
	}
	if pane != "chat" {
		t.Errorf("/max chat parsed pane %q, want chat", pane)
	}

	// Bare /max is restore, not a mistake: it clears ui.max so the grid returns,
	// the gesture the maximized view's cmd:/max button uses.
	pane, matched, err = parseMax("/max")
	if !matched || err != nil {
		t.Fatalf("/max refused: matched=%v err=%v", matched, err)
	}
	if pane != "" {
		t.Errorf("bare /max parsed pane %q, want \"\" (restore)", pane)
	}

	// The on_press surface hands the line with the cmd: prefix already stripped,
	// so the bare-verb spelling must match too.
	if _, matched, _ := parseMax("max team"); !matched {
		t.Error("the bare-verb spelling \"max team\" was not matched; a pressed cmd:/max would not be recognized")
	}
}

// TestParseMaxDeclinesForeignLinesAndRefusesExtraArgs pins the two edges: a line
// that is not /max returns matched=false (left for the next dispatch branch), and
// a /max with more than one argument is refused with a located message rather
// than silently maximizing the wrong token.
func TestParseMaxDeclinesForeignLinesAndRefusesExtraArgs(t *testing.T) {
	for _, line := range []string{"/focus chat", "/model list", "maximize", "/maxi"} {
		if _, matched, _ := parseMax(line); matched {
			t.Errorf("parseMax claimed %q, which is not a /max command; it would swallow the line from the handler that owns it", line)
		}
	}
	_, matched, err := parseMax("/max chat team")
	if !matched {
		t.Fatal("/max chat team was not matched, so its refusal falls through instead of being named")
	}
	if err == nil || !strings.Contains(err.Error(), "at most one pane id") {
		t.Errorf("/max with two args was not refused with the expected message: %v", err)
	}
}

// TestPressCmdMaxWritesUIMax pins the press path: pressing a pane's
// cmd:/max <pane> button writes the host ui.max cursor and does NOT submit the
// line to the agent. The second half is the regression this guards — before the
// interception, cmd:/max chat fell through to SubmitPrompt and sent the literal
// text "max chat" to the model instead of maximizing anything.
func TestPressCmdMaxWritesUIMax(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"go","type":"button","text":"max","on_press":"cmd:/max chat"}
	]}}`)
	notice := ""
	uiMax := ""
	drv := &testDriver{evCh: make(chan fold.Event, 4)}

	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "", "go", &doc, fold.State{}, &notice, map[string]bool{}, &uiMax, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a cmd:/max button was not handled")
	}
	if uiMax != "chat" {
		t.Errorf("pressing cmd:/max chat set ui.max to %q, want chat; the pane the user clicked would not maximize", uiMax)
	}
	if len(drv.submitted) != 0 {
		t.Errorf("pressing cmd:/max chat submitted %v to the agent; a maximize is host view state, not a prompt", drv.submitted)
	}
}

// TestPressCmdMaxBareRestores pins restore: pressing cmd:/max with a pane already
// maximized clears the cursor, so the grid returns. A restore that left the
// cursor set would trap the user in the maximized pane.
func TestPressCmdMaxBareRestores(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"go","type":"button","text":"restore","on_press":"cmd:/max"}
	]}}`)
	notice := ""
	uiMax := "chat"
	drv := &testDriver{evCh: make(chan fold.Event, 4)}

	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "", "go", &doc, fold.State{}, &notice, map[string]bool{}, &uiMax, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a cmd:/max restore button was not handled")
	}
	if uiMax != "" {
		t.Errorf("pressing bare cmd:/max left ui.max = %q, want \"\" (restored); the user is trapped in the maximized pane", uiMax)
	}
}
