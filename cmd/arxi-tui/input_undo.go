package main

import (
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/term"
)

// The input bar remembers what was done to it, so ctrl+z can take it back.
//
// It is a host-owned piece of view state like the buffer and the caret it records: the
// loop notes each edit after the key that made it has run, and ctrl+z / ctrl+y move
// along the notes. A step is a word, not a letter -- typing "hello world" is two steps,
// a run of Backspaces is one, a paste, a wiped line (ctrl+c) or a recalled line is one
// each -- which is what an editor does and what makes the key worth pressing.
//
// Sending a line forgets the notes: the next ctrl+z must not bring a sent message back
// into the bar.

// undoMax bounds the memory of steps so a long session cannot grow it without end.
const undoMax = 200

type inputState struct {
	text  string
	caret int
}

// editKind is what sort of edit a key made; consecutive edits of one sort join into a
// single step.
type editKind int

const (
	editOther  editKind = iota // paste, wipe, recall, word delete: always a step of its own
	editType                   // typed characters
	editDelete                 // Backspace / Delete
)

type inputUndo struct {
	past, future []inputState
	last         editKind // the kind of the step still open, valid only while open
	open         bool     // the last step may still take more edits of the same kind
}

// kindOf classifies the key that was just dispatched.
func kindOf(k term.Key) editKind {
	switch {
	case k.Type == term.KeyRunes && k.Mod&(term.ModCtrl|term.ModAlt) == 0:
		return editType
	case (k.Type == term.KeyBackspace || k.Type == term.KeyDelete) && k.Mod&(term.ModCtrl|term.ModAlt) == 0:
		return editDelete
	}
	return editOther
}

// note records that a key of the given kind took the bar from before to after. Nothing
// is recorded when the text did not change (a caret move only closes the open step, so
// the next letter starts a new one).
func (u *inputUndo) note(before, after inputState, kind editKind, typed string) {
	if before.text == after.text {
		u.open = false
		return
	}
	if !(u.open && u.last == kind && kind != editOther) {
		u.past = append(u.past, before)
		if len(u.past) > undoMax {
			u.past = u.past[len(u.past)-undoMax:]
		}
	}
	u.future = nil
	u.last = kind
	u.open = kind != editOther
	// A space or a newline ends the word: what is typed next is the next step.
	if kind == editType {
		for _, r := range typed {
			if unicode.IsSpace(r) {
				u.open = false
			}
		}
	}
}

// forget drops every note: the line was sent, or the conversation moved on.
func (u *inputUndo) forget() { *u = inputUndo{} }

// undo steps back from cur, reporting whether there was a step to take.
func (u *inputUndo) undo(cur inputState) (inputState, bool) {
	if len(u.past) == 0 {
		return cur, false
	}
	prev := u.past[len(u.past)-1]
	u.past = u.past[:len(u.past)-1]
	u.future = append(u.future, cur)
	u.open = false
	return prev, true
}

// redo undoes an undo.
func (u *inputUndo) redo(cur inputState) (inputState, bool) {
	if len(u.future) == 0 {
		return cur, false
	}
	next := u.future[len(u.future)-1]
	u.future = u.future[:len(u.future)-1]
	u.past = append(u.past, cur)
	u.open = false
	return next, true
}

// undoKey and redoKey are the keys the bar answers to. They sit behind the user's own
// shortcuts in the loop, so a key the user bound to something else is theirs.
func isUndoKey(k term.Key) bool { return ctrlLetter(k, 'z') }
func isRedoKey(k term.Key) bool { return ctrlLetter(k, 'y') }

func ctrlLetter(k term.Key, c rune) bool {
	return k.Type == term.KeyRunes && len(k.Runes) == 1 && k.Runes[0] == c && k.Mod&term.ModCtrl != 0 && k.Mod&term.ModAlt == 0
}
