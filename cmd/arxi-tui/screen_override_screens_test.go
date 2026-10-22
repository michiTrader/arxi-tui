package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The flow, team and auto screens come from the profile when it carries a file for them.
// Consequence: only /provider was proven through the real loop, so a screen whose
// load bypassed loadScreen would still pass every unit test and ignore the profile.
// Remedy: main.go must load each of the four screens through screenFor.

func TestLoopShowsTheProfilesFlowScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "flow", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE FLOW", 1))
	drv := &testDriver{evCh: make(chan fold.Event, 64)}
	out := screenLoopWith(t, builtinDoc(t), drv, drv.evCh, "/flow", "PROFILE FLOW")
	if !strings.Contains(out, "PROFILE FLOW") {
		t.Errorf("/flow did not show the profile's screen:\n%s", out)
	}
}

func TestLoopShowsTheProfilesTeamScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "team", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE TEAM", 1))
	root := t.TempDir()
	writeAgents(t, root, "feature-team.yaml")
	t.Chdir(root)
	core := teamHubCore{fakeTeamCore: &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"feature-team.yaml": featureTeamInfo()}}}
	drv := &teamTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: core}
	out := screenLoopWith(t, builtinDoc(t), drv, drv.evCh, "/team", "PROFILE TEAM")
	if !strings.Contains(out, "PROFILE TEAM") {
		t.Errorf("/team did not show the profile's screen:\n%s", out)
	}
}

func TestLoopShowsTheProfilesAutoScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "auto", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE AUTO", 1))
	fixedClock(t)
	core, root := newFakeAutoCore(t)
	t.Chdir(root)
	drv := &autoTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: autoHubCore{fakeAutoCore: core}}
	out := screenLoopWith(t, builtinDoc(t), drv, drv.evCh, "/auto", "PROFILE AUTO")
	if !strings.Contains(out, "PROFILE AUTO") {
		t.Errorf("/auto did not show the profile's screen:\n%s", out)
	}
}
