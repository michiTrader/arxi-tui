// Command arxi-tui is the terminal interface driver for arxi.
//
// This is Phase 0's raw scene: it reads a scene file (default: factory RAW),
// renders the frame, and exits. The fold is built from a mock log.
// Phase 1 replaces the mock with NDJSON from the arxi core.
package main

import (
	"fmt"
	"os"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

const factoryRAW = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}`

func main() {
	// Load the scene document (fallback to factory RAW if missing)
	data, err := os.ReadFile("testdata/RAW.json")
	if err != nil {
		data = []byte(factoryRAW)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scene parse error: %v\n", err)
		os.Exit(1)
	}

	// Phase 0 mock driver: fixed log to prove the pipeline end-to-end.
	events := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola! ¿En qué puedo ayudarte?"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}

	state := fold.Fold(events)
	r := engine.Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	fmt.Print(f.Plain())
}
