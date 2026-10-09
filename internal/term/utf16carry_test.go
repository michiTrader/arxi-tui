package term

import (
	"testing"
	"unicode/utf16"
)

// ctrl+z reaches the decoder as the byte 0x1a. The standard library's console read
// stopped the stream at it (end of file), and a press of ctrl+z ended the program; the
// conversion here must pass it through like any other unit.
func TestCtrlZIsAByteLikeAnyOtherNotTheEndOfTheInput(t *testing.T) {
	var c utf16Carry
	got := c.bytes([]uint16{'a', 0x1a, 'b'})
	if string(got) != "a\x1ab" {
		t.Fatalf("got %q, want the three bytes a, 0x1a, b", got)
	}
	evs, _ := Decode([]byte{0x1a})
	if len(evs) != 1 || evs[0].Key.String() != "ctrl+z" {
		t.Fatalf("0x1a decodes to %+v, want the key ctrl+z", evs)
	}
}

// An emoji is two units; the console may deliver them in two reads.
func TestAPairSplitAcrossTwoReadsStillMakesOneCharacter(t *testing.T) {
	hi, lo := utf16.EncodeRune('😀')
	var c utf16Carry
	first := c.bytes([]uint16{uint16(hi)})
	second := c.bytes([]uint16{uint16(lo)})
	if len(first) != 0 || string(second) != "😀" {
		t.Fatalf("first %q second %q, want nothing then the emoji", first, second)
	}
}

func TestALoneHalfIsReplacedNotKept(t *testing.T) {
	var c utf16Carry
	got := c.bytes([]uint16{0xd83d, 'x'})
	if string(got) != "\uFFFDx" {
		t.Fatalf("got %q, want a replacement character then x", got)
	}
}
