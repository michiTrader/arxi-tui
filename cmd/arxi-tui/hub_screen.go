package main

import (
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// factoryHub is the Scene 12 document: the one screen behind /provider.
//
// The layout follows the slash menu on purpose. The explanation and the title sit
// where the chat sits, the input line is where the user already types, and the
// choices hang BELOW the input as a bottom overlay -- the same place the command menu
// opens, so the eye does not travel. The input line is the filter: typing "other" or
// "gem" narrows the choices; on a form it is the focused field (masked for secrets).
//
// It is embedded, like the other factory scenes, so the binary does not depend on
// testdata/ existing on the user's disk. TestTheEmbeddedHubSceneMatchesTheFixture
// holds it equal to testdata/HUB.json.
const factoryHub = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "header"},
    "text": "Δr×i v0.1.0 · providers" },

  { "id": "notice", "type": "text", "bind": "host.scene.error",
    "when": "host.scene.error", "style": {"style": "banner"} },

  { "id": "title", "type": "text", "bind": "hub.title" },

  { "id": "detail", "type": "markdown", "bind": "hub.detail", "grow": 1 },

  { "id": "prompt", "type": "input", "bind": "user.input", "prefix": "┃ " },

  { "id": "choices", "type": "overlay", "anchor": "bottom",
    "children": [
      { "type": "rule" },
      { "id": "options", "type": "list", "bind": "hub.rows",
        "row_template": { "type": "row", "children": [
          { "type": "text", "bind": "row.line", "weight": 9 },
          { "type": "text", "bind": "row.status", "weight": 7 }
        ]}},
      { "id": "hint", "type": "text", "bind": "hub.hint", "style": {"style": "dim"} }
    ]}
]}}`

// loadHubScene parses and validates the embedded screen. A failure is a programming
// error (the fixture and this constant are held equal by a test), so the caller
// refuses to open the screen with the reason rather than swap a broken document in.
func loadHubScene() (*scene.Document, error) {
	doc, err := scene.ParseDocument([]byte(factoryHub))
	if err != nil {
		return nil, fmt.Errorf("provider scene: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("provider scene: %w", err)
	}
	return doc, nil
}
