package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// A screen's file is read when the screen opens, not once at start.
// Consequence: a user who edits the file (or asks arxi to) sees nothing until the next
// launch, so the edit-and-look loop that "ask arxi to rebuild it" depends on is broken.
// Remedy: reread the file each time the screen opens (reread in main.go).

func TestLoopAnEditedHubScreenShowsWithoutARestart(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	core := &modelCore{def: "openai/gpt-4o", models: sampleModels().models}
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, hub: core}
	out := screenLoopEdit(t, builtinDoc(t), drv, drv.evCh, "/provider", "PROFILE HUB", func() {
		writeScreen(t, "hub", replacementScreen)
	})
	if !strings.Contains(out, "PROFILE HUB") {
		t.Errorf("/provider did not show the file written while running:\n%s", out)
	}
}

func TestLoopAnEditedFlowScreenShowsWithoutARestart(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	drv := &testDriver{evCh: make(chan fold.Event, 64)}
	out := screenLoopEdit(t, builtinDoc(t), drv, drv.evCh, "/flow", "PROFILE FLOW", func() {
		writeScreen(t, "flow", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE FLOW", 1))
	})
	if !strings.Contains(out, "PROFILE FLOW") {
		t.Errorf("/flow did not show the file written while running:\n%s", out)
	}
}

func TestLoopAnEditedTeamScreenShowsWithoutARestart(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	drv := teamDriver(t)
	out := screenLoopEdit(t, builtinDoc(t), drv, drv.evCh, "/team", "PROFILE TEAM", func() {
		writeScreen(t, "team", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE TEAM", 1))
	})
	if !strings.Contains(out, "PROFILE TEAM") {
		t.Errorf("/team did not show the file written while running:\n%s", out)
	}
}

func TestLoopAnEditedAutoScreenShowsWithoutARestart(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	fixedClock(t)
	core, root := newFakeAutoCore(t)
	t.Chdir(root)
	drv := &autoTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: autoHubCore{fakeAutoCore: core}}
	out := screenLoopEdit(t, builtinDoc(t), drv, drv.evCh, "/auto", "PROFILE AUTO", func() {
		writeScreen(t, "auto", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE AUTO", 1))
	})
	if !strings.Contains(out, "PROFILE AUTO") {
		t.Errorf("/auto did not show the file written while running:\n%s", out)
	}
}

func TestLoopAnEditedInstallerScreenShowsWithoutARestart(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	out := installerLoopEdit(t, "PROFILE INSTALLER", func() {
		writeScreen(t, "installer", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE INSTALLER", 1))
	})
	if !strings.Contains(out, "PROFILE INSTALLER") {
		t.Errorf("the installer did not show the file written while running:\n%s", out)
	}
}

// A file broken while the app runs is reported, and the built-in screen is drawn.
// Consequence: a half-written edit would blank the screen or kill the interface.
// Remedy: a file that does not load is refused with a notice naming it, as at start.
func TestLoopABrokenEditWhileRunningIsReportedAndKeepsTheBuiltInScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	drv := &testDriver{evCh: make(chan fold.Event, 64)}
	out := screenLoopEdit(t, builtinDoc(t), drv, drv.evCh, "/flow", "your flow screen", func() {
		writeScreen(t, "flow", "{ not json")
	})
	// The notice is one line cut to the terminal width, so a long path may lose its tail;
	// the words that name which screen was refused always fit.
	if !strings.Contains(out, "your flow screen") {
		t.Errorf("the broken file was not reported:\n%s", out)
	}
	if !strings.Contains(out, "esc close") {
		t.Errorf("the built-in flow screen was not drawn after a broken edit:\n%s", out)
	}
}
