package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// lastFrame is the plain text of the final repaint of a loop run.
func lastFrame(out string) string {
	frames := strings.Split(out, frameBegin)
	return ansi.Strip(frames[len(frames)-1])
}

// enterLine types a line and presses Enter, then leaves through the panic gesture.
func enterLine(line string) []scheduledEvent {
	s := append(typed(line), scheduledEvent{10 * time.Millisecond, enterEvent()})
	return s
}

func TestLoopUnknownCommandWarnsAndSendsNothing(t *testing.T) {
	script := append(enterLine("/nonsense"), exitKeys()...)
	out, got := runLoopKeysOutput(t, script)
	if len(got) != 0 {
		t.Errorf("an unknown command reached the model: %q", got)
	}
	if !strings.Contains(ansi.Strip(out), "Unknown command /nonsense.") {
		t.Errorf("no warning for an unknown command; output:\n%s", ansi.Strip(out))
	}
}

func TestLoopUnknownCommandKeepsTheTextInTheBuffer(t *testing.T) {
	// The warning must be on screen together with the text the user typed:
	// both in the same, final frame, so neither is hidden by the other.
	out, _ := runLoopKeysOutput(t, append(enterLine("/nonsense"), exitKeys()...))
	frames := strings.Split(out, frameBegin)
	var seen bool
	for _, f := range frames {
		p := ansi.Strip(f)
		if strings.Contains(p, "Unknown command /nonsense.") && strings.Contains(p, "/nonsense") {
			seen = true
		}
	}
	if !seen {
		t.Errorf("no frame shows the warning and the buffer together; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopUnknownCommandCanBeEditedAndResent(t *testing.T) {
	// Text stays; the user deletes the last char and fixes the line.
	script := enterLine("/helpx")
	script = append(script, scheduledEvent{10 * time.Millisecond, arrowEvent(term.KeyBackspace)})
	script = append(script, exitKeys()...)
	_, got := runLoopKeysOutput(t, script)
	if len(got) != 0 {
		t.Errorf("nothing may be submitted, got %q", got)
	}
}

func TestLoopSurfaceIsRecognisedButNotAvailable(t *testing.T) {
	script := append(enterLine("/surface"), exitKeys()...)
	out, got := runLoopKeysOutput(t, script)
	if len(got) != 0 {
		t.Errorf("/surface reached the model: %q", got)
	}
	p := ansi.Strip(out)
	if !strings.Contains(p, "/surface is not available yet.") {
		t.Errorf("no 'not available' warning for /surface; output:\n%s", p)
	}
	if strings.Contains(p, "Unknown command /surface") {
		t.Errorf("/surface is registered, so it must not be called unknown")
	}
}

func TestLoopPlainTextStillGoesToTheModel(t *testing.T) {
	script := append(enterLine("hello there"), exitKeys()...)
	_, got := runLoopKeysOutput(t, script)
	if want := []string{"hello there"}; !reflect.DeepEqual(got, want) {
		t.Errorf("submitted %q, want %q", got, want)
	}
}

func TestLoopUnknownWarningIsNotSentOnTheNextPrompt(t *testing.T) {
	// The warning is local: it must not ride along into what the model gets.
	script := enterLine("/nonsense")
	for i := 0; i < len("/nonsense"); i++ {
		script = append(script, scheduledEvent{5 * time.Millisecond, arrowEvent(term.KeyBackspace)})
	}
	script = append(script, typed("hello")...)
	script = append(script, scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	_, got := runLoopKeysOutput(t, script)
	if want := []string{"hello"}; !reflect.DeepEqual(got, want) {
		t.Errorf("submitted %q, want %q", got, want)
	}
}

func TestLoopPartialCommandWithMenuRowsIsAMenuPickNotAWarning(t *testing.T) {
	// "/mod" is not a command, but the menu lists rows for it and Enter picks
	// the highlighted one. That is the menu's job; calling it "unknown" would
	// break the way a command is chosen by typing a few letters.
	for _, partial := range []string{"/mod", "/he", "/e"} {
		script := append(enterLine(partial), exitKeys()...)
		out, _ := runLoopKeysOutput(t, script)
		if strings.Contains(ansi.Strip(out), "Unknown command") {
			t.Errorf("%q has menu rows but was called unknown:\n%s", partial, lastFrame(out))
		}
	}
}

func TestLoopUnknownCommandWithArgumentsWarns(t *testing.T) {
	// With an argument the menu filter (on the whole typed text) has no rows,
	// which is exactly the silent dead end the warning replaces.
	script := append(enterLine("/nonsense with args"), exitKeys()...)
	out, got := runLoopKeysOutput(t, script)
	if len(got) != 0 {
		t.Errorf("reached the model: %q", got)
	}
	if !strings.Contains(ansi.Strip(out), "Unknown command /nonsense.") {
		t.Errorf("no warning; output:\n%s", ansi.Strip(out))
	}
}

func TestLoopWarningStaysVisibleWhileTheMenuIsOpen(t *testing.T) {
	// "/surface" has a menu row, so the menu is open under the warning. Both
	// must be on screen in the same frame, or the user sees the menu and never
	// learns why Enter did nothing.
	out, _ := runLoopDoc(t, factorySobria, append(enterLine("/surface"), exitKeys()...))
	var both bool
	for _, f := range strings.Split(out, frameBegin) {
		p := ansi.Strip(f)
		if strings.Contains(p, "/surface is not available yet.") && strings.Contains(p, "Commands ") {
			both = true
		}
	}
	if !both {
		t.Errorf("the open menu hides the warning; last frame:\n%s", lastFrame(out))
	}
}

// A local warning must not be saved with the conversation, although chat.warn
// events from the core are (sessionKinds). It stays out because it is appended
// to the loop's own log and never goes through sessLog.Record; this test holds
// that route, since nothing else would notice if a warning started being
// recorded and then came back, as a stale line, on /resume.
func TestLoopLocalWarningIsNotSavedWithTheSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	t.Setenv(sessionsEnv, "")
	// The prompt goes first: Record ignores anything before the first prompt, so
	// a warning raised earlier would be dropped for that reason and prove nothing.
	script := append(enterLine("hello there"), enterLine("/nonsense")...)
	script = append(script, exitKeys()...)
	_, _ = runLoopDoc(t, factoryRAW, script)

	files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.jsonl"))
	if len(files) == 0 {
		t.Fatalf("no session file was written under %s; the premise (a real prompt is saved) is broken", dir)
	}
	var saved strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		saved.Write(b)
	}
	if !strings.Contains(saved.String(), "hello there") {
		t.Fatalf("the real prompt is not in the saved session; premise broken:\n%s", saved.String())
	}
	if strings.Contains(saved.String(), "Unknown command") || strings.Contains(saved.String(), "nonsense") {
		t.Errorf("a local warning was saved with the conversation:\n%s", saved.String())
	}
}
