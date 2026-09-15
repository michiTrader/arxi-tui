package term

import (
	"strings"
)

// Keys, as the user will name them.
//
// Two layers, on purpose. Decode turns bytes into a Key — a thing with a type and
// a modifier set — and String turns a Key into the name a config file binds:
// "ctrl+w", "alt+left", "f5". Nothing above this package ever sees a byte, so
// rebinding is a table lookup on a string and a keymap can be printed, diffed and
// documented. The alternative, matching escape sequences at the call site, is how
// you end up with a keymap nobody can expose.

// KeyType is which key it was. KeyRunes means it was text.
type KeyType uint8

const (
	KeyRunes KeyType = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyDelete
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPgUp
	KeyPgDn
	KeyInsert
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12

	// The wheel. It arrives as a mouse report rather than as a key, and it is turned into
	// one here because a wheel notch means the same thing to the app as shift+up does: move
	// the view. Making it a KeyType puts it in the keymap with everything else, so it is
	// bindable, printable and rebindable by the same table, instead of needing a mouse
	// handler with rules of its own. Buttons and drags are not decoded at all — the terminal
	// keeps its own selection behaviour — so this is the whole of the mouse as far as
	// anything above this package is concerned.
	KeyWheelUp
	KeyWheelDown
)

// Mod is the modifier set. Shift is only reported when the terminal tells us, which
// for a plain letter it never does: shift+a arrives as "A".
type Mod uint8

const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
)

// Key is one keypress.
type Key struct {
	Type  KeyType
	Runes []rune
	Mod   Mod
}

// keyNames is the single source of truth for both directions. A name that appears
// here can be bound in a config file; one that does not, cannot, and the parse
// error says so rather than silently ignoring the line.
var keyNames = map[KeyType]string{
	KeyEnter:     "enter",
	KeyTab:       "tab",
	KeyBackspace: "backspace",
	KeyDelete:    "delete",
	KeyEscape:    "esc",
	KeyUp:        "up",
	KeyDown:      "down",
	KeyLeft:      "left",
	KeyRight:     "right",
	KeyHome:      "home",
	KeyEnd:       "end",
	KeyPgUp:      "pgup",
	KeyPgDn:      "pgdown",
	KeyInsert:    "insert",
	KeyF1:        "f1",
	KeyF2:        "f2",
	KeyF3:        "f3",
	KeyF4:        "f4",
	KeyF5:        "f5",
	KeyF6:        "f6",
	KeyF7:        "f7",
	KeyF8:        "f8",
	KeyF9:        "f9",
	KeyF10:       "f10",
	KeyF11:       "f11",
	KeyF12:       "f12",
	KeyWheelUp:   "wheelup",
	KeyWheelDown: "wheeldown",
}

var namedKeys = func() map[string]KeyType {
	m := make(map[string]KeyType, len(keyNames))
	for t, n := range keyNames {
		m[n] = t
	}
	return m
}()

// String is the bindable name: modifiers in a fixed order so that "ctrl+alt+x" is
// the only spelling of itself, then the key.
func (k Key) String() string {
	var b strings.Builder
	if k.Mod&ModCtrl != 0 {
		b.WriteString("ctrl+")
	}
	if k.Mod&ModAlt != 0 {
		b.WriteString("alt+")
	}
	if k.Mod&ModShift != 0 {
		b.WriteString("shift+")
	}
	if k.Type == KeyRunes {
		if string(k.Runes) == " " {
			b.WriteString("space")
		} else {
			b.WriteString(string(k.Runes))
		}
		return b.String()
	}
	if n, ok := keyNames[k.Type]; ok {
		b.WriteString(n)
		return b.String()
	}
	b.WriteString("unknown")
	return b.String()
}

// ParseKey reads a name back. It exists so a keymap can come out of a config file,
// and so the default keymap can be written as strings and checked by a test rather
// than being a pile of struct literals.
func ParseKey(s string) (Key, bool) {
	var k Key
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			k.Mod |= ModCtrl
			s = s[5:]
		case strings.HasPrefix(s, "alt+"):
			k.Mod |= ModAlt
			s = s[4:]
		case strings.HasPrefix(s, "shift+"):
			k.Mod |= ModShift
			s = s[6:]
		default:
			if s == "space" {
				k.Type, k.Runes = KeyRunes, []rune{' '}
				return k, true
			}
			if t, ok := namedKeys[s]; ok {
				k.Type = t
				return k, true
			}
			if r := []rune(s); len(r) == 1 {
				k.Type, k.Runes = KeyRunes, r
				return k, true
			}
			return Key{}, false
		}
	}
}
