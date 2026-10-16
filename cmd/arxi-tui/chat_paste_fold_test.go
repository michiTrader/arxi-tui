package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// A long paste reads in the chat the way it read in the input bar: as its marker. What is
// SENT is still the real text (the driver sees it, the model reads it, the session file keeps it).

func TestLoopTheChatShowsTheMarkerOfABigPaste(t *testing.T) {
	big := bigPaste(68)
	out, sub := runLoopDoc(t, factorySobria, append([]scheduledEvent{
		at(10, pasteEvent(big)),
		at(150, enterEvent()),
	}, exitKeys()...))
	last := lastFrame(out)
	if !strings.Contains(last, "[Pasted text #1 +68 lines]") {
		t.Errorf("the chat does not show the marker of the paste:\n%s", last)
	}
	if strings.Contains(last, "row ") {
		t.Errorf("the chat shows the pasted lines themselves:\n%s", last)
	}
	if len(sub) != 1 || sub[0] != big {
		t.Fatalf("what was sent must be the real 68 lines, got %q", sub)
	}
}

// Pasting the same text twice opens the marker in the bar; the chat must then show the
// text, exactly what the bar showed.
func TestLoopPastingTwiceShowsTheTextInTheChat(t *testing.T) {
	big := bigPaste(8)
	out, sub := runLoopDoc(t, factorySobria, append([]scheduledEvent{
		at(10, pasteEvent(big)),
		at(100, pasteEvent(big)),
		at(100, enterEvent()),
	}, exitKeys()...))
	last := lastFrame(out)
	if strings.Contains(last, "[Pasted text") {
		t.Errorf("the chat shows a marker for a paste that was opened in the bar:\n%s", last)
	}
	if !strings.Contains(last, "row ") {
		t.Errorf("the chat does not show the opened text:\n%s", last)
	}
	if len(sub) != 1 || sub[0] != big {
		t.Fatalf("sent %q", sub)
	}
}

// Words typed around the marker stay where they were.
func TestLoopTextAroundAMarkerReadsTheSameInTheChat(t *testing.T) {
	big := bigPaste(10)
	script := append(typed("mira esto: "), at(20, pasteEvent(big)))
	script = append(script, typed(" gracias")...)
	script = append(script, at(100, enterEvent()))
	out, sub := runLoopDoc(t, factorySobria, append(script, exitKeys()...))
	last := lastFrame(out)
	if !strings.Contains(last, "mira esto: [Pasted text #1 +10 lines] gracias") {
		t.Errorf("chat line differs from the bar's:\n%s", last)
	}
	if len(sub) != 1 || sub[0] != "mira esto: "+big+" gracias" {
		t.Fatalf("sent %q", sub)
	}
}

// A small paste, or an ordinary line, is not touched.
func TestLoopASmallLineIsDrawnAsTyped(t *testing.T) {
	out, _ := runLoopDoc(t, factorySobria, append(append(typed("hola"), at(50, enterEvent())), exitKeys()...))
	if last := lastFrame(out); !strings.Contains(last, "hola") {
		t.Errorf("the line is missing:\n%s", last)
	}
}

func TestSentPasteOnlyTouchesTheLineItWasRememberedFor(t *testing.T) {
	var s sentPaste
	s.remember("a [Pasted text #1 +9 lines] b", "a LONG b")
	other := fold.Event{Type: "run.prompt", Payload: map[string]any{"text": "something else"}}
	if got := s.forChat(other).Payload["text"]; got != "something else" {
		t.Errorf("another line was rewritten: %v", got)
	}
	// that prompt spent the memory: the words typed by hand later are not rewritten
	if got := s.forChat(fold.Event{Type: "run.prompt", Payload: map[string]any{"text": "a LONG b"}}); got.Payload["text"] != "a LONG b" {
		t.Errorf("a later line with the same words was rewritten: %v", got.Payload["text"])
	}
	s.remember("a [Pasted text #1 +9 lines] b", "a LONG b")
	notPrompt := fold.Event{Type: "llm.response", Payload: map[string]any{"text": "a LONG b"}}
	if got := s.forChat(notPrompt).Payload["text"]; got != "a LONG b" {
		t.Errorf("an answer was rewritten: %v", got)
	}
	e := fold.Event{Type: "run.prompt", Payload: map[string]any{"text": "a LONG b"}}
	got := s.forChat(e)
	if got.Payload["text"] != "a [Pasted text #1 +9 lines] b" {
		t.Errorf("the prompt kept its real text: %v", got.Payload["text"])
	}
	if e.Payload["text"] != "a LONG b" {
		t.Error("the original event was modified; the session file must keep the real text")
	}
	// spent: the same text typed again by hand is not rewritten
	if again := s.forChat(e); again.Payload["text"] != "a LONG b" {
		t.Errorf("the memory outlived its line: %v", again.Payload["text"])
	}
	// equal means nothing to hide
	s.remember("same", "same")
	if got := s.forChat(fold.Event{Type: "run.prompt", Payload: map[string]any{"text": "same"}}); got.Payload["text"] != "same" {
		t.Error("equal texts rewritten")
	}
}
