package term

import (
	"strconv"
	"strings"
	"testing"
)

// The decoder's contract has two halves and this file checks both. One: a sequence
// produces the name a user will bind, which is the only thing any layer above sees.
// Two: it produces it whether the bytes arrive in one read or one at a time, because
// a terminal splitting an escape sequence across two reads is not an edge case, it is
// what happens when the machine is busy.

// keyTable is bytes in, binding name out. It is written as names rather than as Key
// literals on purpose: a table of structs would test the decoder against itself,
// while a table of names tests it against the thing the user types into a config.
var keyTable = []struct {
	in   string
	want string
}{
	{"a", "a"},
	{"A", "A"}, // shift+a is a capital letter and nothing else; no terminal says otherwise
	{"\x01", "ctrl+a"},
	{"\x1a", "ctrl+z"},
	{"\x00", "ctrl+space"},
	{"\x1f", "ctrl+_"},
	{"\r", "enter"},
	// LF is ctrl+j and not a second spelling of enter, which is what makes a second line
	// reachable on a terminal that cannot send shift+enter: Windows Terminal and conhost
	// send this byte for ctrl+enter. Folding it into enter here is the mistake this row
	// exists to catch, and it is the mistake that made ctrl+enter submit the line.
	{"\n", "ctrl+j"},
	{"\t", "tab"},
	{"\x7f", "backspace"},
	{"\x1b\x7f", "alt+backspace"},
	// The second line of the input rests on the rows above and below. shift+enter is what
	// every editor binds and a legacy terminal cannot send — the modifier is dropped before
	// the byte leaves, so it is 0x0d, which is enter — whereas ESC CR is unambiguous
	// everywhere, and the CSI u rows are those chords once a terminal will encode them.
	{"\x1b\r", "alt+enter"},
	{"\x1ba", "alt+a"},
	{"\x1b\x1b", "esc"},
	{"\x1b[A", "up"},
	{"\x1b[B", "down"},
	{"\x1b[C", "right"},
	{"\x1b[D", "left"},
	{"\x1bOA", "up"},
	{"\x1bOD", "left"},
	{"\x1b[H", "home"},
	{"\x1b[F", "end"},
	{"\x1b[1~", "home"},
	{"\x1b[4~", "end"},
	{"\x1bOH", "home"},
	{"\x1b[2~", "insert"},
	{"\x1b[3~", "delete"},
	{"\x1b[5~", "pgup"},
	{"\x1b[6~", "pgdown"},
	{"\x1b[Z", "shift+tab"},
	{"\x1bOP", "f1"},
	{"\x1bOS", "f4"},
	{"\x1b[15~", "f5"},
	{"\x1b[21~", "f10"},
	{"\x1b[24~", "f12"},
	{"\x1b[1;5D", "ctrl+left"},
	{"\x1b[1;3C", "alt+right"},
	{"\x1b[1;2A", "shift+up"},
	{"\x1b[1;4A", "alt+shift+up"},
	{"\x1b[1;8B", "ctrl+alt+shift+down"},
	{"\x1b[3;5~", "ctrl+delete"},
	{"\x1b[1;2P", "shift+f1"},
	{"\x1b[1;5R", "ctrl+f3"},
	{"\x1b[97;5u", "ctrl+a"},      // the Kitty protocol reports the key, not the character
	{"\x1b[13;2u", "shift+enter"}, // a keypress the legacy encoding cannot express at all
	{"\x1b[13;5u", "ctrl+enter"},  // nor this one, and the input's second line is bound to both
	{"\x1b[27;3u", "alt+esc"},
	// The other side of asking for the disambiguate flag — arxi-sim's Emitter.Enter
	// enabled it on entry, and the enable side here belongs to raw-mode entry, not
	// yet written: a terminal that grants it stops sending the legacy bytes for these
	// three and sends the key instead. Nothing above this file may notice the
	// difference — esc is still esc, and it now arrives whole rather than as a lone
	// 0x1b waiting out a 50 ms timeout.
	{"\x1b[13u", "enter"},
	{"\x1b[27u", "esc"},
	{"\x1b[127u", "backspace"},

	// The wheel. It arrives as a mouse report and leaves as a key, so it belongs in this
	// table with everything else: what the layers above see is a name they can bind.
	// Both encodings are here because a terminal answers our request for SGR coordinates
	// with whichever one it knows, and the legacy form is the dangerous half — its three
	// coordinate bytes are printable ASCII, so a report that is not consumed whole is
	// typed into the input line. The byte-by-byte test then proves it waits for all three.
	{"\x1b[<64;10;5M", "wheelup"},
	{"\x1b[<65;10;5M", "wheeldown"},
	{"\x1b[<68;10;5M", "shift+wheelup"}, // the mouse writes its modifiers straight, not biased by one
	{"\x1b[<80;1;1M", "ctrl+wheelup"},
	{"\x1b[<81;200;60M", "ctrl+wheeldown"},
	{"\x1b[M`!!", "wheelup"},   // X10: button and column and row, each biased by 32
	{"\x1b[Ma!!", "wheeldown"}, // 65 + 32 = 'a', a wheel notch that looks like text
}

func TestDecodeNamesEveryKey(t *testing.T) {
	for _, c := range keyTable {
		evs, rest := Decode([]byte(c.in))
		if len(evs) != 1 {
			t.Errorf("%q decoded to %d events, want 1 (rest %q)", c.in, len(evs), rest)
			continue
		}
		if evs[0].Kind != EventKey {
			t.Errorf("%q decoded to kind %d, want a key", c.in, evs[0].Kind)
			continue
		}
		if got := evs[0].Key.String(); got != c.want {
			t.Errorf("%q decoded to %q, want %q", c.in, got, c.want)
		}
	}
}

// feedByByte pushes bytes through Decode one at a time, carrying the undecoded tail
// forward exactly the way the read loop does. Every intermediate call must report
// nothing: a decoder that guesses early turns a slow terminal into typed garbage.
func feedByByte(t *testing.T, in string) ([]Event, []byte) {
	t.Helper()
	var out []Event
	var rest []byte
	for i := 0; i < len(in); i++ {
		// A fresh buffer each time, like the read loop: rest aliases the previous
		// buffer, so appending to it in place would test something else entirely.
		buf := make([]byte, 0, len(rest)+1)
		buf = append(append(buf, rest...), in[i])
		evs, r := Decode(buf)
		rest = r
		out = append(out, evs...)
	}
	return out, rest
}

func TestDecodeWaitsForTheRestOfASequence(t *testing.T) {
	for _, c := range keyTable {
		whole, _ := Decode([]byte(c.in))
		split, _ := feedByByte(t, c.in)
		if len(split) != len(whole) {
			t.Errorf("%q: %d events byte by byte, %d in one read", c.in, len(split), len(whole))
			continue
		}
		for i := range whole {
			if split[i].Key.String() != whole[i].Key.String() {
				t.Errorf("%q: byte by byte gave %q, one read gave %q",
					c.in, split[i].Key.String(), whole[i].Key.String())
			}
		}
	}
}

// A prefix of a sequence is not a keypress. This is the guarantee that lets the read
// loop keep a tail in the buffer instead of flushing it, and it is what stops a
// resize arriving mid-sequence from spelling "[1;5D" into the input bar.
//
// The X10 mouse report is in the list because it is the one sequence whose tail is not
// self-describing: CSI M says nothing about the three bytes that follow it, so the only
// thing that keeps a wheel notch arriving in two reads from typing "!!" is the decoder
// refusing to finish until it has all three.
func TestDecodePrefixesReportNothing(t *testing.T) {
	for _, seq := range []string{"\x1b[1;5D", "\x1b[15~", "\x1b[97;5u", "\x1bOP", "\x1b[200~",
		"\x1b[M`!!", "\x1b[<64;10;5M"} {
		for i := 1; i < len(seq); i++ {
			evs, rest := Decode([]byte(seq[:i]))
			if len(evs) != 0 {
				t.Errorf("%q (prefix of %q) decoded to %d events, want none", seq[:i], seq, len(evs))
			}
			if string(rest) != seq[:i] {
				t.Errorf("%q kept %q, want the whole prefix back", seq[:i], rest)
			}
		}
	}
}

// names is the shape most assertions want: what the decoder would hand the keymap.
func names(evs []Event) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		switch ev.Kind {
		case EventKey:
			out = append(out, ev.Key.String())
		case EventPaste:
			out = append(out, "paste("+ev.Text+")")
		case EventMouse:
			out = append(out, mouseName(ev.Mouse))
		default:
			out = append(out, "other")
		}
	}
	return out
}

// mouseName is what names calls a pointer report. A mouse event has no String of its own —
// nothing above the decoder needs one, because a pointer is hit-tested and never bound by
// name — so this lives in the test file that wants to read one.
//
// The modifiers are spelled in Key.String's fixed ctrl, alt, shift order so that one table in
// this file can read the same whichever kind of event a row is about. Every field is in the
// string on purpose: an assertion that named only the action would pass on a report whose
// column was off by the one-based bias, which is the mistake this encoding invites.
func mouseName(m Mouse) string {
	var b strings.Builder
	if m.Mod&ModCtrl != 0 {
		b.WriteString("ctrl+")
	}
	if m.Mod&ModAlt != 0 {
		b.WriteString("alt+")
	}
	if m.Mod&ModShift != 0 {
		b.WriteString("shift+")
	}
	b.WriteString([...]string{"press", "drag", "release"}[m.Action])
	b.WriteString("(" + [...]string{"left", "middle", "right"}[m.Button] + " ")
	b.WriteString(strconv.Itoa(m.Col) + "," + strconv.Itoa(m.Row) + ")")
	return b.String()
}

func TestDecodeTextRuns(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// A burst is one event. Typing fast, and a paste from a terminal that does not
		// support bracketed paste, both arrive as a run, and one event per rune would
		// mean one frame per rune.
		{"hello", []string{"hello"}},
		{"héllo", []string{"héllo"}},
		// ESC modifies exactly one key. The rest of the run is a separate event, and
		// getting this wrong spells alt+the-whole-paragraph.
		{"\x1bhello", []string{"alt+h", "ello"}},
		// A control byte cuts the run without being swallowed by it.
		{"ab\x01cd", []string{"ab", "ctrl+a", "cd"}},
		{"hi\rthere", []string{"hi", "enter", "there"}},
	}
	for _, c := range cases {
		evs, rest := Decode([]byte(c.in))
		if got := names(evs); !equal(got, c.want) {
			t.Errorf("%q decoded to %v, want %v", c.in, got, c.want)
		}
		if len(rest) != 0 {
			t.Errorf("%q left %q behind", c.in, rest)
		}
	}
}

// A rune split across two reads must wait; a byte that is not UTF-8 at all must be
// dropped. Those look the same at the first byte and the difference is whether the
// decoder makes progress: treating garbage as "incomplete" wedges it forever, and a
// terminal that stops responding to keys is the worst bug this package can have.
func TestDecodeHandlesBrokenUTF8(t *testing.T) {
	evs, rest := Decode([]byte{0xc3})
	if len(evs) != 0 || len(rest) != 1 {
		t.Errorf("half a rune decoded to %v with rest %q, want nothing held whole", names(evs), rest)
	}
	evs, rest = Decode([]byte{0xc3, 0xa9})
	if got := names(evs); !equal(got, []string{"é"}) || len(rest) != 0 {
		t.Errorf("a rune in two reads decoded to %v rest %q, want [é]", got, rest)
	}
	evs, rest = Decode([]byte{0xff, 'a'})
	if got := names(evs); !equal(got, []string{"a"}) || len(rest) != 0 {
		t.Errorf("a non-UTF-8 byte decoded to %v rest %q, want it dropped and [a] kept", got, rest)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A paste is text, never keys. This is the guarantee that makes pasting a code block
// safe: the escape sequence inside it is content, and a decoder that interpreted it
// would move the cursor and run bindings the user never pressed.
func TestPasteIsTextAndArrivesWhole(t *testing.T) {
	body := "first\r\nsecond\r\x1b[Athird"
	// CR becomes LF and the escape sequence stays exactly as it arrived: it is content.
	want := "paste(first\nsecond\n\x1b[Athird)"
	in := pasteStart + body + pasteEnd

	evs, rest := Decode([]byte(in))
	if got := names(evs); !equal(got, []string{want}) {
		t.Errorf("a paste in one read decoded to %v, want [%s]", got, want)
	}
	if len(rest) != 0 {
		t.Errorf("a paste in one read left %q behind", rest)
	}
	// The same paste split at every possible point, which is what a paste larger than
	// one read actually is.
	evs, rest = feedByByte(t, in)
	if got := names(evs); !equal(got, []string{want}) {
		t.Errorf("a paste byte by byte decoded to %v, want [%s]", got, want)
	}
	if len(rest) != 0 {
		t.Errorf("a paste byte by byte left %q behind", rest)
	}
	// An empty paste is still a paste: the user pressed the keys, and a widget that
	// counts events must see it.
	evs, _ = Decode([]byte(pasteStart + pasteEnd))
	if got := names(evs); !equal(got, []string{"paste()"}) {
		t.Errorf("an empty paste decoded to %v, want [paste()]", got)
	}
	// A keypress after the paste is a keypress, not part of it.
	evs, _ = Decode([]byte(in + "\x1b[A"))
	if got := names(evs); !equal(got, []string{want, "up"}) {
		t.Errorf("paste then up decoded to %v, want [%s up]", got, want)
	}
}

// What a terminal says when nobody asked. Every one of these is consumed whole and
// reported as nothing: the failure mode is not a missed event, it is "1;2R" appearing
// in the input bar because a reply fell through to the text decoder.
func TestRepliesAreConsumedAndReportNothing(t *testing.T) {
	for _, in := range []string{
		"\x1b[24;80R",                    // cursor position report, the CSI R ambiguity
		"\x1b[?62;1;2c",                  // primary device attributes
		"\x1b[<64;10;5m",                 // a wheel release, which no terminal sends and which is not a notch
		"\x1b[<66;10;5M",                 // a horizontal notch, dropped rather than aimed at a vertical one
		"\x1b[<128;10;5M",                // the extra buttons some mice have
		"\x1b[<192;10;5M",                // an extra button with the notch bit set too: bit 7 decides, not bit 6
		"\x1b[<35;10;5M",                 // motion with no button down: 1003's, which we never ask for
		"\x1b[<0;10M",                    // a press with no row is not a press
		"\x1b[<0;0;5M",                   // column zero, which one-based counting leaves no cell for
		"\x1b[<0;10;0M",                  // and row zero, which would be hit-tested against a rectangle
		"\x1b[M !!",                      // a press in the legacy encoding, which is wheel-only here
		"\x1b[Mb!!",                      // a horizontal notch, legacy
		"\x1b[2M",                        // CSI 2 M is DL, not a mouse report: parameters tell them apart
		"\x1b[?1u",                       // the Kitty keyboard flags
		"\x1b[8;24;80t",                  // a window report
		"\x1b]11;rgb:1e1e/1e1e/1e1e\x07", // OSC 11 background, BEL-terminated
		"\x1b]10;rgb:0/0/0\x1b\\",        // OSC 10 foreground, ST-terminated
		"\x1bP>|xterm(400)\x1b\\",        // DCS version report
	} {
		evs, rest := Decode([]byte(in))
		if len(evs) != 0 {
			t.Errorf("%q reported %v, want nothing", in, names(evs))
		}
		if len(rest) != 0 {
			t.Errorf("%q left %q behind, want it all consumed", in, rest)
		}
	}
}

// The pointer, which is the other half of the previous test: those reports are consumed and
// forgotten, these become events. Both lists are needed, because the failure that matters is a
// report drifting from one to the other — a button silently dropped is a scrollbar that cannot
// be grabbed, and a report claimed that should not be is a click somewhere else on the screen
// moving the transcript.
//
// The table is written as names rather than as Mouse literals for the reason keyTable is: a
// struct table would compare the decoder against a transcription of itself, and the two fields
// most easily got wrong here are the two a literal would copy without thinking — the one-based
// bias on the cell, and modifier bits that are written straight where every key parameter in
// this file's other half is biased by one.
var mouseTable = []struct {
	in   string
	want string
}{
	// Press, motion and release: the three shapes a drag is made of. Bit 5 is what makes the
	// middle one motion, and the final byte is what makes the last one a release.
	{"\x1b[<0;10;5M", "press(left 9,4)"},
	{"\x1b[<32;10;5M", "drag(left 9,4)"},
	{"\x1b[<0;10;5m", "release(left 9,4)"},
	// A release with the motion bit still set is a release. Terminals do send this — the button
	// comes up while the pointer is moving — and reading the bits in the other order would spell
	// the end of a drag as more of one, leaving the thumb held by a hand that let go.
	{"\x1b[<32;10;5m", "release(left 9,4)"},
	// The origin, which is the one cell where the bias could go negative rather than merely
	// wrong, and 1;1 is what a terminal sends for the top-left corner.
	{"\x1b[<0;1;1M", "press(left 0,0)"},
	// A column no legacy report can name. 199 is past 223-32, which is why the X10 encoding is
	// wheel-only: the right edge of a wide window is exactly where the scrollbar lives.
	{"\x1b[<0;200;60M", "press(left 199,59)"},
	// The other two buttons. They are decoded and named rather than dropped, because dropping
	// them here would mean the layer above could not tell a right-click from nothing at all.
	{"\x1b[<1;5;9M", "press(middle 4,8)"},
	{"\x1b[<2;5;9M", "press(right 4,8)"},
	{"\x1b[<33;5;9M", "drag(middle 4,8)"},
	{"\x1b[<34;5;9m", "release(right 4,8)"},
	// The modifiers, one bit at a time and then all three. Through modOf — the reader the key
	// path uses — 4 would come out alt and 8 would come out ctrl|shift, so each of these rows
	// fails loudly if the two readers are ever collapsed into one.
	{"\x1b[<4;10;5M", "shift+press(left 9,4)"},
	{"\x1b[<8;10;5M", "alt+press(left 9,4)"},
	{"\x1b[<16;10;5M", "ctrl+press(left 9,4)"},
	{"\x1b[<28;10;5M", "ctrl+alt+shift+press(left 9,4)"},
	{"\x1b[<48;10;5m", "ctrl+release(left 9,4)"},
}

func TestDecodeReportsThePointer(t *testing.T) {
	for _, c := range mouseTable {
		evs, rest := Decode([]byte(c.in))
		if got := names(evs); !equal(got, []string{c.want}) {
			t.Errorf("%q decoded to %v, want [%s]", c.in, got, c.want)
		}
		if len(rest) != 0 {
			t.Errorf("%q left %q behind", c.in, rest)
		}
		// Byte by byte as well, because a drag is the one gesture that floods the pipe: reports
		// arrive faster than the loop reads, so a report split across two reads is the normal
		// case here rather than the unlucky one.
		evs, rest = feedByByte(t, c.in)
		if got := names(evs); !equal(got, []string{c.want}) {
			t.Errorf("%q byte by byte decoded to %v, want [%s]", c.in, got, c.want)
		}
		if len(rest) != 0 {
			t.Errorf("%q byte by byte left %q behind", c.in, rest)
		}
	}
}

// A drag arrives as a run, and every report in it must survive the read. The app folds a whole
// read into one frame, so a decoder that stopped at the first report would move the thumb one
// row per read no matter how far the pointer went — the pill would lag behind the finger and
// catch up only when the hand stopped.
func TestDecodeReportsEveryStepOfADrag(t *testing.T) {
	in := "\x1b[<0;10;5M\x1b[<32;10;6M\x1b[<32;10;7M\x1b[<32;10;8m"
	want := []string{"press(left 9,4)", "drag(left 9,5)", "drag(left 9,6)", "release(left 9,7)"}
	evs, rest := Decode([]byte(in))
	if got := names(evs); !equal(got, want) {
		t.Errorf("a drag in one read decoded to %v, want %v", got, want)
	}
	if len(rest) != 0 {
		t.Errorf("a drag in one read left %q behind", rest)
	}
	// And a keypress in the middle of one stays a keypress in the middle of one: ctrl+c during
	// a drag is still the door out.
	evs, _ = Decode([]byte("\x1b[<32;10;6M\x03\x1b[<32;10;7M"))
	want = []string{"drag(left 9,5)", "ctrl+c", "drag(left 9,6)"}
	if got := names(evs); !equal(got, want) {
		t.Errorf("a key mid-drag decoded to %v, want %v", got, want)
	}
}

// default keymap is written as strings and the user's config is too. A name that
// prints one way and parses another is a binding that silently never fires.
func TestEveryKeyNameRoundTrips(t *testing.T) {
	var keys []Key
	for _, m := range []Mod{0, ModShift, ModAlt, ModCtrl, ModShift | ModAlt,
		ModShift | ModCtrl, ModAlt | ModCtrl, ModShift | ModAlt | ModCtrl} {
		for tp := range keyNames {
			keys = append(keys, Key{Type: tp, Mod: m})
		}
		for _, r := range []rune{'a', 'Z', '_', ' ', 'é', '中'} {
			keys = append(keys, Key{Type: KeyRunes, Runes: []rune{r}, Mod: m})
		}
	}
	for _, k := range keys {
		s := k.String()
		got, ok := ParseKey(s)
		if !ok {
			t.Errorf("%q does not parse back", s)
			continue
		}
		if got.Type != k.Type || got.Mod != k.Mod || string(got.Runes) != string(k.Runes) {
			t.Errorf("%q parsed to %+v, want %+v", s, got, k)
		}
	}
	// A run of text is not a binding, and ParseKey has to say so rather than inventing
	// a key nobody can press.
	if _, ok := ParseKey("hello"); ok {
		t.Error(`ParseKey("hello") succeeded; a text burst is not a bindable name`)
	}
	if _, ok := ParseKey("ctrl+nonesuch"); ok {
		t.Error(`ParseKey("ctrl+nonesuch") succeeded; an unknown name must fail loudly`)
	}
}

// flushPartial is what the 50 ms timeout does to a tail nobody finished. The read loop
// owns the clock; this is the decision it makes when the clock runs out.
func TestFlushPartialResolvesTheEscAmbiguity(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// The key, alone, which is the whole reason a timeout exists.
		{"\x1b", []string{"esc"}},
		// alt+[ is byte-identical to the start of a CSI. Nothing but time can tell
		// them apart, and after 50 ms this is the right answer.
		{"\x1b[", []string{"alt+["}},
		{"\x1bO", []string{"alt+O"}},
		// The honest cost, written down: a real sequence that stalled mid-parameter
		// gets read as the text it looks like. Every library with a working esc key
		// pays this, and the alternative is an esc that needs a second keypress.
		{"\x1b[1;5", []string{"alt+[1;5"}},
	}
	for _, c := range cases {
		evs, rest := flushPartial([]byte(c.in))
		if got := names(evs); !equal(got, c.want) {
			t.Errorf("a stalled %q flushed to %v, want %v", c.in, got, c.want)
		}
		// Whatever it decided, the buffer is empty afterwards. A tail that survives a
		// flush is a tail that gets flushed again, forever.
		if len(rest) != 0 {
			t.Errorf("a stalled %q left %q in the buffer", c.in, rest)
		}
	}
	// Not our business: the loop only arms the timer on an ESC, and a tail that is not
	// one is handed back untouched rather than guessed at.
	if evs, rest := flushPartial([]byte("ab")); len(evs) != 0 || string(rest) != "ab" {
		t.Errorf("flushPartial(%q) = %v, %q; want it left alone", "ab", names(evs), rest)
	}
}

// The decoder must always make progress. Every bound in it — the CSI parameter limit,
// the control byte that ends a scan, the byte that is not UTF-8 — exists so that a
// terminal cannot put it in a state where it holds bytes forever, which the user
// experiences as a keyboard that stopped working.
func TestDecodeAlwaysMakesProgress(t *testing.T) {
	inputs := []string{
		"\x1b[" + strings.Repeat("1", 200), // more parameters than any real sequence
		"\x1b[\x01A",                       // a control byte inside a CSI
		"\x1b\x1b\x1b\x1b",                 // esc held down
		string([]byte{0xff, 0xfe, 0xfd}),   // not UTF-8 at all
		"\x1b[999999999999999999999~",      // a number no int wants
		"\x1b[0u",                          // Kitty reporting codepoint zero
	}
	for _, in := range inputs {
		evs, rest := Decode([]byte(in))
		if len(rest) >= len(in) {
			t.Errorf("%q made no progress: %d bytes in, %d held (%v)", in, len(in), len(rest), names(evs))
		}
	}
}
