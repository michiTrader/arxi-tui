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
	"errors"
	"flag"
	"fmt"
	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
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
  { "id": "banner", "type": "row", "children": [
    { "type": "text", "text": "Δ", "style": {"style": "brand.1"} },
    { "type": "text", "text": "r", "style": {"style": "brand.2"} },
    { "type": "text", "text": "×", "style": {"style": "brand.3"} },
    { "type": "text", "text": "i", "style": {"style": "brand.4"} },
    { "type": "text", "text": " v0.1.0 · Run /help for commands", "style": {"style": "dim"} } ] },

  { "id": "banner_gap", "type": "text", "text": "" },

  ` + factoryNoticeNode + `,

  { "id": "chat", "type": "markdown", "bind": "chat.history", "grow": 1, "fit": true },

  { "id": "thinking_gap", "type": "text", "text": "", "when": "agent.working" },

  { "id": "thinking", "type": "marquee", "when": "agent.working",
    "bind": "thinking.text", "style": {"style": "dim"},
    "prefix": { "bind": "host.thinking" },
    "scroll": { "speed": 1 } },

  { "id": "input_gap_top", "type": "text", "text": "" },

  { "id": "prompt", "type": "input", "bind": "user.input", "prefix": "┃ " },

  { "id": "input_gap_bottom", "type": "text", "text": "", "when": "status.active" },

  { "id": "escape_hint", "type": "text", "text": "press ctrl+c again to exit",
    "when": "host.escape.armed", "style": {"style": "dim"} },

  { "id": "menu", "type": "overlay", "anchor": "bottom", "when": "slash.active",
    "children": [
      { "type": "rule", "style": {"style": "menu.rule"} },
      { "id": "cmds", "type": "list", "bind": "slash.matches",
        "filter_by": "typed", "count": true,
        "categories": ["All","General","Session","Account","Model",
                       "Appearance","Security","Workspace","Media","Extensions","Product"] },
      { "type": "rule", "style": {"style": "menu.rule"} } ] },

  { "id": "model_menu", "type": "overlay", "anchor": "bottom", "when": "model.active",
    "children": [
      { "id": "models", "type": "list", "bind": "model.matches" } ] },

  { "id": "status", "type": "row", "children": [
    { "type": "text", "bind": "slash.hint", "style": {"style": "menu.hint"},
      "when": "slash.hint" },
    { "type": "text", "bind": "host.mode", "style": {"style": "header"},
      "when": "host.mode" },
    { "type": "text", "text": " · ", "style": {"style": "dim"}, "when": "model.name" },
    { "type": "text", "bind": "model.name", "style": {"style": "dim"},
      "when": "model.name" },
    { "type": "text", "text": " · ", "style": {"style": "dim"}, "when": "host.effort" },
    { "type": "text", "bind": "host.effort", "style": {"style": "dim"},
      "when": "host.effort" },
    { "type": "text", "text": " · ", "style": {"style": "dim"}, "when": "host.cwd" },
    { "type": "text", "bind": "host.cwd", "style": {"style": "dim"},
      "when": "host.cwd" } ] }
]}}`

// version is the build's version string, stamped by the release build
// (scripts/build-release.sh) through -ldflags "-X main.version=…". It defaults
// to "dev" for a plain `go build`/`go install`, so an un-stamped binary says so
// honestly rather than claiming a release it is not. The install rule
// (docs/PLAN.md) ships one static binary per platform; `arxi-tui -version` is how
// a user and a bug report name which one they are running.
var version = "dev"

func main() {
	// -scene names the document to boot. Its default is the shipped SOBRIA
	// scene; a path lets a user (or a tester) boot any document —
	// testdata/ANIMATION.json to see the motion props, testdata/SUBAGENTS.json
	// the row template, and so on — without editing the binary.
	scenePath := flag.String("scene", "",
		`scene document to boot (a file path); by default the built-in sobria scene, which works from any folder; use -raw for the factory raw scene`)
	// -raw is the start-time escape hatch invariant 6 names beside double Ctrl-C:
	// it boots the factory raw scene regardless of -scene. It exists as its own
	// flag because the documented spelling -scene "" is unreachable from
	// PowerShell, which strips the empty quotes and leaves -scene with no
	// argument (a flag-parse error); a boolean has no argument to strip, so the
	// escape hatch works from every shell.
	raw := flag.Bool("raw", false, `boot the factory raw scene (the start-time escape hatch)`)
	showVersion := flag.Bool("version", false, `print the build version and exit`)
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	// With no -scene the built-in sobria scene boots, from any folder. An explicit
	// -scene "" keeps its old meaning, the raw scene, as does -raw.
	scenePath0 := builtinScene
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "scene" {
			scenePath0 = *scenePath
		}
	})
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

	// Cursor shape: a STEADY block (DECSCUSR 2) — the thick caret the user asked
	// for, deliberately not left to the terminal's own blink. Two earlier attempts
	// kept the terminal-native blink (a blinking block, DECSCUSR 1) and tried to
	// stop the emit path from disturbing it: first by not re-showing the caret each
	// frame, then by not re-positioning it. Both failed on an animated scene,
	// because the ~12×/s in-place repaint walks the cursor down every row it paints
	// (a CUP per row), and this terminal restarts its blink on any cursor motion —
	// not only on the caret's own CUP. A blinking shape simply cannot survive a
	// constant repaint. So the terminal is told to hold the block steady and the
	// host owns the blink instead: the emit path shows or hides the caret on a
	// wall-clock cadence (see emitFrame / the loop's blink ticker), which is one
	// deterministic on/off square wave on the idle and the animated scene alike —
	// the consistency the user reported missing. Restored to the terminal default
	// (0) on the way out. A terminal that does not implement DECSCUSR ignores it,
	// and the host blink still runs by toggling visibility.
	fmt.Fprint(tty, "\033[2 q")
	defer fmt.Fprint(tty, "\033[0 q")

	// Caret colour: amber, the same warm family as the Δr×i mark, instead of whatever
	// the terminal defaults to (often green). OSC 12 sets it and OSC 112 gives the
	// terminal's own colour back on the way out. A terminal that does not implement
	// OSC 12 drops it silently and keeps its own caret colour.
	fmt.Fprint(tty, "\033]12;"+caretColor+"\033\\")
	defer fmt.Fprint(tty, "\033]112\033\\")

	// Selection highlight: teal (OSC 17 sets the highlight background). When the
	// user drags to copy from the transcript, the default highlight on many
	// terminals is a muddy inverse that fights the sobria palette; a teal wash
	// reads as a deliberate part of the theme. Reset with OSC 117 on the way out
	// so the terminal's own selection colour returns. This is terminal-dependent
	// like the Kitty push above: a terminal that does not implement OSC 17 drops
	// it silently, and the selection simply keeps its native colour.
	fmt.Fprint(tty, "\033]17;#0f766e\033\\")
	defer fmt.Fprint(tty, "\033]117\033\\")

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

// actorLabeler is the optional capability a Driver has when it is following a
// real run: it can name that run's actor blueprint for the status bar
// (host.run.actor, BINDS.md §4.3). serveDriver implements it; the mock driver
// does not, because a mock follows no run and has no actor to name. The loop
// asserts for it rather than widening Driver, so a fake with no run is not
// forced to return a meaningless label.
type actorLabeler interface {
	ActorLabel() string
}

// sessionClearer is the optional capability a Driver has when it owns a
// conversation that /clear can end (serveDriver). The mock keeps no state beyond
// the loop's own event list, so it does not implement it.
type sessionClearer interface {
	ClearSession()
}

// sessionResumer is the optional capability a Driver has when it keeps the chat history
// it sends with each question (serveDriver): /resume hands it the history of the
// conversation brought back, so the model carries on from where it was.
type sessionResumer interface {
	ResumeSession(history []driver.ChatTurn)
}

// turnCanceller is the optional capability a Driver has when it can stop the turn in
// flight (serveDriver). CancelTurn reports whether there was a turn to stop, which is
// how Esc and Ctrl-C know whether "cancel" is what the key means right now.
type turnCanceller interface {
	CancelTurn() bool
}

// cancelRunningTurn stops the turn in flight, if the driver has one. The conversation
// gets its "Cancelled" line from the driver's own event, not from here.
func cancelRunningTurn(drv Driver) bool {
	c, ok := drv.(turnCanceller)
	return ok && c.CancelTurn()
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

	if err := checkArxiBin(arxiBin); err != nil {
		return nil, nil, err
	}

	// Phase 0.5: spawn arxi serve as a subprocess. The subprocess communicates
	// via stdin/stdout using the NDJSON request/response protocol (ADR-0002).
	// Log-follow reads the run's event log file separately.
	return openServeDriver(ctx, arxiBin)
}

// checkArxiBin refuses an ARXI_BIN that cannot be the arxi core, before it is spawned.
// The commonest slip is pointing it at this program (arxi-tui itself): the TUI would
// launch a copy of itself, read its terminal escape codes as the core's hello, and die
// with "hello is not JSON: invalid character", which names a protocol problem when the
// real cause is a wrong path. The sentence here names the cause and the fix instead.
func checkArxiBin(path string) error {
	// Split on both separators by hand: filepath.Base on Linux would not treat the
	// backslashes of a Windows path as separators.
	base := strings.ToLower(path[strings.LastIndexAny(path, `/\`)+1:])
	base = strings.TrimSuffix(base, ".exe")
	if strings.HasPrefix(base, "arxi-tui") {
		return fmt.Errorf("ARXI_BIN points at %q, which is this TUI, not the arxi core; "+
			"set ARXI_BIN to the arxi core executable (for example arxi.exe), or unset it to use the offline demo", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("ARXI_BIN is set to %q but that file cannot be read (%v); "+
			"fix the path to the arxi core executable, or unset ARXI_BIN to use the offline demo", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("ARXI_BIN is set to %q, which is a directory; it must be the arxi core executable", path)
	}
	return nil
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

// openServeDriver spawns `arxi serve`, performs the NDJSON handshake, and
// builds the serveDriver that begins a run on the first user line.
//
// Phase 0.5 / M2 wiring: the subprocess communicates over stdin/stdout using
// NDJSON (one JSON object per line). The first line from the server is a hello;
// the client begins a run with run.start (run.prompt/run.steer have no executor
// on this build, M1b) and follows the run's event log.
//
// Log-follow is NOT armed here. A run's event log does not exist until the run
// is created, and on this build run.start is the only way to create one -- and
// it needs the prompt. So the run.start round-trip and its log-follow are
// deferred to serveDriver.SubmitPrompt (the first user line); openServeDriver
// only spawns, handshakes and hands the loop the relay channel every run's
// events will arrive on. The <runsRoot>/<job_id>/events.ndjson layout and the
// run.start wire round-trip are the facts only a live `arxi serve` confirms;
// isolating the spawn/handshake here keeps that confirmation to serveDriver.
func openServeDriver(ctx context.Context, arxiBin string) (Driver, <-chan fold.Event, error) {
	runsRoot, err := defaultRunsRoot()
	if err != nil {
		return nil, nil, fmt.Errorf("phase 0.5: %w", err)
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

	// Handshake: read the hello, validate the surface version. requireRunStart
	// (inside startRun, at the first prompt) then gates on the hello's
	// implemented list -- the handshake here is what makes that hello available.
	if err := nd.Handshake(ctx); err != nil {
		stdin.Close()
		stdout.Close()
		cmd.Process.Kill()
		return nil, nil, fmt.Errorf("phase 0.5: handshake: %w", err)
	}

	sd := &serveDriver{
		rs:       nd,
		inbox:    nd,
		hub:      nd,
		getenv:   os.Getenv,
		runsRoot: runsRoot,
		follow:   driver.LogFollow,
		relay:    make(chan fold.Event, 64),
		closer: func() error {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil
		},
	}
	sd.chat = newChatSession(nd, sd.relay)
	sd.SetMode(defaultMode)
	// The model may look into the folder arxi-tui was opened in.
	if cwd, err := os.Getwd(); err == nil {
		sd.chat.setWorkdir(cwd)
	}
	sd.chat.dial = func(ctx context.Context) (chatSender, func(), error) {
		return dialChatConn(ctx, arxiBin)
	}
	return sd, sd.relay, nil
}

// dialChatConn starts a second `arxi serve` for one chat turn and returns the
// connection with the function that ends it. The provider store lives on disk, so the
// new process sees the same providers, keys and selected model as the main one; it is
// the process that gets killed when the user cancels the turn, which is the only way
// the protocol lets a request in flight be abandoned cleanly (see chatDialer).
func dialChatConn(ctx context.Context, arxiBin string) (chatSender, func(), error) {
	cmd := exec.CommandContext(ctx, arxiBin, "serve")
	cmd.Env = chatCoreEnv()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("chat: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, nil, fmt.Errorf("chat: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, nil, fmt.Errorf("chat: start arxi serve: %w", err)
	}
	closeConn := func() {
		stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	nd := driver.NewNDJSON(struct {
		io.Reader
		io.Writer
	}{stdout, stdin})
	if err := nd.Handshake(ctx); err != nil {
		closeConn()
		return nil, nil, fmt.Errorf("chat: handshake: %w", err)
	}
	return nd, closeConn, nil
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
	// caretState carries the emit path's memory of the caret across frames — its
	// visibility and its last position — so an unchanged caret is neither re-shown
	// nor re-positioned. Both are strobe sources on an animated scene that repaints
	// ~12×/s: re-showing restarts the blink, and re-issuing the position CUP is read
	// by the terminal as "the app moved the caret, light it solid", which kept the
	// block lit ~85% of the time instead of letting it breathe (see emitFrame). It
	// starts zeroed: the first frame that hosts a caret shows and positions it once,
	// and the terminal's own blink runs from there.
	caretState := &emitState{}
	var collected []fold.Event
	var input string
	// hist is the sent lines the up and down arrows walk, kept across runs.
	hist := loadHistory(historyPath())
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
	// expandTools is Ctrl+O: tool output and diffs in full instead of cut. It is a
	// view setting like the scroll position, so /clear leaves it alone.
	expandTools := false
	// slashSel is the menu's highlighted row. The host owns it across frames
	// the way it owns the input buffer: the fold is rebuilt per frame and
	// carries it, but the state of the menu is not the scene's business.
	slashSel := 0
	// slashCat is the menu's active category tab ("" or "All" = every command),
	// host-owned like slashSel. Tab and Shift-Tab step it; it falls back to "All"
	// when the typed filter leaves its category with no match.
	slashCat := fold.SlashAll
	// uiHidden is the `ui.hidden` set (BINDS.md §4.3): the ids the user has
	// hidden with `/ui hide`. It is host-owned view state held across frames
	// like the input buffer and slashSel, because no core event produces it —
	// the fold is rebuilt from the log each frame and would forget a set kept
	// only in State. The engine walk reads it as a visibility filter, so a
	// keystroke that hides a node must survive the next repaint.
	uiHidden := map[string]bool{}

	// uiFocus is the `ui.focus` cursor (BINDS.md §4.3/§4.8): the id of the
	// currently focused node, host-owned view state held across frames like
	// uiHidden. The empty string is the input's home slot — focus defaults there
	// at boot (H8). Tab/Shift-Tab move it over the pressable nodes, and Enter on a
	// focused node dispatches its on_press. The engine already reads state.UIFocus
	// for focus_glow, so a moved cursor lights the glow with no new engine code.
	uiFocus := ""

	// uiMax is the `ui.max` cursor (BINDS.md §4.3, Q21): the logical id of the
	// maximized pane, host-owned view state held across frames like uiFocus. The
	// empty string means nothing is maximized — the boot state, in which Scene
	// 10's grid shows (ui.max.none is truthy). `/max <pane>` sets it and `/max`
	// alone restores it, both through parseMax. The engine already reads
	// state.UIMax (and derives ui.max.none / ui.max.is.<id> from it), so writing
	// the cursor here is all the loop adds for click-to-maximize.
	uiMax := ""

	// pluginFetch is the network side of `/ui plugin add <url>` (H6). It is the
	// only real HTTP client the loop holds, injected into the /ui dispatch so the
	// patch surface stays a pure offline transform (patch.Fetcher is the seam).
	// Built once here so its timeout and size cap are the loop's, not re-chosen
	// per keystroke.
	pluginFetch := newHTTPManifestFetcher()

	// pluginActions is the routing table for behavioral-plugin presses (I4): an
	// on_press ext:<plugin-id>:<action> is routed through it to the named plugin's
	// subprocess. It is the host→plugin companion to pluginFetch's host→core
	// surface, held beside ui.hidden/ui.focus as host view-side state. It is empty
	// until I5 mounts a plugin behind the consent gate, so today an ext: press
	// reports "no plugin with that id is mounted" rather than dispatching — the
	// routing mechanism is complete, filling the registry with a live supervisor
	// is I5's to do.
	pluginActions := supervisor.NewRegistry()

	// The behavioral-plugin install surface (I6). These sit beside pluginActions
	// as host view-side state the loop owns across frames:
	//
	//   - pluginGate decides consent by a plugin's identity; a warning (empty when
	//     clean) is surfaced as the scene notice so a consent-store problem is
	//     visible without bricking the interface (openConsentGate's contract).
	//   - pluginStore is where a mounted plugin's frames drain (I3). It is created
	//     here so supervisor.Mount has somewhere to pump into; reading its binds
	//     back into the per-frame fold is a separate wiring (still to land), so a
	//     mounted plugin runs and is held, but its binds are not yet drawn.
	//   - archiveFetch is the one network client the install path uses, built once
	//     so its cap and timeout are the loop's, like pluginFetch above.
	//   - modal holds the in-progress install's loop-visible state (busy + the
	//     consent screen); consentReqCh/installDoneCh bridge the worker goroutine
	//     back to this select.
	//   - mountedPlugins holds each live behavioral plugin's supervisor by id, so
	//     `/ui plugin remove <id>` can Close the process it spawned and shutdown can
	//     Close them all.
	pluginGate, gateWarn := openConsentGate()
	if gateWarn != "" {
		// Surface the consent-store warning without clobbering an invariant-3 scene
		// notice: if the requested scene was refused, that reason stays on screen
		// and the gate warning rides after it, because both are load-bearing — the
		// first says why this scene is showing, the second why grants will not
		// persist this session.
		if sceneNotice == "" {
			sceneNotice = gateWarn
		} else {
			sceneNotice = sceneNotice + " · " + gateWarn
		}
	}
	pluginStore := ext.NewPluginStore()
	archiveFetch := newHTTPArchiveFetcher()
	var modal installModal
	consentReqCh := make(chan consentRequest)
	installDoneCh := make(chan installOutcome, 1)
	mountedPlugins := map[string]*supervisor.Supervisor{}

	// The bundle install surface (J4): `/ui plugin bundle <url>` fetches a bundle
	// document, lays out and decides each referenced plugin, shows ONE consent
	// screen, and on a grant composes the bundle's scene, theme and plugins together.
	// It sits beside the single-plugin modal above and shares its gate, store and
	// registry — a bundle plugin flows through the same H6/I5 pipeline — so the loop
	// refuses a bundle while a single install is busy and vice versa (two consent
	// screens would race the one input focus, and the fan-out grants against the one
	// gate one identity at a time). bundleMod holds the loop-visible bits (busy + the
	// one screen); bundleConsentReqCh/bundleDoneCh bridge the worker back to this
	// select, exactly as consentReqCh/installDoneCh do for a single plugin.
	var bundleMod bundleModal
	bundleConsentReqCh := make(chan bundleConsentRequest)
	bundleDoneCh := make(chan bundleOutcome, 1)

	// The community installer's loop-visible state (J3 follow-up). `browse` is
	// non-nil exactly while the installer is open: it holds the fetched index, the
	// typed query and the selection cursor (installerBrowse's pure core), and the
	// repaint swaps LiveInstallerScene onto the display and publishes its
	// community.* triple while it is set — the browse analogue of modal.capturing()
	// owning the frame. `/ui plugin browse <url>` fetches the index on a worker
	// (startBrowseFetch) so a hung registry never freezes the loop or the panic
	// gesture (invariant 6); browseBusy refuses a second fetch while one is in
	// flight, and browseDoneCh carries the worker's outcome back to the select.
	// liveInstaller is the installer document, a pure constant built once here: if
	// it fails to build the browse command refuses rather than opening a broken
	// screen.
	var browse *installerBrowse
	var browseBusy bool
	// hub is the open provider hub, non-nil exactly while it is showing. It replaces
	// the scene on display and owns the keyboard (like browse above), so the arrows
	// move its highlight and every typed character goes to its filter or its masked
	// field -- never to the chat. hubBusy refuses a second request while one is in
	// flight, and hubDoneCh carries the worker's answer back to the select.
	var hub *providerHub
	var hubWant hubOpen
	var hubBusy bool
	// hubDefault is the default model the core last reported ("provider/id"); the
	// status bar shows it until a reply names the model that really answered.
	var hubDefault string
	// cwd is where the TUI was started, shown in the bottom bar. effort is the
	// thinking level the next request asks for; empty sends nothing until the user
	// sets one with /effort. mode is how much the agent may do unasked (/mode).
	cwd, _ := os.Getwd()
	effort := ""
	mode := defaultMode
	hubDoneCh := make(chan hubOutcome, 1)
	// modelMenu is the `/model ` menu's state; modelCh carries its worker's answers.
	var modelMn modelMenu
	modelCh := make(chan modelRead, 2)
	// effortMn is the `/effort ` menu: the same shape as the model menu over a fixed
	// list, so it needs no worker.
	var effortMn modelMenu
	// modeMn is the `/mode ` menu, built the same way.
	var modeMn modelMenu
	// resumeMn is the `/resume ` menu: the saved conversations, newest first. sessLog
	// writes the conversation on screen to disk as it happens.
	var resumeMn modelMenu
	sessLog := newSessionLog(sessionsDir())
	hubDoc, hubDocErr := loadHubScene()
	// openHub is the one door a typed command and a slash-menu pick both take. It
	// reads the core's state on a worker first so a hung core never freezes the loop.
	// With no live core the hub still opens, empty, with the reason in the banner: a
	// user who asked to see the providers must see a screen, not a vanished word.
	openHub := func(open hubOpen) {
		hubWant = open
		if hubDocErr != nil {
			sceneNotice = "/provider: " + hubDocErr.Error()
			return
		}
		if open == hubOpenSearch {
			// A local setting: it needs no core, so it opens even with none.
			hub, _ = newHub(hubData{}, open)
			sceneNotice = ""
			return
		}
		hc, _ := drv.(interface{ Hub() hubCore })
		if hc == nil || hc.Hub() == nil {
			hub = newOfflineHub(noLiveCoreNotice)
			sceneNotice = noLiveCoreNotice
			return
		}
		if err := requireHubVerbs(hc.Hub().Hello()); err != nil {
			hub = newOfflineHub(err.Error())
			sceneNotice = err.Error()
			return
		}
		if hubBusy {
			sceneNotice = "the providers screen is still working; wait a moment"
			return
		}
		hubBusy = true
		sceneNotice = "reading providers …"
		startHubOpen(ctx, hc.Hub(), hubDoneCh)
	}
	browseDoneCh := make(chan browseOutcome, 1)
	registryFetchClient := newRegistryFetchClient()
	liveInstaller, liveInstallerErr := ext.LiveInstallerScene()
	// Every spawned plugin process is stopped when the loop ends, so a normal quit
	// does not leak a subprocess. Close is idempotent and the pump's DrainInto
	// already exits on process death, so closing one already reaped is harmless.
	defer func() {
		for _, s := range mountedPlugins {
			s.Close()
		}
	}()

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

	// The plugin token layers (H4). baseTheme is the factory theme the loop was
	// handed; pluginLayers holds one entry per mounted plugin. The active `theme`
	// variable is recomposed from them whenever a plugin is added or removed, so
	// a plugin's tokens win over factory and a `/ui plugin remove` drops exactly
	// what its `add` contributed. applyPluginTokens is the one place that mutates
	// the layer set and reassigns `theme`; it also re-points the clock's anim
	// lookup, so a one-shot token a plugin contributes (reveal/transition/enter)
	// resolves the same way a factory one does. The continuous-marquee tick rate
	// stays at the boot value (marqueeFPS above): re-arming the ticker mid-session
	// is out of H4's scope, and a plugin that adds a faster marquee cadence is a
	// rare case not worth risking the ticker invariants for.
	baseTheme := theme
	var pluginLayers []pluginThemeLayer
	applyPluginTokens := func(op *patch.PluginTokens) {
		pluginLayers = applyTokenLayer(pluginLayers, op)
		theme = composeTheme(baseTheme, pluginLayers)
		clock.resolveAnim = theme.Anim
	}
	var animTicker *time.Ticker
	var tickCh <-chan time.Time
	// escapeTimer fires once, ArmTimeout after the first Ctrl-C, to disarm the
	// "press ctrl+c again to exit" hint and repaint it away when no second press
	// followed. It is nil the rest of the time, and a receive on a nil channel
	// blocks forever, so the case below simply never fires until a first Ctrl-C
	// sets it.
	var escapeTimer <-chan time.Time
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
	// blinkTicker drives the host-owned caret blink. Unlike animTicker it is armed
	// by the mere presence of a caret, not by an animation: the cursor shape is
	// steady (DECSCUSR 2) and the terminal does not blink it, so without a wake-up
	// the block would sit frozen on an idle scene. It runs at the blink half-period,
	// the coarsest rate that still turns the block fully on and fully off.
	var blinkTicker *time.Ticker
	var blinkCh <-chan time.Time
	armBlink := func(caretPresent bool) {
		switch {
		case caretPresent && blinkTicker == nil:
			blinkTicker = time.NewTicker(blinkHalfPeriod)
			blinkCh = blinkTicker.C
		case !caretPresent && blinkTicker != nil:
			blinkTicker.Stop()
			blinkTicker = nil
			blinkCh = nil
		}
	}
	defer func() {
		if animTicker != nil {
			animTicker.Stop()
		}
		if blinkTicker != nil {
			blinkTicker.Stop()
		}
	}()

	// lastKey is when the user last pressed a key; see caretLit.
	var lastKey time.Time

	// turnStart is when the turn now in flight began, as the Thinking line counts
	// it: set on the first frame that finds the agent working, cleared on the
	// first that does not. The fold cannot hold it (it knows no wall time), so it
	// lives here with the other per-frame host state.
	var turnStart time.Time

	repaint := func() {
		state := fold.Fold(collected)
		state.UserInput = input // view state: the host owns the input buffer
		if hubDefault != "" {
			// Chat always uses the default model, so the status bar shows it.
			state.ModelName = hubDefault
		}
		state.UserInputCaret = caret
		// ui.hidden is host-owned view state the loop keeps across frames, so it
		// is re-attached on every repaint for the same reason the input buffer
		// and the scene error are (Fold rebuilds State from the log each frame).
		state.UIHidden = uiHidden
		// ui.focus is host-owned view state re-attached each frame like ui.hidden
		// above (H8): the fold would forget a cursor kept only in State. The
		// engine reads it for focus_glow, so the focused node lights up.
		state.UIFocus = uiFocus
		// ui.max is host-owned view state re-attached each frame for the same
		// reason (Q21/Scene 10): Fold leaves it empty every frame, so a maximized
		// pane set by /max would collapse back to the grid on the next keystroke
		// if the loop did not re-apply its cursor here. The engine derives
		// ui.max.none and the ui.max.is.<id> family from it, so the layout follows.
		state.UIMax = uiMax
		// Invariant 3's other half: the fallback scene is on screen, and this
		// is the notice that says why. It is re-applied on every repaint
		// because Fold rebuilds State from the event list each frame
		// (ADR-0004, pull by frame), so a value set once would vanish on the
		// next keystroke.
		state.SceneError = sceneNotice

		// slash.* view-state: when the buffer starts with "/", the slash
		// menu is active and the typed substring filters the command list.
		// This is arxi-tui's own contract (BINDS.md §4.3), not a core event.
		if filter, open := modelMenuOpen(input); open && hub == nil {
			// The `/model ` menu replaces the command menu while the buffer holds
			// the command and a space. It is the whole of the model picker: no
			// title, no help text.
			state.ModelActive = true
			state.ModelMatches, state.ModelSelected = modelMn.view(filter)
			if !modelMn.loaded && !modelMn.loading {
				// Read the models once per opening, on a worker, so a slow core
				// never freezes the loop. The rows already known stay on screen
				// meanwhile.
				hc, _ := drv.(interface{ Hub() hubCore })
				switch {
				case hc == nil || hc.Hub() == nil:
					modelMn.loaded = true
					sceneNotice = noLiveCoreNotice
				default:
					if err := requireHubVerbs(hc.Hub().Hello()); err != nil {
						modelMn.loaded = true
						sceneNotice = err.Error()
					} else {
						modelMn.loading = true
						startModelRead(ctx, hc.Hub(), modelCh)
					}
				}
			}
		} else if filter, open := effortMenuOpen(input); open && hub == nil {
			// `/effort ` borrows the model menu's overlay: same rows, same keys.
			if !effortMn.loaded {
				effortMn.setRows(effortMenuData(hubDefault, effort))
			}
			state.ModelActive = true
			state.ModelMatches, state.ModelSelected = effortMn.view(filter)
		} else if filter, open := modeMenuOpen(input); open && hub == nil {
			// `/mode ` borrows the same overlay.
			if !modeMn.loaded {
				modeMn.setRows(modeMenuData(mode))
			}
			state.ModelActive = true
			state.ModelMatches, state.ModelSelected = modeMn.view(filter)
		} else if filter, open := resumeMenuOpen(input); open && hub == nil {
			// `/resume ` borrows the same overlay; it is read from disk each time it opens.
			if !resumeMn.loaded {
				resumeMn.setRows(resumeMenuData(sessionsDir(), time.Now()))
			}
			state.ModelActive = true
			state.ModelMatches, state.ModelSelected = resumeMn.view(filter)
		} else if strings.HasPrefix(input, "/") {
			effortMn.loaded = false
			modeMn.loaded = false
			resumeMn.loaded = false
			modelMn.loaded = false // the next opening reads the core again
			state.SlashActive = true
			state.SlashTyped = input[1:]
			slashCat = fold.NormalizeSlashCategory(state.SlashTyped, slashCat)
			state.SlashCategory = slashCat
			state.SlashTabs = fold.SlashCategories(state.SlashTyped)
			state.SlashMatches = fold.FilterSlashCategory(state.SlashTyped, slashCat)
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
			modelMn.loaded = false
			effortMn.loaded = false
			modeMn.loaded = false
			resumeMn.loaded = false
			state.SlashActive = false
			state.SlashTyped = ""
			state.SlashMatches = nil
			slashCat = fold.SlashAll
		}

		// The bottom line is either the live status row or the menu's
		// navigation hint, never both: the menu's help replaces the status
		// row so there is exactly one line of info at the edge of the screen.
		// The scene gates each row with `when`, so the host only has to publish
		// the two view-state binds that drive it (BINDS.md §4.3).
		if state.SlashActive {
			state.SlashHint = "  ↑↓ navigate · tab category · enter open · esc close"
			state.StatusActive = "false"
		} else {
			state.SlashHint = ""
			state.StatusActive = "true"
		}

		// host.escape.armed mirrors the panic gesture's current arm state.
		// A scene may show it (e.g. a dim "press Ctrl-C again to quit" hint),
		// but no scene may capture the gesture (invariant 6).
		state.EscapeArmed = panicGesture.Armed()

		// host.run.actor: the actor of the run the driver is following, published
		// into the fold like the input buffer above (Fold rebuilds State each
		// frame and no event carries it). It is blanked while the slash menu is
		// open so the status row stays the same either/or the mode/model fields
		// already are (gated on status.active) -- the actor is one more field of
		// that row, and letting it linger while the menu's hint owns the bottom
		// line would put two kinds of content there at once. A driver that
		// follows no run (the mock) does not implement actorLabeler, so the label
		// stays empty and its when-gated node never draws.
		if labeler, ok := drv.(actorLabeler); ok && !state.SlashActive {
			state.RunActor = labeler.ActorLabel()
		} else {
			state.RunActor = ""
		}

		// host.cwd / host.effort: the working directory and the thinking level, shown
		// in the bottom bar. They (and the model name) are blanked while the slash menu is
		// open, like the actor above, so the bar carries the menu hint alone.
		if state.AgentWorking {
			if turnStart.IsZero() {
				turnStart = time.Now()
			}
			state.HostThinking = thinkingLabel(time.Since(turnStart))
		} else {
			turnStart = time.Time{}
		}
		if state.SlashActive {
			state.HostEffort, state.HostCwd, state.HostMode, state.ModelName = "", "", "", ""
		} else {
			state.HostEffort, state.HostCwd, state.HostMode = effort, shortCwd(cwd), mode
		}

		var r engine.Renderer
		w, h := tty.Size()
		r.Width, r.Height = w, h

		// Decode a row-target ui.focus into the (node, row) pair the engine lights
		// an instantiated template row by (H8 row-click). A plain focused node
		// glows through state.UIFocus, but a row_template's authored id repeats
		// across every instantiated row, so a row cursor cannot be named by id
		// alone; the host owns the rowFocusKey encoding and hands the engine the
		// decoded pair. A focus that is not a row key decodes to ok=false and
		// leaves FocusRowNode empty, which lights no row and moves no golden.
		if node, row, ok := parseRowFocusKey(uiFocus); ok {
			r.FocusRowNode, r.FocusRowIndex = node, row
		}

		// A mounted behavioral plugin's frames drain into pluginStore from the pump
		// goroutine (I3 DrainInto); this is where they reach the walk. The snapshot
		// is a fresh copy taken under the store's lock, so the render reads a stable
		// frame while the pump keeps writing, and it is fed as r.PluginValues — a
		// SEPARATE input from fold.State (invariant 2 / ADR-0003), because a
		// stranger's process value entering the fold would make it an authority over
		// the run log. A nil snapshot (no plugin ever mounted) resolves every plugin
		// bind to the placeholder, so no non-plugin scene is touched.
		r.PluginValues = pluginStore.Snapshot()

		// The animation clock advances by wall time, hands the renderer the
		// phase, and reads back which nodes are still animating; then the
		// ticker is armed or stopped to match. The frame goes through the same
		// emit path render() uses, so the animated repaint and a plain one
		// cannot diverge in how they reach the terminal.
		clock.advance(time.Now())
		r.AnimTicks = clock.ticks()
		r.AnimPhase = clock.phases()
		r.ChatScroll = chatScroll
		r.ExpandTools = expandTools
		// While a consent screen is up it replaces the scene on display: the modal
		// owns the whole frame so the identity the user is judging is the only thing
		// they see, and a keypress cannot be split between the prompt and the scene
		// behind it. The fold state is passed unchanged — ext.ConsentScene is static
		// text and binds to none of it — so the swap is purely which document is
		// walked.
		activeDoc := doc
		switch {
		case modal.capturing():
			activeDoc = modal.consent.doc
		case bundleMod.capturing():
			// The one bundle consent screen is up: like the single-plugin modal it
			// replaces the scene on display so the identity the user is judging (the
			// bundle plus every plugin it installs) is the only thing they see, and a
			// keypress cannot be split between the prompt and the scene behind it. It
			// binds to none of the fold state, so the swap is purely which document is
			// walked.
			activeDoc = bundleMod.consent.doc
		case hub != nil:
			// The provider hub is open: it replaces the scene on display. Its rows
			// are published as display text only -- a typed key is bullets by the
			// time it reaches the fold (providerHub.publish).
			activeDoc = hubDoc
			hub.publish(&state)
		case browse != nil:
			// The installer is open: it replaces the scene on display and its
			// community.* triple is published onto the fold the renderer reads, the
			// single place a query, its matches and the selection are set together
			// (installerBrowse.publish) so a repaint cannot show a query without its
			// matches or a selection past the list. The consent modal takes priority
			// above: pressing Enter on an entry starts an install whose consent screen
			// must own the frame, and when it closes the installer returns.
			activeDoc = liveInstaller
			browse.publish(&state)
		}
		frame, active := r.RenderFrameActive(activeDoc, state)
		// Pin the scroll offset to what the renderer could actually honour: it
		// alone knows the wrapped line count and the pane budget, so a wheel spun
		// past the top settles here instead of banking dead scroll that a later
		// wheel-down would have to unwind first.
		if chatScroll > r.ChatScrollMax {
			chatScroll = r.ChatScrollMax
		}
		clock.reconcile(active)
		// The caret's blink is host-driven off the wall clock (see emitFrame): the
		// phase is recomputed every repaint so a keystroke, an animation tick and the
		// blink ticker all agree on it. armBlink keeps a ~2Hz ticker running whenever
		// the frame hosts a caret, so an idle scene still blinks it — an animated
		// scene's own ticker would do it, but an idle one repaints on input alone and
		// would otherwise freeze the block on whichever half it last painted.
		emitFrame(tty, frame, theme, h, caretState, caretLit(time.Now(), lastKey))
		armTicker()
		// The Thinking line counts seconds, so the loop must wake while a turn is
		// in flight even when the caret is off screen: the blink ticker is the
		// slowest tick that still moves a seconds counter on time.
		armBlink(!frame.Cursor.Hidden || state.AgentWorking)
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

		case <-blinkCh:
			// The caret's blink half-period elapsed: repaint so emitFrame flips the
			// block's visibility for the new phase. Like an animation tick it only
			// repaints — it reads no input and dispatches no gesture, so the escape
			// hatch (invariant 6) is untouched. blinkCh is nil while no caret is on
			// screen, so a receive on it blocks forever and this case never fires.
			repaint()

		case <-pluginStore.Changed():
			// A mounted plugin pushed a value (or its liveness moved) into the store
			// from the pump goroutine, and the store posted a wake-up. Like an
			// animation tick this case ONLY repaints — it reads no input and
			// dispatches no gesture, so the escape hatch stays uncapturable
			// (invariant 6) even under a plugin flooding frames. The signal is
			// coalesced (buffered at 1), so a burst collapses to one repaint and the
			// next Snapshot reads the latest of each value. An empty channel (no
			// plugin has ever written) blocks this case, so a plain session never
			// fires it.
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
					lastKey = time.Now()
					// Remember the line this key may send: Enter that empties a
					// non-empty buffer is a submit (or a command), and what was
					// typed is what the history keeps.
					lineBefore := input
					if isCtrlC(ev.Key) && cancelRunningTurn(drv) {
						// A turn was running, so Ctrl-C means "stop that": it is the key
						// every terminal tool uses to interrupt work. It does not arm the
						// leave-the-program gesture — the press was spent on the cancel —
						// so quitting still takes the usual double Ctrl-C once idle, and
						// a hung turn can always be cancelled and then escaped from
						// (invariant 6 holds: no scene or plugin sees this key).
						panicGesture.Reset()
					} else if isCtrlC(ev.Key) {
						if panicGesture.HandleCtrlC(time.Now()) {
							return nil // second press within the window: leave
						}
						// First press: clear the line the user is typing and arm
						// the visible "press ctrl+c again to exit" hint
						// (host.escape.armed). The chat is never cleared — Ctrl-C
						// empties the input, not the transcript the user is reading
						// — which is the reported change from the old behaviour
						// that wiped the whole run. escapeTimer disarms the hint
						// after the window so a single press does not leave the
						// line armed forever; nothing else would wake the loop to
						// notice the window closed.
						input = ""
						caret = 0
						escapeTimer = time.After(driver.ArmTimeout)
					} else if modal.capturing() {
						// A consent screen is up: it owns every non-panic key. This
						// sits immediately after the Ctrl-C branch so the escape hatch
						// still reaches HandleCtrlC (invariant 6) and cannot be captured
						// by the prompt, and before every other handler so a 'y' meant
						// for the prompt cannot reach the chat buffer or the slash menu.
						// The keypress is an intentional answer, so it disarms the panic
						// gesture like any other input; handleKey routes it through
						// consentAnswerForKey, answering the blocked worker and closing
						// the screen on y/r/n/Esc and leaving it standing otherwise.
						panicGesture.Reset()
						modal.handleKey(ev.Key)
					} else if bundleMod.capturing() {
						// The one bundle consent screen is up: it owns every non-panic
						// key exactly as the single-plugin modal above does. This sits
						// after the Ctrl-C branch so the escape hatch still reaches
						// HandleCtrlC (invariant 6) and cannot be captured by the prompt,
						// and after modal.capturing so the two screens never both claim a
						// key. The keypress is an intentional answer, so it disarms the
						// panic gesture; bundleMod.handleKey routes it through
						// bundleAnswerForKey, answering the blocked worker and closing the
						// screen on y/r/n/Esc and leaving it standing otherwise.
						panicGesture.Reset()
						bundleMod.handleKey(ev.Key)
					} else if isCtrlO(ev.Key) {
						// Show the cut part of a command's output or of a change, or cut it
						// again. It sits after the consent screens (which own every key)
						// and before the approval prompt, so a long change can be opened
						// in full while the question is still waiting.
						panicGesture.Reset()
						expandTools = !expandTools
					} else if ap, ok := drv.(approver); ok && ap.PendingApproval() &&
						ev.Key.Type != term.KeyWheelUp && ev.Key.Type != term.KeyWheelDown {
						// The model wants to change a file and the turn is waiting for
						// the user: y allows it, n or Esc declines it, and every other
						// key is not an answer. It sits after the Ctrl-C branch (the
						// escape hatch is never captured) and before the other
						// handlers, so a "y" cannot reach the chat input.
						panicGesture.Reset()
						if allow, decided := approvalKey(ev.Key); decided {
							ap.Decide(allow)
						}
					} else if hub != nil {
						// The provider hub owns the keyboard, after the Ctrl-C branch
						// so the escape hatch still reaches HandleCtrlC (invariant 6).
						// Every key here, printable or not, goes to the hub and never
						// to the chat input: a key typed into the API-key field must
						// not become a prompt.
						panicGesture.Reset()
						res := routeHubKey(hub, ev.Key)
						switch {
						case res.close:
							hub.wipe()
							hub = nil
							sceneNotice = ""
							input = ""
							caret = 0
						case res.work != nil:
							if hubBusy {
								sceneNotice = "the providers screen is still working; wait a moment"
							} else if res.work.Op == opSearch {
								// Saved on this computer, so no core is involved.
								hubBusy = true
								hub.working = res.work.Op.String()
								sceneNotice = hub.working + " …"
								startHubWork(ctx, nil, *res.work, hubDoneCh)
							} else if hc, _ := drv.(interface{ Hub() hubCore }); hc == nil || hc.Hub() == nil {
								sceneNotice = noLiveCoreNotice
							} else {
								hubBusy = true
								hub.working = res.work.Op.String()
								sceneNotice = hub.working + " …"
								startHubWork(ctx, hc.Hub(), *res.work, hubDoneCh)
							}
						case res.notice != "":
							sceneNotice = res.notice
						case res.clear:
							sceneNotice = ""
						}
					} else if browse != nil {
						// The installer is open and owns the keyboard, like the consent
						// modal above. This sits after the Ctrl-C branch so the escape
						// hatch still reaches HandleCtrlC (invariant 6) and cannot be
						// captured by the installer, and after modal.capturing so an
						// install started from a card lets its consent screen take the
						// keys. An installer keystroke is intentional input, so it disarms
						// the panic gesture like any other. routeBrowseKey maps the key to
						// a pure browse transition and reports the two loop-visible
						// outcomes: Esc closes the installer, and Enter on a highlighted
						// entry yields its manifest_url — dispatched through the exact
						// startInstall path a typed `/ui plugin install` takes, so a
						// pressed card and a typed line cannot install different bytes. The
						// browse stays open across an install: the consent modal draws over
						// it and the installer returns when the modal closes.
						panicGesture.Reset()
						switch res := routeBrowseKey(browse, ev.Key); {
						case res.close:
							browse = nil
						case res.installURL != "":
							switch {
							case modal.busy:
								sceneNotice = "/ui plugin install: an install is already in progress; finish it or answer the consent screen before starting another"
							case bundleMod.busy:
								sceneNotice = "/ui plugin install: a bundle install is in progress; finish it or answer its consent screen before starting another"
							default:
								root, rerr := pluginsRootPath()
								if rerr != nil {
									sceneNotice = "/ui plugin install: " + rerr.Error()
								} else {
									modal.busy = true
									sceneNotice = "/ui plugin install: fetching " + res.installURL + " …"
									startInstall(ctx, res.installURL, archiveFetch, root, pluginGate, pluginStore, pluginActions, consentReqCh, installDoneCh)
								}
							}
						}
					} else if ev.Key.Type == term.KeyEscape && !strings.HasPrefix(input, "/") && cancelRunningTurn(drv) {
						// Esc stops the turn in flight. A line starting with "/" is a
						// menu, and there Esc keeps its meaning of closing it, so a menu
						// opened while an answer is pending can still be dismissed.
						panicGesture.Reset()
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
						if regURL, matched, perr := parsePluginBrowse(input); ev.Key.Type == term.KeyEnter && matched {
							// `/ui plugin browse <url>` opens the community installer over a
							// fetched registry index. Like install it is host-owned, not a
							// patch: it fetches over the network, swaps a document onto the
							// display and drives a keystroke loop, none of which the pure
							// patch surface can do. The fetch runs on a worker
							// (startBrowseFetch) so a hung registry never freezes the loop or
							// the panic gesture; the loop records browseBusy and opens the
							// browse when the outcome arrives (browseDoneCh below).
							switch {
							case perr != nil:
								sceneNotice = perr.Error()
							case liveInstallerErr != nil:
								// The installer document itself failed to build (a programming
								// error, not user input); refuse to open rather than swap a
								// broken screen onto the display.
								sceneNotice = "/ui plugin browse: " + liveInstallerErr.Error()
							case browseBusy:
								sceneNotice = "/ui plugin browse: a registry fetch is already in progress; wait for it to finish before starting another"
							default:
								browseBusy = true
								sceneNotice = "/ui plugin browse: fetching " + regURL + " …"
								startBrowseFetch(regURL, registryFetchClient, browseDoneCh)
							}
							input = ""
							caret = 0
						} else if url, matched, perr := parsePluginInstall(input); ev.Key.Type == term.KeyEnter && matched {
							// `/ui plugin install <url>` is host-owned, not a patch:
							// it fetches a bundle, spawns a process and holds it, none
							// of which the patch surface (a pure document transform)
							// can do, so it is intercepted here before uiCommandKey.
							// The work runs on a worker goroutine (startInstall) so a
							// hung fetch never freezes the loop or the panic gesture;
							// the loop only records that one is in flight (modal.busy)
							// and reacts to the worker's messages in the select cases
							// below.
							switch {
							case perr != nil:
								sceneNotice = perr.Error()
							case modal.busy:
								sceneNotice = "/ui plugin install: an install is already in progress; finish it or answer the consent screen before starting another"
							case bundleMod.busy:
								sceneNotice = "/ui plugin install: a bundle install is in progress; finish it or answer its consent screen before starting another"
							default:
								root, rerr := pluginsRootPath()
								if rerr != nil {
									sceneNotice = "/ui plugin install: " + rerr.Error()
								} else {
									modal.busy = true
									sceneNotice = "/ui plugin install: fetching " + url + " …"
									startInstall(ctx, url, archiveFetch, root, pluginGate, pluginStore, pluginActions, consentReqCh, installDoneCh)
								}
							}
							input = ""
							caret = 0
						} else if bundleURL, matched, perr := parsePluginBundle(input); ev.Key.Type == term.KeyEnter && matched {
							// `/ui plugin bundle <url>` installs a whole bundle (scene +
							// theme + N plugins) behind ONE consent screen (J4). Like
							// install it is host-owned, not a patch: it fetches a bundle
							// document and N plugin archives, swaps a scene onto the display,
							// merges a theme layer and spawns held subprocesses, none of
							// which the pure patch surface can do. The resolve→consent→plan
							// thread runs on a worker (startBundleInstall) so a hung fetch of
							// the bundle or any of its plugins never freezes the loop or the
							// panic gesture; the loop records bundleMod.busy and reacts to
							// the worker's messages in the select cases below. It shares the
							// single-plugin gate/store/registry, so it refuses to start while
							// either install is in flight — one consent screen at a time.
							switch {
							case perr != nil:
								sceneNotice = perr.Error()
							case bundleMod.busy:
								sceneNotice = "/ui plugin bundle: a bundle install is already in progress; finish it or answer its consent screen before starting another"
							case modal.busy:
								sceneNotice = "/ui plugin bundle: a plugin install is in progress; finish it or answer its consent screen before starting another"
							default:
								root, rerr := pluginsRootPath()
								if rerr != nil {
									sceneNotice = "/ui plugin bundle: " + rerr.Error()
								} else {
									bundleMod.busy = true
									sceneNotice = "/ui plugin bundle: fetching " + bundleURL + " …"
									startBundleInstall(ctx, bundleURL, pluginFetch, archiveFetch, root, pluginGate, bundleConsentReqCh, bundleDoneCh)
								}
							}
							input = ""
							caret = 0
						} else if id, matched := parsePluginRemoveID(input); ev.Key.Type == term.KeyEnter && matched && mountedPlugins[id] != nil {
							// A behavioral plugin's remove stops the process this host
							// spawned. Such a plugin (mounted by install) has no
							// document subtree — install spawns and registers actions,
							// it does not place fragments — so the patch surface would
							// refuse "not mounted here"; the host owns this remove. Close
							// reaps the process, the pump's DrainInto then drops the
							// store namespace, and the Mount goroutine removes it from
							// the routing table on exit. A declarative plugin (no held
							// supervisor) is not matched here and falls through to the
							// patch unmount below.
							mountedPlugins[id].Close()
							delete(mountedPlugins, id)
							sceneNotice = "/ui plugin remove: stopped and unmounted plugin " + id
							input = ""
							caret = 0
						} else if handled, next := uiCommandKey(input, ev.Key, &doc, &sceneNotice, uiHidden, pluginFetch, applyPluginTokens); handled {
							input = next
							caret = clampCaret(input, caret)
						} else if ev.Key.Type == term.KeyTab && ev.Key.Mod&term.ModShift != 0 && !strings.HasPrefix(input, "/") {
							// Shift+Tab walks the agent modes without opening a menu.
							mode = nextMode(mode)
							if ms, ok := drv.(modeSetter); ok {
								ms.SetMode(mode)
							}
							modeMn.loaded = false
						} else if _, open := modelMenuOpen(input); open {
							// The `/model ` menu owns the keys while it is open: the
							// arrows move its highlight, typing filters it and Enter
							// switches the chat model. Ctrl-C never gets here
							// (invariant 6).
							var pick string
							input, caret, pick = modelMenuKey(&modelMn, input, caret, ev.Key)
							if pick != "" {
								if hc, _ := drv.(interface{ Hub() hubCore }); hc != nil && hc.Hub() != nil {
									startModelPick(ctx, hc.Hub(), pick, modelCh)
									sceneNotice = "switching to " + pick + " …"
								} else {
									sceneNotice = noLiveCoreNotice
								}
							}
						} else if _, open := effortMenuOpen(input); open {
							// The `/effort ` menu: same keys as the model menu; a pick
							// sets the thinking level the next requests ask for.
							var pick string
							input, caret, pick = choiceMenuKey(&effortMn, effortPrefix, input, caret, ev.Key)
							if pick != "" {
								effort = effortAfterPick(effort, pick)
								if s, ok := drv.(effortSetter); ok {
									s.SetEffort(effort)
								}
								effortMn.loaded = false
								sceneNotice = ""
							}
						} else if _, open := resumeMenuOpen(input); open {
							// The `/resume ` menu: same keys; a pick brings that
							// conversation back onto the screen and into the history.
							var pick string
							input, caret, pick = choiceMenuKey(&resumeMn, resumePrefix, input, caret, ev.Key)
							resumeMn.loaded = pick == ""
							if pick != "" {
								id := resumePick(pick)
								evs, err := loadSession(sessionsDir(), id)
								if err != nil {
									sceneNotice = err.Error()
								} else {
									if c, ok := drv.(sessionClearer); ok {
										c.ClearSession()
									}
									if r, ok := drv.(sessionResumer); ok {
										r.ResumeSession(historyFromEvents(evs))
									}
									for drained := false; !drained; {
										select {
										case <-eventCh:
										default:
											drained = true
										}
									}
									collected = evs
									sessLog.Continue(id)
									chatScroll = 0
									slashSel = 0
									sceneNotice = ""
								}
							}
						} else if _, open := modeMenuOpen(input); open {
							// The `/mode ` menu: same keys; a pick sets the mode.
							var pick string
							input, caret, pick = choiceMenuKey(&modeMn, modePrefix, input, caret, ev.Key)
							if pick != "" {
								mode = pick
								if ms, ok := drv.(modeSetter); ok {
									ms.SetMode(mode)
								}
								modeMn.loaded = false
								sceneNotice = ""
							}
						} else if ev.Key.Type == term.KeyEnter && resumeCommand(input, slashSel, slashCat) {
							input = resumePrefix
							caret = len([]rune(input))
							slashSel = 0
							resumeMn.loaded = false
						} else if ev.Key.Type == term.KeyEnter && modeCommand(input, slashSel, slashCat) {
							input = modePrefix
							caret = len([]rune(input))
							slashSel = 0
							modeMn.loaded = false
						} else if ev.Key.Type == term.KeyEnter && effortCommand(input, slashSel, slashCat) {
							input = effortPrefix
							caret = len([]rune(input))
							slashSel = 0
							effortMn.loaded = false
						} else if ev.Key.Type == term.KeyEnter && modelCommand(input, slashSel, slashCat) {
							// `/model` picked from the command menu (or typed whole)
							// opens the model menu: the buffer becomes `/model `.
							input = modelPrefix
							caret = len([]rune(input))
							slashSel = 0
							modelMn.loaded = false
						} else if ev.Key.Type == term.KeyEnter && clearCommand(input, slashSel, slashCat) {
							// `/clear` starts a new session: the transcript, the
							// chat history the driver sends along, any run being
							// followed and the scroll position are all dropped.
							// The provider, the model and the thinking level
							// are settings, not conversation, so they stay.
							if c, ok := drv.(sessionClearer); ok {
								c.ClearSession()
							}
							collected = nil
							sessLog.End()
							for drained := false; !drained; {
								select {
								case <-eventCh:
								default:
									drained = true
								}
							}
							sceneNotice = ""
							chatScroll = 0
							input = ""
							caret = 0
							slashSel = 0
						} else if open, isHub := hubCommand(input); isHub && ev.Key.Type == term.KeyEnter {
							// `/provider`, `/providers` and `/login` are host-owned and
							// all open the one provider hub. They take
							// no arguments on purpose, so a key can never be typed on (and
							// echoed from) the command line.
							openHub(open)
							input = ""
							caret = 0
							slashSel = 0
						} else if line, ok := menuHostCommand(input, slashSel, slashCat); ok && ev.Key.Type == term.KeyEnter {
							// Enter on a slash-menu row the host implements as a screen.
							// The row resolves to the same line a typed command is, and
							// takes the same door (the menu used to send the word to the
							// chat instead).
							open, _ := hubCommand(line)
							openHub(open)
							input = ""
							caret = 0
							slashSel = 0
						} else if pane, matched, perr := parseMax(input); ev.Key.Type == term.KeyEnter && matched {
							// `/max <pane>` writes the host ui.max cursor, Scene 10's
							// write half, intercepted here before the slash menu for the
							// same reason the plugin verbs are: a recognized command line
							// is never the menu's to swallow (the menu lists "max", and its
							// Enter would otherwise submit the bare word as a prompt). The
							// same parseMax backs the pressed cmd:/max button, so typing and
							// pressing cannot diverge. `/max` alone restores (clears ui.max).
							if perr != nil {
								sceneNotice = perr.Error()
							} else {
								uiMax = pane
							}
							input = ""
							caret = 0
						} else if step := historyStep(ev.Key); step != 0 && (!strings.HasPrefix(input, "/") || hist.Browsing() || ev.Key.Type == term.KeyRunes) {
							// Up/Down (or ctrl+p/ctrl+n) walk the sent lines. On a
							// multi-line input they first move the caret between
							// the wrapped rows, and only at the first or last row
							// fall through to the history — the editor of
							// arxi_cli_sim does the same, so a long prompt can be
							// edited without losing it to the arrow key. While the
							// slash menu is open the arrows steer its highlight
							// instead, unless the line on screen is a recalled one.
							w, _ := tty.Size()
							if moved := engine.InputCaretVerticalMove(input, caret, inputWrapRoom(doc, w), step); moved != caret {
								caret = moved
							} else {
								var line string
								var ok bool
								if step < 0 {
									line, ok = hist.Older(input)
								} else {
									line, ok = hist.Newer()
								}
								if ok {
									input = line
									caret = len([]rune(input))
									slashSel = 0
								}
							}
						} else if strings.HasPrefix(input, "/") {
							// The menu is open: navigation steers the highlight
							// and never reaches the buffer. Ctrl-C never gets
							// here, so the escape hatch stays uncapturable
							// (invariant 6) no matter what the menu does.
							input, caret, slashSel, slashCat = slashMenuKey(input, caret, ev.Key, slashSel, slashCat, ctx, drv)
						} else if handled, nextInput, nextFocus := focusKey(ev.Key, input, uiFocus, &doc, fold.Fold(collected), &sceneNotice, uiHidden, &uiMax, pluginFetch, applyPluginTokens, pluginActions, ctx, drv); handled {
							// H8 press routing: Tab/Shift-Tab move the ui.focus
							// cursor over the pressable nodes (ordinary buttons and
							// each instantiated template row), and Enter on a
							// focused node dispatches its on_press. The fold is
							// rebuilt from the log here because a template row's
							// focus target and its {row.field} on_press resolve
							// against the array the fold holds — the same state the
							// repaint folds, so Tab and the frame agree on the rows.
							// It sits after the slash branch so the menu keeps
							// Tab/Enter while open, and returns handled=false for
							// Enter while the input holds focus — so typeKey's submit
							// below is untouched. Ctrl-C never reaches here (invariant 6).
							input = nextInput
							uiFocus = nextFocus
							caret = clampCaret(input, caret)
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
					if ev.Key.Type == term.KeyEnter && input == "" && strings.TrimSpace(lineBefore) != "" {
						hist.Add(strings.TrimSpace(lineBefore))
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
					if hub != nil {
						// A pasted key goes to the hub's focused field or filter,
						// never to the chat input.
						hub.paste(ev.Text)
						break
					}
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

		case <-escapeTimer:
			// The escape window closed with no second Ctrl-C: disarm the gesture
			// so the "press ctrl+c again to exit" hint stops showing, and repaint
			// so it actually leaves the screen. A key pressed in the meantime
			// already called panicGesture.Reset(), so this is a no-op then except
			// for the harmless repaint; guarding on Armed() avoids even that.
			escapeTimer = nil
			if panicGesture.Armed() {
				panicGesture.Reset()
				repaint()
			}

		case e, ok := <-eventCh:
			if !ok {
				// Driver channel closed: no more events. Keep running for
				// terminal input (e.g. user wants to review the transcript).
				eventCh = nil
			} else {
				collected = append(collected, e)
				if err := sessLog.Record(e); err != nil {
					sceneNotice = err.Error()
				}
				repaint()
			}

		case req := <-consentReqCh:
			// The install worker reached supervisor.Mount's prompt for an unseen
			// plugin and is blocked awaiting the user's answer. Put its consent
			// screen up: repaint now swaps to modal.consent.doc and the key branch
			// above routes every non-panic key through modal.handleKey, which sends
			// the answer back on req.reply. A remembered plugin never lands here
			// (Mount does not prompt), so a silent remount shows no screen.
			modal.beginConsent(req.doc, req.declared, req.reply)
			repaint()

		case out := <-installDoneCh:
			// The worker finished — granted-and-spawned, rejected, or failed. Clear
			// busy so a next install may start, and report the outcome. On success
			// the live supervisor is held by id so `/ui plugin remove` can Close it;
			// a rejection is named distinctly from a failure (ErrConsentRejected) so
			// the user learns their own "no" was honoured rather than something
			// broke. The consent screen, if it was up, was already closed by the
			// key that answered; nothing to tear down here.
			modal.busy = false
			switch {
			case out.err == nil:
				id := out.installed.Manifest.ID
				mountedPlugins[id] = out.sup
				sceneNotice = "/ui plugin install: mounted plugin " + id
			case errors.Is(out.err, supervisor.ErrConsentRejected):
				sceneNotice = "/ui plugin install: you rejected " + out.url + "; nothing was spawned"
			default:
				sceneNotice = "/ui plugin install: " + out.err.Error()
			}
			repaint()

		case req := <-bundleConsentReqCh:
			// The bundle worker resolved the bundle and built the one consent screen;
			// it is blocked awaiting the single y/r/n answer. Put the screen up:
			// repaint now swaps to bundleMod.consent.doc and the key branch routes
			// every non-panic key through bundleMod.handleKey, which sends the answer
			// back on req.reply. Unlike a single plugin a bundle ALWAYS reaches here
			// (the screen is a named confirm even when every plugin is remembered).
			bundleMod.beginConsent(req.doc, req.reply)
			repaint()

		case out := <-bundleDoneCh:
			// The bundle worker finished — granted-and-planned, rejected, or failed.
			// Clear busy so a next install may start. On a plan, compose it here where
			// the live scene/theme/supervisor state lives: executeBundleComposePlan
			// merges the theme layer, replaces the document and Starts each granted
			// plugin (the grants already happened in the worker's planBundleCompose, so
			// this is pure composition). A rejection is named distinctly from a failure
			// (ErrBundleRejected) so the user learns their own "no" was honoured. The
			// consent screen, if it was up, was already closed by the key that answered.
			bundleMod.busy = false
			switch {
			case out.err == nil:
				if cerr := executeBundleComposePlan(ctx, out.name, out.plan, &doc, applyPluginTokens, pluginStore, pluginActions, mountedPlugins); cerr != nil {
					sceneNotice = "/ui plugin bundle: " + cerr.Error()
				} else {
					sceneNotice = "/ui plugin bundle: installed " + out.name
				}
			case errors.Is(out.err, ext.ErrBundleRejected):
				sceneNotice = "/ui plugin bundle: you rejected " + out.url + "; nothing was installed"
			default:
				sceneNotice = "/ui plugin bundle: " + out.err.Error()
			}
			repaint()

		case out := <-hubDoneCh:
			// The hub worker finished. Clear busy, then apply the answer: an opening
			// read creates the hub; any other answer only updates one still showing,
			// so a user who pressed Esc meanwhile does not see it reappear. The notice
			// was scrubbed of the key on the worker.
			hubBusy = false
			sceneNotice = out.notice
			if out.hasData {
				hubDefault = out.data.def
			}
			if out.opened && hub == nil {
				var why string
				hub, why = newHub(out.data, hubWant)
				sceneNotice = why
			} else if hub != nil {
				n, closeIt := hub.apply(out)
				sceneNotice = n
				if closeIt {
					hub.wipe()
					hub = nil
				}
			}
			repaint()

		case out := <-modelCh:
			// The model worker finished: a read refreshes the menu; a pick reports
			// the new chat model in the status bar, or why it failed.
			modelMn.loading = false
			switch {
			case out.err != "":
				sceneNotice = out.err
				modelMn.loaded = true
			default:
				if out.hasData || out.picked == "" {
					modelMn.setData(out.data)
					hubDefault = out.data.def
				}
				// A model that does not take the chosen thinking level drops it,
				// so the bar never claims a level the request will not carry.
				if effort != "" && !effortAllowed(hubDefault, effort) {
					effort = ""
					if s, ok := drv.(effortSetter); ok {
						s.SetEffort("")
					}
				}
				if out.picked != "" {
					sceneNotice = out.notice
				}
			}
			repaint()

		case out := <-browseDoneCh:
			// The registry fetch worker finished. Clear busy so a next browse may
			// start. On success open the installer over the fetched index — browse
			// becomes non-nil, so the repaint above swaps LiveInstallerScene onto the
			// display and the key branch routes keys through routeBrowseKey — and clear
			// the notice so the installer opens on a clean frame (its notice node is
			// gated on host.scene.error). On failure report err and stay on the normal
			// scene: a malformed or unreachable index leaves the user where they were,
			// named rather than dropped.
			browseBusy = false
			if out.err != nil {
				sceneNotice = "/ui plugin browse: " + out.err.Error()
			} else {
				browse = newInstallerBrowse(out.reg)
				sceneNotice = ""
			}
			repaint()
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
// caret motion (Left/Right/Home/End, and by word with Ctrl/Alt+Left/Right),
// deletion on both sides of the caret (Backspace before, Alt+Backspace a whole
// word before, Delete under), and insertion of a printable run at the
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
	case k.Type == term.KeyRunes && k.Mod&term.ModAlt != 0 && string(k.Runes) == "b":
		// ESC b is how macOS Terminal and readline-style terminals spell Option+Left.
		return input, wordLeft(r, caret), true
	case k.Type == term.KeyRunes && k.Mod&term.ModAlt != 0 && string(k.Runes) == "f":
		// ESC f is the Option+Right counterpart.
		return input, wordRight(r, caret), true
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
	case k.Type == term.KeyBackspace && k.Mod&(term.ModCtrl|term.ModAlt) != 0:
		// Alt+Backspace (ESC DEL on the wire) deletes the word before the caret: the
		// span the matching Alt+Left would cross. Ctrl+Backspace, where a terminal
		// can tell it apart, does the same.
		start := wordLeft(r, caret)
		r = append(r[:start], r[caret:]...)
		return string(r), start, true
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
//
// # Why the fetcher is a parameter
//
// `/ui plugin add <url>` is the one command that reaches the network, and the
// fetch is injected (patch.Fetcher) rather than reached for here so the patch
// surface stays offline-pure and this function stays testable with a fake. A
// nil fetch is legal — every non-plugin command ignores it, and `plugin add`
// with no fetch is refused with a reason rather than a panic.
//
// # Why the token callback is a parameter
//
// A `plugin add`/`remove` changes the active theme's token layers (H4), and
// that composition state lives in the loop, not here — the patch surface is
// stateless across commands. So the loop passes applyTokens, the one closure
// that owns the layer set, and this function hands it the Result's token op
// when one is present. A nil callback is legal for the same reason a nil fetch
// is: every non-plugin command leaves Result.Tokens nil, so the callback is
// never reached, and the direct-call tests can pass nil.
func uiCommandKey(input string, k term.Key, doc **scene.Document, notice *string, hidden map[string]bool, fetch patch.Fetcher, applyTokens func(*patch.PluginTokens)) (bool, string) {
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

	res, err := patch.ApplyWithFetch((*doc).Name(), src, text, fetch)
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
	// A plugin add/remove also changes the active theme's token layers (H4).
	// This rides alongside the document replacement rather than instead of it,
	// because a mount both places fragments (the new *doc) and contributes
	// tokens: the caller owns the layer set, so it applies the op. Guarded on
	// applyTokens so the direct-call tests, which never issue a plugin command,
	// can pass nil.
	if res.Tokens != nil && applyTokens != nil {
		applyTokens(res.Tokens)
	}
	// The change-diff view PLAN.md requires is, at this stage, the summary
	// line: the patch states what it altered in the user's vocabulary before
	// the change is trusted. A diff of re-indented JSON is not a description
	// of a change, and the full side-by-side view belongs with the
	// agent-driven half, where the proposal arrives before it is applied.
	*notice = "/ui: " + res.Summary
	return true, ""
}

// pluginThemeLayer is one mounted plugin's contributed token block, kept in the
// loop beside the factory base so the active theme can be recomposed whenever a
// plugin is added or removed (H4). The id is the plugin's — the same one its
// mounted node prefix uses — so a `/ui plugin remove <id>` drops exactly the
// layer its `add` contributed.
type pluginThemeLayer struct {
	id  string
	thm *theme.Theme
}

// composeTheme layers every active plugin's tokens over the factory base and
// returns the result, the token half of TOKENS.md's `user > plugin > factory`
// precedence. The order is the whole point and it is spelled here rather than in
// theme.Merge (which only knows "over wins"): factory is the base, each plugin
// layer is merged in mount order so a later plugin's token overrides an earlier
// one's, and the user layer — when the boot path grows one — is merged last so a
// user token overrides every plugin. Recomposing from the base each time, rather
// than un-merging a single layer, is why a remove is exact: there is no residue
// of a dropped plugin's tokens because the merge starts fresh from factory.
func composeTheme(base *theme.Theme, layers []pluginThemeLayer) *theme.Theme {
	out := base
	for _, l := range layers {
		out = theme.Merge(out, l.thm)
	}
	return out
}

// applyTokenLayer folds one plugin token op into the ordered layer set and
// returns the new set. A remove drops the layer whose id matches, keyed by id so
// it names no factory or user token; an add replaces the layer for an id already
// present (a re-add of the same plugin is not a second layer) and otherwise
// appends, so the order the set is composed in is mount order — a plugin mounted
// later wins a token conflict with one mounted earlier, the same "over wins" rule
// theme.Merge applies within a single pair. It is a free function taking the set
// because the set is a loop local, not a field on any type the loop owns —
// exactly as applyViewState is a free function over the ui.hidden map.
func applyTokenLayer(layers []pluginThemeLayer, op *patch.PluginTokens) []pluginThemeLayer {
	if op.Remove {
		next := make([]pluginThemeLayer, 0, len(layers))
		for _, l := range layers {
			if l.id != op.ID {
				next = append(next, l)
			}
		}
		return next
	}
	for i := range layers {
		if layers[i].id == op.ID {
			layers[i].thm = op.Theme
			return layers
		}
	}
	return append(layers, pluginThemeLayer{id: op.ID, thm: op.Theme})
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

// isCtrlO reports whether a key event is Ctrl-O (0x0f), which expands or cuts the
// output of the tools in the conversation.
func isCtrlO(k term.Key) bool {
	return k.Type == term.KeyRunes &&
		len(k.Runes) == 1 && k.Runes[0] == 'o' &&
		k.Mod&term.ModCtrl != 0
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
func slashMenuKey(input string, caret int, k term.Key, sel int, cat string, ctx context.Context, drv Driver) (string, int, int, string) {
	typed := strings.TrimPrefix(input, "/")
	cat = fold.NormalizeSlashCategory(typed, cat)
	matches := fold.FilterSlashCategory(typed, cat)
	switch k.Type {
	case term.KeyUp:
		if len(matches) == 0 {
			return input, caret, sel, cat
		}
		// Wrap at both ends: the highlight is the only thing the keyboard moves,
		// so the user must always feel a row under it no matter how far up they
		// spin the wheel (rotary, as requested).
		sel = (sel - 1 + len(matches)) % len(matches)
		return input, caret, sel, cat
	case term.KeyDown:
		if len(matches) == 0 {
			return input, caret, sel, cat
		}
		sel = (sel + 1) % len(matches)
		return input, caret, sel, cat
	case term.KeyTab:
		// Tab steps to the next category tab (All, General, Session, ...) and
		// Shift-Tab to the previous one, wrapping at both ends. The highlight goes
		// back to the first row of the new tab.
		dir := 1
		if k.Mod&term.ModShift != 0 {
			dir = -1
		}
		return input, caret, 0, fold.NextSlashCategory(typed, cat, dir)
	case term.KeyEscape:
		return "", 0, 0, fold.SlashAll
	case term.KeyEnter:
		if len(matches) == 0 {
			return input, caret, sel, cat
		}
		if sel >= len(matches) {
			sel = len(matches) - 1
		}
		// Phase 0: a command submits as a prompt (typeKey's contract); Phase 2
		// routes /ui to the mutation surface.
		_ = drv.SubmitPrompt(ctx, matches[sel].Name)
		return "", 0, 0, fold.SlashAll
	default:
		// The same caret-aware editor the ordinary path uses, so editing the
		// slash line (Left/Right/Home/End/Delete/Backspace/insert) behaves
		// identically. A change to the filtered text reselects the first row,
		// because the previously highlighted row may no longer exist.
		next, nextCaret, ok := applyEdit(input, caret, k)
		if !ok {
			return input, caret, sel, cat
		}
		if next != input {
			return next, nextCaret, 0, cat
		}
		return next, nextCaret, sel, cat
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
	if path == builtinScene {
		doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
		return vetScene(defaultscene.Name, doc, err, factoryRAW)
	}
	return loadScene(path, factoryRAW)
}

// builtinScene stands for the scene compiled into the binary. It is not a path a
// person could type (it holds a NUL), so it can never be mistaken for a file.
const builtinScene = "\x00builtin"

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
	return vetScene(path, doc, err, fallback)
}

// vetScene is the rest of loadScene for a document already read (from disk or from
// the binary): the same refusals, the same fallback, the same notice.
func vetScene(path string, doc *scene.Document, err error, fallback string) (*scene.Document, string, error) {
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
	// A single non-interactive frame: a nil emitState means "emit the caret
	// escapes unconditionally", which is right for a one-shot paint that has no
	// loop, no blink ticker, and no previous frame to compare against. The blink
	// phase is irrelevant with no loop, so pass false; a hosted caret is shown once.
	emitFrame(w, r.RenderFrame(doc, state), theme, r.Height, nil, false)
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
//
// emitState is the emit path's cross-frame memory of the caret: whether it is
// currently shown. The loop owns one and passes it to every emitFrame; render()'s
// one-shot paint passes nil. It exists so the show/hide escapes that produce the
// host-driven blink fire only on a change, not on every one of an animated
// scene's ~12 repaints a second.
type emitState struct {
	shown bool // whether the terminal caret is currently visible
}

// blinkHalfPeriod is one half of the caret's blink cycle: the caret is shown for
// this long, then hidden for this long, so the full period is twice this. The
// blink is host-driven (the cursor shape is steady, DECSCUSR 2) because the
// terminal's own blink cannot survive a constantly-repainting scene — see the
// cursor-shape comment in run(). 530ms is the cadence most terminals use for
// their native caret, chosen so the block reads as an ordinary blinking cursor
// rather than a strobe or a slow pulse.
const blinkHalfPeriod = 530 * time.Millisecond

// caretColor is the caret's colour: amber, in the family of the product mark.
const caretColor = "#ffb000"

// blinkHold is how long the caret stays solid after the last key. A caret that
// blinks while the user types or moves with the arrows disappears exactly when they
// are looking for it, so every key keeps it lit and the blink resumes only once the
// keyboard has been quiet for this long.
const blinkHold = 1200 * time.Millisecond

// caretLit reports whether the caret should be drawn at now: always while the user
// has touched the keyboard within blinkHold, otherwise by the blink phase.
func caretLit(now, lastKey time.Time) bool {
	if !lastKey.IsZero() && now.Sub(lastKey) < blinkHold {
		return true
	}
	return blinkOn(now)
}

// blinkOn reports whether the caret is in the shown half of its blink cycle at
// wall time t. It is a pure function of the clock, so every repaint — driven by a
// keystroke, an animation tick, or the blink ticker — agrees on the phase without
// any shared counter to keep in step.
func blinkOn(t time.Time) bool {
	return (t.UnixMilli()/int64(blinkHalfPeriod/time.Millisecond))%2 == 0
}

// emitFrame writes one already-rendered frame to the terminal. It is the single
// emit path: the loop's animation-aware repaint and the plain render() below both
// go through it, so "the frame that knows about the clock" and "the frame that
// does not" cannot emit differently.
//
// The repaint is in place, never a full clear: each row is positioned absolutely
// (CUP) and erased to end-of-line (EL) before it is painted, and the region below
// the last row is erased once (ED) so a shorter frame leaves no tail behind.
// Nothing sends CSI 2J. The whole paint is wrapped in synchronized output so it
// is presented once.
//
// The caret does NOT blink by the terminal's own clock here. Its shape is steady
// (DECSCUSR 2, set in run()); the blink is produced by this function showing or
// hiding it according to blinkOn, passed in per frame. Two earlier attempts kept
// the terminal-native blink and tried to keep the emit path from disturbing it —
// first by not re-showing the caret each frame, then by not re-positioning it —
// and both failed on an animated scene, because the ~12×/s in-place repaint walks
// the cursor down every painted row and this terminal restarts its blink on any
// cursor motion, not only on the caret's own CUP. A native blink cannot survive a
// constant repaint. Owning the blink makes it one deterministic on/off square
// wave, identical whether the scene animates or sits idle — the consistency the
// user reported missing.
//
// With a steady cursor there is no blink phase for a CUP to restart, so the caret
// is simply repositioned every frame; the strobe machinery the two failed fixes
// added is gone. The show/hide escapes still fire only on a change, so an
// unchanged blink phase costs nothing. The caret is never drawn as a cell
// (renderInput's contract): this is the native terminal caret, positioned by CUP
// and blinked by toggling its visibility. screenH is the terminal's row count,
// needed for the tail erase.
func emitFrame(w io.Writer, f ui.Frame, theme *theme.Theme, screenH int, st *emitState, on bool) {
	var b strings.Builder
	b.WriteString(frameBegin)
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
	// blinks on a row that means nothing. A steady cursor can be repositioned every
	// frame without restarting any blink, so the CUP is unconditional; only the
	// visibility escape — the blink itself — is gated on a change, both by the phase
	// (on) and by whether the frame hosts a caret at all.
	want := !f.Cursor.Hidden && (st == nil || on)
	if !f.Cursor.Hidden {
		b.WriteString(fmt.Sprintf("\033[%d;%dH", f.Cursor.Line+1, f.Cursor.Col+1))
	}
	switch {
	case st == nil:
		// One-shot paint: no loop, no blink ticker, so show a hosted caret once and
		// hide an absent one. There is no previous frame to diff against.
		if f.Cursor.Hidden {
			b.WriteString("\033[?25l")
		} else {
			b.WriteString("\033[?25h")
		}
	case want && !st.shown:
		b.WriteString("\033[?25h")
		st.shown = true
	case !want && st.shown:
		b.WriteString("\033[?25l")
		st.shown = false
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

// shortCwd is the working directory as the bottom bar shows it: the home directory
// collapses to "~" and a path too long for a status bar keeps its tail, which is the
// part that tells projects apart.
func shortCwd(p string) string {
	if p == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(p, home+string(os.PathSeparator)); ok {
			p = "~" + string(os.PathSeparator) + rest
		}
	}
	const keep = 40
	if r := []rune(p); len(r) > keep {
		return "…" + string(r[len(r)-keep+1:])
	}
	return p
}

// thinkingLabel is the Thinking line's lead: "• Thinking (3s) " while a turn is
// in flight, "• Thinking (1m 5s) " once it passes a minute. The trailing space
// separates it from the model's own words that scroll after it.
func thinkingLabel(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("• Thinking (%ds) ", secs)
	}
	return fmt.Sprintf("• Thinking (%dm %ds) ", secs/60, secs%60)
}
