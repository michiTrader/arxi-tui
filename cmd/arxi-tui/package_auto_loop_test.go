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

// exitAfter waits ms (so what a package runs by itself has time to land) and then leaves
// through the panic gesture.
func exitAfter(ms int) []scheduledEvent {
	return []scheduledEvent{
		{time.Duration(ms) * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
}

// These drive the real loop (real fold, real renderer, real packageAuto) with only the
// terminal and the model faked.

func TestLoopServiceListsWhatPackagesOffer(t *testing.T) {
	script := append(enterLine("/service"), exitKeys()...)
	out, sent := runPackageFolders(t, nil, servicePkg(false, "sleep 30"), []string{"sv"}, script)
	if len(sent) != 0 {
		t.Fatalf("the model was sent %q; /service is the host's", sent)
	}
	if s := screen(out); !strings.Contains(s, "sv/watch · stopped") {
		t.Errorf("the list is not on screen; last frame:\n%s", lastFrame(out))
	}
}

func TestLoopServiceStartedByTheUserRunsAndItsLineAppears(t *testing.T) {
	script := append(enterLine("/service start watch"), exitAfter(400)...)
	out, sent := runPackageFolders(t, nil, servicePkg(false, "printf 'hello from it\\n'; sleep 30"), []string{"sv"}, script)
	if len(sent) != 0 {
		t.Fatalf("the model was sent %q", sent)
	}
	if s := screen(out); !strings.Contains(s, "sv/watch: hello from it") {
		t.Errorf("the service's line did not reach the screen; last frame:\n%s", lastFrame(out))
	}
}

// A behaviour command that runs "/service start" is not the user typing it.
func TestLoopServiceStartFromABehaviourCommandIsRefused(t *testing.T) {
	beh := `{"commands":[{"name":"go","description":"d","run":["cmd:/service start watch"]}]}`
	script := append(enterLine("/go"), exitAfter(300)...)
	out, _ := runPackageWithBehaviour(t, beh, nil, servicePkg(false, "printf 'started\\n'; sleep 30"), []string{"sv"}, script)
	s := screen(out)
	if !strings.Contains(s, "only works when you type it yourself") {
		t.Errorf("the refusal is not on screen; last frame:\n%s", lastFrame(out))
	}
	if strings.Contains(s, "sv/watch: started") {
		t.Error("a service started although the line was not typed by the user")
	}
}

func TestLoopAPanelShowsOnTheBar(t *testing.T) {
	script := exitAfter(500)
	out, _ := runPackageFolders(t, nil, panelPkg("printf 'tick 7'"), []string{"pn"}, script)
	if s := screen(out); !strings.Contains(s, "Clock: tick 7") {
		t.Errorf("the panel is not on the bar; last frame:\n%s", lastFrame(out))
	}
}

// Changing a setting reaches the packages' events through the loop's own settingChanged.
func TestLoopASettingChangeFiresAnEvent(t *testing.T) {
	files := map[string]string{
		"ev/package.json": execPkg("ev", "", `"events":[{"id":"e","on":"style","op":{"kind":"exec","argv":["./e.sh"]}}]`),
		"ev/e.sh":         "#!/bin/sh\nprintf 'style changed'\n",
	}
	script := append(enterLine("/style band"), exitAfter(500)...)
	out, _ := runPackageFolders(t, nil, files, []string{"ev"}, script)
	if s := screen(out); !strings.Contains(s, "ev/e: style changed") && !strings.Contains(s, "style changed") {
		t.Errorf("the event's note is not on screen; last frame:\n%s", lastFrame(out))
	}
}

// The end of a turn reaches a package's panel (refresh: turn.done) and its events through
// the loop. The model is faked: the turn's end is the event the core would send.
func TestLoopTheEndOfATurnRefreshesAPanelAndFailureFiresAnEvent(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv(configDirEnv, cfg)
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	counter := filepath.Join(t.TempDir(), "n")
	files := map[string]string{
		"pn/package.json": execPkg("pn", "", `"panels":[{"id":"p","title":"Clock","refresh":"turn.done","op":{"kind":"exec","argv":["./p.sh"]}}],`+
			`"events":[{"id":"f","on":"turn.failed","op":{"kind":"exec","argv":["./f.sh"]}}]`),
		"pn/p.sh": "#!/bin/sh\necho x >> " + counter + "\nprintf 'run %s' \"$(wc -l < " + counter + " | tr -d ' ')\"\n",
		"pn/f.sh": "#!/bin/sh\nprintf 'the turn failed note'\n",
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
	script := []scheduledEvent{
		{600 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}
	tty := newFakeTTY(100, 30, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	go func() {
		time.Sleep(200 * time.Millisecond) // after the first run at load
		drv.evCh <- fold.Event{Type: "agent.turn_done", Seq: 5000}
		time.Sleep(100 * time.Millisecond)
		drv.evCh <- fold.Event{Type: "agent.failed", Seq: 6000, Payload: map[string]any{"error": "boom"}}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	s := screen(tty.output())
	if !strings.Contains(s, "Clock: run 2") {
		t.Errorf("the panel was not refreshed when the turn ended; last frame:\n%s", lastFrame(tty.output()))
	}
	if !strings.Contains(s, "the turn failed note") {
		t.Errorf("the turn.failed event did not show its note; last frame:\n%s", lastFrame(tty.output()))
	}
}

// shellTestDriver is a driver that owns a registry of background shells, as the real one
// does through its chat session.
type shellTestDriver struct {
	testDriver
	reg *shellRegistry
}

func (d *shellTestDriver) Shells() *shellRegistry { return d.reg }

// A shell started by hand is counted on the real status line.
func TestLoopARunningShellIsCountedOnTheBar(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(sessionsEnv, "")
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	drv := &shellTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, reg: newShellRegistry(nil)}
	defer drv.reg.stopAll()
	if _, err := drv.reg.start("sleep 30", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, exitAfter(400))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	if s := screen(tty.output()); !strings.Contains(s, "1 shell") {
		t.Errorf("the running shell is not counted on the bar; last frame:\n%s", lastFrame(tty.output()))
	}
}
