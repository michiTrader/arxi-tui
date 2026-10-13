package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// menuAfter reports whether a frame that shows the whole, unfiltered command
// menu was drawn after the last frame that showed the menu filtered down to a
// single row. That ordering is what tells a menu that reopened from one that
// merely stayed.
func menuReopenedAfterFilter(out string) bool {
	all := fmt.Sprintf("Commands %d", len(fold.Commands))
	lastOne, lastAll := -1, -1
	for i, f := range strings.Split(out, frameBegin) {
		p := ansi.Strip(f)
		if strings.Contains(p, "Commands 1 ") || strings.Contains(p, "Commands 1\t") || strings.HasSuffix(strings.TrimSpace(p), "Commands 1") {
			lastOne = i
		}
		if strings.Contains(p, all) {
			lastAll = i
		}
	}
	return lastOne >= 0 && lastAll > lastOne
}

func TestLoopHelpOpensTheCommandMenuWithoutTheModel(t *testing.T) {
	out, got := runLoopDoc(t, factorySobria, append(enterLine("/help"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("/help reached the model: %q", got)
	}
	if !menuReopenedAfterFilter(out) {
		t.Errorf("typing /help and Enter did not open the full command menu")
	}
	if strings.Contains(ansi.Strip(out), "Unknown command") {
		t.Errorf("/help was called unknown")
	}
}

func TestLoopHelpPickedFromTheMenuBehavesLikeTypingIt(t *testing.T) {
	// "/help" leaves help as the only row (a shorter filter such as "/he" also matches shell); Enter picks it.
	out, got := runLoopDoc(t, factorySobria, append(enterLine("/help"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("picking help reached the model: %q", got)
	}
	if !menuReopenedAfterFilter(out) {
		t.Errorf("picking help from the menu did not open the full command menu")
	}
}

func TestLoopHelpIsNotInTheTranscript(t *testing.T) {
	out, _ := runLoopDoc(t, factorySobria, append(enterLine("/help"), exitKeys()...))
	for _, f := range strings.Split(out, frameBegin) {
		if frameHasTranscriptLine(f, "┃ help") || frameHasTranscriptLine(f, "┃ /help") {
			// The input bar draws "┃ /help" while typing; a transcript line
			// is one drawn when the menu is not.
			if !strings.Contains(ansi.Strip(f), "Commands ") {
				t.Errorf("/help became a transcript line:\n%s", ansi.Strip(f))
			}
		}
	}
}

func TestApplyFocus(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"row","children":[{"type":"text","text":"a","id":"here"},{"type":"text","text":"b","id":"there"}]}}`)
	cases := []struct {
		name, id, cur, want string
		notice              bool
	}{
		{"known node moves focus", "there", "here", "there", false},
		{"unknown node keeps focus and reports", "nowhere", "here", "here", true},
		{"empty id keeps focus and reports", "", "here", "here", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, notice := applyFocus(doc, c.id, c.cur)
			if got != c.want {
				t.Errorf("focus = %q, want %q", got, c.want)
			}
			if (notice != "") != c.notice {
				t.Errorf("notice = %q, want notice=%v", notice, c.notice)
			}
			if c.notice && c.id != "" && !strings.Contains(notice, c.id) {
				t.Errorf("notice %q does not name the missing id %q", notice, c.id)
			}
		})
	}
}

// The press path and the typed path are one operation: same result, same words.
func TestFocusActionAndCommandAreTheSameOperation(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"x","id":"here"}}`)
	notice := ""
	viaPress := dispatchPress("focus:nowhere", "here", "", fold.State{}, &doc, &notice, map[string]bool{}, nil, nil, nil, nil, nil, nil, &testDriver{})
	_, viaHelper := applyFocus(doc, "nowhere", "here")
	if viaPress != "here" || notice != viaHelper {
		t.Errorf("press gave focus %q notice %q; applyFocus gave notice %q", viaPress, notice, viaHelper)
	}
}

func TestLoopFocusUnknownNodeReportsAndSendsNothing(t *testing.T) {
	out, got := runLoopDoc(t, factorySobria, append(enterLine("/focus nowhere"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("/focus reached the model: %q", got)
	}
	p := ansi.Strip(out)
	if !strings.Contains(p, `no node with id "nowhere"`) {
		t.Errorf("no notice for an unknown node; output:\n%s", p)
	}
	if strings.Contains(p, "Unknown command") {
		t.Errorf("/focus was called unknown")
	}
}

func TestLoopFocusKnownNodeIsSilentAndSendsNothing(t *testing.T) {
	out, got := runLoopDoc(t, factorySobria, append(enterLine("/focus chat"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("/focus reached the model: %q", got)
	}
	p := ansi.Strip(out)
	if strings.Contains(p, "no node with id") || strings.Contains(p, "Unknown command") {
		t.Errorf("focusing a node that exists produced a complaint:\n%s", p)
	}
}

func TestLoopBareFocusExplainsTheUsage(t *testing.T) {
	out, got := runLoopDoc(t, factorySobria, append(enterLine("/focus"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("a bare /focus reached the model: %q", got)
	}
	if !strings.Contains(ansi.Strip(out), "/focus needs one node id") {
		t.Errorf("a bare /focus gave no usage; output:\n%s", ansi.Strip(out))
	}
}

func TestLoopFocusPickedFromTheMenuDoesNotSendTheWord(t *testing.T) {
	// "/foc" leaves focus as the only row. Enter picks it: it needs an id, so it
	// explains instead of sending "focus".
	_, got := runLoopDoc(t, factorySobria, append(enterLine("/foc"), exitKeys()...))
	if len(got) != 0 {
		t.Errorf("picking focus reached the model: %q", got)
	}
}
