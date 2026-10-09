package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/term"
)

func barEv(d time.Duration, e term.Event) scheduledEvent { return scheduledEvent{d, e} }

func send() scheduledEvent { return barEv(10*time.Millisecond, enterEvent()) }
func undoKey() scheduledEvent {
	return barEv(10*time.Millisecond, ctrlCharEvent('z'))
}
func redoKey() scheduledEvent {
	return barEv(10*time.Millisecond, ctrlCharEvent('y'))
}

func runBar(t *testing.T, parts ...[]scheduledEvent) []string {
	t.Helper()
	t.Setenv(historyEnv, t.TempDir())
	t.Setenv(configDirEnv, t.TempDir())
	var script []scheduledEvent
	for _, p := range parts {
		script = append(script, p...)
	}
	return runLoopKeys(t, append(script, exitKeys()...))
}

func one(e scheduledEvent) []scheduledEvent { return []scheduledEvent{e} }

// ctrl+z takes the last word back, one step at a time, and the program stays open: the
// loop went on to read the Enter that sent the line.
func TestCtrlZTakesBackTheLastWordTypedInTheBar(t *testing.T) {
	got := runBar(t, typed("hello world"), one(undoKey()), one(send()))
	if want := []string{"hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q: ctrl+z should take back the last word and leave the bar open", got, want)
	}
}

func TestCtrlZTwiceTakesBackBothWords(t *testing.T) {
	got := runBar(t, typed("hello world"), one(undoKey()), one(undoKey()), typed("x"), one(send()))
	if want := []string{"x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q", got, want)
	}
}

func TestCtrlYPutsBackWhatCtrlZTook(t *testing.T) {
	got := runBar(t, typed("hello world"), one(undoKey()), one(redoKey()), one(send()))
	if want := []string{"hello world"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q", got, want)
	}
}

func TestTypingAfterAnUndoDropsWhatCouldHaveBeenPutBack(t *testing.T) {
	got := runBar(t, typed("ab cd"), one(undoKey()), typed("z"), one(redoKey()), one(send()))
	if want := []string{"ab z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q: a new edit ends the redo", got, want)
	}
}

func TestARunOfBackspacesIsOneStep(t *testing.T) {
	bs := barEv(5*time.Millisecond, term.Event{Kind: term.EventKey, Key: term.Key{Type: term.KeyBackspace}})
	got := runBar(t, typed("hello"), []scheduledEvent{bs, bs, bs}, one(undoKey()), one(send()))
	if want := []string{"hello"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q: three Backspaces in a row are one thing to take back", got, want)
	}
}

func TestAPasteIsOneStep(t *testing.T) {
	got := runBar(t, typed("a "), one(barEv(10*time.Millisecond, pasteEvent("pasted text"))), one(undoKey()), one(send()))
	if want := []string{"a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q", got, want)
	}
}

// A message that was sent is not the bar's to give back.
func TestCtrlZDoesNotBringASentLineBack(t *testing.T) {
	got := runBar(t, typed("sent"), one(send()), one(undoKey()), one(send()), typed("next"), one(send()))
	if want := []string{"sent", "next"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q", got, want)
	}
}

func TestCtrlZWithNothingToTakeBackDoesNothing(t *testing.T) {
	got := runBar(t, one(undoKey()), typed("ok"), one(send()))
	if want := []string{"ok"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("submitted %q, want %q", got, want)
	}
}

// The user's own shortcut on the key is theirs: ctrl+z means undo only when nobody
// has said otherwise.
func TestAShortcutOnCtrlZBeatsTheUndo(t *testing.T) {
	t.Setenv(configDirEnv, t.TempDir())
	b := behaviour{Keys: map[string]actionList{"ctrl+z": {"cmd:/effort max"}}}
	if _, ok := b.boundActions(term.Key{Type: term.KeyRunes, Runes: []rune{'z'}, Mod: term.ModCtrl}); !ok {
		t.Fatal("a shortcut on ctrl+z is no longer answered first, so a user who bound it would still get the undo")
	}
	if err := b.validate(); err != nil {
		t.Fatalf("ctrl+z is a key a user may bind, and was refused: %v", err)
	}
}

func TestTheUndoStepsGroupByWord(t *testing.T) {
	var u inputUndo
	text := ""
	for _, r := range "ab cd" {
		before := inputState{text, len([]rune(text))}
		text += string(r)
		u.note(before, inputState{text, len([]rune(text))}, editType, string(r))
	}
	if len(u.past) != 2 {
		t.Fatalf("%d steps for two words, want 2", len(u.past))
	}
}

func TestTheUndoMemoryIsBounded(t *testing.T) {
	var u inputUndo
	text := ""
	for i := 0; i < undoMax*3; i++ {
		before := inputState{text, 0}
		text += "x"
		u.note(before, inputState{text, 0}, editOther, "")
	}
	if len(u.past) != undoMax {
		t.Fatalf("%d steps kept, want %d", len(u.past), undoMax)
	}
}
