package main

import (
	"strings"
	"testing"
)

// A side panel written as a scene and nothing else: a row of a list of the shells (left) and
// the chat (right), with the input under both. This is acceptance scenario 13 of
// docs/DESIGN-OPEN-TUI.md, with the shell list standing in for the project's files.
const sidePanelScene = `{"root":{"type":"stack","children":[
 {"type":"row","grow":1,"children":[
   {"id":"side","type":"list","bind":"host.shell.rows","weight":1,"row_template":{"type":"row","children":[
     {"id":"side_pick","type":"text","bind":"row.command","on_press":"cmd:/shell show {row.id}"}]}},
   {"id":"main","type":"text","text":"MAIN AREA","weight":3}]},
 {"id":"prompt","type":"input","bind":"user.input","prefix":"> "}
]}}`

// The list and the chat share rows: a row of the screen holds a shell of the list and a line of
// the main area.
// Consequence: a side panel (files, shells, notes) cannot be asked of arxi without a Go layout.
// Remedy: a `row` splits its width by `weight`, and its children are drawn side by side.
func TestLoopASidePanelSharesRowsWithTheMainArea(t *testing.T) {
	script := typed("x")
	raw, _ := sceneLoopRaw(t, sidePanelScene, script)
	frames := strings.Split(raw, frameBegin)
	var last string
	for _, f := range frames {
		s := stripANSI(f)
		if strings.Contains(s, "simulated command 1") {
			last = s
		}
	}
	if last == "" {
		t.Fatalf("no frame lists the shells. Consequence: the panel is not drawn. Remedy: a list inside a row must render.\n%s", stripANSI(raw))
	}
	shared := false
	for _, line := range strings.Split(last, "\n") {
		if strings.Contains(line, "simulated command 1") && strings.Contains(line, "MAIN AREA") {
			shared = true
			// 80 columns split 1:3 give the panel 20: the main area starts at column 20.
			if col := len([]rune(line[:strings.Index(line, "MAIN AREA")])); col != 20 {
				t.Errorf("the main area starts at column %d, want 20. Consequence: the panel is not the width its weight says. Remedy: divide the row by weight.", col)
			}
		}
	}
	if !shared {
		t.Errorf("no row holds both a shell and the main area. Consequence: the panel pushes the main area down instead of sitting beside it. Remedy: render row children side by side.\n%s", last)
	}
}

// A row of a side panel presses like a row of a window: a click on it shows that shell in the
// chat that sits beside the panel.
// Consequence: a docked panel looks alive and answers neither the keyboard nor the pointer.
// Remedy: the Hit of a pressable row survives the side-by-side join of a `row`.
func TestLoopClickingARowOfASidePanelPressesIt(t *testing.T) {
	const withChat = `{"root":{"type":"stack","children":[
 {"type":"row","grow":1,"children":[
   {"id":"side","type":"list","bind":"host.shell.rows","weight":1,"row_template":{"type":"row","children":[
     {"id":"side_pick","type":"text","bind":"row.command","on_press":"cmd:/shell show {row.id}"}]}},
   {"id":"chat","type":"markdown","bind":"chat.history","weight":3}]},
 {"id":"prompt","type":"input","bind":"user.input","prefix":"> "}
]}}`
	script := append(click(3, 1), click(3, 1)...) // the second shell, twice: the first may land before the first frame
	raw, sub := sceneLoopRaw(t, withChat, append(script, exitKeys()...))
	out := stripANSI(raw)
	if len(sub) != 0 {
		t.Errorf("the model was sent %q. Consequence: a click on a panel talks to the model. Remedy: on_press is a command.", sub)
	}
	if !strings.Contains(out, "simulated output of shell 2") {
		t.Errorf("a click on the second row of the side panel showed nothing. Consequence: a docked panel is dead to the pointer. Remedy: keep Hit when a row joins its children.\n%s", out)
	}
	if strings.Contains(out, "simulated output of shell 1") {
		t.Errorf("shell 1's output was shown though the second row was clicked. Consequence: the click pressed the wrong row. Remedy: key a row by its index.")
	}
}
