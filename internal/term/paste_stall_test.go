package term

import (
	"os"
	"strings"
	"testing"
	"time"
)

// pipeTTY is a TTY reading from a pipe, so the real decode loop (timers included) runs.
func pipeTTY(t *testing.T) (*TTY, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	tty := &TTY{in: r, out: w, events: make(chan Event, 64), done: make(chan struct{})}
	t.Cleanup(func() { close(tty.done); r.Close(); w.Close() })
	go tty.read()
	return tty, w
}

func collectFor(tty *TTY, d time.Duration) []Event {
	var evs []Event
	end := time.After(d)
	for {
		select {
		case e := <-tty.events:
			evs = append(evs, e)
		case <-end:
			return evs
		}
	}
}

// A long paste can reach the program in pieces with a pause between them (a busy window, a
// slow console). The pause used to outlast csiTimeout, the half already received was thrown
// away, and the rest was read as typing: its newlines sent messages, and an ESC in it
// (pasted terminal output has them) cancelled the turn in flight - with nobody touching
// the keyboard. The paste must arrive whole, as one paste, and nothing else.
func TestAPasteThatPausesInTheMiddleArrivesWholeAndTypesNothing(t *testing.T) {
	tty, w := pipeTTY(t)
	first := "primera parte del mensaje pegado \x1b[31mroja\x1b[0m"
	second := " segunda parte\nconEnter y \x1b otra"
	w.WriteString("\x1b[200~" + first)
	time.Sleep(csiTimeout + 150*time.Millisecond)
	w.WriteString(second + "\x1b[201~")
	evs := collectFor(tty, 300*time.Millisecond)

	var pastes int
	for _, e := range evs {
		switch e.Kind {
		case EventPaste:
			pastes++
			if e.Text != pasteText(first+second) {
				t.Errorf("the paste is not whole: %q", e.Text)
			}
		default:
			t.Errorf("a paste that paused produced a %v event (%v): it was typed", e.Kind, e.Key)
		}
	}
	if pastes != 1 {
		t.Errorf("%d paste events, want 1", pastes)
	}
}

// A paste whose end never comes (the terminal died, the marker was lost) must not hold the
// keyboard for ever: after pasteTimeout what arrived is delivered as a paste, not dropped.
func TestAPasteThatNeverEndsIsDeliveredNotDropped(t *testing.T) {
	saved := pasteTimeout
	pasteTimeout = 120 * time.Millisecond
	t.Cleanup(func() { pasteTimeout = saved })
	tty, w := pipeTTY(t)
	w.WriteString("\x1b[200~lo que llego")
	evs := collectFor(tty, 700*time.Millisecond)
	if len(evs) != 1 || evs[0].Kind != EventPaste || !strings.Contains(evs[0].Text, "lo que llego") {
		t.Fatalf("events = %+v; want the received text as one paste", evs)
	}
}

func TestOpenPasteMeansBegunAndNotEnded(t *testing.T) {
	for in, want := range map[string]bool{
		"\x1b[200~abc":          true,
		"\x1b[200~":             true,
		"\x1b[200~abc\x1b[201~": false, // whole: it is decoded, not waited for
		"\x1b[20":               false, // a cut marker is an ordinary stalled sequence
		"\x1b":                  false,
		"\x1b[31mred":           false,
		"text \x1b[200~":        false,
	} {
		if got := openPaste([]byte(in)); got != want {
			t.Errorf("openPaste(%q) = %v, want %v", in, got, want)
		}
	}
}
