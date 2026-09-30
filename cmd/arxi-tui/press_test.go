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

	handled, input, focus := focusKey(term.Key{Type: term.KeyTab}, "typed", "", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if !handled {
		t.Fatal("Tab was not handled, so it would fall through to typeKey and insert a literal tab into the buffer")
	}
	if input != "typed" {
		t.Errorf("Tab changed the input buffer to %q; navigation must not edit text", input)
	}
	if focus != "one" {
		t.Errorf("Tab from the input home moved focus to %q, want the first pressable node %q", focus, "one")
	}

	_, _, back := focusKey(term.Key{Type: term.KeyTab, Mod: term.ModShift}, "typed", "", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
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

	handled, input, focus := focusKey(term.Key{Type: term.KeyEnter}, "", "go", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
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

	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "", "run", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a cmd: button was not handled")
	}
	if len(drv.submitted) != 1 || drv.submitted[0] != "agent 5" {
		t.Errorf("pressing cmd:/agent 5 submitted %v, want one prompt \"agent 5\" (leading slash stripped, as typeKey does)", drv.submitted)
	}
}

// Tab builds the ring from the live fold, so a template's instantiated rows are
// Tab targets: from the input home, Tab lands on the first pressable node, and
// stepping through reaches the per-row focus keys the row-click work adds. This
// is the wiring proof that focusKey consults focusRing (not the old static
// pressableIDs) — a regression to the static walk would put the raw template id
// in the ring instead of one slot per row.
func TestFocusKeyTabRingIncludesTemplateRows(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"list","bind":"team.members",
	  "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}}}`)
	notice := ""

	// From the input home, Tab lands on row 0's target; a second Tab on row 1's.
	tab := term.Key{Type: term.KeyTab}
	_, _, first := focusKey(tab, "", "", &doc, twoMemberFold(), &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if first != rowFocusKey("go", 0) {
		t.Fatalf("Tab from the input home focused %q, want the row-0 key %q\n"+
			"consequence: a template's instantiated rows are not in the ring, so Tab cannot reach a\n"+
			"pressable row — the row-click work never becomes usable.\n"+
			"remedy: focusKey must build the ring from focusRing over the live fold.", first, rowFocusKey("go", 0))
	}
	_, _, second := focusKey(tab, "", first, &doc, twoMemberFold(), &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if second != rowFocusKey("go", 1) {
		t.Fatalf("Tab from row 0 focused %q, want the row-1 key %q; the two rows are distinct ring slots", second, rowFocusKey("go", 1))
	}
}

// Enter on a focused template row dispatches that row's expanded on_press, not
// the template's verbatim {row.id}: focus names row 1, so the driver must see the
// second member's command. This is the payoff of the whole row-click chain — the
// pure halves resolve the action and the loop fires it against the pressed row.
func TestFocusKeyEnterDispatchesRowPress(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"list","bind":"team.members",
	  "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}}}`)
	notice := ""
	drv := &testDriver{evCh: make(chan fold.Event, 4)}

	handled, input, _ := focusKey(term.Key{Type: term.KeyEnter}, "", rowFocusKey("go", 1), &doc, twoMemberFold(), &notice, map[string]bool{}, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a focused template row was not handled, so a row cannot be pressed")
	}
	if len(drv.submitted) != 1 || drv.submitted[0] != "agent fe" {
		t.Errorf("pressing row 1 submitted %v, want one prompt \"agent fe\" (the row's {row.id} resolved to fe)\n"+
			"consequence: pressing a row dispatches the wrong member's command, or the un-expanded\n"+
			"{row.id} verbatim — the silent-wrong-frame this repo holds worse than an error.\n"+
			"remedy: recover the (node,row) with parseRowFocusKey and dispatch rowPressOnPress's expanded action.", drv.submitted)
	}
	if input != "" {
		t.Errorf("a row press left %q in the input buffer; a submitted action leaves no text", input)
	}
}

// A focus key naming a row that is no longer instantiated (the array shrank under
// the cursor) is reported and swallowed, never submitted as a prompt: the user
// pressed Enter on a focused row, so the buffer must not be sent to the driver.
func TestFocusKeyEnterOnStaleRowIsReportedNotSubmitted(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"list","bind":"team.members",
	  "row_template":{"id":"go","type":"text","bind":"row.role","on_press":"cmd:/agent {row.id}"}}}`)
	notice := ""
	drv := &testDriver{evCh: make(chan fold.Event, 4)}

	// row 5 does not exist in a two-member fold.
	handled, _, focus := focusKey(term.Key{Type: term.KeyEnter}, "", rowFocusKey("go", 5), &doc, twoMemberFold(), &notice, map[string]bool{}, nil, nil, nil, context.Background(), drv)
	if !handled {
		t.Fatal("Enter on a stale row target fell through; it must be swallowed so the buffer is not submitted")
	}
	if len(drv.submitted) != 0 {
		t.Errorf("a stale row press submitted %v to the driver; a row that no longer exists must dispatch nothing", drv.submitted)
	}
	if notice == "" {
		t.Error("a stale row press was silent; the missing row must be reported so the drift is visible")
	}
	if focus != rowFocusKey("go", 5) {
		t.Errorf("a stale row press moved focus to %q; it must leave the cursor where it was", focus)
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

	handled, _, _ := focusKey(term.Key{Type: term.KeyEnter}, "hello", "", &doc, fold.State{}, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if handled {
		t.Error("Enter was captured while the input held focus; it must fall through so typeKey submits the buffer")
	}
}

// answerDriver is a Driver that also answers inbox items, standing in for the
// live serveDriver so a press test can prove an answer: routes to the decider
// without a subprocess. The mock testDriver deliberately does NOT implement
// inboxDecider, so it exercises the "follows no run" branch.
type answerDriver struct {
	testDriver
	verb   string
	itemID string
	text   string
}

func (d *answerDriver) ApproveInboxItem(_ context.Context, itemID string) error {
	d.verb, d.itemID = "approve", itemID
	return nil
}
func (d *answerDriver) RejectInboxItem(_ context.Context, itemID, reason string) error {
	d.verb, d.itemID, d.text = "reject", itemID, reason
	return nil
}
func (d *answerDriver) ReplyInboxItem(_ context.Context, itemID, text string) error {
	d.verb, d.itemID, d.text = "reply", itemID, text
	return nil
}

// blockedState returns a fold.State with one item blocked on an approval, the
// shape inboxItemID sources an answer: press's target from (agent.blocked.blocked_ref).
func blockedState(inboxID string) fold.State {
	return fold.State{BlockedRef: map[string]any{"inbox_id": inboxID}}
}

// An answer: press with no pending item reports it and does not route: the item a
// decision answers is the one the run is blocked on, so with nothing blocked
// there is nothing to answer, and the press must not fabricate an item id.
func TestDispatchAnswerWithNoPendingItemReports(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"OK","on_press":"answer:approve"}}`)
	notice := ""
	drv := &answerDriver{}

	focus := dispatchPress("answer:approve", "btn", "", fold.State{}, &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), drv)
	if focus != "btn" {
		t.Errorf("an answer: press moved focus to %q; it should leave the cursor where it was", focus)
	}
	if !strings.Contains(notice, "no inbox item is pending") {
		t.Errorf("an answer: press with nothing blocked left notice %q, want it to name that no item is pending", notice)
	}
	if drv.verb != "" {
		t.Errorf("an answer: press with no pending item routed to verb %q; it must not answer a fabricated item", drv.verb)
	}
}

// An answer: press on a driver that follows no run (the mock, which does not
// implement inboxDecider) reports it rather than dropping silently — the twin of
// the ext: nil-router branch.
func TestDispatchAnswerOnADriverThatFollowsNoRunReports(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"OK","on_press":"answer:approve"}}`)
	notice := ""

	focus := dispatchPress("answer:approve", "btn", "", blockedState("abc123"), &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if focus != "btn" {
		t.Errorf("an answer: press moved focus to %q; it should leave the cursor where it was", focus)
	}
	if !strings.Contains(notice, "cannot be routed") {
		t.Errorf("an answer: press on a non-answering driver left notice %q, want it to name that the press cannot be routed", notice)
	}
}

// An answer: press with a pending item on a driver that answers routes the kind
// to the driver's verb, addressing the item the run is blocked on and carrying
// the typed line as the reply's answer / reject's reason.
func TestDispatchAnswerRoutesToTheDecider(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"OK","on_press":"answer:reply"}}`)
	notice := ""
	drv := &answerDriver{}

	focus := dispatchPress("answer:reply", "btn", "here is my answer", blockedState("abc123"), &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), drv)
	if focus != "btn" {
		t.Errorf("an answer: press moved focus to %q; it should leave the cursor where it was", focus)
	}
	if notice != "" {
		t.Errorf("a routed answer: press left notice %q, want none", notice)
	}
	if drv.verb != "reply" || drv.itemID != "abc123" {
		t.Errorf("answer:reply routed to (verb %q, item %q), want (reply, abc123)", drv.verb, drv.itemID)
	}
	if drv.text != "here is my answer" {
		t.Errorf("answer:reply carried text %q, want the typed line \"here is my answer\"", drv.text)
	}
}

// A focus: naming no node in the scene leaves the cursor unchanged and reports
// it — never a crash, never a jump to a phantom id.
func TestDispatchFocusUnknownNodeReportsAndKeepsFocus(t *testing.T) {
	doc := pressDoc(t, `{"root":{"type":"text","text":"x","id":"here","on_press":"focus:nowhere"}}`)
	notice := ""

	focus := dispatchPress("focus:nowhere", "here", "", fold.State{}, &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
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

	focus := dispatchPress("ext:tick:refresh", "btn", "", fold.State{}, &doc, &notice, map[string]bool{}, nil, nil, router, context.Background(), &testDriver{})
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

	focus := dispatchPress("ext:tick:refresh", "btn", "", fold.State{}, &doc, &notice, map[string]bool{}, nil, nil, router, context.Background(), &testDriver{})
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

	focus := dispatchPress("ext:tick:refresh", "btn", "", fold.State{}, &doc, &notice, map[string]bool{}, nil, nil, nil, context.Background(), &testDriver{})
	if focus != "btn" {
		t.Errorf("an ext: press with no router moved focus to %q; it must leave the cursor put", focus)
	}
	if !strings.Contains(notice, "ext:tick:refresh") {
		t.Errorf("an ext: press with no router left notice %q, want it to name the action rather than drop silently or panic", notice)
	}
}
