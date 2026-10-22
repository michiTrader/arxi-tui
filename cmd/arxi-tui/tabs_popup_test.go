package main

import (
	"strings"
	"testing"
)

// Tabs and a popup written as a scene and nothing else. The tab strip is three buttons; the
// body under it is one of three panes gated on ui.max.is.<tab>; the popup is an overlay gated
// on the same cursor. No Go knows what a "tab" or a "popup" is: the chosen name lives in
// ui.max, which any scene may use as a selector (/max <name>, cmd:/max <name>).
const tabsScene = `{"root":{"type":"stack","children":[
 {"id":"strip","type":"row","children":[
   {"id":"tab_a","type":"button","text":"[ one ]","on_press":"cmd:/max one"},
   {"id":"tab_b","type":"button","text":"[ two ]","on_press":"cmd:/max two"},
   {"id":"tab_c","type":"button","text":"[ help ]","on_press":"cmd:/max help"}]},
 {"id":"body_none","type":"text","text":"BODY DEFAULT","when":"ui.max.none"},
 {"id":"body_one","type":"text","text":"BODY ONE","when":"ui.max.is.one"},
 {"id":"body_two","type":"text","text":"BODY TWO","when":"ui.max.is.two"},
 {"id":"fill","type":"text","text":"","grow":1},
 {"id":"prompt","type":"input","bind":"user.input","prefix":"> "},
 {"id":"help_popup","type":"overlay","anchor":"top","when":"ui.max.is.help","children":[
   {"type":"text","text":"POPUP: press close"},
   {"id":"help_close","type":"button","text":"[ close ]","on_press":"cmd:/max"}]}
]}}`

// frameWith is the last frame that contains want, or "".
func lastFrameWith(raw, want string) string {
	var last string
	for _, f := range strings.Split(raw, frameBegin) {
		if s := stripANSI(f); strings.Contains(s, want) {
			last = s
		}
	}
	return last
}

// A click on a tab button swaps the body under the strip, and a click on the other tab swaps
// it back.
// Consequence: tabs would need a Go widget, so every "show me X or Y" request becomes a PR.
// Remedy: a tab is a button that writes ui.max, and the panes gate on ui.max.is.<name>.
func TestLoopTabsSwapTheBodyOfAScene(t *testing.T) {
	script := append(click(2, 0), click(12, 0)...) // [ one ], then [ two ]
	raw, sub := sceneLoopRaw(t, tabsScene, append(script, exitKeys()...))
	if len(sub) != 0 {
		t.Errorf("the model was sent %q. Consequence: a tab press talks to the model. Remedy: on_press is a command.", sub)
	}
	for _, want := range []string{"BODY DEFAULT", "BODY ONE", "BODY TWO"} {
		if lastFrameWith(raw, want) == "" {
			t.Errorf("no frame shows %q. Consequence: a tab does not switch the body. Remedy: gate each pane on ui.max.is.<tab>.\n%s", want, stripANSI(raw))
		}
	}
	// The body of the first tab must be gone when the second is shown.
	two := lastFrameWith(raw, "BODY TWO")
	if strings.Contains(two, "BODY ONE") || strings.Contains(two, "BODY DEFAULT") {
		t.Errorf("two bodies are shown at once:\n%s", two)
	}
}

// A popup opens over the scene from a button, takes the click on its own button and closes.
// Consequence: a help or confirmation popup cannot be asked of arxi without Go.
// Remedy: an overlay gated on ui.max.is.<name>, closed by cmd:/max.
func TestLoopAPopupOpensAndClosesFromButtons(t *testing.T) {
	script := append(click(26, 0), click(10, 1)...) // [ help ] opens it; its [ close ] row is under the title
	raw, _ := sceneLoopRaw(t, tabsScene, append(script, exitKeys()...))
	if lastFrameWith(raw, "POPUP: press close") == "" {
		t.Fatalf("the popup never appeared. Consequence: a button cannot open a window over the scene. Remedy: gate an overlay on ui.max.is.<name>.\n%s", stripANSI(raw))
	}
	frames := strings.Split(raw, frameBegin)
	closed := false
	seen := false
	for _, f := range frames {
		s := stripANSI(f)
		if strings.Contains(s, "POPUP: press close") {
			seen = true
		} else if seen && strings.Contains(s, "BODY DEFAULT") {
			closed = true
		}
	}
	if !closed {
		t.Errorf("no frame shows the scene again after the popup's close button was clicked. Consequence: a popup cannot be dismissed with the pointer. Remedy: the click must press the button of the overlay.\n%s", stripANSI(raw))
	}
}
