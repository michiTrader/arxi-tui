// Command arxi-tui is the terminal interface driver for arxi.
//
// This is Phase 0's raw scene: it reads a scene file (default: factory RAW),
// drives the fold with events, and renders frames. The escape gesture
// (Ctrl-C x2) is immovable, never delegated to scenes or plugins.
package main

import (
	"fmt"
	"os"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

func main() {
	// Load the scene document ("raw" for Phase 0)
	scenePath := "testdata/RAW.json"
	data, err := os.ReadFile(scenePath)
	if err != nil {
		// Fallback to raw if file missing
		data = []byte(`{ "root": { "type": "stack", "children": [
			{ "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
			{ "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
		]}}`)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scene parse error: %v\n", err)
		os.Exit(1)
	}

	// Setup renderer
	r := engine.Renderer{Width: 80, Height: 24}

	// Fold the log (empty for Phase 0: RAW scene with no events yet)
	_ = fold.Fold([]fold.Event{})

	// Render the frame — this is the Phase 0 exit criterion:
	// the raw scene draws and is usable.
	f := r.RenderFrame(doc)
	fmt.Print(f.Plain())
}
