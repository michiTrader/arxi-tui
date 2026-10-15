//go:build !windows

package chattools

import (
	"strings"
	"testing"
)

// A browser opened by a command (xdg-open, start) stays alive holding the output pipe.
// The command itself worked; reporting it as "could not be started" made the model run
// it again, and the page opened twice.
func TestACommandThatLeavesAProgramRunningIsNotAFailure(t *testing.T) {
	tb := runBox(t)
	res, err := tb.Run(ToolRun, runArg("(sleep 6 &) ; echo opened", 30))
	if err != nil {
		t.Fatalf("a command that finished and left a program running must not be an error: %v", err)
	}
	if res.Failed {
		t.Fatalf("it worked, it must not be reported as failed: %+v", res)
	}
	if !strings.Contains(res.Text, "opened") || !strings.Contains(res.Text, "do not run it again") {
		t.Errorf("the model must be told it ran and must not repeat it: %q", res.Text)
	}
	if !strings.Contains(res.Summary, "left a program running") {
		t.Errorf("the user must see why it took a moment: %q", res.Summary)
	}
}

// A plain command is unchanged: no note about leftovers.
func TestAPlainCommandHasNoLeftoverNote(t *testing.T) {
	tb := runBox(t)
	res, err := tb.Run(ToolRun, runArg("echo hi", 0))
	if err != nil || strings.Contains(res.Text, "left a program") || strings.Contains(res.Summary, "left a program") {
		t.Errorf("plain command got a leftover note: %+v %v", res, err)
	}
}
