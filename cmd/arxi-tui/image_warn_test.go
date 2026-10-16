package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func TestImageModelWarningSaysItOnlyWhenTheModelIsKnownAndRefuses(t *testing.T) {
	cases := []struct {
		name    string
		ref     string
		known   bool
		accepts bool
		warns   bool
	}{
		{"known model not set for images", "openai/gpt-4o", true, false, true},
		{"known model set for images", "openai/gpt-4o", true, true, false},
		{"still reading the model", "", false, false, false},
		{"read done but no model chosen", "", true, false, false},
		{"unknown flag with a stale name", "openai/gpt-4o", false, false, false},
	}
	for _, c := range cases {
		got := imageModelWarning(c.ref, c.known, c.accepts)
		if (got != "") != c.warns {
			t.Errorf("%s: warning = %q, want warning %v", c.name, got, c.warns)
		}
	}
	w := imageModelWarning("openai/gpt-4o", true, false)
	for _, part := range []string{"openai/gpt-4o", "/model", "Accepts images"} {
		if !strings.Contains(w, part) {
			t.Errorf("the warning %q does not mention %q", w, part)
		}
	}
}

// warnLoop runs the real loop against a hub core, waits until the status bar names the
// model (the core has answered), then plays the script and quits.
func warnLoop(t *testing.T, models []driver.ModelRow, def string, script []scheduledEvent) (string, *testDriver) {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	core := &modelCore{def: def, models: models}
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, hub: core}
	tty := newOpenFakeTTY(140, 24, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "") }()
	waitFor := func(what string, ok func() bool) {
		deadline := time.Now().Add(30 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %s:\n%s", what, stripANSI(tty.output()))
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if def != "" {
		waitFor("the model in the status bar", func() bool { return strings.Contains(stripANSI(tty.output()), def[strings.Index(def, "/")+1:]) })
	}
	for _, ev := range script {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	time.Sleep(300 * time.Millisecond)
	for _, ev := range exitKeys() {
		time.Sleep(ev.delay)
		tty.events <- ev.event
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the loop did not quit")
	}
	return stripANSI(tty.output()), &drv.testDriver
}

const imageNotSetText = "is not set to take images"

func TestLoopAttachingAPictureWarnsWhenTheModelIsNotSetForImages(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	models := []driver.ModelRow{{Provider: "openai", ID: "gpt-4o", Enabled: true, Images: false}}

	// Pasted path.
	frames, d := warnLoop(t, models, "openai/gpt-4o", []scheduledEvent{at(10, pasteEvent(p))})
	if !strings.Contains(frames, "openai/gpt-4o "+imageNotSetText) {
		t.Errorf("a pasted picture for a model that takes none raised no early warning:\n%s", frames)
	}
	if len(d.submitted) != 0 {
		t.Errorf("the warning must not send anything: %q", d.submitted)
	}

	// /image <path>.
	script := append(typed("/image "+p), at(10, enterEvent()))
	frames, _ = warnLoop(t, models, "openai/gpt-4o", script)
	if !strings.Contains(frames, "openai/gpt-4o "+imageNotSetText) {
		t.Errorf("/image for a model that takes none raised no early warning:\n%s", frames)
	}
}

func TestLoopAttachingAPictureStaysQuietWhenTheModelTakesImages(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	models := []driver.ModelRow{{Provider: "openai", ID: "gpt-4o", Enabled: true, Images: true}}
	frames, d := warnLoop(t, models, "openai/gpt-4o", []scheduledEvent{at(10, pasteEvent(p)), at(100, enterEvent())})
	if strings.Contains(frames, imageNotSetText) {
		t.Errorf("a model set for images was warned about:\n%s", frames)
	}
	if !strings.Contains(frames, "[Image #1 shot.png]") || len(d.submitted) != 1 {
		t.Errorf("the picture was not attached and sent: submitted %q", d.submitted)
	}
}

func TestLoopAttachingAPictureStaysQuietWithNoModelChosen(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	frames, _ := warnLoop(t, nil, "", []scheduledEvent{at(10, pasteEvent(p))})
	if strings.Contains(frames, imageNotSetText) {
		t.Errorf("warned about a model that does not exist:\n%s", frames)
	}
	if !strings.Contains(frames, "[Image #1 shot.png]") {
		t.Error("the picture was not attached")
	}
}

// Changing the chat model changes the answer: the warning follows the model in use now.
func TestLoopTheWarningFollowsTheModelChosenInTheMenu(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	models := []driver.ModelRow{
		{Provider: "openai", ID: "gpt-4o", Enabled: true, Images: true},
		{Provider: "tokenharbor", ID: "textonly", Enabled: true, Images: false},
	}
	script := append(typed("/model textonly"), at(50, enterEvent()))
	script = append(script, at(400, pasteEvent(p)))
	frames, _ := warnLoop(t, models, "openai/gpt-4o", script)
	if !strings.Contains(frames, "tokenharbor/textonly "+imageNotSetText) {
		t.Errorf("after switching to a text-only model the picture raised no warning:\n%s", frames)
	}
	if strings.Contains(frames, "openai/gpt-4o "+imageNotSetText) {
		t.Errorf("the warning named the old model:\n%s", frames)
	}
}

// ---- ctrl+z around a picture marker ----

func TestLoopCtrlZAfterAPictureMarkerTakesItBackAndTheLineGoesWithoutIt(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	script := append(typed("look "), at(10, pasteEvent(p)))
	script = append(script, undoKey(), at(20, enterEvent()))
	_, d := imageLoop(t, script)
	// The line was "look " + marker; ctrl+z took the marker back, so what is sent holds
	// no marker and no picture.
	if len(d.submitted) != 1 || strings.Contains(d.submitted[0], "[Image") {
		t.Fatalf("submitted %q: the marker should be gone after ctrl+z", d.submitted)
	}
	for _, a := range d.attached {
		if len(a) != 0 {
			t.Fatalf("a picture travelled without its marker: %+v", d.attached)
		}
	}
}

func TestLoopCtrlZAndCtrlYBringTheMarkerAndItsPictureBack(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	script := append(typed("look "), at(10, pasteEvent(p)))
	script = append(script, undoKey(), redoKey(), at(20, enterEvent()))
	_, d := imageLoop(t, script)
	if len(d.submitted) != 1 || !strings.Contains(d.submitted[0], "[Image #1 shot.png]") {
		t.Fatalf("submitted %q: ctrl+y should have put the marker back", d.submitted)
	}
	last := d.attached[len(d.attached)-1]
	if len(last) != 1 || last[0].MediaType != "image/png" {
		t.Fatalf("the marker came back without its picture: %+v", d.attached)
	}
}

func TestLoopCtrlZAfterDeletingAMarkerBringsTheMarkerAndItsPictureBack(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	script := append(typed("look "), at(10, pasteEvent(p)))
	script = append(script, at(10, barBackspace()), undoKey(), at(20, enterEvent()))
	_, d := imageLoop(t, script)
	if len(d.submitted) != 1 || !strings.Contains(d.submitted[0], "[Image #1 shot.png]") {
		t.Fatalf("submitted %q: ctrl+z after deleting the marker should bring it back", d.submitted)
	}
	last := d.attached[len(d.attached)-1]
	if len(last) != 1 {
		t.Fatalf("the marker came back without its picture: %+v", d.attached)
	}
}

func barBackspace() term.Event {
	return term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}}
}

// flipCore says "no images" on the first read (the startup one) and "images" on every
// later read, as a core does after the operator turned the switch on in /providers.
type flipCore struct {
	modelCore
	reads int
}

func (c *flipCore) SubmitModelList(ctx context.Context) (*driver.ModelListResult, error) {
	c.mu.Lock()
	c.reads++
	on := c.reads > 1
	c.mu.Unlock()
	rows := append([]driver.ModelRow(nil), c.models...)
	for i := range rows {
		rows[i].Images = on
	}
	return &driver.ModelListResult{Models: rows}, nil
}

// The providers screen re-reads the models when it opens; what it learns (the model now
// takes images) must stop the warning, with no restart.
func TestLoopTheProvidersScreenRefreshesWhetherTheModelTakesImages(t *testing.T) {
	p := writePic(t, "shot.png", pngBytes(t))
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatal(err)
	}
	core := &flipCore{modelCore: modelCore{def: "openai/gpt-4o", models: []driver.ModelRow{{Provider: "openai", ID: "gpt-4o", Enabled: true}}}}
	drv := &hubTestDriver{testDriver: testDriver{evCh: make(chan fold.Event, 64), seq: 1000}, hub: core}
	tty := newOpenFakeTTY(140, 24, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "") }()
	send := func(evs ...scheduledEvent) {
		for _, ev := range evs {
			time.Sleep(ev.delay)
			tty.events <- ev.event
		}
	}
	waitUntil := func(what string, ok func() bool) {
		for deadline := time.Now().Add(30 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %s:\n%s", what, stripANSI(tty.output()))
			}
		}
	}
	waitUntil("the model in the status bar", func() bool { return strings.Contains(stripANSI(tty.output()), "gpt-4o") })
	send(typed("/providers")...)
	send(at(20, enterEvent()))
	waitUntil("the providers screen read", func() bool {
		core.mu.Lock()
		defer core.mu.Unlock()
		return core.reads >= 2
	})
	time.Sleep(300 * time.Millisecond)
	send(at(10, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyEscape}}))
	time.Sleep(200 * time.Millisecond)
	before := len(tty.output())
	send(at(10, pasteEvent(p)))
	time.Sleep(300 * time.Millisecond)
	after := stripANSI(tty.output()[before:])
	send(exitKeys()...)
	<-done
	if !strings.Contains(after, "[Image #1 shot.png]") {
		t.Fatalf("the picture was not attached after the providers screen:\n%s", after)
	}
	if strings.Contains(after, imageNotSetText) {
		t.Errorf("the providers screen learned the model takes images, yet the warning came:\n%s", after)
	}
}
