package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// borderScene is Scene 3's tokens overlay from SCENES.md, reduced to the part
// under test: a border declared in the object form, carrying a style token.
const borderScene = `{ "root": { "type": "stack", "children": [
  { "id": "prompt", "type": "input", "bind": "user.input", "prefix": "> " },
  { "id": "tokens", "type": "overlay", "anchor": "top-right", "min_width": 12,
    "border": { "shape": "single", "style": "%s" },
    "children": [ { "type": "text", "bind": "session.tokens_used" } ] }
]}}`

// TestABorderStyleTokenReachesTheFrame pins the object-form border's style
// token against the drawn frame.
//
// SCENES.md Scene 3 signs the object form specifically so a border can carry a
// token — `"border": { "shape": "single", "style": "warn" }` — and node.go's
// BorderStyleName exists to read it, with a comment naming that scene.
// ValidateTokens checks the value against the theme and refuses an undefined
// one with an address. Every part of the contract was present except the one
// that shows it: both border-drawing paths hardcoded Style: "border" and never
// called BorderStyleName, so the token was validated and then discarded.
//
// The resulting failure is silent in the same way the style-key defect was, and
// worse in one respect: the author gets a refusal if they name a token the
// theme lacks, which is positive evidence that the field is wired up. It is
// checked, so it looks live. It simply never reaches the screen.
//
// The assertion is on the styled frame rather than on a helper, because a
// helper returning the right string proves nothing about which span the
// renderer stamps it onto.
func TestABorderStyleTokenReachesTheFrame(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(strings.Replace(borderScene, "%s", "header", 1)))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 8}
	styled := r.RenderFrame(doc, fold.Fold(nil)).Styled()

	if !strings.Contains(styled, "«header:") {
		t.Errorf("a border declaring the token \"header\" draws no span under it\n"+
			"consequence: SCENES.md Scene 3 signs the object border form precisely so a frame can carry a token, and ValidateTokens refuses an undefined one — so the field is checked, looks wired, and is then dropped on the way to the screen. A scene author gets a refusal for a typo and silence for a correct value.\n"+
			"remedy: draw the border spans under BorderStyleName() when the node declares one.\nstyled frame:\n%s", styled)
	}
}

// TestABorderWithNoStyleKeepsTheDefaultToken is the other half, and it is what
// keeps the fix from being a golden break.
//
// MAXIMUM declares its borders in the bare string form ("border": "single"),
// which carries no token, and its styled golden pins six spans under "border".
// A fix that read BorderStyleName unconditionally would stamp those spans with
// the empty string and move a golden that no feature asked to move — invariant
// 1 says the factory scene draws byte-identical frames, so the default must
// survive the change untouched.
func TestABorderWithNoStyleKeepsTheDefaultToken(t *testing.T) {
	src := `{ "root": { "type": "stack", "children": [
  { "id": "panel", "type": "box", "border": "single", "title": "Tasks",
    "children": [ { "type": "text", "text": "one" } ] },
  { "id": "prompt", "type": "input", "bind": "user.input", "prefix": "> " }
]}}`
	doc, err := scene.ParseDocument([]byte(src))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 8}
	styled := r.RenderFrame(doc, fold.Fold(nil)).Styled()

	if !strings.Contains(styled, "«border:") {
		t.Errorf("a bare string border no longer draws under the default \"border\" token\n"+
			"consequence: MAXIMUM declares its borders this way and its styled golden pins six spans under \"border\"; changing it moves a default golden that no feature asked to move, against invariant 1.\n"+
			"remedy: fall back to \"border\" when the node declares no border style token.\nstyled frame:\n%s", styled)
	}
}

// TestABorderStyleTokenIsHeldToTheTheme records the consequence of drawing the
// token: once the border's declared token reaches the frame, it is a reference
// like any other and the validator's rule applies to it.
//
// This is the check that would have caught the defect from the other side. The
// hardcoded "border" span was itself a token reference the theme does not
// define — SOBRIA has no "border" key — so the renderer was emitting a name no
// theme signs while the validator policed the name the scene wrote. Pinning the
// direction here keeps a future fix from "solving" the mismatch by inventing a
// token in the renderer instead of in the theme.
func TestABorderStyleTokenIsHeldToTheTheme(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(strings.Replace(borderScene, "%s", "not.a.token", 1)))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	errs := scene.ValidateTokens(doc, theme.SOBRIA())
	if len(errs) == 0 {
		t.Fatalf("ValidateTokens accepted the undefined border token %q\n"+
			"consequence: the border style is drawn, so an undefined one silently resolves to no style — exactly the unreadable failure the token validator exists to prevent.\n"+
			"remedy: keep collectTokenErrors checking BorderStyleName against the theme.", "not.a.token")
	}
	if errs[0].Loc.Line == 0 {
		t.Errorf("the border token refusal carries no address\n" +
			"consequence: invariant 4 requires file:line on every refusal.\n" +
			"remedy: populate Loc from the offending node's path.")
	}
}
