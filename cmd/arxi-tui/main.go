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
// so the interface boots even when testdata/SOBRIA.json is missing.
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
                       "Appearance","Security","Workspace","Media","Extensions","Product"] } ] },

  { "id": "status", "type": "row", "children": [
    { "type": "text", "bind": "slash.hint", "style": {"style": "dim"},
      "when": "slash.hint" },
    { "type": "text", "bind": "agent.mode", "style": {"style": "header"},
      "when": "status.active" },
    { "type": "text", "text": " · ", "style": {"style": "dim"}, "when": "status.active" },
    { "type": "text", "bind": "model.name", "style": {"style": "dim"},
      "when": "status.active" },
    { "type": "text", "text": " · ⚡︎", "style": {"style": "dim"},
      "when": "status.active" } ] }
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
	doc, sceneNotice, err := loadScene("testdata/SOBRIA.json", factoryRAW)
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
		return runNonInteractive(doc, theme, sceneNotice)
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

	return loop(ctx, tty, doc, theme, eventCh, drv, sceneNotice)
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
// sceneNotice is the addressed reason the requested scene was refused, or ""
// when the active scene is the one that was asked for. It is host state exactly
// as the input buffer is — the fold is rebuilt every frame and carries it, but
// no core event produces it (BINDS.md §2: `host.scene.error` is "the one bind
// the scene may render but the core never provides").
func loop(ctx context.Context, tty Terminal, doc *scene.Document, theme *theme.Theme, eventCh <-chan fold.Event, drv Driver, sceneNotice string) error {
	panicGesture := &driver.PanicGesture{}
	var collected []fold.Event
	var input string
	// slashSel is the menu's highlighted row. The host owns it across frames
	// the way it owns the input buffer: the fold is rebuilt per frame and
	// carries it, but the state of the menu is not the scene's business.
	slashSel := 0

	repaint := func() {
		state := fold.Fold(collected)
		state.UserInput = input // view state: the host owns the input buffer
		// Invariant 3's other half: the fallback scene is on screen, and this
		// is the notice that says why. It is re-applied on every repaint
		// because Fold rebuilds State from the event list each frame
		// (ADR-0004, pull by frame), so a value set once would vanish on the
		// next keystroke.
		state.SceneError = sceneNotice

		// slash.* view-state: when the buffer starts with "/", the slash
		// menu is active and the typed substring filters the command list.
		// This is arxi-tui's own contract (BINDS.md §4.3), not a core event.
		if strings.HasPrefix(input, "/") {
			state.SlashActive = true
			state.SlashTyped = input[1:]
			state.SlashMatches = fold.FilterSlashMatches(state.SlashTyped)
			// The selection indexes the filtered list, so a keystroke that
			// shrinks it must not leave the highlight past the last row: the
			// menu would show no bright row while Enter would still submit
			// the clamped one.
			if slashSel >= len(state.SlashMatches) {
				slashSel = len(state.SlashMatches) - 1
			}
			if slashSel < 0 {
				slashSel = 0
			}
			state.SlashSelected = slashSel
		} else {
			state.SlashActive = false
			state.SlashTyped = ""
			state.SlashMatches = nil
		}

		// The bottom line is either the live status row or the menu's
		// navigation hint, never both: the menu's help replaces the status
		// row so there is exactly one line of info at the edge of the screen.
		// The scene gates each row with `when`, so the host only has to publish
		// the two view-state binds that drive it (BINDS.md §4.3).
		if state.SlashActive {
			state.SlashHint = "↑↓ navigate · enter use · esc close"
			state.StatusActive = "false"
		} else {
			state.SlashHint = ""
			state.StatusActive = "true"
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
					if strings.HasPrefix(input, "/") {
						// The menu is open: navigation steers the highlight
						// and never reaches the buffer. Ctrl-C never gets
						// here, so the escape hatch stays uncapturable
						// (invariant 6) no matter what the menu does.
						input, slashSel = slashMenuKey(input, ev.Key, slashSel, ctx, drv)
					} else {
						input = typeKey(input, ev.Key, ctx, drv)
					}
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

// slashMenuKey applies one keypress while the slash menu is open. Up and down
// steer the highlight, tab walks the categories, escape closes the menu by
// dropping the line — the menu is derived from the "/" prefix, so a dismissed
// menu is a cleared buffer — and enter runs the highlighted command, which is
// what the menu's footer promises. The selection comes back because it lives
// across frames in the loop, the way the input buffer does; any key that
// changes the filter puts it back on the first row.
func slashMenuKey(input string, k term.Key, sel int, ctx context.Context, drv Driver) (string, int) {
	matches := fold.FilterSlashMatches(strings.TrimPrefix(input, "/"))
	switch k.Type {
	case term.KeyUp:
		if len(matches) == 0 {
			return input, sel
		}
		// Wrap at both ends: the highlight is the only thing the keyboard moves,
		// so the user must always feel a row under it no matter how far up they
		// spin the wheel (rotary, as requested).
		sel = (sel - 1 + len(matches)) % len(matches)
		return input, sel
	case term.KeyDown:
		if len(matches) == 0 {
			return input, sel
		}
		sel = (sel + 1) % len(matches)
		return input, sel
	case term.KeyTab:
		// Walk to the first match of the next distinct category, wrapping to
		// the top. With a single category this lands on row 0, which is also
		// the sane thing for tab to do there.
		if len(matches) == 0 {
			return input, sel
		}
		if sel >= len(matches) {
			sel = 0
		}
		cur := matches[sel].Category
		next := 0
		for i := sel + 1; i < len(matches); i++ {
			if matches[i].Category != cur {
				next = i
				break
			}
		}
		return input, next
	case term.KeyEscape:
		return "", 0
	case term.KeyEnter:
		if len(matches) == 0 {
			return input, sel
		}
		if sel >= len(matches) {
			sel = len(matches) - 1
		}
		// Phase 0: a command submits as a prompt (typeKey's contract); Phase 2
		// routes /ui to the mutation surface.
		_ = drv.SubmitPrompt(ctx, matches[sel].Name)
		return "", 0
	default:
		next := typeKey(input, k, ctx, drv)
		if next != input {
			return next, 0 // the filter changed: the first row is selected again
		}
		return input, sel
	}
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
// The second return value is the notice, and it is the half that was missing:
// the fallback worked, but the refusal was dropped on the floor, so a user whose
// scene failed to load got the raw interface and no statement of why. Invariant
// 3 names both halves — "falls back to the raw scene *with the file:line:
// notice on screen*" — and BINDS.md §2 signs the channel for it
// (`host.scene.error`, "the last-good-scene notice; null means the active scene
// validated"). An empty notice means the active scene is the one asked for.
func loadScene(path string, fallback string) (*scene.Document, string, error) {
	// ParseFile carries the path into the error, so the notice addresses the
	// file the user would open rather than the bytes the host happened to read.
	doc, err := scene.ParseFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A missing scene file is not a defect: the default install has
			// no user scene, so the factory scene is the intended document
			// and there is nothing to report.
			fbDoc, fbErr := scene.ParseDocument([]byte(fallback))
			return fbDoc, "", fbErr
		}
		// Parse error: fall back to the factory scene so the interface always
		// boots (invariant 3), and carry the addressed reason to the screen.
		fbDoc, fbErr := scene.ParseDocument([]byte(fallback))
		if fbErr != nil {
			return nil, "", fmt.Errorf("scene parse %s: %w (and fallback also failed: %v)", path, err, fbErr)
		}
		return fbDoc, err.Error(), nil
	}
	// A document that parsed into no tree is refused before its binds are
	// checked, because there is nothing to check and "valid" would be the
	// wrong word for a scene that draws nothing. Measured: the whole tree
	// under a wrong top-level key (`{"scene": …}`) parsed, validated clean,
	// and rendered an empty screen with no statement of why — the fallback
	// never fired, because nothing told the load path anything was wrong.
	if err := doc.RefuseEmpty(); err != nil {
		fbDoc, fbErr := scene.ParseDocument([]byte(fallback))
		if fbErr != nil {
			return doc, "", fmt.Errorf("scene %s has no root: %w (fallback also failed: %v)", path, err, fbErr)
		}
		return fbDoc, err.Error(), nil
	}

	if err := doc.Validate(); err != nil {
		// Validation error (e.g. unsigned bind): same contract as a parse
		// error. A scene that parses but cannot be satisfied fails the way a
		// syntax error does — PLAN.md invariant 3 makes that equivalence
		// explicit so a semantically dead scene cannot take the session down.
		fbDoc, fbErr := scene.ParseDocument([]byte(fallback))
		if fbErr != nil {
			return doc, "", fmt.Errorf("scene validate %s: %w (fallback also failed: %v)", path, err, fbErr)
		}
		return fbDoc, err.Error(), nil
	}

	// Warnings are the other half of PLAN.md's forward-compatibility rule,
	// and they ride the channel invariant 3 already built: the scene loads,
	// and the notice says what the engine did not understand. Reaching the
	// screen is the whole point — a warning computed and dropped would be
	// this project's recurring failure in its purest form, a remedy that
	// satisfies the guard and changes nothing the user can see.
	//
	// The document is returned as-is, not replaced by the fallback: an
	// unknown property is a v1 document under a v0 engine, and refusing it
	// would break the compatibility promise the warning exists to keep.
	if warnings := doc.Warnings(); len(warnings) > 0 {
		return doc, warningNotice(warnings), nil
	}
	return doc, "", nil
}

// warningNotice renders scene warnings into the single line host.scene.error
// carries.
//
// Only the first is shown in full, with a count for the rest. The notice is
// one line of a terminal interface: printing ten warnings there would push the
// interface off the screen to complain about properties that, by definition,
// did not stop it from loading. The first has an address, and fixing it is how
// the author finds the next.
func warningNotice(warnings []scene.Warning) string {
	first := warnings[0].String()
	if len(warnings) == 1 {
		return first
	}
	return fmt.Sprintf("%s (and %d more)", first, len(warnings)-1)
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
func runNonInteractive(doc *scene.Document, theme *theme.Theme, sceneNotice string) error {
	r := engine.Renderer{Width: 80, Height: 24}
	state := fold.Fold(nil)
	state.SceneError = sceneNotice
	f := r.RenderFrame(doc, state)
	// No terminal: plain output. ANSI escapes in a pipe would pollute greps.
	fmt.Print(f.Plain())
	return nil
}
