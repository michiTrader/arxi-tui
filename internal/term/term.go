package term

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
)

// The impure package. Above this line everything takes a Frame and returns bytes and
// can be tested on a machine with no terminal at all; a file descriptor, raw mode, a
// signal and a poll all live here, in three short files. The port into this repo
// landed with charmbracelet/x/term named in go.mod rather than the syscalls it
// wraps; replacing it is its own change, and these three files are the whole
// review surface for it.

// escTimeout is how long a stalled escape sequence waits before we decide it was not
// a sequence at all. 50 ms is where every terminal library lands: a real sequence
// arrives inside one read, or microseconds after it, and no human presses esc and
// then a bracket that fast on purpose.
const escTimeout = 50 * time.Millisecond

// ErrNotATerminal is what Open returns when stdin is a pipe. Worth a named error
// because the caller's answer is not "fail" but "run the scenario without a tty".
var ErrNotATerminal = errors.New("standard input is not a terminal")

// TTY is one terminal session.
type TTY struct {
	in    *os.File
	out   *os.File
	state *term.State

	events chan Event
	done   chan struct{}
	stop   sync.Once
	begin  sync.Once
}

// Open takes the process's standard streams. It deliberately does not enter raw
// mode: Raw is a separate call so a caller can fail, print an ordinary error and
// exit without having anything to undo.
func Open() (*TTY, error) {
	t := &TTY{
		in:     os.Stdin,
		out:    os.Stdout,
		events: make(chan Event, 64),
		done:   make(chan struct{}),
	}
	if !term.IsTerminal(t.in.Fd()) {
		return nil, ErrNotATerminal
	}
	return t, nil
}

// Raw turns off echo and line buffering so that a keypress arrives as a keypress.
func (t *TTY) Raw() error {
	if t.state != nil {
		return nil
	}
	st, err := term.MakeRaw(t.in.Fd())
	if err != nil {
		return err
	}
	t.state = st
	return nil
}

// Restore puts the terminal back. Like the emitter's Exit it has to be safe to call
// twice and safe to call from a signal handler, because those are the two places it
// is called from and the cost of getting it wrong is a shell the user has to reset
// blind.
func (t *TTY) Restore() error {
	if t.state == nil {
		return nil
	}
	st := t.state
	t.state = nil
	return term.Restore(t.in.Fd(), st)
}

// Size reports the terminal in cells. A terminal that cannot answer is not an error
// worth propagating: 80x24 is the answer every tty has agreed on since 1978.
func (t *TTY) Size() (width, height int) {
	w, h, err := term.GetSize(t.out.Fd())
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

func (t *TTY) Write(p []byte) (int, error) { return t.out.Write(p) }

// Events is the only way in. One channel carries keys, pastes, resizes and the close,
// so the application is a single select and cannot process a resize and a keystroke
// in the wrong order — which is how a frame gets rendered at a width the terminal no
// longer has.
func (t *TTY) Events() <-chan Event {
	t.begin.Do(func() {
		go t.read()
		t.watchResize()
	})
	return t.events
}

// Close stops the watchers. It does not close os.Stdin: the blocking read may sit
// there until the user presses one more key, and the process is exiting anyway,
// whereas closing fd 0 is a thing whose blast radius includes the shell.
func (t *TTY) Close() error {
	t.stop.Do(func() { close(t.done) })
	return t.Restore()
}

// emit hands an event to the application, or gives up if we are shutting down. The
// select is what keeps Close from deadlocking against a full channel.
func (t *TTY) emit(ev Event) bool {
	select {
	case t.events <- ev:
		return true
	case <-t.done:
		return false
	}
}

// chunk is one read from the terminal.
type chunk struct {
	b   []byte
	err error
}

// read is the decode loop. The blocking read lives in its own goroutine so that this
// one can wait on a timer too: a lone ESC is indistinguishable from the first byte of
// a sequence until time passes, and no time passes inside a blocking read.
func (t *TTY) read() {
	raw := make(chan chunk)
	go t.pump(raw)

	var rest []byte
	var timeout <-chan time.Time
	for {
		select {
		case <-t.done:
			return
		case c := <-raw:
			timeout = nil
			if len(c.b) > 0 {
				buf := make([]byte, 0, len(rest)+len(c.b))
				buf = append(append(buf, rest...), c.b...)
				var evs []Event
				evs, rest = Decode(buf)
				for _, ev := range evs {
					if !t.emit(ev) {
						return
					}
				}
			}
			if c.err != nil {
				t.emit(Event{Kind: EventClosed, Err: c.err})
				return
			}
			if len(rest) > 0 && rest[0] == 0x1b {
				timeout = time.After(escTimeout)
			}
		case <-timeout:
			timeout = nil
			var evs []Event
			evs, rest = flushPartial(rest)
			for _, ev := range evs {
				if !t.emit(ev) {
					return
				}
			}
		}
	}
}

// pump does the blocking reads. It exists as its own goroutine only so the decode
// loop can also wait on time; when the session ends it may still be parked inside
// Read until the user presses one more key, which is the leak Close documents.
func (t *TTY) pump(out chan<- chunk) {
	buf := make([]byte, 4096)
	for {
		n, err := t.in.Read(buf)
		var b []byte
		if n > 0 {
			b = append(b, buf[:n]...)
		}
		select {
		case out <- chunk{b: b, err: err}:
		case <-t.done:
			return
		}
		if err != nil {
			return
		}
	}
}

// flushPartial decides what a stalled escape sequence was. A tail that has sat in the
// buffer for escTimeout is not something the terminal is still writing: it is the user
// having pressed esc, or having pressed alt and a key the terminal spells the same way
// as the start of a sequence — alt+[ and ESC [ are the same two bytes.
//
// The cost of this is honest: if a terminal ever stalls in the middle of a real
// sequence for 50 ms, its parameters get read as text. Every library that supports
// the esc key makes that trade, and the alternative is an esc that only registers
// once you press something else.
func flushPartial(rest []byte) ([]Event, []byte) {
	if len(rest) == 0 || rest[0] != 0x1b {
		return nil, rest
	}
	if len(rest) == 1 {
		return []Event{{Kind: EventKey, Key: Key{Type: KeyEscape}}}, nil
	}
	evs, _ := Decode(rest[1:])
	for i := range evs {
		if evs[i].Kind == EventKey {
			evs[i].Key.Mod |= ModAlt
		}
	}
	// Whatever did not decode is dropped rather than carried: it has already had its
	// chance, and a buffer that never empties is a terminal that stops responding.
	return evs, nil
}
