package main

import (
	"reflect"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/term"
)

func upKey() scheduledEvent {
	return at(5, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyUp}})
}
func downKey() scheduledEvent {
	return at(5, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyDown}})
}
func newlineKey() scheduledEvent { return at(5, ctrlCharEvent('j')) }
func sendKey() scheduledEvent    { return at(5, enterEvent()) }

func cat(parts ...[]scheduledEvent) []scheduledEvent {
	var out []scheduledEvent
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func one1(e scheduledEvent) []scheduledEvent { return []scheduledEvent{e} }

func runDraft(t *testing.T, parts ...[]scheduledEvent) []string {
	t.Helper()
	t.Setenv(configDirEnv, t.TempDir())
	t.Setenv(historyEnv, t.TempDir())
	_, sub := runLoopDoc(t, factorySobria, append(cat(parts...), exitKeys()...))
	return sub
}

// The owner's loss: a line brought back from the history, edited, then Down on its last
// row (by mistake) used to show the old stash (an empty bar) and the edit was gone. The
// edit is the user's work: Up finds it again, and it is what gets sent.
func TestLoopAnEditedRecalledLineSurvivesAMistakenDown(t *testing.T) {
	got := runDraft(t,
		typed("alpha"), one1(newlineKey()), typed("beta"), one1(sendKey()),
		one1(upKey()), typed(" EDIT"), // the recalled line, edited
		one1(downKey()), // by mistake, on its last row
		one1(upKey()),   // and back: the edit must be there
		one1(sendKey()))
	want := []string{"alpha\nbeta", "alpha\nbeta EDIT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %q, want %q: the edit of the recalled line was lost", got, want)
	}
}

// Walking away from an edited line and back through several lines keeps the edit of each.
func TestLoopEditsOfTwoRecalledLinesAreBothKept(t *testing.T) {
	got := runDraft(t,
		typed("one"), one1(sendKey()), typed("two"), one1(sendKey()),
		one1(upKey()), typed("!"), // "two!"
		one1(upKey()), typed("?"), // "one?"
		one1(downKey()), // back to "two!"
		one1(sendKey()))
	want := []string{"one", "two", "two!"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %q, want %q", got, want)
	}
	got = runDraft(t,
		typed("one"), one1(sendKey()), typed("two"), one1(sendKey()),
		one1(upKey()), typed("!"),
		one1(upKey()), typed("?"),
		one1(downKey()), one1(upKey()), // and back to "one?"
		one1(sendKey()))
	want = []string{"one", "two", "one?"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %q, want %q: the edit of the older line was lost on the way", got, want)
	}
}

// The draft being typed, set aside by Up and brought back by Down, is what is sent.
func TestLoopTheDraftComesBackWithDown(t *testing.T) {
	got := runDraft(t,
		typed("old"), one1(sendKey()),
		typed("draft row one"), one1(newlineKey()), typed("row two"),
		one1(upKey()), one1(upKey()), // row one, then past it: the history
		one1(downKey()), // back to the draft
		one1(sendKey()))
	want := []string{"old", "draft row one\nrow two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %q, want %q", got, want)
	}
}

// A draft left behind when an older line is sent instead stays in the history.
func TestLoopADraftLeftBehindWhenAnOldLineIsSentStaysInTheHistory(t *testing.T) {
	got := runDraft(t,
		typed("old"), one1(sendKey()),
		typed("my precious draft"), one1(upKey()), one1(sendKey()), // sends "old"
		one1(upKey()), one1(upKey()), // the draft is the entry before it
		one1(sendKey()))
	want := []string{"old", "old", "my precious draft"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %q, want %q: the draft was lost", got, want)
	}
}

func TestHistoryEditsAreForgottenOnceALineIsSent(t *testing.T) {
	h := loadHistory("")
	h.Add("a")
	h.Older("")
	h.Newer("a!") // edited "a" kept
	h.Older("")
	if got, _ := h.Older(""); got == "" {
		_ = got
	}
	h.Add("b")
	if h.edits != nil {
		t.Errorf("edits survived a send: %v", h.edits)
	}
	if got, _ := h.Older(""); got != "b" {
		t.Errorf("after a send the newest entry is %q, want b", got)
	}
	if got, _ := h.Older(""); got != "a" {
		t.Errorf("the old edit leaked into the history: %q, want a", got)
	}
}
