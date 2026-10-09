package term

import (
	"unicode/utf16"
	"unicode/utf8"
)

// utf16Carry turns what the Windows console hands over (UTF-16 code units) into the
// UTF-8 bytes the decoder reads. It exists because the standard library does this
// conversion itself inside os.File.Read for a console, and in the same place it treats
// the byte 0x1A (ctrl+z) as the end of the file: the read returns zero bytes and no
// error, the caller sees io.EOF, and a press of ctrl+z ended the program. Reading the
// console ourselves, with the one conversion and without that rule, is what lets
// ctrl+z be a key.
//
// A surrogate pair can arrive split across two reads, so a trailing high surrogate is
// held until its partner comes.
type utf16Carry struct{ high uint16 }

// bytes converts one read's worth of code units.
func (c *utf16Carry) bytes(units []uint16) []byte {
	out := make([]byte, 0, len(units)*3)
	for _, u := range units {
		if c.high != 0 {
			h := c.high
			c.high = 0
			if u >= 0xdc00 && u < 0xe000 {
				out = utf8.AppendRune(out, utf16.DecodeRune(rune(h), rune(u)))
				continue
			}
			out = utf8.AppendRune(out, utf8.RuneError)
		}
		switch {
		case u >= 0xd800 && u < 0xdc00:
			c.high = u
		case u >= 0xdc00 && u < 0xe000:
			out = utf8.AppendRune(out, utf8.RuneError)
		default:
			out = utf8.AppendRune(out, rune(u))
		}
	}
	return out
}
