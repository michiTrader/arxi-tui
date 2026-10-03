package main

import (
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// This file is the document half of Scene 13 (LOGIN): the /login wizard. The
// document is testdata/LOGIN.json; the state it binds to and the keys that move it
// live in login_wizard.go. Like the providers screen it is a replacement document
// that owns the keyboard while open.

// factoryLogin is the Scene 13 document, embedded so the binary does not depend on
// testdata/ existing on the user's disk. TestTheEmbeddedLoginSceneMatchesTheFixture
// holds it equal to testdata/LOGIN.json.
const factoryLogin = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "header"},
    "text": "Δr×i v0.1.0 · login" },

  { "id": "notice", "type": "text", "bind": "host.scene.error",
    "when": "host.scene.error", "style": {"style": "banner"} },

  { "id": "title", "type": "text", "bind": "login.title" },

  { "id": "choices", "type": "list", "bind": "login.rows", "grow": 1,
    "row_template": { "type": "row", "children": [
      { "type": "text", "bind": "row.marker", "weight": 1 },
      { "type": "text", "bind": "row.label", "weight": 9 },
      { "type": "text", "bind": "row.status", "weight": 7 }
    ]}},

  { "id": "pager", "type": "text", "bind": "login.pager", "style": {"style": "dim"} },

  { "id": "hint", "type": "text", "bind": "login.hint", "style": {"style": "dim"} }
]}}`

// loadLoginScene parses and validates the embedded screen. A failure is a
// programming error; the caller refuses to open the wizard with the reason.
func loadLoginScene() (*scene.Document, error) {
	doc, err := scene.ParseDocument([]byte(factoryLogin))
	if err != nil {
		return nil, fmt.Errorf("login scene: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("login scene: %w", err)
	}
	return doc, nil
}
