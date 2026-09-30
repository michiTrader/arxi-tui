package main

import (
	"context"
	"errors"
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

// fakeDecider records the verb, item and text answerInbox routed, so a test can
// assert the kind→verb mapping without a live serveDriver or a subprocess.
type fakeDecider struct {
	verb   string
	itemID string
	text   string
	err    error
}

func (f *fakeDecider) ApproveInboxItem(_ context.Context, itemID string) error {
	f.verb, f.itemID = "approve", itemID
	return f.err
}

func (f *fakeDecider) RejectInboxItem(_ context.Context, itemID, reason string) error {
	f.verb, f.itemID, f.text = "reject", itemID, reason
	return f.err
}

func (f *fakeDecider) ReplyInboxItem(_ context.Context, itemID, text string) error {
	f.verb, f.itemID, f.text = "reply", itemID, text
	return f.err
}

// TestAnswerInboxRoutesEachKindToItsVerb pins the kind→verb mapping: approve
// reaches approve, reject reaches reject, reply reaches reply, each with the item
// id, and the text lands on the two text-bearing verbs but not approve. If this
// fails, a pressed approve button could fire reject (or answer the wrong item),
// the exact button/verb drift the single-switch join exists to prevent.
func TestAnswerInboxRoutesEachKindToItsVerb(t *testing.T) {
	cases := []struct {
		kind     string
		wantVerb string
		wantText string // the text the verb should have received
	}{
		{"approve", "approve", ""},        // approve ignores the text
		{"reject", "reject", "no thanks"}, // reject carries the text as its reason
		{"reply", "reply", "no thanks"},   // reply carries the text as its answer
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			f := &fakeDecider{}
			if err := answerInbox(context.Background(), c.kind, "item-7", "no thanks", f); err != nil {
				t.Fatalf("answerInbox(%q): unexpected error %v", c.kind, err)
			}
			if f.verb != c.wantVerb {
				t.Errorf("answer:%s routed to verb %q, want %q; a press fired the wrong inbox verb", c.kind, f.verb, c.wantVerb)
			}
			if f.itemID != "item-7" {
				t.Errorf("answer:%s routed item %q, want \"item-7\"; the verb would answer the wrong item", c.kind, f.itemID)
			}
			if f.text != c.wantText {
				t.Errorf("answer:%s carried text %q, want %q; approve must ignore the text and the text-bearing verbs must receive it", c.kind, f.text, c.wantText)
			}
		})
	}
}

// TestAnswerInboxSurfacesTheDriverRefusal pins that a driver refusal reaches the
// caller rather than being swallowed. If this fails, an approve that the core
// rejected (a stale item, a finished run) would read at the press as success.
func TestAnswerInboxSurfacesTheDriverRefusal(t *testing.T) {
	want := errors.New("ndjson: inbox.approve: not_found")
	f := &fakeDecider{err: want}
	if err := answerInbox(context.Background(), "approve", "item-7", "", f); !errors.Is(err, want) {
		t.Errorf("answerInbox: got %v, want the driver refusal %v surfaced to the caller", err, want)
	}
}

// TestAnswerInboxRefusesAnUnknownKind pins the exhaustiveness guard: a kind
// outside the closed vocabulary is named, not silently dropped. Counterfactual
// run by hand: replacing the default arm with `return nil` makes this pass
// silently while a signed-but-unrouted kind does nothing — the AGENTS.md missing
// switch variant failure this message exists to catch.
func TestAnswerInboxRefusesAnUnknownKind(t *testing.T) {
	f := &fakeDecider{}
	if err := answerInbox(context.Background(), "escalate", "item-7", "", f); err == nil {
		t.Error("answerInbox: got nil for an unknown kind, want a named refusal; a kind not in the closed set must not be a silent no-op")
	}
	if f.verb != "" {
		t.Errorf("answerInbox: an unknown kind routed to verb %q, want no verb; it must reach no driver method", f.verb)
	}
}
