package term

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Decoding. The contract is the one every incremental reader wants: given the
// bytes that have arrived, return the events that are certainly complete and the
// tail that is not. A read that lands in the middle of an escape sequence is not an
// error, it is Tuesday — the terminal writes ESC and the rest arrives 200 µs later.
//
// The parse is generic over CSI parameters rather than a map of literal sequences,
// because modifiers multiply: ctrl+shift+alt+left is one code path here and would be
// eight table entries otherwise.

const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// EventKind says which field of an Event to read.
type EventKind uint8

const (
	EventKey EventKind = iota
	EventPaste
	EventResize
	EventClosed
	EventMouse
)

// Event is something that arrived from outside the program. One struct with a kind
// rather than an interface: the app selects on a single channel, and a tagged
// struct keeps that channel from allocating on every keystroke.
type Event struct {
	Kind          EventKind
	Key           Key
	Mouse         Mouse
	Text          string
	Width, Height int
	Err           error
}

// Mouse is a press, a drag or a release, in the cell it happened in.
//
// It arrives only while the program has claimed the mouse, which is not the default: a
// pointer that drags is how a reader selects text, and taking that away to gain a hit test
// is a trade only the reader can make. So these events exist on the far side of a flag, and
// a session that never sets it never sees one.
//
// The wheel is deliberately not here. A notch means "move the view" wherever the pointer
// happens to be, so it is a key — bindable, printable and rebindable by the same table as
// ctrl+up — while these three carry a position, which no keymap can hold. That split is why
// the wheel works on a surface that has claimed nothing.
type Mouse struct {
	Action MouseAction
	Button MouseButton

	// Col and Row are zero-based, from the top left of the frame that is on the screen.
	// The terminal reports them one-based; correcting it here means every reader indexes
	// rows the way the renderer builds them, instead of each one remembering to subtract.
	Col, Row int

	Mod Mod
}

// MouseAction is what the pointer did. A drag is a motion report with a button still down,
// which is the only kind of motion we ask to hear about.
type MouseAction uint8

const (
	MousePress MouseAction = iota
	MouseDrag
	MouseRelease
)

// MouseButton is which button did it. A report names a button in two bits, so there are
// three of them; the extra buttons a mouse may have are dropped rather than numbered, since
// nothing here would know what to do with a fourth.
type MouseButton uint8

const (
	MouseLeft MouseButton = iota
	MouseMiddle
	MouseRight
)

// Decode returns the complete events in b and the bytes left over.
func Decode(b []byte) (evs []Event, rest []byte) {
	for len(b) > 0 {
		ev, n, ok := decodeOne(b)
		if !ok {
			return evs, b
		}
		if ev != nil {
			evs = append(evs, *ev)
		}
		b = b[n:]
	}
	return evs, nil
}

// decodeOne reads one event. ok is false when b is a prefix of something longer, in
// which case the caller must wait for more bytes; a nil event with ok true means the
// bytes were understood and carry nothing we report, which is what a terminal's
// unsolicited replies are.
func decodeOne(b []byte) (ev *Event, n int, ok bool) {
	if b[0] != 0x1b {
		return decodeText(b)
	}
	if len(b) == 1 {
		return nil, 0, false // a lone ESC, or the start of a sequence
	}
	switch b[1] {
	case '[':
		return decodeCSI(b)
	case 'O':
		if len(b) < 3 {
			return nil, 0, false
		}
		if k, ok := ss3Keys[b[2]]; ok {
			return &Event{Kind: EventKey, Key: k}, 3, true
		}
		return nil, 3, true
	case ']', 'P', '^', '_': // OSC, DCS, PM, APC: a reply, not a keypress
		if end := stringTerminator(b); end > 0 {
			return nil, end, true
		}
		return nil, 0, false
	case 0x1b:
		return &Event{Kind: EventKey, Key: Key{Type: KeyEscape}}, 1, true
	}
	// ESC followed by anything else is alt+that.
	inner, n, ok := decodeOne(b[1:])
	if !ok {
		return nil, 0, false
	}
	if inner == nil {
		return nil, n + 1, true
	}
	if inner.Kind == EventKey {
		inner.Key.Mod |= ModAlt
		if inner.Key.Type == KeyRunes && len(inner.Key.Runes) > 1 {
			// ESC modifies exactly one keypress. decodeText greedily took a whole
			// run of printable bytes; the rest of that run is its own event and has
			// to be left in the buffer for the next call.
			r := inner.Key.Runes[0]
			inner.Key.Runes = []rune{r}
			n = utf8.RuneLen(r)
		}
	}
	return inner, n + 1, true
}

// decodeText handles everything that is not an escape sequence: the C0 controls,
// which is where ctrl+letter arrives, and runs of printable runes.
//
// 0x0a is ctrl+j and not enter, and that one byte is the whole of multi-line input on a
// terminal that has never heard of CSI u. Windows Terminal and conhost send LF for
// ctrl+enter — the chord the Kitty negotiation would otherwise have to earn — so the
// modifier is not lost there, it is spelled in the control range. It used to be folded in
// beside 0x0d here, which threw away the only distinction a legacy terminal offers; now it
// falls through to the generic case below and names itself, with no new code for the name.
// ctrl+j is the key ishakat and Claude Code bind their newline to for this exact reason.
//
// Safe because raw mode clears ICRNL (term.MakeRaw, in term.go): a plain return arrives as
// 0x0d and is never translated to 0x0a behind our back, so enter still sends the line and
// only the chord asks for a row. A newline inside a paste is not affected either way —
// decodeCSI takes the whole bracketed payload before this function sees it, and pasteText
// normalizes the endings.
func decodeText(b []byte) (*Event, int, bool) {
	switch c := b[0]; {
	case c == 0x0d:
		return &Event{Kind: EventKey, Key: Key{Type: KeyEnter}}, 1, true
	case c == 0x09:
		return &Event{Kind: EventKey, Key: Key{Type: KeyTab}}, 1, true
	case c == 0x7f, c == 0x08:
		return &Event{Kind: EventKey, Key: Key{Type: KeyBackspace}}, 1, true
	case c == 0x00:
		return &Event{Kind: EventKey, Key: Key{Type: KeyRunes, Runes: []rune{' '}, Mod: ModCtrl}}, 1, true
	case c < 0x20:
		// ctrl+a is 0x01, and 0x1c..0x1f are ctrl+\ ] ^ _. Reported as the character
		// plus the modifier, because that is how the user thinks of it and how a
		// config file spells it.
		r := rune(c + 0x60)
		if c >= 0x1c {
			r = rune(c + 0x40)
		}
		return &Event{Kind: EventKey, Key: Key{Type: KeyRunes, Runes: []rune{r}, Mod: ModCtrl}}, 1, true
	}
	// A run of printable text. Grouping it matters for paste-by-typing and for
	// scenario playback: one event per burst instead of one per rune.
	var runes []rune
	n := 0
	for n < len(b) {
		if b[n] < 0x20 || b[n] == 0x7f {
			break
		}
		r, size := utf8.DecodeRune(b[n:])
		if r == utf8.RuneError && size <= 1 {
			if !utf8.FullRune(b[n:]) {
				if len(runes) == 0 {
					return nil, 0, false // a rune split across two reads
				}
				break
			}
			if len(runes) == 0 {
				return nil, 1, true // a byte that is not UTF-8 at all: drop it
			}
			break
		}
		runes = append(runes, r)
		n += size
	}
	if len(runes) == 0 {
		return nil, 0, false
	}
	return &Event{Kind: EventKey, Key: Key{Type: KeyRunes, Runes: runes}}, n, true
}

// csiMax bounds the parameter scan. A CSI carrying more parameter bytes than this
// is not something a terminal sent on purpose, and without a bound a stream of
// digits would keep the decoder waiting for a final byte that never comes.
const csiMax = 64

// decodeCSI handles ESC [ …: the arrows, the editing and function keys, every
// modifier form of them, bracketed paste, mouse reports, and the replies a terminal
// sends unbidden. Parameters are read as numbers rather than matched as strings, so a
// modifier is one line of code instead of one table row per combination.
func decodeCSI(b []byte) (*Event, int, bool) {
	// Parameter bytes run 0x30..0x3f, intermediates 0x20..0x2f, and the first byte
	// in 0x40..0x7e ends the sequence.
	i := 2
	for i < len(b) && (b[i] < 0x40 || b[i] > 0x7e) {
		if b[i] < 0x20 || i > csiMax {
			return nil, 2, true // not a CSI after all: drop ESC [ and re-read the rest
		}
		i++
	}
	if i == len(b) {
		return nil, 0, false
	}
	n := i + 1
	if string(b[:n]) == pasteStart {
		end := bytes.Index(b[n:], []byte(pasteEnd))
		if end < 0 {
			return nil, 0, false // the paste is still arriving
		}
		text := pasteText(string(b[n : n+end]))
		return &Event{Kind: EventPaste, Text: text}, n + end + len(pasteEnd), true
	}
	// A mouse report, read before the parameters are, because it does not have any: the
	// '<' is a private marker and parseParams answers nil for it. The wheel becomes a key
	// and a press, drag or release becomes a mouse event; everything else is consumed and
	// dropped, which is the part that matters, since a report that reaches decodeText is
	// typed into the input line and its coordinates are ordinary printable bytes.
	if b[2] == '<' {
		return decodeSGRMouse(parseParams(string(b[3:i])), b[i], n)
	}
	params, final := parseParams(string(b[2:i])), b[i]
	// key reports a named key with whatever modifier the second parameter carries,
	// which is the same position for CSI 1;5A as for CSI 3;5~.
	key := func(t KeyType) (*Event, int, bool) {
		k := Key{Type: t}
		if len(params) >= 2 {
			k.Mod = modOf(params[1])
		}
		return &Event{Kind: EventKey, Key: k}, n, true
	}
	switch final {
	case 'A':
		return key(KeyUp)
	case 'B':
		return key(KeyDown)
	case 'C':
		return key(KeyRight)
	case 'D':
		return key(KeyLeft)
	case 'H':
		return key(KeyHome)
	case 'F':
		return key(KeyEnd)
	case 'P':
		return key(KeyF1)
	case 'Q':
		return key(KeyF2)
	case 'S':
		return key(KeyF4)
	case 'Z':
		return &Event{Kind: EventKey, Key: Key{Type: KeyTab, Mod: ModShift}}, n, true
	case 'R':
		// CSI R is both modified F3 and the cursor position report, an ambiguity
		// xterm's own documentation admits to. A report carries a row and a column;
		// the only reading that is a keypress is the one whose first parameter is
		// the literal 1 that every modified function key sends.
		if len(params) == 2 && params[0] == 1 && params[1] > 1 {
			return key(KeyF3)
		}
		return nil, n, true
	case '~':
		if len(params) == 0 {
			return nil, n, true
		}
		if t, ok := tildeKeys[params[0]]; ok {
			return key(t)
		}
		return nil, n, true
	case 'M':
		// The X10 mouse report, from a terminal that took our request for tracking and
		// ignored the one for SGR coordinates: CSI M and then exactly three bytes, a button
		// and a column and a row, each biased by 32. They are printable ASCII, so leaving
		// them in the buffer means a wheel notch types " !!" into the line — this is the one
		// case in the decoder where the damage is caused by not consuming enough. Waiting
		// for all three is what makes that safe across a read boundary.
		//
		// Only the wheel comes out of it, and a button is consumed and dropped. A single byte
		// per axis cannot name a column past 223, which on a wide window is exactly the right
		// edge the bar is drawn at, so the drag this encoding could carry is the drag it would
		// get wrong; and no terminal in reach speaks it, so a second coordinate path here
		// would be a hit test nobody can try. A wheel that works and no drag is the limit.
		//
		// With parameters, CSI M is DL, a sequence a terminal does not send us, and it falls
		// through to the branch that skips replies.
		if len(params) == 0 {
			if len(b) < n+3 {
				return nil, 0, false
			}
			return wheelKey(int(b[n])-32, n+3)
		}
	case 'u':
		return decodeKitty(params, n)
	}
	// Anything else is a reply, or a sequence from a terminal we do not know. Skipping it
	// is the whole job: unknown bytes must never reach the editor as text, which is what
	// happens if you fall through to decodeText.
	return nil, n, true
}

// tildeKeys is the CSI n ~ family. Two numbers mean home and two mean end because
// vt220 and xterm disagreed, and both are still sent today.
var tildeKeys = map[int]KeyType{
	1: KeyHome, 2: KeyInsert, 3: KeyDelete, 4: KeyEnd, 5: KeyPgUp, 6: KeyPgDn,
	7: KeyHome, 8: KeyEnd,
	11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5, 17: KeyF6, 18: KeyF7,
	19: KeyF8, 20: KeyF9, 21: KeyF10, 23: KeyF11, 24: KeyF12,
}

// ss3Keys is ESC O x: the arrows and F1-F4 as a terminal in application keypad
// mode sends them, which is the mode most terminals are in by default.
var ss3Keys = map[byte]Key{
	'A': {Type: KeyUp}, 'B': {Type: KeyDown}, 'C': {Type: KeyRight}, 'D': {Type: KeyLeft},
	'H': {Type: KeyHome}, 'F': {Type: KeyEnd}, 'M': {Type: KeyEnter},
	'P': {Type: KeyF1}, 'Q': {Type: KeyF2}, 'R': {Type: KeyF3}, 'S': {Type: KeyF4},
}

// kittyNamed is the handful of codepoints the Kitty protocol reports for keys that
// also have a legacy control byte. Everything else it sends is either real text or
// a private-use codepoint we have no name for.
var kittyNamed = map[int]KeyType{
	9: KeyTab, 13: KeyEnter, 27: KeyEscape, 127: KeyBackspace,
}

// decodeKitty reads CSI codepoint ; modifiers u. The protocol reports the key the
// user physically pressed rather than the character it produced, so ctrl+a arrives
// as 'a' with a ctrl bit — the same shape as the legacy 0x01, which is why nothing
// above this file has to care which protocol the terminal is speaking.
func decodeKitty(params []int, n int) (*Event, int, bool) {
	if len(params) == 0 || params[0] <= 0 {
		return nil, n, true
	}
	var mod Mod
	if len(params) >= 2 {
		mod = modOf(params[1])
	}
	if t, ok := kittyNamed[params[0]]; ok {
		return &Event{Kind: EventKey, Key: Key{Type: t, Mod: mod}}, n, true
	}
	r := rune(params[0])
	if r < 0x20 || !utf8.ValidRune(r) || (r >= 0xe000 && r <= 0xf8ff) {
		// The private use area is where Kitty puts the keys that have no character:
		// caps lock, the media keys, the numeric keypad. Reporting them as text
		// would type U+E000 into the editor.
		return nil, n, true
	}
	return &Event{Kind: EventKey, Key: Key{Type: KeyRunes, Runes: []rune{r}, Mod: mod}}, n, true
}

// decodeSGRMouse reads CSI < button ; column ; row M, and the same with a final 'm' for a
// release: the modern mouse report, and the one we asked the terminal for.
//
// The wheel leaves as a key and a button leaves as a mouse event, which is the split the
// Mouse type explains from the other side. What is dropped is what nothing above could act
// on: bare motion with no button down, the extra buttons, and a truncated report.
//
// A press that was not the wheel used to be dropped too, on the grounds that a click is the
// terminal's own business and leaving it there kept text selection working. That reasoning
// holds exactly while nothing on screen can be clicked, and it survives here in a better
// place: tracking is off by default, so with the mouse unclaimed the terminal never sends us
// one of these and the reader's drag-to-select is untouched by this function existing. A
// reader who does hand over the mouse has already given the selection up, and dropping the
// report as well would leave them nothing in exchange.
func decodeSGRMouse(params []int, final byte, n int) (*Event, int, bool) {
	if len(params) == 0 || (final != 'M' && final != 'm') {
		return nil, n, true
	}
	btn := params[0]
	if isWheel(btn) {
		// A notch has no release. A terminal that sends one is describing a wheel event that
		// has already been reported, and turning it into a second KeyWheelUp scrolls twice.
		if final != 'M' {
			return nil, n, true
		}
		return wheelKey(btn, n)
	}
	if btn < 0 || btn&0x80 != 0 || len(params) < 3 {
		return nil, n, true
	}
	var b MouseButton
	switch btn & 3 {
	case 0:
		b = MouseLeft
	case 1:
		b = MouseMiddle
	case 2:
		b = MouseRight
	default:
		// Button 3 without the wheel bit is "no button", which is how a terminal reports the
		// pointer merely passing through. Only 1003 asks for those and we ask for 1002, so
		// this is a terminal being generous; a drag with nothing held is not a gesture.
		return nil, n, true
	}
	act := MousePress
	switch {
	case final == 'm':
		act = MouseRelease
	case btn&32 != 0:
		// Bit 5 is motion, which under 1002 is only ever sent while a button is down. It is
		// the difference between "the pointer arrived here" and "the pointer is being dragged
		// through here", and the second is the whole of a drag.
		act = MouseDrag
	}
	col, row := params[1]-1, params[2]-1
	if col < 0 || row < 0 {
		// One-based means zero is a terminal getting it wrong, and a negative cell would be
		// hit-tested against a rectangle that starts at zero. Dropping it costs one report.
		return nil, n, true
	}
	ev := Event{Kind: EventMouse, Mouse: Mouse{Action: act, Button: b, Col: col, Row: row, Mod: mouseMod(btn)}}
	return &ev, n, true
}

// isWheel is the one test for "this report is a notch and not a button": bit 6 set, bit 7
// clear, since bit 7 marks the extra buttons some mice have. It is a function so that its two
// callers cannot drift — decodeSGRMouse routes on it and wheelKey refuses everything else —
// and a report classified one way here and the other way there is a click that scrolls.
func isWheel(btn int) bool { return btn >= 0 && btn&0xc0 == 0x40 }

// wheelKey turns a mouse button field into a wheel key, or into nothing at all.
//
// Bits 0 and 1 say which way the wheel turned, in the same two slots a left or middle click
// uses, which is why isWheel is asked first.
//
// Horizontal notches — a trackpad swiped sideways — are dropped rather than mapped onto the
// vertical ones. The transcript does not scroll sideways, and a swipe that jumped the
// conversation would be worse than a swipe that did nothing.
func wheelKey(btn, n int) (*Event, int, bool) {
	if !isWheel(btn) {
		return nil, n, true
	}
	var t KeyType
	switch btn & 3 {
	case 0:
		t = KeyWheelUp
	case 1:
		t = KeyWheelDown
	default:
		return nil, n, true
	}
	return &Event{Kind: EventKey, Key: Key{Type: t, Mod: mouseMod(btn)}}, n, true
}

// mouseMod reads the modifiers a mouse report carries: shift, meta and ctrl, in bits 2, 3
// and 4. It is not modOf, and the difference is the reason this exists — a mouse report
// writes the bits straight where a key parameter biases the whole field by one, so running
// one through the other reports every modifier as the next one along.
func mouseMod(btn int) Mod {
	var m Mod
	if btn&4 != 0 {
		m |= ModShift
	}
	if btn&8 != 0 {
		m |= ModAlt
	}
	if btn&16 != 0 {
		m |= ModCtrl
	}
	return m
}

// modOf reads a modifier parameter. It is a bitfield biased by one, so that a key
// with no modifiers still sends a non-empty parameter. Bit 8 is super/meta, which
// we drop rather than fold into ctrl: a wrong binding is worse than a missing one.
func modOf(p int) Mod {
	if p < 1 {
		return 0
	}
	p--
	var m Mod
	if p&1 != 0 {
		m |= ModShift
	}
	if p&2 != 0 {
		m |= ModAlt
	}
	if p&4 != 0 {
		m |= ModCtrl
	}
	return m
}

// parseParams splits a CSI parameter string into numbers. A missing parameter is 0,
// which is what the standard says the default is. A sub-parameter after ':' is
// dropped: Kitty puts alternate keycodes there and we bind the primary one. A
// leading private marker (CSI ? …, CSI < …) yields nothing, which sends the whole
// sequence to the "not a keypress" branch where a mouse report or a DEC reply
// belongs.
func parseParams(s string) []int {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return nil
	}
	fields := strings.Split(s, ";")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		if i := strings.IndexByte(f, ':'); i >= 0 {
			f = f[:i]
		}
		v, err := strconv.Atoi(f)
		if err != nil {
			v = 0
		}
		out = append(out, v)
	}
	return out
}

// pasteText normalizes line endings. A terminal sends Enter inside a paste as CR,
// and every layer above this one counts lines by LF.
func pasteText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// stringTerminator finds the end of an OSC, DCS, PM or APC string: a BEL, or ESC \.
// It returns the length of the whole thing, or 0 while it is still arriving. A bare
// ESC that is not part of ST ends the scan short, because a terminal that got cut
// off mid-reply must not swallow the keys the user pressed afterwards.
func stringTerminator(b []byte) int {
	for i := 2; i < len(b); i++ {
		switch b[i] {
		case 0x07:
			return i + 1
		case 0x1b:
			if i+1 >= len(b) {
				return 0
			}
			if b[i+1] == '\\' {
				return i + 2
			}
			return i
		}
	}
	return 0
}
