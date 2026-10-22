package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// A window listing the shells, written as a scene and nothing else: a full overlay opened by
// ui.max, one pressable row per shell. This is acceptance scenario 3 of
// docs/DESIGN-OPEN-TUI.md, with no Go that draws it.
const shellsWindowScene = `{"root":{"type":"stack","children":[
 {"id":"chat","type":"markdown","bind":"chat.history","grow":1},
 {"id":"prompt","type":"input","bind":"user.input","prefix":"> "},
 {"id":"shells_window","type":"overlay","anchor":"full","when":"ui.max.is.shells","children":[
   {"type":"text","text":"SHELLS - Enter shows one, /max closes","style":{"style":"header"}},
   {"id":"shell_rows","type":"list","bind":"host.shell.rows","row_template":{"type":"row","children":[
     {"id":"shell_pick","type":"text","bind":"row.command","on_press":"cmd:/shell show {row.id}"}]}},
   {"id":"shell_close","type":"button","text":"[close]","on_press":"cmd:/max"}]}
]}}`

func windowLoop(t *testing.T, script []scheduledEvent) (string, []string) {
	t.Helper()
	raw, sub := windowLoopRaw(t, script)
	return stripANSI(raw), sub
}

// windowLoopRaw is windowLoop with the bytes as written, so a test can cut them into frames.
func windowLoopRaw(t *testing.T, script []scheduledEvent) (string, []string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(shellsWindowScene))
	if err != nil {
		t.Fatal(err)
	}
	td := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	drv := &shellDriver{testDriver: td, reg: newSimShells(2, func(string) {})}
	tty := newFakeTTY(80, 20, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), td.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), td.submitted
}

// The window opens with /max, lists every shell, and a row pressed with Tab and Enter shows
// that shell's output. Closing the window brings the conversation back with that output in it.
func TestLoopAShellsWindowOfASceneListsShellsAndShowsTheChosenOne(t *testing.T) {
	script := append(typed("/max shells"), chipEnter())
	script = append(script, arrows(term.KeyTab, term.KeyTab)...) // the second shell
	script = append(script, chipEnter())
	script = append(script, arrows(term.KeyTab)...) // the close button, after the rows
	script = append(script, chipEnter())
	out, sub := windowLoop(t, append(script, exitKeys()...))
	if len(sub) != 0 {
		t.Errorf("the model was sent %q. Consequence: choosing a shell talks to the model. Remedy: the row's on_press is a command.", sub)
	}
	if !strings.Contains(out, "SHELLS - Enter shows one") || !strings.Contains(out, "simulated command 1") || !strings.Contains(out, "simulated command 2") {
		t.Errorf("the window did not list both shells. Consequence: a scene cannot make a window. Remedy: a full overlay must replace the screen.\n%s", out)
	}
	if !strings.Contains(out, "simulated output of shell 2") {
		t.Errorf("the second row did not show its shell output. Consequence: Tab and Enter reach no row of the window. Remedy: the press path must resolve rows of an overlay.\n%s", out)
	}
	if strings.Contains(out, "simulated output of shell 1") {
		t.Errorf("shell 1's output was shown though the second row was chosen. Consequence: a row shows another shell. Remedy: resolve {row.id} for the focused row.")
	}
}

func click(col, row int) []scheduledEvent {
	return []scheduledEvent{
		{20 * time.Millisecond, mouseEvent(term.MousePress, col, row)},
		{20 * time.Millisecond, mouseEvent(term.MouseRelease, col, row)},
	}
}

// The same window, worked with the pointer alone: a click on the second row shows that
// shell, a click on [close] closes the window. A press with the pointer goes through the same
// door as Enter on the focused node.
// Consequence: a scene made of buttons and lists works with the keyboard and is dead to a
// mouse, so every window needs a Go handler to be clickable.
// Remedy: spans carry the key of the pressable node that drew them (ui.Span.Hit) and the loop
// presses it on a left click.
func TestLoopClickingTheRowsOfAWindowPressesThem(t *testing.T) {
	script := append(typed("/max shells"), chipEnter())
	script = append(script, click(3, 2)...) // the second shell
	script = append(script, click(3, 3)...) // [close]
	raw, sub := windowLoopRaw(t, append(script, exitKeys()...))
	out := stripANSI(raw)
	if len(sub) != 0 {
		t.Errorf("the model was sent %q. Consequence: a click talks to the model. Remedy: a click presses on_press, a command.", sub)
	}
	// The click on [close] must have closed the window: the frame before the exit keys
	// shows the conversation, not the window.
	frames := strings.Split(raw, frameBegin)
	closed := false
	for _, f := range frames {
		if s := stripANSI(f); strings.Contains(s, "simulated output of shell 2") && !strings.Contains(s, "SHELLS - Enter shows one") {
			closed = true
		}
	}
	if !closed {
		t.Errorf("no frame shows the conversation with the output after [close] was clicked. Consequence: a click on a close button leaves the window open. Remedy: the click must press the button under it.")
	}
	if !strings.Contains(out, "simulated output of shell 2") {
		t.Errorf("a click on the second row did not show its shell. Consequence: the mouse cannot press a row. Remedy: resolve the span under the pointer to its row key.\n%s", out)
	}
	if strings.Contains(out, "simulated output of shell 1") {
		t.Errorf("shell 1's output was shown though the second row was clicked. Consequence: a click presses the wrong row. Remedy: key a row by its index.")
	}
}

// A click on nothing that presses leaves everything as it was.
func TestLoopClickingOnTextPressesNothing(t *testing.T) {
	script := append(typed("/max shells"), chipEnter())
	script = append(script, click(3, 0)...)  // the title
	script = append(script, click(60, 1)...) // past the end of a row
	out, sub := windowLoop(t, append(script, exitKeys()...))
	if len(sub) != 0 || strings.Contains(out, "simulated output") {
		t.Errorf("a click on text did something (sent %q). Consequence: the screen reacts where nothing says it should. Remedy: only spans with a Hit press.", sub)
	}
}

// A click keeps the line being typed: pressing a node is not sending the draft.
// Consequence: clicking a button throws away what the user was writing, or sends it.
// Remedy: the click path leaves input alone (Enter on a focused node clears it; a click does not).
func TestLoopAClickKeepsTheDraftBeingTyped(t *testing.T) {
	script := append(typed("/max shells"), chipEnter())
	script = append(script, typed("draft")...) // written while the window is open, under it
	script = append(script, click(3, 1)...)    // shell 1 shows its output in the conversation
	script = append(script, click(3, 3)...)    // [close]
	raw, sub := windowLoopRaw(t, append(script, exitKeys()...))
	if len(sub) != 0 {
		t.Errorf("the model was sent %q", sub)
	}
	frames := strings.Split(raw, frameBegin)
	if last := stripANSI(frames[len(frames)-2]); !strings.Contains(last, "> draft") {
		t.Errorf("the line typed after the clicks is not on screen:\n%s", last)
	}
}
