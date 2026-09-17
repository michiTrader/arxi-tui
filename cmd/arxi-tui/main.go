// Command arxi-tui is the terminal interface driver for arxi.
//
// Phase 0: loads the factory RAW scene, drives the fold with mock events,
// renders frames in a full-screen terminal, and handles the immovable
// escape gesture (Ctrl-C twice restores the raw scene).
//
// Phase 0.5: the mock driver is replaced by an NDJSON connection to the
// arxi core's serve subprocess. Log-follow reads the run's event log;
// run.prompt requests go over the NDJSON request/response protocol.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

const factoryRAW = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}`

// factorySobria is the Scene 2 sobria default, embedded as a fallback constant
// so the interface boots even when testdata/SOARIA.json is missing.
const factorySobria = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "header"},
    "text": "Δr×i v0.1.0 · Run /help for commands" },
  { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "thinking", "type": "marquee", "when": "agent.working",
    "bind": "thinking.text",
    "prefix": { "text": "• Thinking · ", "style": {"style": "dim"} },
    "suffix": { "bind": "usage.delta", "style": {"style": "dim"} } },
  { "id": "prompt", "type": "input", "bind": "user.input",
    "prefix": "┃ ", "placeholder": "ask anything, or / for commands" },
  { "id": "menu", "type": "overlay", "anchor": "bottom", "when": "slash.active",
    "children": [
      { "type": "rule" },
      { "id": "cmds", "type": "list", "bind": "slash.matches",
        "filter_by": "typed", "count": true,
        "categories": ["All","General","Session","Account","Model",
                       "Appearance","Security","Workspace","Media","Extensions","Product"] },
      { "type": "text", "style": {"style": "dim"},
        "text": "↑↓ navigate · tab category · enter open · esc close" },
      { "type": "rule" } ] },
  { "id": "status", "type": "row", "style": {"style": "dim"}, "children": [
    { "type": "text", "bind": "agent.mode" },
    { "type": "text", "text": " · " },
    { "type": "text", "bind": "model.name" },
    { "type": "text", "text": " · ⚡︎" } ] }
]}}`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "arxi-tui: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load scene document. The default is the sobria scene (Scene 2, the
	// fx-inspired default per PLAN.md); if it fails to parse or validate, fall
	// back to the factory RAW scene (Scene 1) so the interface always boots.
	doc, err := loadScene("testdata/SOARIA.json", factoryRAW)
	if err != nil {
		return fmt.Errorf("scene load: %w", err)
	}

	// Load theme. The factory SOBRIA theme is compiled in and adapts to the
	// terminal's background (light/dark detection via OSC 11).
	theme := theme.SOBRIA()

	// Initialize terminal
	tty, err := term.Open()
	if err != nil {
		// No tty (piped output): render the frame once and exit. This keeps
		// the pipeline inspectable — the same frames a tty would draw, on stdout.
		return runNonInteractive(doc, theme)
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
	// Leaving the alternate buffer restores the cursor position it saved, not
	// its visibility, so a frame that hid the caret would leave the shell with
	// no cursor at all. Show it unconditionally on the way out.
	defer fmt.Fprint(tty, "\033[?25h\033[?1049l")

	// Phase 0.5: spawn the arxi core as a serve subprocess and speak the
	// NDJSON request/response protocol. Log-follow reads the run's event
	// log file. When no arxi binary is available (Phase 0 dev, or non-interactive
	// pipe), fall back to the mock driver.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	drv, eventCh, err := openDriver(ctx, doc)
	if err != nil {
		return err
	}
	defer drv.Close()

	return loop(ctx, tty, doc, theme, eventCh, drv)
}

// Driver is the minimal interface the event loop needs from whatever feeds it
// events and accepts prompt submissions. The mock and the NDJSON driver both
// satisfy it, so the loop never knows which path it is on.
type Driver interface {
	// SubmitPrompt sends the user's typed text as a run.prompt request.
	// In Phase 0 (mock) this feeds events directly to the channel; in Phase 0.5
	// (NDJSON) this sends the request over the serve protocol and the core's
	// response arrives as log events on eventCh.
	SubmitPrompt(ctx context.Context, text string) error
	// Close tears down any subprocess or resources the driver owns.
	Close() error
}

// openDriver decides whether to spawn the arxi core subprocess or fall back to
// the Phase 0 mock. The mock is used when ARXI_BIN is unset: the binary path
// is optional, and the mock lets the engine run daily without the core present.
func openDriver(ctx context.Context, doc *scene.Document) (Driver, <-chan fold.Event, error) {
	arxiBin := os.Getenv("ARXI_BIN")
	if arxiBin == "" {
		// Phase 0: no arxi binary, use the mock driver that replays a fixed
		// log. The mock submits prompts by appending directly to the event
		// channel, so the fold sees them without a subprocess.
		return openMockDriver(ctx, doc)
	}

	// Phase 0.5: spawn arxi serve as a subprocess. The subprocess communicates
	// via stdin/stdout using the NDJSON request/response protocol (ADR-0002).
	// Log-follow reads the run's event log file separately.
	return openServeDriver(ctx, arxiBin)
}

// openMockDriver creates the Phase 0 mock: a fixed log replay plus a
// prompt-submitter that appends run.prompt events to the channel.
func openMockDriver(ctx context.Context, doc *scene.Document) (Driver, <-chan fold.Event, error) {
	eventCh := make(chan fold.Event, 64)

	mk := driver.NewMock([]fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "actor": "user", "budget_usd": 10.0,
			"max_turns": 10, "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "Hola! ¿En qué puedo ayudarte?",
			"tokens_in": 12, "tokens_out": 18, "cost_usd": 0.0004,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
		{Type: "run.prompt", Seq: 5, Payload: map[string]any{"text": "gracias"}},
		{Type: "agent.activated", Seq: 6, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 7, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "De nada.",
			"tokens_in": 5, "tokens_out": 3, "cost_usd": 0.0001,
		}},
		{Type: "agent.turn_done", Seq: 8, Payload: map[string]any{"agent": "backend"}},
	})
	go mk.Run(ctx, eventCh)

	md := &mockDriver{
		eventCh: eventCh,
		seqBase: 1000, // user-submitted prompts number above the mock's log
	}
	return md, eventCh, nil
}

// mockDriver satisfies Driver: submitting a prompt appends the event directly
// to the channel, simulating what the core would emit after processing it.
type mockDriver struct {
	eventCh chan<- fold.Event
	seqBase int64
}

func (m *mockDriver) SubmitPrompt(ctx context.Context, text string) error {
	m.seqBase++
	ev := fold.Event{
		Type:    "run.prompt",
		Seq:     m.seqBase,
		Payload: map[string]any{"text": text},
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.eventCh <- ev:
		return nil
	}
}

func (m *mockDriver) Close() error { return nil }

// openServeDriver spawns `arxi serve` and performs the NDJSON handshake.
//
// Phase 0.5 wiring: the subprocess communicates over stdin/stdout using
// NDJSON (one JSON object per line). The first line from the server is a
// hello; the client sends protoRequest objects (run.prompt); the server
// answers each with a protoResponse.
//
// Log events are followed by reading the run's event log file (the same
// mechanism as `arxi run attach`). The log path is resolved from ARXI_RUN_DIR
// (or defaults to ~/.arxi/runs/last/events.ndjson). LogFollow polls the file
// at 120ms and feeds events into the same channel the mock used in Phase 0.
//
// The subprocess lifecycle is managed by the procgroup supervisor from
// arxi-sim (internal/ext/supervisor), which will be ported in Phase 2.
func openServeDriver(ctx context.Context, arxiBin string) (Driver, <-chan fold.Event, error) {
	// Resolve the event log path for log-follow. ARXI_RUN_DIR points at the
	// directory for a specific run; if unset, default to ~/.arxi/runs/last.
	logPath := filepath.Join(os.Getenv("ARXI_RUN_DIR"), "events.ndjson")
	if os.Getenv("ARXI_RUN_DIR") == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("phase 0.5: cannot resolve home dir for default log path: %w", err)
		}
		logPath = filepath.Join(home, ".arxi", "runs", "last", "events.ndjson")
	}

	// Start log-follow before spawning the subprocess, so events written by
	// the core are caught from the very first line.
	eventCh, err := driver.LogFollow(ctx, logPath)
	if err != nil {
		return nil, nil, fmt.Errorf("phase 0.5: log-follow %s: %w", logPath, err)
	}

	// Spawn `arxi serve` as a subprocess. Stdin/stdout are pipes for the
	// NDJSON request/response protocol (ADR-0002).
	cmd := exec.CommandContext(ctx, arxiBin, "serve")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("phase 0.5: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, nil, fmt.Errorf("phase 0.5: stdout pipe: %w", err)
	}

	// Connect stderr to the host's stderr so arxi's diagnostics are visible.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, nil, fmt.Errorf("phase 0.5: spawn arxi serve: %w", err)
	}

	nd := driver.NewNDJSON(struct {
		io.Reader
		io.Writer
	}{stdout, stdin})

	// Handshake: read the hello, validate version.
	if err := nd.Handshake(ctx); err != nil {
		stdin.Close()
		stdout.Close()
		cmd.Process.Kill()
		return nil, nil, fmt.Errorf("phase 0.5: handshake: %w", err)
	}

	sd := &serveDriver{
		nd:      nd,
		cmd:     cmd,
		logPath: logPath,
	}
	return sd, eventCh, nil
}

// serveDriver adapts NDJSONDriver to the Driver interface the loop expects.
// Prompts go over the NDJSON request/response protocol; the core's responses
// arrive as log events on the LogFollow channel.
type serveDriver struct {
	nd      *driver.NDJSONDriver
	cmd     *exec.Cmd
	logPath string
}

// runID derives the run ID from the log path directory name. In Phase 0.5 the
// run is identified by where its log lives; later phases may negotiate this
// during handshake instead.
func (d *serveDriver) runID() string {
	dir := filepath.Base(filepath.Dir(d.logPath))
	if dir == "" || dir == "." {
		return "last"
	}
	return dir
}

func (d *serveDriver) SubmitPrompt(ctx context.Context, text string) error {
	_, err := d.nd.SubmitPrompt(ctx, d.runID(), text)
	return err
}

func (d *serveDriver) Close() error {
	_ = d.cmd.Process.Kill()
	_ = d.cmd.Wait()
	return nil
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
func loop(ctx context.Context, tty Terminal, doc *scene.Document, theme *theme.Theme, eventCh <-chan fold.Event, drv Driver) error {
	panicGesture := &driver.PanicGesture{}
	var collected []fold.Event
	var input string

	repaint := func() {
		state := fold.Fold(collected)
		state.UserInput = input // view state: the host owns the input buffer

		// slash.* view-state: when the buffer starts with "/", the slash
		// menu is active and the typed substring filters the command list.
		// This is arxi-tui's own contract (BINDS.md §4.3), not a core event.
		if strings.HasPrefix(input, "/") {
			state.SlashActive = true
			state.SlashTyped = input[1:]
			state.SlashMatches = fold.FilterSlashMatches(state.SlashTyped)
		} else {
			state.SlashActive = false
			state.SlashTyped = ""
			state.SlashMatches = nil
		}

		// host.escape.armed mirrors the panic gesture's current arm state.
		// A scene may show it (e.g. a dim "press Ctrl-C again to quit" hint),
		// but no scene may capture the gesture (invariant 6).
		state.EscapeArmed = panicGesture.Armed()

		var r engine.Renderer
		w, h := tty.Size()
		r.Width, r.Height = w, h
		render(tty, doc, r, theme, state)
	}

	repaint()

	termEvents := tty.Events()
	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-termEvents:
			if !ok {
				return nil // TTY channel closed: session ended
			}
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
					input = typeKey(input, ev.Key, ctx, drv)
				}
				repaint()
			case term.EventResize:
				repaint()
			case term.EventClosed:
				return nil
			}

		case e, ok := <-eventCh:
			if !ok {
				// Driver channel closed: no more events. Keep running for
				// terminal input (e.g. user wants to review the transcript).
				eventCh = nil
			} else {
				collected = append(collected, e)
				repaint()
			}
		}
	}
}

// typeKey applies one keypress to the input buffer, or submits the line.
// Enter submits as run.prompt: in Phase 0 the event lands in the local fold
// directly (the mock driver feeds it); Phase 0.5 the same event goes to the
// core over the NDJSON wire and comes back through the log, so the fold never
// learns a second path.
func typeKey(input string, k term.Key, ctx context.Context, drv Driver) string {
	switch {
	case k.Type == term.KeyEnter:
		text := strings.TrimSpace(input)
		if text == "" {
			return input
		}
		// Slash command: the buffer starts with "/". Phase 2 introduces /ui
		// mutation commands; for now a slash prefix is treated as a normal
		// prompt so the transcript round-trips during Phase 0 development.
		if strings.HasPrefix(text, "/") {
			text = strings.TrimSpace(text[1:])
			if text == "" {
				return input
			}
		}
		_ = drv.SubmitPrompt(ctx, text)
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

// loadScene reads a scene document from path, validates it, and falls back to
// the factory RAW scene if the file is missing or fails to parse/validate.
// This implements invariant 3: a corrupt scene on disk falls back to the raw
// scene with a file:line notice, never a crash.
func loadScene(path string, fallback string) (*scene.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Scene file missing: use the fallback factory scene.
		return scene.ParseDocument([]byte(fallback))
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		// Parse error: fall back to the factory RAW scene so the
		// interface always boots (invariant 3).
		doc, fbErr := scene.ParseDocument([]byte(fallback))
		if fbErr != nil {
			return nil, fmt.Errorf("scene parse %s: %w (and fallback also failed: %v)", path, err, fbErr)
		}
		return doc, nil
	}
	if err := doc.Validate(); err != nil {
		// Validation error (e.g. unsigned bind): fall back to factory RAW.
		fbDoc, fbErr := scene.ParseDocument([]byte(fallback))
		if fbErr != nil {
			return doc, fmt.Errorf("scene validate %s: %w (fallback also failed: %v)", path, err, fbErr)
		}
		return fbDoc, nil
	}
	return doc, nil
}

// render repaints the whole screen. Phase 0 uses clear-home + full redraw;
// the cell-diff repaint (the emitter's own machinery) is Phase 0.5 work on
// top of the same Frame.
func render(w io.Writer, doc *scene.Document, r engine.Renderer, theme *theme.Theme, state fold.State) {
	f := r.RenderFrame(doc, state)
	fmt.Fprint(w, "\033[H\033[2J")
	// Raw mode turned the terminal's output processing off (OPOST/ONLCR), so
	// the terminal no longer translates \n into \r\n: a bare newline drops
	// one row and keeps the column, and every line after the first climbs
	// further right — the staircase. The Frame carries plain \n because it is
	// mode-blind; the emit path is where they become \r\n, because the emit
	// path is the only code that knows the terminal is in raw mode.
	//
	// ANSI() resolves token names ("input", "text", "input.placeholder") to
	// styles and emits SGR escape codes. When no theme is wired, it falls back
	// to Plain() — the same unstyled text the goldens compare.
	fmt.Fprint(w, strings.ReplaceAll(f.ANSI(theme), "\n", "\r\n"))
	// The caret is the frame's, not the last byte's. Writing the frame leaves
	// the terminal cursor at the end of the final row — the bottom-right corner
	// of a full repaint — and a text field whose caret sits in the corner is a
	// text field that does not look like one. A frame that hosts no caret has
	// the terminal's hidden instead, so nothing blinks on a row that means
	// nothing.
	if f.Cursor.Hidden {
		fmt.Fprint(w, "\033[?25l")
		return
	}
	fmt.Fprint(w, "\033[?25h")
	fmt.Fprintf(w, "\033[%d;%dH", f.Cursor.Line+1, f.Cursor.Col+1)
}

// runNonInteractive renders a single frame to stdout when stdin is not a
// terminal. Same scene, same fold, same renderer — no tty path is a second
// renderer.
func runNonInteractive(doc *scene.Document, theme *theme.Theme) error {
	r := engine.Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, fold.Fold(nil))
	// No terminal: plain output. ANSI escapes in a pipe would pollute greps.
	fmt.Print(f.Plain())
	return nil
}
