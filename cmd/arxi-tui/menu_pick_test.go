package main

import (
	"context"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// TestMenuPickNeverSendsACommandWordToTheModel picks every row of the "/" menu
// with the keyboard. A row is a command, not a question: whatever it does, the
// word on it ("help", "focus", ...) must never go to the model as a prompt.
func TestMenuPickNeverSendsACommandWordToTheModel(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	for i, c := range fold.Commands {
		c := c
		script := []scheduledEvent{{0, keyEvent('/')}}
		for j := 0; j < i; j++ {
			script = append(script, scheduledEvent{3 * time.Millisecond, arrowEvent(term.KeyDown)})
		}
		script = append(script, scheduledEvent{10 * time.Millisecond, enterEvent()})
		// Close whatever the pick opened, then leave.
		script = append(script, scheduledEvent{30 * time.Millisecond, arrowEvent(term.KeyEscape)})
		script = append(script, exitKeys()...)
		t.Run(c.Name, func(t *testing.T) {
			_, got := runLoopDoc(t, factorySobria, script)
			for _, s := range got {
				if s == c.Name || s == "/"+c.Name {
					t.Errorf("picking /%s from the menu sent %q to the model.\nConsequence: a command word reaches the model as if the user had asked it something.\nRemedy: route the pick through the command's own handler.", c.Name, s)
				}
			}
		})
	}
}

// TestTypeKeyNeverSubmitsASlashLine is the last door: even if a slash line
// reaches the plain submit path, it is not sent, and not sent minus its "/".
func TestTypeKeyNeverSubmitsASlashLine(t *testing.T) {
	for _, line := range []string{"/help", "/nonsense", "/ui hello", "/", "/send hi"} {
		drv := &testDriver{evCh: make(chan fold.Event, 4)}
		out, caret := typeKey(line, len([]rune(line)), term.Key{Type: term.KeyEnter}, context.Background(), drv)
		if len(drv.submitted) != 0 {
			t.Errorf("typeKey(%q) submitted %q", line, drv.submitted)
		}
		if out != line || caret != len([]rune(line)) {
			t.Errorf("typeKey(%q) changed the buffer to %q (caret %d); the text must stay", line, out, caret)
		}
	}
	drv := &testDriver{evCh: make(chan fold.Event, 4)}
	if out, _ := typeKey("hello", 5, term.Key{Type: term.KeyEnter}, context.Background(), drv); out != "" || len(drv.submitted) != 1 || drv.submitted[0] != "hello" {
		t.Errorf("an ordinary line must still be sent: out=%q submitted=%q", out, drv.submitted)
	}
}
