package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
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

// teamDriver is a driver whose core describes one team, in a fresh working folder.
func teamDriver(t *testing.T) *teamTestDriver {
	t.Helper()
	root := t.TempDir()
	writeAgents(t, root, "feature-team.yaml")
	t.Chdir(root)
	core := teamHubCore{fakeTeamCore: &fakeTeamCore{infos: map[string]*driver.BlueprintInfo{"feature-team.yaml": featureTeamInfo()}}}
	return &teamTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64)}, core: core}
}

func TestLoopShowsTheProfilesTeamScreen(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	writeScreen(t, "team", strings.Replace(replacementScreen, "PROFILE HUB", "PROFILE TEAM", 1))
	drv := teamDriver(t)
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

// A click while a screen is open never presses a node of the scene behind it. The profile's
// screen and the main scene both have a button with the id "go"; the click lands on the
// screen's button, so only the guard in the loop (clickMayPress) keeps it from pressing the
// main scene's "go". After the screen is closed the same click must press the main button, so
// the test also proves that the click path works at all.
// Consequence: a click on a screen runs an action the user cannot see on that screen.
// Remedy: the loop asks clickMayPress, with the screen state, before it presses.
func TestLoopAClickOnAScreenDoesNotPressTheSceneBehindIt(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	writeScreen(t, "hub", `{"root":{"type":"stack","children":[`+
		`{"id":"go","type":"button","text":"[ HUB BUTTON ]","on_press":"say:HUB PRESSED"}]}}`)
	doc, err := scene.ParseNamed(defaultscene.Name, []byte(`{"root":{"type":"stack","children":[`+
		`{"id":"go","type":"button","text":"[ MAIN BUTTON ]","on_press":"say:MAIN PRESSED"},`+
		`{"id":"chat","type":"markdown","bind":"chat.history","grow":1},`+
		`{"id":"prompt","type":"input","bind":"user.input","prefix":"> "}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	core := &modelCore{def: "openai/gpt-4o", models: sampleModels().models}
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, hub: core}
	tty := newOpenFakeTTY(100, 24, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "") }()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		// The last frame, not all output: "[ MAIN BUTTON ]" was drawn before the screen
		// opened, and the wait for it after the screen closes must see a newer frame.
		for !strings.Contains(lastFrame(tty.output()), want) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if !strings.Contains(lastFrame(tty.output()), want) {
			t.Fatalf("never saw %q; last frame:\n%s", want, lastFrame(tty.output()))
		}
	}
	send := func(evs []scheduledEvent) {
		for _, ev := range evs {
			time.Sleep(ev.delay)
			tty.events <- ev.event
		}
	}
	waitFor("[ MAIN BUTTON ]")
	send(append(typed("/provider"), at(20, enterEvent())))
	waitFor("[ HUB BUTTON ]")
	send(click(2, 0))
	send([]scheduledEvent{at(20, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEscape}})})
	waitFor("[ MAIN BUTTON ]")
	mark := len(tty.output())
	send(click(2, 0)) // the control: the screen is closed, this click must press
	waitFor("MAIN PRESSED")
	send(exitKeys())
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the loop did not quit")
	}
	out := tty.output()
	if n := strings.Count(stripANSI(out[:mark]), "MAIN PRESSED"); n != 0 {
		t.Errorf("a click on the open screen pressed the main scene's button (%d frames show it).\nConsequence: a click runs an action the user cannot see.\nRemedy: ask clickMayPress with the screen state before pressing.", n)
	}
}
