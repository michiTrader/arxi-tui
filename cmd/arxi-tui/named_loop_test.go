package main

import (
	"context"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// runNamedLoop drives the real loop on the built-in scene, by its name: the only scene
// that reads the user's saved texts (texts.json in the config folder), so a test about a
// setting goes through the door the setting uses. It returns what the loop drew and what
// it sent to the model.
func runNamedLoop(t *testing.T, w, h int, script []scheduledEvent) (string, []string) {
	t.Helper()
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(w, h, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64), seq: 1000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	return tty.output(), drv.submitted
}
