package ext

import (
	"strings"
	"testing"
)

// A behavioral manifest whose binds declare mocks — the shape the installer
// parses to preview an entry before installing it (J1 / Q16). It carries an
// executable on purpose: a mock-bearing manifest is behavioral (binds is a
// behavioral field), so PreviewMocks must read it from a plainly-parsed manifest
// without ever validating or spawning it. price declares a text mock, history a
// series mock, and status declares NO mock — the un-mocked field the table must
// omit rather than store empty.
const behavioralWithMocks = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "capabilities": ["events.emit"],
  "binds": {
    "tick.price":   { "kind": "text",   "mock": "$1.23" },
    "tick.history": { "kind": "series", "mock": [1, 3, 2] },
    "tick.status":  { "kind": "text" }
  }
}`

// TestPreviewMocksProjectsTextAndSeriesUnderFullPaths is the positive half of the
// J1 builder: a parsed manifest's declared mocks become the renderer's substitution
// table, keyed by the same fully-qualified `<plugin-id>.<field>` paths the mounted
// fragment binds and the store publishes under — so a previewed bind and its live
// counterpart resolve as one path. A text mock is its unquoted contents; a series
// mock is the compact JSON form a live series frame renders as, so the preview is a
// faithful stand-in for the stream rather than a differently-shaped placeholder.
func TestPreviewMocksProjectsTextAndSeriesUnderFullPaths(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(behavioralWithMocks))
	if err != nil {
		t.Fatalf("ParseNamed refused a well-formed mock-bearing manifest: %v; a manifest is parseable data even when behavioral, and preview reads it without validating or spawning", err)
	}
	got := m.PreviewMocks()

	if got["tick.price"] != "$1.23" {
		t.Errorf("text mock projected to %q, want \"$1.23\" under the full path tick.price\n"+
			"consequence: the preview pane cannot show a text bind's declared placeholder, so a browsed scene is a wall of \"[…]\" and the user cannot see what the plugin looks like before installing (Q16).\n"+
			"remedy: PreviewMocks must project decl.Mock through renderValue and key it by the binds map's own <id>.<field> path.", got["tick.price"])
	}
	if got["tick.history"] != "[1,3,2]" {
		t.Errorf("series mock projected to %q, want the compact JSON \"[1,3,2]\"\n"+
			"consequence: a previewed series renders in a different shape than the live sparkline frame would, so the preview misrepresents the stream — the exact drift renderValue is single-sourced to prevent.\n"+
			"remedy: reuse renderValue so a series mock is the same compact form a live series frame yields.", got["tick.history"])
	}
}

// TestPreviewMocksOmitsAFieldWithNoMock pins the decision that separates "no
// declared placeholder" from "an empty placeholder": a bind that declares a kind
// but no mock is absent from the table, so at render time it falls through to the
// engine's own "[…]" — the honest "no value yet" — rather than a "" the renderer
// would draw as blank and a `when` would read as falsy for the wrong reason. The
// counterfactual is the same manifest with a mock added to that field: it must
// then appear, proving the omission was about the missing mock and not the field.
func TestPreviewMocksOmitsAFieldWithNoMock(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(behavioralWithMocks))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if _, present := m.PreviewMocks()["tick.status"]; present {
		t.Errorf("a bind declaring no mock appeared in the preview table\n" +
			"consequence: an un-mocked field previews as an empty string instead of the placeholder, so the preview shows blank where it should show the honest \"no value\" and a gate on it reads falsy for a reason the author never expressed.\n" +
			"remedy: PreviewMocks must skip a decl whose Mock is empty; an absent path falls through to the engine placeholder.")
	}

	// Counterfactual: give tick.status a mock and it must now be present, so the
	// omission above was exactly the missing mock, not an unrelated skip.
	withMock := strings.Replace(behavioralWithMocks,
		`"tick.status":  { "kind": "text" }`,
		`"tick.status":  { "kind": "text", "mock": "idle" }`, 1)
	m2, err := ParseNamed("plugin.json", []byte(withMock))
	if err != nil {
		t.Fatalf("ParseNamed (with status mock): %v", err)
	}
	if got := m2.PreviewMocks()["tick.status"]; got != "idle" {
		t.Errorf("adding a mock to tick.status did not make it appear (got %q); the only change was the mock, so the earlier omission must have been exactly its absence", got)
	}
}

// TestPreviewMocksIsEmptyForADeclarativeManifest proves the builder is inert on a
// plugin with nothing to preview: a declarative manifest declares no binds, so its
// table is empty, and an empty table is the renderer's no-op (render.go treats a
// nil/empty PreviewMocks as byte-identical to no preview at all). This is what lets
// the installer call PreviewMocks unconditionally on any entry without a
// declarative one perturbing a frame.
func TestPreviewMocksIsEmptyForADeclarativeManifest(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validDeclarative))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if got := m.PreviewMocks(); len(got) != 0 {
		t.Errorf("a declarative manifest produced a non-empty preview table: %v\n"+
			"consequence: previewing a declarative entry would substitute binds it never declared, so the installer cannot call PreviewMocks uniformly — it would have to special-case the plugin kind, the branch this projection exists to avoid.\n"+
			"remedy: a manifest with no binds yields an empty table; the renderer no-ops on it.", got)
	}
}
