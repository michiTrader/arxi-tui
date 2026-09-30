package main

import (
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// TestInboxItemIDReturnsTheBlockedItem pins the answerable case: a run blocked on
// an item with an inbox_id resolves to that id. If this fails, an answer: press
// has no item to target and the approve/reject/reply buttons cannot address the
// item the run is waiting on.
func TestInboxItemIDReturnsTheBlockedItem(t *testing.T) {
	state := fold.State{BlockedRef: map[string]any{"inbox_id": "abc123"}}
	id, ok := inboxItemID(state)
	if !ok {
		t.Fatal("inboxItemID: got ok=false for a blocked_ref carrying an inbox_id; " +
			"an answer: press would have no item to answer and the button would report 'nothing to answer' while the run is in fact blocked")
	}
	if id != "abc123" {
		t.Errorf("inboxItemID: got id %q, want \"abc123\"; the driver would address the wrong inbox item", id)
	}
}

// TestInboxItemIDRefusesWhenNothingIsBlocked pins the empty-state: no blocked_ref
// means nothing to answer. If this fails, an answer: press with no pending item
// would send an empty item id to the driver, asking the core to approve nothing.
func TestInboxItemIDRefusesWhenNothingIsBlocked(t *testing.T) {
	if id, ok := inboxItemID(fold.State{}); ok || id != "" {
		t.Errorf("inboxItemID: got (%q, %v) for a nil blocked_ref, want (\"\", false); "+
			"a press with no blocked item must resolve to nothing to answer, not an empty item id sent onward", id, ok)
	}
}

// TestInboxItemIDRefusesAMissingInboxID pins that a blocked_ref without an
// inbox_id key is not answerable. A block can be on a lock, budget, or timer
// (BINDS.md §4.2) rather than an approval, so a ref that names no inbox item must
// not be treated as one. If this fails, a non-approval block would be answered as
// if it were an inbox item, addressing an id that is not there.
func TestInboxItemIDRefusesAMissingInboxID(t *testing.T) {
	state := fold.State{BlockedRef: map[string]any{"kind": "tool"}}
	if id, ok := inboxItemID(state); ok || id != "" {
		t.Errorf("inboxItemID: got (%q, %v) for a blocked_ref with no inbox_id, want (\"\", false); "+
			"a block on something other than an approval names no inbox item to answer", id, ok)
	}
}

// TestInboxItemIDRefusesANonStringInboxID pins the type guard: blocked_ref is a
// map[string]any decoded from JSON, so a malformed inbox_id (a number, an object)
// must not panic or coerce. If this fails, the type assertion is unguarded and a
// non-string id either crashes the press or is sent to the driver as a zero value.
func TestInboxItemIDRefusesANonStringInboxID(t *testing.T) {
	state := fold.State{BlockedRef: map[string]any{"inbox_id": 42}}
	if id, ok := inboxItemID(state); ok || id != "" {
		t.Errorf("inboxItemID: got (%q, %v) for a non-string inbox_id, want (\"\", false); "+
			"an unguarded assertion would panic on a press or send a zero-valued id", id, ok)
	}
}

// TestInboxItemIDRefusesAnEmptyInboxID pins the empty-string guard — the
// load-bearing one this project's wrong-frame rule motivates. A present-but-empty
// inbox_id reads as ok under a bare `_, ok := m[k].(string)` check, so the empty
// guard is what stops "" from reaching the driver's inbox verbs (which would then
// refuse an empty item, but only after the request left the client naming
// nothing). Counterfactual run by hand: dropping `|| id == ""` from inboxItemID
// makes this case return ("", true) and fail here.
func TestInboxItemIDRefusesAnEmptyInboxID(t *testing.T) {
	state := fold.State{BlockedRef: map[string]any{"inbox_id": ""}}
	if id, ok := inboxItemID(state); ok || id != "" {
		t.Errorf("inboxItemID: got (%q, %v) for an empty inbox_id, want (\"\", false); "+
			"an empty id sent onward asks the core to answer nothing, the wrong-frame failure this guard exists to prevent", id, ok)
	}
}
