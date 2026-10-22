package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// shellDriver is a driver that owns pretend background shells, so the status area has a chip.
type shellDriver struct {
	*testDriver
	reg *shellRegistry
}

func (d *shellDriver) Shells() *shellRegistry { return d.reg }

func chipLoop(t *testing.T, shells int, script []scheduledEvent) (string, []string) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	td := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	drv := &shellDriver{testDriver: td, reg: newSimShells(shells, func(string) {})}
	tty := newFakeTTY(100, 30, script)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), td.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return stripANSI(tty.output()), td.submitted
}

func arrows(types ...term.KeyType) []scheduledEvent {
	var out []scheduledEvent
	for _, k := range types {
		out = append(out, scheduledEvent{20 * time.Millisecond, arrowEvent(k)})
	}
	return out
}

// With a shell running, down then Enter presses the chip: /shell lists the shells. Nothing
// is sent to the model.
func TestLoopDownThenEnterPressesTheShellsChip(t *testing.T) {
	script := append(arrows(term.KeyDown), scheduledEvent{20 * time.Millisecond, enterEvent()})
	out, sub := chipLoop(t, 1, append(script, exitKeys()...))
	if len(sub) != 0 {
		t.Errorf("the model was sent %q. Consequence: pressing the chip talks to the model. Remedy: the chip's on_press is the command /shell.", sub)
	}
	if !strings.Contains(out, "simulated command 1") {
		t.Errorf("Enter on the chip did not list the shells. Consequence: down reaches nothing, or the chip does nothing. Remedy: down must run focus:shells_chip and Enter must press it.\n%s", out)
	}
}

// Up gives the focus back: the next line is typed on the input and sent, not swallowed by
// the chip.
func TestLoopUpFromTheChipReturnsToTheInput(t *testing.T) {
	script := arrows(term.KeyDown, term.KeyUp)
	script = append(script, typed("hi")...)
	script = append(script, scheduledEvent{20 * time.Millisecond, enterEvent()})
	_, sub := chipLoop(t, 1, append(script, exitKeys()...))
	if len(sub) != 1 || sub[0] != "hi" {
		t.Errorf("submitted %q, want [hi]. Consequence: after down and up the focus is still on the chip and Enter presses it instead of sending the line. Remedy: up must run do:focus.input.", sub)
	}
}

// With no shell running down is the history's, as before: Enter after it sends the line.
func TestLoopDownWithNoShellsLeavesTheKeyToTheInput(t *testing.T) {
	script := append(typed("hi"), arrows(term.KeyDown)...)
	script = append(script, scheduledEvent{20 * time.Millisecond, enterEvent()})
	_, sub := chipLoop(t, 0, append(script, exitKeys()...))
	if len(sub) != 1 || sub[0] != "hi" {
		t.Errorf("submitted %q, want [hi]. Consequence: down moved the focus to a chip that is not drawn. Remedy: the down binding needs shells.count > 0.", sub)
	}
}

func chipEnter() scheduledEvent { return scheduledEvent{20 * time.Millisecond, enterEvent()} }

// With the caret inside the line, down is the editor's, not the chip's.
func TestLoopDownWithTheCaretInsideTheLineDoesNotReachTheChip(t *testing.T) {
	script := append(typed("hi"), arrows(term.KeyLeft, term.KeyDown)...)
	script = append(script, chipEnter())
	_, sub := chipLoop(t, 1, append(script, exitKeys()...))
	if len(sub) != 1 || sub[0] != "hi" {
		t.Errorf("submitted %q, want [hi]. Consequence: down jumped to the chip while the user was editing, and Enter pressed it instead of sending the line. Remedy: keymapFacts must answer input.at_end from the caret.", sub)
	}
}

// A recalled line keeps down for the history walk, even with a shell running.
func TestLoopDownOnARecalledLineWalksTheHistoryNotTheChip(t *testing.T) {
	script := append(typed("a"), chipEnter())
	script = append(script, arrows(term.KeyUp, term.KeyDown)...)
	script = append(script, typed("b")...)
	script = append(script, chipEnter())
	_, sub := chipLoop(t, 1, append(script, exitKeys()...))
	if len(sub) != 2 || sub[0] != "a" || sub[1] != "b" {
		t.Errorf("submitted %q, want [a b]. Consequence: down left the history walk for the chip, and the next line was lost to it. Remedy: keymapFacts must answer history.browsing from the history.", sub)
	}
}

// On the phone arrangement the arrows scroll the chat; down must stay that.
func TestLoopDownOnThePhoneScrollsAndDoesNotReachTheChip(t *testing.T) {
	old := scrollArrows
	scrollArrows = true
	defer func() { scrollArrows = old }()
	script := append(arrows(term.KeyDown), typed("hi")...)
	script = append(script, chipEnter())
	_, sub := chipLoop(t, 1, append(script, exitKeys()...))
	if len(sub) != 1 || sub[0] != "hi" {
		t.Errorf("submitted %q, want [hi]. Consequence: a swipe on the phone moved the focus to the chip. Remedy: keymapFacts must answer arrows.scroll from scrollArrows.", sub)
	}
}
