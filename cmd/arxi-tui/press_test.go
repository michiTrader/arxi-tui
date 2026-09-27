package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

// pressDoc builds a scene from JSON for the press tests, failing loudly if it
// does not parse — a probe that does not parse measures the harness.
func pressDoc(t *testing.T, body string) *scene.Document {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: the probe scene must parse; got %v", err)
	}
	return doc
}

// The Tab ring is the pressable nodes in document order (Q19 "scene order"). A
// node with no id cannot be a focus target (ui.focus names an id), a node with
// no on_press is not pressable, and a hidden node is skipped — so the order and
// the membership are both asserted here, because getting either wrong sends Tab
// to the wrong node or to none.
func TestPressableIDsInSceneOrderSkippingHiddenAndIdless(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"first","type":"text","text":"1","on_press":"cmd:/a"},
	  {"type":"text","text":"no id","on_press":"cmd:/b"},
	  {"id":"plain","type":"text","text":"not pressable"},
	  {"id":"hidden","type":"text","text":"h","on_press":"cmd:/c"},
	  {"id":"last","type":"text","text":"2","on_press":"cmd:/d"}
	]}}`)

	got := pressableIDs(doc, map[string]bool{"hidden": true})
	want := []string{"first", "last"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("pressableIDs = %v, want %v.\n"+
			"consequence: Tab would move focus to the wrong set of nodes — an id-less node cannot\n"+
			"be focused (ui.focus names an id), a node with no on_press is not pressable, and a\n"+
			"hidden node is not on screen. Any of those in the ring points Tab at a dead target.\n"+
			"remedy: collect only nodes with both an id and an on_press, skipping hidden subtrees,\n"+
			"in document order.", got, want)
	}
}

// advanceFocus treats the empty string as the input's home slot: Tab from the
// input lands on the first pressable node, Tab from the last returns to the
// input, and Shift-Tab from the input wraps to the last. This is what keeps the
// typing flow one Tab away (Q19).
func TestAdvanceFocusRingWithInputHome(t *testing.T) {
	ids := []string{"a", "b", "c"}
	cases := []struct {
		current string
		forward bool
		want    string
	}{
		{"", true, "a"},   // input -> first
		{"a", true, "b"},  // first -> second
		{"c", true, ""},   // last -> input home
		{"", false, "c"},  // input -> last (wrap back)
		{"a", false, ""},  // first -> input home
		{"b", false, "a"}, // second -> first
	}
	for _, tc := range cases {
		if got := advanceFocus(ids, tc.current, tc.forward); got != tc.want {
			t.Errorf("advanceFocus(%v, %q, forward=%v) = %q, want %q.\n"+
				"consequence: the focus ring skips a node or never returns to the input, so a user\n"+
				"either cannot reach a button or cannot get back to typing — the flow Q19 protects.",
				ids, tc.current, tc.forward, got, tc.want)
		}
	}
}

// Tab and Shift-Tab move the cursor through focusKey; the input buffer is
// untouched (Tab is navigation, not text). handled must be true so the key does
// not fall through to typeKey and get typed as a literal tab.
func TestFocusKeyTabMovesTheCursor(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"one","type":"text","text":"1","on_press":"cmd:/a"},
	  {"id":"two","type":"text","text":"2","on_press":"cmd:/b"}
	]}}`)
	notice := ""

	handled, input, focus := focusKey(term.Key{Type: term.KeyTab}, "typed", "", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if !handled {
		t.Fatal("Tab was not handled, so it would fall through to typeKey and insert a literal tab into the buffer")
	}
	if input != "typed" {
		t.Errorf("Tab changed the input buffer to %q; navigation must not edit text", input)
	}
	if focus != "one" {
		t.Errorf("Tab from the input home moved focus to %q, want the first pressable node %q", focus, "one")
	}

	_, _, back := focusKey(term.Key{Type: term.KeyTab, Mod: term.ModShift}, "typed", "", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if back != "two" {
		t.Errorf("Shift-Tab from the input home moved focus to %q, want the last pressable node %q", back, "two")
	}
}

// Enter while a node holding a focus: action is focused sets the cursor to the
// named node — the declarative twin of cmd:/focus. The input is cleared, as a
// submitted action leaves no text behind.
func TestFocusKeyEnterDispatchesFocusAction(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"go","type":"text","text":"Go","on_press":"focus:target"},
	  {"id":"target","type":"text","text":"T","on_press":"cmd:/x"}
	]}}`)
	notice := ""

	handled, input, focus := focusKey(term.Key{Type: term.KeyEnter}, "", "go", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if !handled {
		t.Fatal("Enter on a focused button was not handled, so the button cannot be pressed")
	}
	if focus != "target" {
		t.Errorf("pressing focus:target moved focus to %q, want %q", focus, "target")
	}
	if input != "" {
		t.Errorf("a press left %q in the input buffer; a submitted action leaves no text", input)
	}
}

// A cmd: action that is not a /ui command is submitted to the driver as a
// prompt, exactly as typing the command line would — the Phase-0 contract, so a
// button and a keystroke cannot diverge on what a command means.
func TestFocusKeyEnterDispatchesCmdViaDriver(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"run","type":"text","text":"Run","on_press":"cmd:/agent 5"}
	]}}`)
	notice := ""
	drv := &testDriver{evCh: make(chan fold.Event, 4)}

	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "", "run", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a cmd: button was not handled")
	}
	if len(drv.submitted) != 1 || drv.submitted[0] != "agent 5" {
		t.Errorf("pressing cmd:/agent 5 submitted %v, want one prompt \"agent 5\" (leading slash stripped, as typeKey does)", drv.submitted)
	}
}

// Enter while the input holds focus (uiFocus == "") is NOT focusKey's to take:
// it must fall through so typeKey submits the typed buffer. Capturing it here
// would break ordinary prompting.
func TestFocusKeyEnterOnInputFallsThrough(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"btn","type":"text","text":"B","on_press":"cmd:/a"}
	]}}`)
	notice := ""

	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "hello", "", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if handled {
		t.Error("Enter was captured while the input held focus; it must fall through so typeKey submits the buffer")
	}
}

// answer: is recognised but not yet actioned — the behavioral driver channel is
// Block I. The press reports the deferral rather than dropping silently, and the
// focus cursor does not move.
func TestDispatchAnswerIsDeferredWithNotice(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"OK","on_press":"answer:approve"}}`)
	notice := ""

	focus := dispatchPress("answer:approve", "btn", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if focus != "btn" {
		t.Errorf("an answer: press moved focus to %q; it should leave the cursor where it was", focus)
	}
	if !strings.Contains(notice, "Block I") {
		t.Errorf("an answer: press left notice %q, want it to name the deferral (Block I) so the press is not a silent no-op", notice)
	}
}

// A focus: naming no node in the scene leaves the cursor unchanged and reports
// it — never a crash, never a jump to a phantom id.
func TestDispatchFocusUnknownNodeReportsAndKeepsFocus(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"x","id":"here","on_press":"focus:nowhere"}}`)
	notice := ""

	focus := dispatchPress("focus:nowhere", "here", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if focus != "here" {
		t.Errorf("a focus: to an unknown node moved focus to %q; it must leave the cursor where it was", focus)
	}
	if !strings.Contains(notice, "nowhere") {
		t.Errorf("a focus: to an unknown node left notice %q, want it to name the missing id", notice)
	}
}

// fakeRouter records the ext: presses routed to it, standing in for the live
// supervisor.Registry the loop holds. It lets the press tests prove dispatch
// reaches the router with the plugin id and action split correctly, without
// spawning a subprocess.
type fakeRouter struct {
	calls []routedAction
	err   error
}

type routedAction struct {
	pluginID string
	action   string
	args     map[string]string
}

func (r *fakeRouter) SendAction(pluginID, action string, args map[string]string) error {
	r.calls = append(r.calls, routedAction{pluginID, action, args})
	return r.err
}

// An ext: press routes to the plugin action router with the plugin id and action
// name that ParseAction split off, and leaves the focus cursor where it was (a
// press is not a focus move). This is I4's core: the dispatcher reaches the
// behavioral plugin rather than dropping the press or crashing.
func TestDispatchExtRoutesToPluginRouter(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"R","id":"btn","on_press":"ext:tick:refresh"}}`)
	notice := ""
	router := &fakeRouter{}

	focus := dispatchPress("ext:tick:refresh", "btn", &doc, &notice, map[string]bool{}, nil, nil, router, context.Background(), &testDriver{})
	if focus != "btn" {
		t.Errorf("an ext: press moved focus to %q; a plugin press is not a focus move and must leave the cursor put", focus)
	}
	if len(router.calls) != 1 {
		t.Fatalf("an ext: press routed %d actions, want exactly 1; the dispatcher must reach the plugin router, not drop the press", len(router.calls))
	}
	if got := router.calls[0]; got.pluginID != "tick" || got.action != "refresh" {
		t.Errorf("ext:tick:refresh routed to plugin %q action %q, want plugin \"tick\" action \"refresh\"; the id and action were split wrong or swapped", got.pluginID, got.action)
	}
	if notice != "" {
		t.Errorf("a successful ext: press left notice %q, want none; a routed action is not an error", notice)
	}
}

// A router error (the plugin is unmounted, ungranted, or down) is reported to the
// user, never a crash and never a silent drop — the §I-G placeholder-not-crash
// rule on the input side.
func TestDispatchExtReportsRouterError(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"R","id":"btn","on_press":"ext:tick:refresh"}}`)
	notice := ""
	router := &fakeRouter{err: errors.New("no behavioral plugin with that id is mounted: \"tick\"")}

	focus := dispatchPress("ext:tick:refresh", "btn", &doc, &notice, map[string]bool{}, nil, nil, router, context.Background(), &testDriver{})
	if focus != "btn" {
		t.Errorf("a refused ext: press moved focus to %q; it must leave the cursor put", focus)
	}
	if !strings.Contains(notice, "mounted") {
		t.Errorf("a refused ext: press left notice %q, want the router's reason so the press is not a silent no-op", notice)
	}
}

// With no router attached (the loop context that mounts no plugins), an ext:
// press still reports rather than dropping silently or panicking on a nil
// interface.
func TestDispatchExtWithNoRouterReports(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"R","id":"btn","on_press":"ext:tick:refresh"}}`)
	notice := ""

	focus := dispatchPress("ext:tick:refresh", "btn", &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if focus != "btn" {
		t.Errorf("an ext: press with no router moved focus to %q; it must leave the cursor put", focus)
	}
	if !strings.Contains(notice, "ext:tick:refresh") {
		t.Errorf("an ext: press with no router left notice %q, want it to name the action rather than drop silently or panic", notice)
	}
}
