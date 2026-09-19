package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// TestEveryStyleKeyTheValidatorAcceptsAlsoReachesTheRenderer closes the second
// half of a defect whose first half was already paid for.
//
// The first half: ValidateTokens read style["token"] while the scenes, both
// format documents and styleName() all write style["style"], so the validator
// was checking a key the format does not use. That was fixed by teaching the
// validator to read both keys, on the argument that "token" was already written
// into corpus cases and tests, so rejecting it would turn a validator fix into a
// format break.
//
// The half that argument left open is the one this test pins. Accepting a key
// in the validator is a statement that scenes may be written that way, and the
// renderer never got the same news: styleName() still read style["style"]
// alone. So style {"token": "dim"} passed validation and rendered with no
// style at all — the worst of the three possible outcomes, because the other
// two are honest. Refusing it would tell the author to fix the spelling;
// rendering it dim would do what they meant. Validating it clean and then
// dropping the style silently tells them it worked while the screen disagrees,
// and there is no diagnostic anywhere to read.
//
// Measured rather than supposed, on the corpus' own gold answer: the converged
// document of sobria-dim-the-footer — the case whose order is literally "grey
// it out" — styles model.name as {"token": "dim"} and rendered it unstyled,
// while the usage.in/out spans beside it spell the key "style" and came out
// dim. The corpus' definition of success for a styling order was a document
// that does not apply the style.
//
// This is deliberately a property over StyleTokenKeys rather than two hardcoded
// cases. The failure was an inventory in one package growing a key the other
// package did not learn about, so a test that enumerates the keys itself would
// reproduce exactly the drift it is meant to catch: a third spelling added to
// the validator tomorrow would be accepted, unread by the renderer, and unseen
// here. Driving the loop from the validator's own list means the renderer is
// held to whatever the validator accepts, today and after the next edit.
func TestEveryStyleKeyTheValidatorAcceptsAlsoReachesTheRenderer(t *testing.T) {
	for _, key := range scene.StyleTokenKeys() {
		t.Run(key, func(t *testing.T) {
			got := styleName(map[string]string{key: "dim"})
			if got != "dim" {
				t.Errorf("the validator accepts style {%q: \"dim\"} but styleName reads %q from it\n"+
					"consequence: a scene spelled this way validates clean and renders with no style, which is the one outcome that reports success and shows the wrong screen — a refusal would name the fix and a correct render would apply it, but this tells the author it worked and silently drops the style, with nothing to read anywhere. The corpus already gold-plates such a document: sobria-dim-the-footer converges on {\"token\": \"dim\"} for an order that says \"grey it out\".\n"+
					"remedy: read the style reference through the same key list the validator enforces (scene.StyleTokenKeys), or stop accepting the key in ValidateTokens so the scene is refused with an address instead.", key, got)
			}
		})
	}
}

// TestASceneStyledWithEitherKeyRendersTheSameFrame is the end-to-end statement
// of the same rule, because agreement between two helpers is not the promise —
// the promise is about what the user sees.
//
// styleName could be made to satisfy the property test above and the frame
// could still differ, if some node type reached its style by another route.
// Rendering two documents that differ only in the spelling of an accepted key
// and diffing the styled frames is the assertion that actually matches the
// product claim: the key a scene picks from the accepted set is a spelling, not
// a behaviour.
func TestASceneStyledWithEitherKeyRendersTheSameFrame(t *testing.T) {
	const tmpl = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "status", "type": "row", "children": [
    { "type": "text", "bind": "model.name", "style": { "%s": "dim" } }
  ] },
  { "id": "prompt", "type": "input", "bind": "user.input", "prefix": "┃ " }
]}}`

	keys := scene.StyleTokenKeys()
	if len(keys) < 2 {
		t.Skip("only one style key is accepted; there is no second spelling to compare")
	}

	render := func(t *testing.T, key string) string {
		t.Helper()
		doc, err := scene.ParseDocument([]byte(strings.Replace(tmpl, "%s", key, 1)))
		if err != nil {
			t.Fatalf("ParseDocument(%s): %v", key, err)
		}
		state := fold.Fold([]fold.Event{
			{Type: "llm.response", Seq: 1, Payload: map[string]any{"text": "hi"}},
		})
		state.ModelName = "kimi-k3"
		r := Renderer{Width: 40, Height: 6}
		return r.RenderFrame(doc, state).Styled()
	}

	want := render(t, keys[0])
	if !strings.Contains(want, "«dim:kimi-k3»") {
		t.Fatalf("the reference spelling %q did not style the model name\n"+
			"consequence: the comparison below would pass by both spellings being equally broken.\n"+
			"remedy: fix styleName before reading the rest of this failure.\nframe:\n%s", keys[0], want)
	}
	for _, key := range keys[1:] {
		if got := render(t, key); got != want {
			t.Errorf("a scene styled with %q renders differently from one styled with %q\n"+
				"consequence: the validator accepts both spellings, so a user picking the wrong one gets a silently unstyled interface with no refusal to read.\n"+
				"remedy: resolve the style through scene.StyleTokenKeys in the render path.\ngot:\n%s\nwant:\n%s", key, keys[0], got, want)
		}
	}
}
