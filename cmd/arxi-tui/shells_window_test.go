package main

import (
	"context"
	"strings"
	"testing"

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
	return stripANSI(tty.output()), td.submitted
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
