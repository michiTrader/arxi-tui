package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// This file is the host half of Scene 12 (PROVIDERS): the screen `/provider` and
// `/model` open. The document is testdata/PROVIDERS.json; what lives here is the
// state the document binds to (the model rows and the highlighted one) and the
// keys that move it, kept apart from the loop so each decision is a pure function a
// test can pin without a terminal.
//
// # Why the screen is a replacement document, not an overlay
//
// It follows the community installer (installer_browse.go): while it is open it
// replaces the scene on display and owns the keyboard, so Up/Down mean "move the
// highlight" instead of "walk the input's wrapped rows" and Enter means "toggle this
// model" instead of "send this text to the agent". An overlay would have to share
// those keys with the chat input behind it, which is exactly the ambiguity that sent
// `/provider` to the chat in the first place.

// factoryProviders is the Scene 12 document, embedded for the same reason
// factorySobria is: the binary must not depend on testdata/ existing on the user's
// disk. TestTheEmbeddedProvidersSceneMatchesTheFixture holds it equal to
// testdata/PROVIDERS.json, so the golden the engine tests render and the screen the
// user opens cannot drift apart.
const factoryProviders = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "header"},
    "text": "Δr×i v0.1.0 · providers" },

  { "id": "notice", "type": "text", "bind": "host.scene.error",
    "when": "host.scene.error", "style": {"style": "banner"} },

  { "id": "models", "type": "list", "bind": "providers.models", "grow": 1,
    "row_template": { "type": "row", "children": [
      { "type": "text", "bind": "row.marker", "weight": 1 },
      { "type": "text", "bind": "row.provider", "weight": 4 },
      { "type": "text", "bind": "row.model", "weight": 8 },
      { "type": "button", "text": "enable", "on_press": "cmd:/model enable {row.ref}", "when": "row.disabled", "weight": 4 },
      { "type": "button", "text": "disable", "on_press": "cmd:/model disable {row.ref}", "when": "row.enabled", "weight": 4 }
    ]}},

  { "id": "provenance", "type": "text", "style": {"style": "dim"},
    "text": "providers: ./providers/ (the folder arxi starts in) · keys are never stored, only the env var name" },

  { "id": "footer", "type": "text", "style": {"style": "dim"},
    "text": "↑↓ move · enter toggle · /provider add <name> · esc back" },

  { "id": "prompt", "type": "input", "bind": "user.input",
    "prefix": "┃ ", "placeholder": "/provider add <name> --api-key-env <VAR>" }
]}}`

// loadProvidersScene parses and validates the embedded screen. A failure is a
// programming error (the fixture and this constant are held equal by a test), so the
// caller refuses to open the screen with the reason rather than swap a broken
// document onto the display.
func loadProvidersScene() (*scene.Document, error) {
	doc, err := scene.ParseDocument([]byte(factoryProviders))
	if err != nil {
		return nil, fmt.Errorf("providers scene: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("providers scene: %w", err)
	}
	return doc, nil
}

// providersEmptyNotice is what the screen says when the core answered with no
// models. An empty list on its own reads as "the screen is broken"; this names the
// next step instead, with the credential flag spelled so the first command a new
// user types is the safe one (an env var NAME, never a key).
const providersEmptyNotice = "no providers registered yet; add one with " +
	"/provider add <name> --api-key-env <VAR>"

// providersScreen is the loop-visible state of the open screen. A nil
// *providersScreen means the screen is closed, the way a nil *installerBrowse does.
type providersScreen struct {
	models   []fold.ProviderModel
	selected int
}

// setModels replaces the rows with a fresh model.list answer and re-clamps the
// highlight. The clamp matters after a refresh: a list that shrank under the
// highlight would otherwise leave it pointing past the end, and Enter would toggle a
// model that is not there.
func (s *providersScreen) setModels(models []fold.ProviderModel) {
	s.models = models
	if s.selected >= len(models) {
		s.selected = len(models) - 1
	}
	if s.selected < 0 {
		s.selected = 0
	}
}

// move steps the highlight by delta, wrapping at both ends like the slash menu: the
// highlight is the only thing the arrows move here, so the user must always feel a
// row under it. An empty list has nothing to move over.
func (s *providersScreen) move(delta int) {
	n := len(s.models)
	if n == 0 {
		return
	}
	s.selected = ((s.selected+delta)%n + n) % n
}

// publish writes the screen's two view-state fields onto the folded state the
// renderer reads, together, so a frame cannot show rows with a highlight from a
// different list.
func (s *providersScreen) publish(state *fold.State) {
	state.ProviderModels = s.models
	state.ProviderSelected = s.selected
}

// toggleAction is what Enter does on the highlighted row: disable an enabled model,
// enable a disabled one. It reads the row's state rather than asking the user which
// direction, because the row already shows exactly one of the two buttons and Enter
// must do what that button says. ok is false on an empty list, where there is no row
// to act on.
func (s *providersScreen) toggleAction() (act providerAction, ok bool) {
	if s.selected < 0 || s.selected >= len(s.models) {
		return providerAction{}, false
	}
	m := s.models[s.selected]
	verb := "model.enable"
	if m.Enabled {
		verb = "model.disable"
	}
	// Refresh: the toggle changed the list the screen is showing, so the worker
	// re-reads it and the row flips to the other button instead of staying stale.
	return providerAction{Verb: verb, Ref: m.Ref(), Refresh: true}, true
}

// modelsFromRows projects the driver's wire rows onto the fold's view-state rows.
// The two types carry the same three fields; they are separate because the fold
// must not import the driver, and this is the single place they are joined.
func modelsFromRows(rows []driver.ModelRow) []fold.ProviderModel {
	out := make([]fold.ProviderModel, 0, len(rows))
	for _, r := range rows {
		out = append(out, fold.ProviderModel{Provider: r.Provider, ID: r.ID, Enabled: r.Enabled})
	}
	return out
}

// providersKeyResult is what one keypress on the open screen asks the loop to do.
// The screen's own state (highlight) is changed in place; the loop-visible effects
// are the three below, so the loop stays a thin switch and the decision is testable.
type providersKeyResult struct {
	close    bool            // Esc: drop the screen
	dispatch *providerAction // Enter/typed command: run this on the worker
	notice   string          // a refusal to show without dispatching anything
	edited   bool            // the key was text editing for the input line
}

// routeProvidersKey maps one key to its effect on the open screen.
//
// Enter has two meanings, told apart by the input line: empty means "toggle the
// highlighted model", non-empty means "run what I typed". Without that split the
// screen could not add a provider at all, since adding is a typed command with
// arguments. A typed line that is not a /provider or /model command is refused with
// the grammar rather than sent to the agent: this screen is not a chat, and a prompt
// sent from here would vanish into a transcript the user cannot see.
func routeProvidersKey(s *providersScreen, input string, k term.Key) providersKeyResult {
	switch k.Type {
	case term.KeyEscape:
		return providersKeyResult{close: true}
	case term.KeyUp:
		s.move(-1)
		return providersKeyResult{}
	case term.KeyDown:
		s.move(1)
		return providersKeyResult{}
	case term.KeyEnter:
		line := strings.TrimSpace(input)
		if line == "" {
			if act, ok := s.toggleAction(); ok {
				return providersKeyResult{dispatch: &act}
			}
			return providersKeyResult{notice: providersEmptyNotice}
		}
		if act, matched, perr := parseProviderCommand(line); matched {
			return typedProvidersResult(act, perr)
		}
		if act, matched, perr := parseModelCommand(line); matched {
			return typedProvidersResult(act, perr)
		}
		return providersKeyResult{notice: "this screen takes /provider add <name> " +
			"[--base-url <url>] [--api-key-env <VAR>], or /model enable|disable <ref>; " +
			"Esc goes back to the chat"}
	}
	return providersKeyResult{edited: true}
}

// typedProvidersResult turns a parsed typed command into the screen's result. A
// parse error is shown as a notice (the user's typo, cheapest to fix). A valid
// command is dispatched with Refresh set so the visible list reflects what it
// changed -- except the bare `/provider`, which already re-reads the list as its own
// work and so needs no second read.
func typedProvidersResult(act providerAction, perr error) providersKeyResult {
	if perr != nil {
		return providersKeyResult{notice: perr.Error()}
	}
	if !act.OpenScreen {
		act.Refresh = true
	}
	return providersKeyResult{dispatch: &act}
}

// menuHostCommand resolves the slash menu's highlighted row to the line a host
// command should run, for the commands the host implements as screens. It returns
// ok=false for every other row, which keeps the Phase 0 contract (the name is
// submitted as a prompt) until that command has a host implementation of its own.
//
// This exists because the menu's Enter used to submit the highlighted NAME as a
// prompt for every row, so picking "provider" sent the word "provider" to the chat
// instead of opening the screen the row advertises. The loop asks this before the
// menu so a menu pick and a typed `/provider` take the same path and cannot diverge.
func menuHostCommand(input string, sel int) (line string, ok bool) {
	if !strings.HasPrefix(input, "/") {
		return "", false
	}
	matches := fold.FilterSlashMatches(strings.TrimPrefix(input, "/"))
	if len(matches) == 0 {
		return "", false
	}
	// The same clamp slashMenuKey applies, so the row this resolves is the row the
	// highlight is on.
	if sel < 0 || sel >= len(matches) {
		sel = len(matches) - 1
	}
	switch name := matches[sel].Name; name {
	case "provider", "model":
		return "/" + name, true
	}
	return "", false
}
