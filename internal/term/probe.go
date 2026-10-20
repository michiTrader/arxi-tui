package term

import (
	"fmt"
	"io"
	"strings"
)

// Probe answers one question a keymap cannot answer from the inside: which of the
// keys we list does THIS terminal, under THIS shell, actually deliver? Each read
// prints the raw bytes the console handed us and the key they decoded to. A key
// that prints nothing never reached the program (the terminal or shell took it);
// a key that prints bytes but decodes to something else is a decoder bug.
//
// It ends on ctrl+c, the one key that is never anybody's to take.
func (t *TTY) Probe(w io.Writer) error {
	if err := t.Raw(); err != nil {
		return err
	}
	defer t.Restore()
	fmt.Fprint(w, "arxi-tui key probe. Press keys; each prints raw bytes and the decoded key.\r\n"+
		"A key that prints nothing never reached the program. ctrl+c ends the probe.\r\n")

	raw := make(chan chunk)
	go t.pump(raw)
	var rest []byte
	for c := range raw {
		if len(c.b) > 0 {
			fmt.Fprintf(w, "bytes %-28s", hexBytes(c.b))
			buf := append(append([]byte{}, rest...), c.b...)
			var evs []Event
			evs, rest = Decode(buf)
			if len(evs) == 0 {
				fmt.Fprint(w, " (incomplete or ignored)")
			}
			for _, ev := range evs {
				fmt.Fprintf(w, " -> %s", describe(ev))
				if ev.Kind == EventKey && ev.Key.String() == "ctrl+c" {
					fmt.Fprint(w, "\r\nbye\r\n")
					return nil
				}
			}
			fmt.Fprint(w, "\r\n")
			if len(rest) > 0 && rest[0] == 0x1b && len(rest) == len(buf) {
				// a lone ESC or a half sequence: report it as the app would after its timeout
				evs, rest = flushPartial(rest)
				for _, ev := range evs {
					fmt.Fprintf(w, "      (after the escape timeout) -> %s\r\n", describe(ev))
				}
			}
		}
		if c.err != nil {
			return c.err
		}
	}
	return nil
}

func hexBytes(b []byte) string {
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, " ")
}

func describe(ev Event) string {
	switch ev.Kind {
	case EventKey:
		return "key " + ev.Key.String()
	case EventPaste:
		return fmt.Sprintf("paste %q", ev.Text)
	case EventMouse:
		return "mouse"
	case EventResize:
		return "resize"
	}
	return "event"
}
