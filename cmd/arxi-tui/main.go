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
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/patch"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

// The notice row every shipped document carries, and the reason it is not
// optional chrome.
//
// invariant 3 is a conjunction — a corrupt scene on disk "falls back to the raw
// scene **with the `file:line:` notice on screen**" — and until this constant
// existed the two halves were true separately and false together. The fallback
// fired, the notice was computed in full, and nothing drew it, because the
// document on screen after a refusal is this file's factory scene and no
// factory scene bound the field. Measured on the built binary: a scene carrying
// one unsigned bind rendered twenty-four blank rows while
//
//	testdata/SOBRIA.json:3:3: unsigned bind "model.curent" in node type
//	"input"; every bind must appear in BINDS.md §4.5
//
// sat in host state with nowhere to go. Every layer had done its job —
// loadScene addressed it, run() passed it, loop() wrote it to state, and
// resolveBind answered host.scene.error correctly — and the string died one
// function short of a pixel.
//
// BINDS.md §2 calls host.scene.error "the one bind the scene may render but the
// core never provides". A channel no shipped document reads is not a channel,
// and a notice delivered into an unbound field is indistinguishable from a
// notice never computed — worse, because every guard on the computation passes.
// This is the sixth instance of the class this repository keeps paying for, and
// the purest: the previous five were a layer that knew the answer and did not
// print it, and here the answer is printed into a field nobody is listening on.
//
// `when` is what lets this cost nothing. The field is signed "text | null",
// BINDS.md §2 defines null as "the active scene validated", and evalWhen
// already reads the empty string as false — so on a clean boot the node renders
// zero rows and the raw scene is still the two nodes PLAN.md promises. Without
// the gate this would be the always-on warning light that
// TestAValidSceneReportsNoNotice rejects one layer up.
//
// It is spliced into each document as text rather than injected by the renderer,
// and that is the same objection that keeps the planned type list out of the
// engine: a host that welds a node into every tree has made the notice a
// privileged construction the user's format can neither express nor remove,
// which contradicts the thesis the whole project rests on. Every scene says it
// in the user's own vocabulary, so a user who wants it elsewhere moves it, and
// one who deletes it has chosen silence explicitly.
const factoryNoticeNode = `{ "id": "notice", "type": "text", "bind": "host.scene.error", "when": "host.scene.error", "style": {"style": "banner"} }`

const factoryRAW = `{ "root": { "type": "stack", "children": [
  ` + factoryNoticeNode + `,
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}`

// factorySobria is the Scene 2 sobria default, embedded as a fallback constant
// so the interface boots even when testdata/SOBRIA.json is missing.
const factorySobria = `{ "root": { "type": "stack", "children": [
  { "type": "text", "style": {"style": "header"},
    "text": "Δr×i v0.1.0 · Run /help for commands" },

  ` + factoryNoticeNode + `,

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
	// -scene names the document to boot. Its default is the shipped SOBRIA
	// scene; a path lets a user (or a tester) boot any document —
	// testdata/ANIMATION.json to see the motion props, testdata/SUBAGENTS.json
	// the row template, and so on — without editing the binary.
	scenePath := flag.String("scene", "testdata/SOBRIA.json",
		`scene document to boot (a file path); use -raw for the factory raw scene`)
	// -raw is the start-time escape hatch invariant 6 names beside double Ctrl-C:
	// it boots the factory raw scene regardless of -scene. It exists as its own
	// flag because the documented spelling -scene "" is unreachable from
	// PowerShell, which strips the empty quotes and leaves -scene with no
	// argument (a flag-parse error); a boolean has no argument to strip, so the
	// escape hatch works from every shell.
	raw := flag.Bool("raw", false, `boot the factory raw scene (the start-time escape hatch)`)
	flag.Parse()
	scenePath0 := *scenePath
	if *raw {
		scenePath0 = ""
	}
	if err := run(scenePath0); err != nil {
		fmt.Fprintf(os.Stderr, "arxi-tui: %v\n", err)
		os.Exit(1)
	}
}

func run(scenePath string) error {
	// Load scene document. The default is the sobria scene (Scene 2, the
	// fx-inspired default per PLAN.md); if it fails to parse or validate, fall
	// back to the factory RAW scene (Scene 1) so the interface always boots.
	doc, sceneNotice, err := resolveStartScene(scenePath)
	if err != nil {
		return fmt.Errorf("scene load: %w", err)
	}

	// Load theme. The factory SOBRIA theme is compiled in and adapts to the
	// terminal's background through relative dim/bright attributes the terminal
	// itself resolves -- there is no OSC 11 background query (theme.SOBRIA
	// argues why: dim and bright already read correctly on light and dark).
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

	// Ask the terminal to report mouse events so the wheel can scroll the chat
	// pane. ?1000h reports button presses (the wheel is a button), and ?1006h is
	// the SGR extension that carries coordinates past column 223. Button-press
	// tracking (1000) rather than motion tracking (1002/1003) is deliberate: we
	// only need wheel notches, and reporting every drag would fight the
	// terminal's own text selection more than necessary. The decoder already
	// turns a wheel report into KeyWheelUp/KeyWheelDown. Torn down before the
	// alternate buffer is left, in reverse order, so the user's shell is handed
	// back with mouse reporting off exactly as it was found.
	fmt.Fprint(tty, "\033[?1000h\033[?1006h")
	defer fmt.Fprint(tty, "\033[?1006l\033[?1000l")

	// Bracketed paste: the terminal wraps a pasted block in \033[200~ … \033[201~
	// so it arrives as one EventPaste with its newlines intact, instead of a burst
	// of keys in which every newline is a plain Enter. Without it a multi-line
	// paste submits every line but the last (the reported bug); with it the whole
	// block lands at the caret. Torn down before the alternate buffer is left, so
	// the shell is handed back with paste bracketing off exactly as it was found.
	fmt.Fprint(tty, "\033[?2004h")
	defer fmt.Fprint(tty, "\033[?2004l")

	// Kitty keyboard, disambiguate-escape-codes only (CSI > 1 u). This is what
	// lets Shift+Enter arrive as its own key (CSI 13;2u) instead of a bare CR
	// indistinguishable from Enter, so the newline gesture is reachable with the
	// chord most users reach for. The flag is the mildest level: ordinary text
	// still arrives as text and legacy keys keep their bytes, so nothing else in
	// the decoder changes — only the previously-unreachable modified chords gain
	// a spelling. A terminal that does not implement Kitty silently ignores both
	// the push and the pop (an unknown CSI is dropped, never printed), so Ctrl+J
	// remains the newline that works everywhere. Popped (CSI < u) before the
	// alternate buffer is left so the shell's keyboard mode is restored.
	fmt.Fprint(tty, "\033[>1u")
	defer fmt.Fprint(tty, "\033[<u")

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
			"text":      "Hi! How can I help you?",
			"tokens_in": 12, "tokens_out": 18, "cost_usd": 0.0004,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
		{Type: "run.prompt", Seq: 5, Payload: map[string]any{"text": "thanks"}},
		{Type: "agent.activated", Seq: 6, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 7, Payload: map[string]any{
			"agent": "backend", "model": "openai/gpt-4o",
			"text":      "You're welcome.",
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
	// caret is the rune index of the edit point within input, host-owned view
	// state held across frames exactly like input itself. It lets the arrow keys
	// move the cursor and edit the middle of the line; the renderer places the
	// native terminal cursor at its column.
	caret := 0
	// chatScroll is how many lines the chat pane is scrolled up from the tail,
	// host-owned view state held across frames like the input buffer. Zero
	// follows the tail (the default); the mouse wheel raises it to reveal older
	// turns. The renderer clamps it to the frame's line count and reports the
	// ceiling back through r.ChatScrollMax, which the repaint below pins it to.
	chatScroll := 0
	// slashSel is the menu's highlighted row. The host owns it across frames
	// the way it owns the input buffer: the fold is rebuilt per frame and
	// carries it, but the state of the menu is not the scene's business.
	slashSel := 0
	// uiHidden is the `ui.hidden` set (BINDS.md §4.3): the ids the user has
	// hidden with `/ui hide`. It is host-owned view state held across frames
	// like the input buffer and slashSel, because no core event produces it —
	// the fold is rebuilt from the log each frame and would forget a set kept
	// only in State. The engine walk reads it as a visibility filter, so a
	// keystroke that hides a node must survive the next repaint.
	uiHidden := map[string]bool{}

	// The host animation clock (ADR-0005). It holds per-node elapsed time
	// across frames like uiHidden above, feeds the renderer a phase, and reads
	// back which nodes are animating so the ticker below runs only while one
	// is. It is off entirely for a scene with no animation prop, so the common
	// case repaints on input alone, exactly as before Block G.
	clock := newAnimClock(marqueeFPS(theme))
	// The loop has the theme; the renderer does not. A one-shot prop (G3 reveal)
	// reports its token name, and this is what turns that name into a
	// duration/curve/fps (ADR-0005). Set to the active theme's lookup, so a
	// reveal resolves the same anim section ValidateTokens checked its token
	// against at load.
	clock.resolveAnim = theme.Anim
	var animTicker *time.Ticker
	var tickCh <-chan time.Time
	armTicker := func() {
		switch {
		case clock.running() && animTicker == nil:
			animTicker = time.NewTicker(clock.interval())
			tickCh = animTicker.C
		case !clock.running() && animTicker != nil:
			animTicker.Stop()
			animTicker = nil
			tickCh = nil
		}
	}
	defer func() {
		if animTicker != nil {
			animTicker.Stop()
		}
	}()

	repaint := func() {
		state := fold.Fold(collected)
		state.UserInput = input // view state: the host owns the input buffer
		state.UserInputCaret = caret
		// ui.hidden is host-owned view state the loop keeps across frames, so it
		// is re-attached on every repaint for the same reason the input buffer
		// and the scene error are (Fold rebuilds State from the log each frame).
		state.UIHidden = uiHidden
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

		// The animation clock advances by wall time, hands the renderer the
		// phase, and reads back which nodes are still animating; then the
		// ticker is armed or stopped to match. The frame goes through the same
		// emit path render() uses, so the animated repaint and a plain one
		// cannot diverge in how they reach the terminal.
		clock.advance(time.Now())
		r.AnimTicks = clock.ticks()
		r.AnimPhase = clock.phases()
		r.ChatScroll = chatScroll
		frame, active := r.RenderFrameActive(doc, state)
		// Pin the scroll offset to what the renderer could actually honour: it
		// alone knows the wrapped line count and the pane budget, so a wheel spun
		// past the top settles here instead of banking dead scroll that a later
		// wheel-down would have to unwind first.
		if chatScroll > r.ChatScrollMax {
			chatScroll = r.ChatScrollMax
		}
		clock.reconcile(active)
		emitFrame(tty, frame, theme, h)
		armTicker()
	}

	repaint()

	termEvents := tty.Events()
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-tickCh:
			// The animation clock's fourth reason to repaint (ADR-0005). A tick
			// only repaints; it reads no input and dispatches no gesture, so the
			// escape hatch stays uncapturable (invariant 6) — Ctrl-C is handled
			// on the terminal channel, and a runaway animation cannot wedge the
			// door. tickCh is nil while nothing animates, and a receive on a nil
			// channel blocks forever, so this case simply never fires then.
			repaint()

		case ev, ok := <-termEvents:
			if !ok {
				return nil // TTY channel closed: session ended
			}
			// Coalesce a burst of terminal events into a single repaint. A fast
			// wheel spin, a held arrow, or paste-by-typing lands many events on
			// this channel at once; dispatching them all and painting once after
			// the batch is what stops the scroll from trailing the wheel a frame
			// at a time — the "leve retraso" reported after the flicker fix —
			// while dropping no event, since each still runs the same dispatch.
			// The in-place synchronized repaint that removed the flicker is
			// unchanged; only how often it runs under a burst is. The panic
			// gesture is unaffected: HandleCtrlC still sees every press in order,
			// so the double-Ctrl-C timing is decided on the events, not on frames.
			pending := true
			for pending {
				switch ev.Kind {
				case term.EventKey:
					if isCtrlC(ev.Key) {
						if panicGesture.HandleCtrlC(time.Now()) {
							if len(collected) == 0 && input == "" {
								return nil // nothing to restore: the door
							}
							collected = nil
							input = ""
							caret = 0
						} else {
							input = "" // first press clears the line
							caret = 0
						}
					} else if ev.Key.Type == term.KeyWheelUp || ev.Key.Type == term.KeyWheelDown {
						// The mouse wheel scrolls the chat pane and nothing else: it
						// does not type, and it does not disarm the panic gesture (a
						// wheel notch is not the "any other key" that means the user
						// changed their mind about quitting). The renderer clamps the
						// offset to the frame, so scrolling up past the top or down
						// past the tail simply stops.
						const wheelStep = 3
						if ev.Key.Type == term.KeyWheelUp {
							chatScroll += wheelStep
						} else {
							chatScroll -= wheelStep
							if chatScroll < 0 {
								chatScroll = 0
							}
						}
					} else {
						panicGesture.Reset() // any other key disarms the gesture
						// The /ui dispatch is asked before the menu, and the order
						// is the fix for a measured dead end rather than a
						// preference. While the buffer starts with "/", every key
						// goes to slashMenuKey, whose Enter branch returns the
						// buffer untouched when the filter matches nothing — and
						// the filter matches on the whole typed string, so it
						// drops to zero the moment an argument is typed:
						//
						//	typed "ui"                  -> 1 match
						//	typed "ui style"            -> 0 matches
						//	typed "ui style status dim" -> 0 matches
						//
						// So a complete, correct command could be typed and Enter
						// did nothing at all. Not a refusal, not a prompt —
						// nothing, with the menu showing an empty list. Asking
						// the command surface first means a line it recognises is
						// never the menu's to swallow.
						if handled, next := uiCommandKey(input, ev.Key, &doc, &sceneNotice, uiHidden); handled {
							input = next
							caret = clampCaret(input, caret)
						} else if strings.HasPrefix(input, "/") {
							// The menu is open: navigation steers the highlight
							// and never reaches the buffer. Ctrl-C never gets
							// here, so the escape hatch stays uncapturable
							// (invariant 6) no matter what the menu does.
							input, caret, slashSel = slashMenuKey(input, caret, ev.Key, slashSel, ctx, drv)
						} else if ev.Key.Type == term.KeyUp || ev.Key.Type == term.KeyDown {
							// Vertical caret motion on a wrapped (multi-line) input.
							// It is intercepted here rather than in applyEdit because
							// up/down mean "walk the wrapped rows", which needs the wrap
							// width — the terminal width less the input's prefix — that
							// only the host and the document together know. On a
							// single-row line there is no row above or below, so the
							// move is a no-op and the key is harmlessly swallowed. It
							// sits after the slash branch, so while the menu is open
							// up/down still steer the highlight and never the caret.
							w, _ := tty.Size()
							dir := -1
							if ev.Key.Type == term.KeyDown {
								dir = 1
							}
							caret = engine.InputCaretVerticalMove(input, caret, inputWrapRoom(doc, w), dir)
						} else {
							input, caret = typeKey(input, caret, ev.Key, ctx, drv)
						}
					}
				case term.EventPaste:
					// A paste is text, never keys: nothing in it dispatches an
					// action or the escape hatch, so it disarms the panic gesture
					// like any other input and lands whole at the caret. Its
					// newlines are kept — a pasted command block is the block it
					// was, and the buffer holds '\n' so the input renders the
					// extra rows — where the alternative, one Enter per line,
					// submits every line but the last. cleanPaste drops any other
					// control byte so a stray ESC in the clipboard cannot inject
					// an escape sequence into the line the host re-emits.
					panicGesture.Reset()
					input, caret = insertText(input, caret, cleanPaste(ev.Text))
				case term.EventResize:
					// A resize only needs a fresh frame at the new size, delivered
					// by the batch repaint below like every other event in the run.
				case term.EventClosed:
					return nil
				}
				// Pull the next queued terminal event without blocking. When the
				// channel is momentarily empty the batch ends and the frame is
				// painted once below for the whole run of events just dispatched.
				select {
				case ev, ok = <-termEvents:
					if !ok {
						return nil
					}
				default:
					pending = false
				}
			}
			repaint()

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

// inputWrapRoom is the column width the user.input line wraps within: the
// terminal width less the display width of the input node's prefix, matching the
// `r.Width - prefixW` renderInput lays the rows out at. It walks the document for
// the node bound to user.input so the room follows the scene's own prefix rather
// than a hard-coded guess — a downloaded scene may prompt with "> " or nothing at
// all. A width of at least one is always returned, so a pathologically narrow
// terminal cannot make the caller divide by zero.
func inputWrapRoom(doc *scene.Document, width int) int {
	prefixW := 0
	if doc != nil {
		if in := findUserInput(doc.Root); in != nil {
			// Display width, not rune count, so a wide prefix glyph reserves the
			// cells it actually occupies.
			prefixW = ansi.StringWidth(in.PrefixText())
		}
	}
	room := width - prefixW
	if room < 1 {
		room = 1
	}
	return room
}

// findUserInput returns the first node bound to user.input, or nil. It is the
// one place the host resolves "the line the human types into" from the tree, so
// the wrap room and any later input-scoped lookup agree on which node that is.
func findUserInput(n *scene.Node) *scene.Node {
	if n == nil {
		return nil
	}
	if n.Type == "input" && n.Bind == "user.input" {
		return n
	}
	for _, c := range n.Children {
		if got := findUserInput(c); got != nil {
			return got
		}
	}
	return nil
}

// isNewlineGesture reports whether a key should insert a literal newline into the
// input rather than submit the line. Two spellings, so a newline is reachable on
// as many terminals as possible: Shift/Ctrl+Enter (the chord the user reaches
// for, delivered as a modified KeyEnter — Shift needs the Kitty disambiguation
// enabled at startup, Ctrl does not), and Ctrl+J, the 0x0a the decoder reports as
// 'j'+ModCtrl and the newline that needs no negotiation at all (it is also what
// Windows delivers for Ctrl+Enter). Alt+Enter is deliberately not used: Windows
// Terminal binds it to fullscreen, so the sibling project avoids it and so do we.
func isNewlineGesture(k term.Key) bool {
	if k.Type == term.KeyEnter && k.Mod&(term.ModShift|term.ModCtrl) != 0 {
		return true
	}
	if k.Type == term.KeyRunes && len(k.Runes) == 1 && k.Runes[0] == 'j' && k.Mod&term.ModCtrl != 0 {
		return true
	}
	return false
}

// typeKey applies one keypress to the input buffer at the caret, or submits the
// line. It returns the new buffer and the new caret (a rune index into it).
// Enter submits as run.prompt: in Phase 0 the event lands in the local fold
// directly (the mock driver feeds it); Phase 0.5 the same event goes to the
// core over the NDJSON wire and comes back through the log, so the fold never
// learns a second path.
func typeKey(input string, caret int, k term.Key, ctx context.Context, drv Driver) (string, int) {
	// A newline gesture inserts a literal '\n' at the caret instead of
	// submitting, so a prompt can span several lines. Plain Enter still submits;
	// the two are told apart by the modifier (Shift/Ctrl+Enter) or by Ctrl+J,
	// which is the newline Windows Terminal and conhost deliver for Ctrl+Enter
	// with no Kitty negotiation. This is checked before the Enter-submits branch
	// so the modified chord never reaches it.
	if isNewlineGesture(k) {
		return insertText(input, caret, "\n")
	}
	if k.Type == term.KeyEnter {
		text := strings.TrimSpace(input)
		if text == "" {
			return input, caret
		}
		// Slash command: the buffer starts with "/". /ui routes to the
		// mutation surface; every other slash line is still submitted as a
		// prompt, which is Phase 0's contract and stays until each command
		// has a host implementation.
		if strings.HasPrefix(text, "/") {
			text = strings.TrimSpace(text[1:])
			if text == "" {
				return input, caret
			}
		}
		_ = drv.SubmitPrompt(ctx, text)
		return "", 0
	}
	if next, nextCaret, ok := applyEdit(input, caret, k); ok {
		return next, nextCaret
	}
	return input, caret
}

// applyEdit is the caret-aware line editor shared by the ordinary input path and
// the slash line: it is the one place the buffer and the caret move together, so
// the two paths cannot disagree about what Left or Backspace mean. It handles
// caret motion (Left/Right/Home/End), deletion on both sides of the caret
// (Backspace before, Delete under), and insertion of a printable run at the
// caret. It returns ok=false for any key it does not act on (Enter, the menu's
// Up/Down/Tab, and so on) so the caller keeps its own handling for those.
//
// The buffer is treated as a []rune and the caret is a rune index, never a byte
// offset: a byte-indexed caret splits multi-byte glyphs and a run of them
// desynchronises the cursor from the text — the exact class of bug the sibling
// project fixed by storing runes, and there is no reason to relearn it here.
func applyEdit(input string, caret int, k term.Key) (string, int, bool) {
	r := []rune(input)
	if caret < 0 {
		caret = 0
	}
	if caret > len(r) {
		caret = len(r)
	}
	switch {
	case k.Type == term.KeyLeft && k.Mod&(term.ModCtrl|term.ModAlt) != 0:
		return input, wordLeft(r, caret), true
	case k.Type == term.KeyLeft:
		if caret > 0 {
			caret--
		}
		return input, caret, true
	case k.Type == term.KeyRight && k.Mod&(term.ModCtrl|term.ModAlt) != 0:
		return input, wordRight(r, caret), true
	case k.Type == term.KeyRight:
		if caret < len(r) {
			caret++
		}
		return input, caret, true
	case k.Type == term.KeyHome:
		return input, 0, true
	case k.Type == term.KeyEnd:
		return input, len(r), true
	case k.Type == term.KeyBackspace:
		if caret == 0 {
			return input, caret, true
		}
		r = append(r[:caret-1], r[caret:]...)
		return string(r), caret - 1, true
	case k.Type == term.KeyDelete:
		if caret >= len(r) {
			return input, caret, true
		}
		r = append(r[:caret], r[caret+1:]...)
		return string(r), caret, true
	case k.Type == term.KeyRunes && k.Mod&(term.ModCtrl|term.ModAlt) == 0:
		ins := k.Runes
		r = append(r[:caret], append(append([]rune{}, ins...), r[caret:]...)...)
		return string(r), caret + len(ins), true
	default:
		return input, caret, false
	}
}

// wordLeft moves the caret to the start of the previous word: skip any spaces to
// the left, then the run of non-spaces. Boundaries are whitespace runs, not
// punctuation — the same rule the sibling line editor and a shell's Ctrl+Left
// use — and a newline counts as space (unicode.IsSpace), so the jump crosses a
// line break in a multi-line prompt the way the terminal's own word-left does.
func wordLeft(r []rune, caret int) int {
	if caret > len(r) {
		caret = len(r)
	}
	for caret > 0 && unicode.IsSpace(r[caret-1]) {
		caret--
	}
	for caret > 0 && !unicode.IsSpace(r[caret-1]) {
		caret--
	}
	return caret
}

// wordRight is wordLeft's mirror: skip spaces to the right, then the run of
// non-spaces, landing the caret just past the current word.
func wordRight(r []rune, caret int) int {
	if caret < 0 {
		caret = 0
	}
	for caret < len(r) && unicode.IsSpace(r[caret]) {
		caret++
	}
	for caret < len(r) && !unicode.IsSpace(r[caret]) {
		caret++
	}
	return caret
}

// clampCaret pins a caret into a buffer's rune range, used when a caller replaces
// the buffer wholesale (a /ui command clearing the line) and the old caret may
// now point past the end.
func clampCaret(input string, caret int) int {
	n := len([]rune(input))
	if caret < 0 {
		return 0
	}
	if caret > n {
		return n
	}
	return caret
}

// insertText inserts a run of text into the buffer at the caret (a rune index),
// returning the new buffer and the caret past the inserted run. It is the paste
// path: a whole block lands as one edit, where applyEdit inserts a single
// keypress. Rune-indexed for the same reason applyEdit is — a byte offset would
// split a multi-byte glyph and desynchronise the caret from the text.
func insertText(input string, caret int, ins string) (string, int) {
	r := []rune(input)
	if caret < 0 {
		caret = 0
	}
	if caret > len(r) {
		caret = len(r)
	}
	insR := []rune(ins)
	out := make([]rune, 0, len(r)+len(insR))
	out = append(out, r[:caret]...)
	out = append(out, insR...)
	out = append(out, r[caret:]...)
	return string(out), caret + len(insR)
}

// cleanPaste keeps a pasted block insertable. Newlines survive so a pasted code
// block is the block it was (the buffer holds '\n' and the input renders the
// rows); a tab becomes a single space so the caret's column arithmetic stays
// honest; and every other control byte is dropped so a stray ESC or CSI in the
// clipboard cannot inject an escape sequence into the line the host re-emits.
// The terminal already normalised CR to LF before this saw the text.
func cleanPaste(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			// drop: a control byte in the clipboard is not text to edit
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// uiCommandKey handles Enter on a `/ui …` line: it applies the patch to the
// live scene and reports whether it took the key.
//
// It returns (false, _) for every key and every line that is not a submitted
// /ui command, so the caller's ordinary paths are untouched. That shape —
// "handled" rather than a mutation the caller must detect — is what lets the
// dispatch sit ahead of the slash menu without the menu having to know it
// exists.
//
// # Why the document and the notice are pointers
//
// Both are the loop's own state and both must survive the keystroke: the
// patched scene is what the next repaint draws, and the notice is what tells
// the user what changed. Returning them would make every caller responsible
// for storing them, and the loop already has one such value (the input
// buffer) whose handling is the reason slashSel exists.
//
// # Why a failed patch changes nothing but the notice
//
// PLAN.md invariant 3: an invalid patch never kills the session, the last
// good scene stays. The refusal is addressed — the patch surface re-parses
// its output, so the error names `file:line` — and it reaches the screen
// through `host.scene.error`, the bind BINDS.md §2 signs for exactly this.
// The user sees why, on the scene they still have.
func uiCommandKey(input string, k term.Key, doc **scene.Document, notice *string, hidden map[string]bool) (bool, string) {
	if k.Type != term.KeyEnter {
		return false, input
	}
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/ui") {
		return false, input
	}
	// "/uize the thing" is not a /ui command. Checking the prefix alone would
	// capture every word starting with those three letters and answer it with
	// a verb-list refusal, which is a confident wrong diagnosis — the failure
	// mode this project charges a repair turn for.
	if rest := text[len("/ui"):]; rest != "" && !strings.HasPrefix(rest, " ") {
		return false, input
	}

	src := (*doc).Source()
	if src == nil {
		// A hand-built document has no source text, so a source-to-source
		// patch has nothing to edit. Saying so is better than serialising the
		// tree: that would silently produce a *different* document — one
		// whose unknown keys were already dropped by scene.Node — and present
		// it as the user's scene.
		*notice = "/ui: this scene was not loaded from a file, so it has no source to patch"
		return true, ""
	}

	res, err := patch.Apply((*doc).Name(), src, text)
	if err != nil {
		*notice = err.Error()
		return true, ""
	}
	// A view-state command (hide/show) does not change the document — it
	// mutates the loop's ui.hidden set, which the next repaint reads through
	// the engine walk. Its Result carries the set op rather than a new source,
	// so the document is left as it was and only the set and the notice move.
	if res.ViewState != nil {
		applyViewState(hidden, res.ViewState)
		*notice = "/ui: " + res.Summary
		return true, ""
	}
	*doc = res.Doc
	// The change-diff view PLAN.md requires is, at this stage, the summary
	// line: the patch states what it altered in the user's vocabulary before
	// the change is trusted. A diff of re-indented JSON is not a description
	// of a change, and the full side-by-side view belongs with the
	// agent-driven half, where the proposal arrives before it is applied.
	*notice = "/ui: " + res.Summary
	return true, ""
}

// applyViewState folds one hide/show set op into the loop's ui.hidden set.
//
// The map is mutated in place rather than replaced so the loop's reference
// stays live across frames — the same map the repaint reads. ShowAll clears
// every id (the `/ui show *` form); otherwise the named ids are added or
// removed. It is a free function taking the map because the set is a loop local,
// not a field on any type the loop owns.
func applyViewState(hidden map[string]bool, op *patch.ViewStateOp) {
	if op.ShowAll {
		for id := range hidden {
			delete(hidden, id)
		}
		return
	}
	for _, id := range op.Hide {
		hidden[id] = true
	}
	for _, id := range op.Show {
		delete(hidden, id)
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
func slashMenuKey(input string, caret int, k term.Key, sel int, ctx context.Context, drv Driver) (string, int, int) {
	matches := fold.FilterSlashMatches(strings.TrimPrefix(input, "/"))
	switch k.Type {
	case term.KeyUp:
		if len(matches) == 0 {
			return input, caret, sel
		}
		// Wrap at both ends: the highlight is the only thing the keyboard moves,
		// so the user must always feel a row under it no matter how far up they
		// spin the wheel (rotary, as requested).
		sel = (sel - 1 + len(matches)) % len(matches)
		return input, caret, sel
	case term.KeyDown:
		if len(matches) == 0 {
			return input, caret, sel
		}
		sel = (sel + 1) % len(matches)
		return input, caret, sel
	case term.KeyTab:
		// Walk to the first match of the next distinct category, wrapping to
		// the top. With a single category this lands on row 0, which is also
		// the sane thing for tab to do there.
		if len(matches) == 0 {
			return input, caret, sel
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
		return input, caret, next
	case term.KeyEscape:
		return "", 0, 0
	case term.KeyEnter:
		if len(matches) == 0 {
			return input, caret, sel
		}
		if sel >= len(matches) {
			sel = len(matches) - 1
		}
		// Phase 0: a command submits as a prompt (typeKey's contract); Phase 2
		// routes /ui to the mutation surface.
		_ = drv.SubmitPrompt(ctx, matches[sel].Name)
		return "", 0, 0
	default:
		// The same caret-aware editor the ordinary path uses, so editing the
		// slash line (Left/Right/Home/End/Delete/Backspace/insert) behaves
		// identically. A change to the filtered text reselects the first row,
		// because the previously highlighted row may no longer exist.
		next, nextCaret, ok := applyEdit(input, caret, k)
		if !ok {
			return input, caret, sel
		}
		if next != input {
			return next, nextCaret, 0
		}
		return next, nextCaret, sel
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

// resolveStartScene picks the boot document named by the -scene flag. An empty
// path is the start-time escape hatch invariant 6 names beside double Ctrl-C:
// -scene "" restores the factory raw scene, bypassing any on-disk document, so a
// scene that captured the interface cannot also deny the user the flag that
// recovers from it. It loads factoryRAW directly rather than through loadScene's
// missing-file fallback, so the raw scene is the *intended* document with no
// notice, never the residue of a load that failed. A non-empty path goes through
// loadScene, keeping invariant 3's addressed fallback for a corrupt file.
func resolveStartScene(path string) (*scene.Document, string, error) {
	if path == "" {
		doc, err := scene.ParseDocument([]byte(factoryRAW))
		return doc, "", err
	}
	return loadScene(path, factoryRAW)
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
	emitFrame(w, r.RenderFrame(doc, state), theme, r.Height)
}

// frameBegin opens every repaint: it enters synchronized-output mode (DECSET
// 2026), so the terminal buffers the whole update and presents it in one atomic
// swap. This is the single largest flicker source removed — the old path cleared
// the entire screen (CSI 2J) and repainted, so every frame blanked to nothing
// before it drew again, and a wide terminal or a growing input made the blank
// visible. It doubles as the per-frame separator the loop tests split on, the
// role the clear-home sequence used to play.
const frameBegin = "\033[?2026h"

// emitFrame writes one already-rendered frame to the terminal. It is the single
// emit path: the loop's animation-aware repaint and the plain render() above
// both go through it, so "the frame that knows about the clock" and "the frame
// that does not" cannot emit differently.
//
// The repaint is in place, never a full clear. Each row is positioned absolutely
// (CUP) and erased to the end of the line (EL) just before it is painted, and the
// region below the last painted row is erased once (ED) so a frame shorter than
// the previous one cannot leave that frame's tail on screen. Nothing sends CSI 2J:
// erasing only the rows we are about to own — and only the tail below them —
// touches every cell that changed and not one that did not, which is what keeps a
// repaint from flashing. screenH is the terminal's row count, needed for the tail
// erase because the frame's own height is only its content.
//
// Absolute positioning is also why no newline is written between rows: a bare \n
// on a raw terminal (output processing off) drops one row and keeps the column —
// the staircase — and CUP sidesteps it entirely by naming the row and column of
// every line. The whole paint is wrapped in synchronized output so it is seen
// once, done.
func emitFrame(w io.Writer, f ui.Frame, theme *theme.Theme, screenH int) {
	var b strings.Builder
	b.WriteString(frameBegin)
	// Hide the cursor for the duration of the paint so it does not strobe across
	// the rows as they are written, then restore it (positioned) at the end.
	b.WriteString("\033[?25l")
	// Auto-wrap off: a row exactly as wide as the terminal would otherwise wrap
	// onto the next line, push every row below it down by one, and land the caret
	// — and the tail erase — on rows that no longer mean what they say.
	b.WriteString("\033[?7l")

	rows := f.Live
	for i := 0; i < len(rows); i++ {
		b.WriteString(fmt.Sprintf("\033[%d;1H", i+1)) // CUP: row i+1, column 1
		b.WriteString("\033[K")                       // EL: erase this row before painting it
		b.WriteString(rows[i].ANSI(theme))
	}
	// Erase from just below the content to the end of the display, so a shorter
	// frame does not leave the tail of a longer one behind. This is the only
	// erase that reaches rows the frame does not own, and it costs no flicker
	// because those rows are the ones going blank anyway.
	if len(rows) < screenH {
		b.WriteString(fmt.Sprintf("\033[%d;1H", len(rows)+1))
		b.WriteString("\033[0J")
	}
	b.WriteString("\033[?7h") // restore auto-wrap

	// The caret is the frame's, not the last byte's. A text field whose caret sits
	// in the bottom-right corner (where the last row's paint left it) does not look
	// like a text field; a frame that hosts no caret keeps it hidden so nothing
	// blinks on a row that means nothing.
	if !f.Cursor.Hidden {
		b.WriteString(fmt.Sprintf("\033[%d;%dH", f.Cursor.Line+1, f.Cursor.Col+1))
		b.WriteString("\033[?25h")
	}
	b.WriteString("\033[?2026l") // end synchronized output: present the frame
	fmt.Fprint(w, b.String())
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
