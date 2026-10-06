package term

import (
	"testing"
	"time"
)

// A terminal that is interrupted in the middle of a sequence must never see the rest of
// it typed into the input line.
func TestFlushPartialDropsUnfinishedSequences(t *testing.T) {
	for _, tail := range []string{
		"\x1b[<0;10;5",   // mouse report, cut before the final byte
		"\x1b[13;2",      // kitty key, cut
		"\x1b[200",       // paste marker, cut
		"\x1b[?1004",     // a mode report
		"\x1b]11;rgb:00", // an OSC reply with no terminator
		"\x1b[1;5",       // modified arrow
	} {
		evs, rest := flushPartial([]byte(tail))
		if len(evs) != 0 || len(rest) != 0 {
			t.Errorf("%q: got %d events, %d left over; want nothing", tail, len(evs), len(rest))
		}
	}
}

func TestFlushPartialKeepsRealKeys(t *testing.T) {
	cases := map[string]KeyType{"\x1b": KeyEscape}
	for in, want := range cases {
		evs, _ := flushPartial([]byte(in))
		if len(evs) != 1 || evs[0].Key.Type != want {
			t.Errorf("%q: %+v", in, evs)
		}
	}
	// alt+[ and alt+x are still keys.
	for _, in := range []string{"\x1b[", "\x1bx"} {
		evs, _ := flushPartial([]byte(in))
		if len(evs) != 1 || evs[0].Key.Mod&ModAlt == 0 {
			t.Errorf("%q: %+v", in, evs)
		}
	}
}

func TestStallForWaitsLongerOnASequence(t *testing.T) {
	if got := stallFor([]byte("\x1b")); got != escTimeout {
		t.Errorf("lone esc waits %v", got)
	}
	if got := stallFor([]byte("\x1b[<0;1")); got != csiTimeout {
		t.Errorf("a cut mouse report waits %v", got)
	}
	if csiTimeout <= escTimeout || csiTimeout > time.Second {
		t.Errorf("csiTimeout %v is out of range", csiTimeout)
	}
}

// Whatever the chunking, a sequence decodes to the same thing as when whole, and
// nothing is typed.
func TestSplitSequencesNeverLeakText(t *testing.T) {
	seqs := []string{
		"\x1b[<0;10;5M", "\x1b[<0;10;5m", "\x1b[<64;3;3M", "\x1b[13;2u", "\x1b[1;5A",
		"\x1b[A", "\x1b[I", "\x1b[O", "\x1b[200~hi\x1b[201~", "\x1b[?1;2c",
	}
	for _, s := range seqs {
		for cut := 1; cut < len(s); cut++ {
			var rest []byte
			var evs []Event
			for _, part := range [][]byte{[]byte(s[:cut]), []byte(s[cut:])} {
				buf := append(append([]byte{}, rest...), part...)
				var got []Event
				got, rest = Decode(buf)
				evs = append(evs, got...)
			}
			whole, _ := Decode([]byte(s))
			if len(evs) != len(whole) {
				t.Errorf("%q cut at %d: %d events, whole gives %d", s, cut, len(evs), len(whole))
				continue
			}
			for i := range evs {
				if evs[i].Kind == EventKey && evs[i].Key.Type == KeyRunes && whole[i].Key.Type != KeyRunes {
					t.Errorf("%q cut at %d typed %q", s, cut, string(evs[i].Key.Runes))
				}
			}
		}
	}
}
