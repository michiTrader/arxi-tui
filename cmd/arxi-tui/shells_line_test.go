//go:build !windows

package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The row above the info bar names the shells that RUN, by the numbers /shell and the
// model use; an ended one drops out and the numbers are not reused.
func TestShellNamesListsOnlyTheRunningOnes(t *testing.T) {
	infos := []shellInfo{
		{ID: 1, State: shellRunning},
		{ID: 2, State: shellExited},
		{ID: 3, State: shellRunning},
		{ID: 4, State: shellStopped},
	}
	if got := shellNames(infos); got != "shell 1 · shell 3" {
		t.Errorf("got %q", got)
	}
	if got := shellNames(infos[1:2]); got != "" {
		t.Errorf("an ended shell must not be listed: %q", got)
	}
	if got := shellNames(nil); got != "" {
		t.Errorf("no shells, no row: %q", got)
	}
}

// A driver without a registry (a plain test driver) lists nothing and does not panic.
func TestRunningShellListNeedsARegistry(t *testing.T) {
	if got := runningShellList(&testDriver{}); got != "" {
		t.Errorf("got %q", got)
	}
}

// The real loop: two shells running are named in a row of their own right above the
// info bar; after one is stopped, the next frame names only the other.
func TestTheLoopDrawsTheRunningShellsAboveTheInfoBar(t *testing.T) {
	r := shellChat(t, policyAllow)
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, err := r.c.shells.start("sleep 30", dir); err != nil {
			t.Fatal(err)
		}
	}
	drv := &serveDriver{chat: r.c}
	doc := builtinDoc(t)

	run := func(script []scheduledEvent) string {
		tty := newFakeTTY(100, 24, script)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := loop(ctx, tty, doc, theme.SOBRIA(), make(chan fold.Event, 8), drv, ""); err != nil {
			t.Fatalf("loop: %v", err)
		}
		return tty.output()
	}

	out := run(append(typed("x"), exitKeys()...))
	// The frame is drawn row by row with absolute positioning, so its text has no line
	// breaks; folded, it reads in screen order: the shells, then the info bar, with
	// nothing in between.
	if got := screen(out); !strings.Contains(got, "shell 1 · shell 2 ask · no model") {
		t.Fatalf("the two running shells must be named right above the info bar:\n%s", got)
	}

	r.c.shells.stop(1)
	out = run(append(typed("x"), exitKeys()...))
	if got := screen(out); strings.Contains(got, "shell 1") || !strings.Contains(got, "shell 2 ask · no model") {
		t.Errorf("after stopping shell 1 only shell 2 must be named:\n%s", got)
	}
}

// While the slash menu is open the bottom of the screen belongs to the menu: the shell row
// is not drawn behind it, like the rest of the status bar.
func TestTheShellRowStepsAsideForTheSlashMenu(t *testing.T) {
	r := shellChat(t, policyAllow)
	if _, err := r.c.shells.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	drv := &serveDriver{chat: r.c}
	tty := newFakeTTY(100, 24, append(typed("/"), scheduledEvent{300 * time.Millisecond, ctrlCharEvent('c')}, scheduledEvent{50 * time.Millisecond, ctrlCharEvent('c')}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, builtinDoc(t), theme.SOBRIA(), make(chan fold.Event, 8), drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	// The frame drawn while the menu was open: the one that holds the menu's own rule/list.
	var open string
	for _, f := range strings.Split(tty.output(), frameBegin) {
		if g := screen("\x00" + frameBegin + f); strings.Contains(g, "All") && strings.Contains(g, "General") {
			open = g
		}
	}
	if open == "" {
		t.Fatalf("no frame with the slash menu open was drawn")
	}
	if strings.Contains(open, "shell 1") {
		t.Errorf("the shell row showed over the slash menu:\n%s", open)
	}
}

// "Configurable like any other part of the screen" is a claim of the README: /ui hide shells
// must really take the row away while the shells keep running.
func TestTheUserCanHideTheShellRow(t *testing.T) {
	r := shellChat(t, policyAllow)
	if _, err := r.c.shells.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	drv := &serveDriver{chat: r.c}
	script := append(typed("/ui hide shells"), scheduledEvent{10 * time.Millisecond, enterEvent()})
	script = append(script, exitKeys()...)
	tty := newFakeTTY(100, 24, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, builtinDoc(t), theme.SOBRIA(), make(chan fold.Event, 8), drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	out := tty.output()
	if !strings.Contains(screen(out), "ask · no model") {
		t.Fatalf("the info bar must stay:\n%s", screen(out))
	}
	if strings.Contains(screen(out), "shell 1") {
		t.Errorf("/ui hide shells left the row on screen:\n%s", screen(out))
	}
	if r.c.shells.running() != 1 {
		t.Errorf("hiding the row must not stop the shell: %d running", r.c.shells.running())
	}
}

// On a terminal narrower than the names, the shell row wraps like the info bars: it used to
// be cut at the last column (the README said so), and the status.narrow policy now covers it.
func TestTheShellRowWrapsOnANarrowTerminalInsteadOfBeingCut(t *testing.T) {
	r := shellChat(t, policyAllow)
	for i := 0; i < 3; i++ {
		if _, err := r.c.shells.start("sleep 30", t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}
	const width = 14 // "shell 1 · shell 2 · shell 3" is 27 wide
	tty := newFakeTTY(width, 24, append(typed("x"), exitKeys()...))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, builtinDoc(t), theme.SOBRIA(), make(chan fold.Event, 8), &serveDriver{chat: r.c}, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	out := tty.output()
	f := frameWith(out, "shell 3")
	if f == "" {
		t.Fatalf("shell 3 was never drawn:\n%s", lastFrame(out))
	}
	for _, name := range []string{"shell 1", "shell 2", "shell 3"} {
		if !strings.Contains(f, name) {
			t.Errorf("%q missing from the shell row: %s", name, f)
		}
	}
	// The fake terminal does not cut, the real one does: so what matters is that no row
	// the loop paints is wider than the terminal.
	cup := regexp.MustCompile("\x1b\\[\\d+;1H\x1b\\[K")
	tty.mu.Lock()
	defer tty.mu.Unlock()
	for _, fr := range tty.frames {
		for _, piece := range cup.Split(fr, -1)[1:] {
			row := strings.TrimRight(stripANSI(piece), " \r\n")
			if w := ansi.StringWidth(row); w > width {
				t.Errorf("a %d-column row on a %d-column terminal (the terminal would cut it): %q", w, width, row)
			}
		}
	}
}
