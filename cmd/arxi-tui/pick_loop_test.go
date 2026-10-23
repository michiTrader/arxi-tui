package main

import (
	"strings"
	"testing"
)

// Two tab sets on one screen, each with a selector of its own. The first strip is [ a1 ] [ a2 ]
// on row 0 and the second [ b1 ] [ b2 ] on row 1; each has its body, and the default body shows
// while the group has no choice (ui.pick.none.<group>).
const pickScene2 = `{"root":{"type":"stack","children":[
 {"id":"strip_a","type":"row","children":[
   {"id":"a1","type":"button","text":"[ a1 ]","on_press":"cmd:/pick ga one"},
   {"id":"a2","type":"button","text":"[ a2 ]","on_press":"cmd:/pick ga two"}]},
 {"id":"strip_b","type":"row","children":[
   {"id":"b1","type":"button","text":"[ b1 ]","on_press":"cmd:/pick gb one"},
   {"id":"b2","type":"button","text":"[ b2 ]","on_press":"cmd:/pick gb two"}]},
 {"id":"a_none","type":"text","text":"A-DEFAULT","when":"ui.pick.none.ga"},
 {"id":"a_one","type":"text","text":"A-ONE","when":"ui.pick.is.ga.one"},
 {"id":"a_two","type":"text","text":"A-TWO","when":"ui.pick.is.ga.two"},
 {"id":"b_none","type":"text","text":"B-DEFAULT","when":"ui.pick.none.gb"},
 {"id":"b_one","type":"text","text":"B-ONE","when":"ui.pick.is.gb.one"},
 {"id":"b_two","type":"text","text":"B-TWO","when":"ui.pick.is.gb.two"},
 {"id":"fill","type":"text","text":"","grow":1},
 {"id":"prompt","type":"input","bind":"user.input","prefix":"> "}
]}}`

// Two tab sets share one screen and a click in one never changes the other.
// Consequence: with ui.max as the only selector, picking a tab in the second set replaced the
// first set's choice, so a screen with two tab strips could not be built.
// Remedy: each group has its own selector (/pick <group> <name>, ui.pick.is.<group>.<name>).
func TestLoopTwoTabSetsKeepTheirOwnChoice(t *testing.T) {
	script := append(click(2, 0), click(12, 1)...) // [ a1 ], then [ b2 ]
	script = append(script, click(12, 0)...)       // [ a2 ]: the second set must stay on b2
	raw, sub := sceneLoopRaw(t, pickScene2, append(script, exitKeys()...))
	if len(sub) != 0 {
		t.Errorf("the model was sent %q.\nConsequence: a tab press talks to the model.\nRemedy: /pick is a host command.", sub)
	}
	has := func(f string, want ...string) bool {
		for _, w := range want {
			if !strings.Contains(f, w) {
				return false
			}
		}
		return true
	}
	if f := lastFrameWith(raw, "A-DEFAULT"); !has(f, "A-DEFAULT", "B-DEFAULT") {
		t.Errorf("the screen did not start with both sets on their default:\n%s", stripANSI(raw))
	}
	// After a1 and b2 both choices are on screen together.
	var both string
	for _, f := range strings.Split(raw, frameBegin) {
		if s := stripANSI(f); has(s, "A-ONE", "B-TWO") && !strings.Contains(s, "A-DEFAULT") && !strings.Contains(s, "B-DEFAULT") {
			both = s
		}
	}
	if both == "" {
		t.Fatalf("no frame shows A-ONE and B-TWO together.\nConsequence: the second set's click replaced the first set's choice.\nRemedy: give each group its own selector.\n%s", stripANSI(raw))
	}
	// After a2 the first set moved and the second did not.
	var last string
	for _, f := range strings.Split(raw, frameBegin) {
		if s := stripANSI(f); has(s, "A-TWO", "B-TWO") {
			last = s
		}
	}
	if last == "" {
		t.Fatalf("no frame shows A-TWO and B-TWO together.\nConsequence: choosing in one set changed the other.\nRemedy: write only the group named by /pick.\n%s", stripANSI(raw))
	}
	if strings.Contains(last, "A-ONE") || strings.Contains(last, "B-ONE") || strings.Contains(last, "A-DEFAULT") {
		t.Errorf("two bodies of one set at once:\n%s", last)
	}
}

// The typed form works too: /pick writes the same selector as the pressed button and clears
// with the group alone.
func TestLoopTypedPickWritesAndClearsAGroup(t *testing.T) {
	script := append(typed("/pick ga two"), at(20, enterEvent()))
	script = append(script, typed("/pick ga")...)
	script = append(script, at(20, enterEvent()))
	raw, sub := sceneLoopRaw(t, pickScene2, append(script, exitKeys()...))
	if len(sub) != 0 {
		t.Errorf("a typed /pick reached the model: %q", sub)
	}
	if lastFrameWith(raw, "A-TWO") == "" {
		t.Errorf("/pick ga two never showed A-TWO:\n%s", stripANSI(raw))
	}
	seen := false
	back := false
	for _, f := range strings.Split(raw, frameBegin) {
		s := stripANSI(f)
		if strings.Contains(s, "A-TWO") {
			seen = true
		} else if seen && strings.Contains(s, "A-DEFAULT") {
			back = true
		}
	}
	if !back {
		t.Errorf("/pick ga did not bring the default body back:\n%s", stripANSI(raw))
	}
}
