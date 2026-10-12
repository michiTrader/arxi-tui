package main

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// A local warning is the host telling the user something about the line they just
// typed: a command that does not exist, one that is not available yet. It is an
// ordinary chat.warn event, so the fold and the renderer already draw it under the
// warning marker, but it is appended to the loop's own log and never sent through the
// driver's channel. That is the whole mechanism that keeps it out of the model's
// history (the chat session builds that from its own turns) and out of the saved
// conversations (sessionLog.Record only sees what arrives on the driver's channel),
// and it is why /clear and /resume, which replace the log, drop it too.
//
// There is deliberately no "chat.local" event type: the fold has no case for one,
// and a second type for the same line would be a second place to forget it.

// localWarnMax is the most runes a local warning holds. Warnings repeat what the user
// typed, and a pasted megabyte would otherwise sit in the transcript and be folded and
// wrapped again on every frame.
const localWarnMax = 400

// localWarning builds the chat.warn event for text. The text usually quotes the user's
// own input, so it is made safe before it becomes an event: an escape sequence in a
// warning would be written to the terminal as a command, and a carriage return could
// overwrite the line it sits on. The result is one plain line, cut to localWarnMax.
func localWarning(text string) fold.Event {
	return fold.Event{
		Type:    "chat.warn",
		Actor:   "host",
		Payload: map[string]any{"text": cleanWarnText(text)},
	}
}

// cleanWarnText strips escape sequences and control characters, turns line breaks and
// tabs into single spaces, drops the characters that reorder text on screen, and cuts
// the result to localWarnMax runes, ending it with an ellipsis when it was cut.
func cleanWarnText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = ansi.Strip(s)
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == ' ':
			space = true
			continue
		case unicode.IsControl(r) || isBidiControl(r):
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	out := b.String()
	if utf8.RuneCountInString(out) <= localWarnMax {
		return out
	}
	runes := []rune(out)
	return strings.TrimRight(string(runes[:localWarnMax-1]), " ") + "…"
}

// isBidiControl reports the characters that change the order text is drawn in, so a
// quoted name cannot be made to read differently from what it is ("evil\u202egpj.exe").
func isBidiControl(r rune) bool {
	switch {
	case r >= 0x202A && r <= 0x202E: // embeddings and overrides
		return true
	case r >= 0x2066 && r <= 0x2069: // isolates
		return true
	case r == 0x200E || r == 0x200F || r == 0x061C: // directional marks
		return true
	}
	return false
}

// appendLocalWarning returns log with a local warning for text added at its end. It is
// what the loop calls on its own event log: the new event is numbered after the last one
// so the log stays in order, and the slice passed in is never written to.
func appendLocalWarning(log []fold.Event, text string) []fold.Event {
	e := localWarning(text)
	if n := len(log); n > 0 {
		e.Seq = log[n-1].Seq + 1
	}
	out := make([]fold.Event, 0, len(log)+1)
	out = append(out, log...)
	return append(out, e)
}

// localNoteMax and localNoteLines bound what a command may print into the conversation,
// so a command that prints a book cannot make every frame fold and wrap it again.
const (
	localNoteMax   = 4000
	localNoteLines = 40
)

// localNote builds the chat.note event for text: what a command of the user's said. Unlike
// a warning it keeps its line breaks (a list is a list), and like one it is made safe
// first: the text came from a program or a service, so escape sequences, control
// characters and the ones that reorder text are removed, and it is cut to a size.
func localNote(text string) fold.Event {
	return fold.Event{
		Type:    "chat.note",
		Actor:   "host",
		Payload: map[string]any{"text": cleanNoteText(text)},
	}
}

func cleanNoteText(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = ansi.Strip(s)
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(s)
	var b strings.Builder
	lines, runes := 1, 0
	for _, r := range s {
		if r == '\n' {
			if lines >= localNoteLines {
				return strings.TrimRight(b.String(), " \n") + "…"
			}
			lines++
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) || isBidiControl(r) {
			continue
		}
		if runes >= localNoteMax {
			return strings.TrimRight(b.String(), " \n") + "…"
		}
		runes++
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " \n")
}

// appendLocalNote is appendLocalWarning for a note.
func appendLocalNote(log []fold.Event, text string) []fold.Event {
	e := localNote(text)
	if n := len(log); n > 0 {
		e.Seq = log[n-1].Seq + 1
	}
	out := make([]fold.Event, 0, len(log)+1)
	out = append(out, log...)
	return append(out, e)
}
