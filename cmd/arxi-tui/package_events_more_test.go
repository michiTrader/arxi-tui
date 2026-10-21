//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// Each new event fires only its own packages' events (a note per event, named for it).
func TestEachNewEventFiresOnlyItsOwnPackageEvents(t *testing.T) {
	names := map[string]func(a *packageAuto){
		"turn.started":   (*packageAuto).turnStarted,
		"turn.cancelled": (*packageAuto).turnCancelled,
		"shell.ended":    (*packageAuto).shellEnded,
	}
	for on, fire := range names {
		r := newAutoRig(t, eventPkg(on, "", "printf 'it was "+on+"'"), []string{"ev"})
		fire(r.a)
		waitUntil(t, on+" note", func() bool { return strings.Contains(r.notes(), "it was "+on) })
		for other, fireOther := range names {
			if other == on {
				continue
			}
			before := len(r.messages())
			fireOther(r.a)
			time.Sleep(120 * time.Millisecond)
			if n := len(r.messages()); n != before {
				t.Errorf("%s fired an event waiting for %s", other, on)
			}
		}
		r.a.turnDone("")
		r.a.turnFailed()
		time.Sleep(120 * time.Millisecond)
		if strings.Count(r.notes(), "it was") != 1 {
			t.Errorf("an event for %s also ran on turn.done/turn.failed: %q", on, r.notes())
		}
	}
}

// The loop turns the events the chat really emits into the package events: a message to
// the model, a cancelled turn, and a shell that ended by itself.
func TestLoopTurnsChatEventsIntoPackageEvents(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	counter := filepath.Join(t.TempDir(), "shellend-runs")
	files := map[string]string{
		"pn/package.json": execPkg("pn", "", `"events":[`+
			`{"id":"a","on":"turn.started","op":{"kind":"exec","argv":["./a.sh"]}},`+
			`{"id":"b","on":"turn.cancelled","op":{"kind":"exec","argv":["./b.sh"]}},`+
			`{"id":"c","on":"shell.ended","op":{"kind":"exec","argv":["./c.sh"]}}]`),
		"pn/a.sh": "#!/bin/sh\nprintf 'NOTE-STARTED'\n",
		"pn/b.sh": "#!/bin/sh\nprintf 'NOTE-CANCELLED'\n",
		"pn/c.sh": "#!/bin/sh\necho x >> " + counter + "\nprintf 'NOTE-SHELLEND'\n",
	}
	for name, body := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		write(t, filepath.Join(cfg, packagesDirName, filepath.FromSlash(name)), body, mode)
	}
	cwd, _ := os.Getwd()
	p, ok := loadAllPackages(cwd, behaviour{}).find("pn")
	if !ok {
		t.Fatal("did not load")
	}
	if err := grantPackage(p, cwd); err != nil {
		t.Fatal(err)
	}
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(110, 34, exitAfter(1500))
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	go func() {
		time.Sleep(200 * time.Millisecond)
		drv.evCh <- fold.Event{Type: "run.prompt", Seq: 5000, Payload: map[string]any{"text": "hola"}}
		time.Sleep(100 * time.Millisecond)
		drv.evCh <- fold.Event{Type: "chat.cancelled", Seq: 5001}
		time.Sleep(100 * time.Millisecond)
		drv.evCh <- fold.Event{Type: "chat.warn", Seq: 5002, Payload: map[string]any{shellEndedKey: true, "text": "shell 1 ended: x"}}
		// A plain warning (a retry line, say) is not a shell ending. It comes after the
		// shell's own event has finished running, since a run still going is never
		// started again and would hide a wrong second firing.
		time.Sleep(500 * time.Millisecond)
		drv.evCh <- fold.Event{Type: "chat.warn", Seq: 5003, Payload: map[string]any{"text": "just a warning"}}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	// The plain warning sent after the shell's own must not have fired shell.ended again.
	if b, _ := os.ReadFile(counter); strings.Count(string(b), "x") != 1 {
		t.Errorf("shell.ended ran %d times, want exactly 1 (only the marked warning is a shell ending)", strings.Count(string(b), "x"))
	}
	s := screen(tty.output())
	for _, want := range []string{"NOTE-STARTED", "NOTE-CANCELLED", "NOTE-SHELLEND"} {
		if !strings.Contains(s, want) {
			t.Errorf("%s is not on screen; last frame:\n%s", want, lastFrame(tty.output()))
		}
	}
}

// The chat marks a shell that ended by itself, so the loop can tell it from any warning.
func TestAShellThatEndsByItselfIsMarkedForTheLoop(t *testing.T) {
	out := make(chan fold.Event, 8)
	c := newChatSession(retryChat{release: make(chan struct{})}, out)
	c.shellEnded(shellInfo{ID: 1, Command: "true", Code: 0})
	select {
	case ev := <-out:
		if ended, _ := ev.Payload[shellEndedKey].(bool); !ended || ev.Type != "chat.warn" {
			t.Fatalf("event = %+v, want a chat.warn marked as a shell ending", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no warning line")
	}
}
