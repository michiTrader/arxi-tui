// Command arxi-tui is the terminal interface driver for arxi.
//
// Phase 0: loads the factory RAW scene, drives the fold with mock events,
// renders frames in a full-screen terminal, and handles the immovable
// escape gesture (Ctrl-C twice restores the raw scene).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
)

const factoryRAW = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "arxi-tui: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load scene document (fallback to factory RAW)
	data, err := os.ReadFile("testdata/RAW.json")
	if err != nil {
		data = []byte(factoryRAW)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		return fmt.Errorf("scene parse: %w", err)
	}

	// Initialize terminal
	tty, err := term.Open()
	if err != nil {
		// No tty (piped output): render the frame once and exit. This keeps
		// the pipeline inspectable — the same frames a tty would draw, on stdout.
		return runNonInteractive(doc)
	}
	defer tty.Close()

	if err := tty.Raw(); err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}

	// Full-screen frames are drawn on the alternate screen buffer. Nothing this
	// program paints belongs in the user's scrollback — a transcript that
	// repaints per keystroke would bury the shell history under thousands of
	// near-identical copies — and leaving the buffer on exit hands the shell
	// back exactly as it was found, cursor and all.
	fmt.Fprint(tty, "\033[?1049h\033[H")
	defer fmt.Fprint(tty, "\033[?1049l")

	// Phase 0 mock driver: emits a fixed log to prove the pipeline end-to-end.
	// Phase 1 replaces this with the NDJSON reader from the arxi core.
	mockDriver := driver.NewMock([]fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola! ¿En qué puedo ayudarte?"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventCh := make(chan fold.Event, 64)
	go mockDriver.Run(ctx, eventCh)

	return loop(ctx, tty, doc, eventCh)
}

// Loop is the Phase 0 event loop: terminal events and core events on one select,
// the fold projected per event, one full repaint per change.
//
// The escape gesture lives here, in the host, and nowhere below: a scene can
// read host.escape.armed to show it, but no scene, preset or plugin can capture
// the key or redefine what it does (invariant 6). Ctrl-C is the line's key, as
// readline has meant it for forty years: the first press throws away what the
// user was typing and arms the gesture; the second, within armTimeout, restores
// the raw scene — and when there is nothing left to restore, the raw scene is
// already showing and the fold is empty, the gesture has done its whole job and
// the program leaves.
func loop(ctx context.Context, tty Terminal, doc *scene.Document, eventCh <-chan fold.Event) error {
	panicGesture := &driver.PanicGesture{}
	var collected []fold.Event
	var input string
	nextSeq := int64(1000) // user-submitted prompts number above the mock's log

	repaint := func() {
		state := fold.Fold(collected)
		state.UserInput = input // view state: the host owns the input buffer
		var r engine.Renderer
		w, h := tty.Size()
		r.Width, r.Height = w, h
		render(tty, doc, r, state)
	}

	repaint()

	termEvents := tty.Events()
	for {
		select {
		case <-ctx.Done():
			return nil

		case ev := <-termEvents:
			switch ev.Kind {
			case term.EventKey:
				if isCtrlC(ev.Key) {
					if panicGesture.HandleCtrlC(time.Now()) {
						if len(collected) == 0 && input == "" {
							return nil // nothing to restore: the door
						}
						collected = nil
						input = ""
					} else {
						input = "" // first press clears the line
					}
				} else {
					panicGesture.Reset() // any other key disarms the gesture
					input = typeKey(input, ev.Key, &collected, &nextSeq)
				}
				repaint()
			case term.EventResize:
				repaint()
			case term.EventClosed:
				return nil
			}

		case e := <-eventCh:
			collected = append(collected, e)
			repaint()
		}
	}
}

// typeKey applies one keypress to the input buffer, or submits the line.
// Enter submits as run.prompt — in Phase 0 the event lands in the local fold
// directly; Phase 1 the same event goes to the core over the wire and comes
// back through the log, so the fold never learns a second path.
func typeKey(input string, k term.Key, collected *[]fold.Event, nextSeq *int64) string {
	switch {
	case k.Type == term.KeyEnter:
		text := strings.TrimSpace(input)
		if text == "" {
			return input
		}
		*collected = append(*collected, fold.Event{
			Type:    "run.prompt",
			Seq:     *nextSeq,
			Payload: map[string]any{"text": text},
		})
		*nextSeq++
		return ""
	case k.Type == term.KeyBackspace:
		r := []rune(input)
		if len(r) > 0 {
			return string(r[:len(r)-1])
		}
		return input
	case k.Type == term.KeyRunes && k.Mod&(term.ModCtrl|term.ModAlt) == 0:
		return input + string(k.Runes)
	default:
		return input
	}
}

// isCtrlC reports whether a key event is Ctrl-C. The decoder reports control
// bytes as the character plus the modifier (0x03 becomes 'c' with ModCtrl),
// which is how a config file spells it.
func isCtrlC(k term.Key) bool {
	return k.Type == term.KeyRunes &&
		len(k.Runes) == 1 && k.Runes[0] == 'c' &&
		k.Mod&term.ModCtrl != 0
}

// Terminal is the minimal view of a terminal that the event loop needs.
// *term.TTY satisfies this; tests substitute a fake.TTY to drive the loop
// from a script instead of a keyboard.
type Terminal interface {
	io.Writer
	Size() (width, height int)
	Events() <-chan term.Event
}

// render repaints the whole screen. Phase 0 uses clear-home + full redraw;
// the cell-diff repaint (the emitter's own machinery) is Phase 0.5 work on
// top of the same Frame.
func render(w io.Writer, doc *scene.Document, r engine.Renderer, state fold.State) {
	f := r.RenderFrame(doc, state)
	fmt.Fprint(w, "\033[H\033[2J")
	// Raw mode turned the terminal's output processing off (OPOST/ONLCR), so
	// the terminal no longer translates \n into \r\n: a bare newline drops
	// one row and keeps the column, and every line after the first climbs
	// further right — the staircase. The Frame carries plain \n because it is
	// mode-blind; the emit path is where they become \r\n, because the emit
	// path is the only code that knows the terminal is in raw mode.
	fmt.Fprint(w, strings.ReplaceAll(f.Plain(), "\n", "\r\n"))
}

// runNonInteractive renders a single frame to stdout when stdin is not a
// terminal. Same scene, same fold, same renderer — no tty path is a second
// renderer.
func runNonInteractive(doc *scene.Document) error {
	r := engine.Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, fold.Fold(nil))
	fmt.Print(f.Plain())
	return nil
}
