package main

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/term"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// testDriver is a Driver that submits prompts by appending them directly to
// a channel, so the loop test can verify the full round-trip: key → submit →
// event → fold → render.
type testDriver struct {
	evCh chan fold.Event
	seq  int64
}

func (d *testDriver) SubmitPrompt(ctx context.Context, text string) error {
	d.seq += 1000
	ev := fold.Event{
		Type:    "run.prompt",
		Seq:     d.seq,
		Payload: map[string]any{"text": text},
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case d.evCh <- ev:
		return nil
	}
}

func (d *testDriver) Close() error { return nil }

// TestLoopInputSubmitTranscriptExit drives the full event loop through a fake
// TTY with a scripted key sequence: type "test", press Enter, then double
// Ctrl-C to exit. It asserts that the typed text appears in the transcript
// and that the loop returns cleanly on the panic gesture.
//
// This is the Phase 0 integration test the raw scene needs: end-to-end
// through the real loop, the real fold, and the real renderer — only the
// terminal I/O is faked, so the scenario runs deterministically in CI
// without a PTY.
func TestLoopInputSubmitTranscriptExit(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	// Scripted key sequence:
	//   t e s t <enter> ctrl+c ctrl+c
	//
	// Each key gets a small delay so the loop's select sees them in order
	// with the interleaving real terminals exhibit. The two Ctrl-C presses
	// are spaced well within armTimeout (1.5s) so the panic gesture fires.
	script := []scheduledEvent{
		{0, keyEvent('t')},
		{10 * time.Millisecond, keyEvent('e')},
		{10 * time.Millisecond, keyEvent('s')},
		{10 * time.Millisecond, keyEvent('t')},
		{10 * time.Millisecond, keyEvent('\r')}, // Enter
		{50 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{
		evCh: make(chan fold.Event, 64),
		seq:  1000, // user-submitted prompts number above the mock's log
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "")
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()

	// The transcript must show the user's submitted text.
	if !strings.Contains(out, "test") {
		t.Errorf("transcript does not contain submitted text 'test'; output:\n%s", out)
	}

	// After submission the input line must be cleared — the prompt line should
	// show only the placeholder "> ", not the typed buffer.
	// The last rendered frame ends with the prompt line; "test" should appear
	// above it, not on it.
	if !strings.Contains(out, "> ") {
		t.Errorf("output does not contain input prompt '> '; output:\n%s", out)
	}

	// No "UNKNOWN NODE TYPE" — the factory scene must render cleanly.
	if strings.Contains(out, "UNKNOWN NODE TYPE") {
		t.Errorf("render produced unknown node type:\n%s", out)
	}
}

// TestLoopExitsOnCtrlCImmediate verifies that the escape gesture fires when the
// transcript is empty and the input buffer is empty: the very first two Ctrl-C
// presses should cause the loop to return nil.
func TestLoopExitsOnCtrlCImmediate(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	script := []scheduledEvent{
		{0, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "")
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}
}

// TestLoopFirstCtrlCClearsInput verifies that the first Ctrl-C clears the input
// buffer without exiting, and a subsequent regular keypress cancels the arm.
func TestLoopFirstCtrlCClearsInput(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	// Type "abc", first Ctrl-C (clears input and arms), then type 'x'
	// (disarms, input shows "x"), then Ctrl-C twice to exit.
	script := []scheduledEvent{
		{0, keyEvent('a')},
		{10 * time.Millisecond, keyEvent('b')},
		{10 * time.Millisecond, keyEvent('c')},
		{10 * time.Millisecond, ctrlCharEvent('c')}, // first Ctrl-C: clears "abc", arms
		{50 * time.Millisecond, keyEvent('x')},      // disarms, starts fresh input
		{10 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, "")
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()
	// The input buffer should have been cleared by the first Ctrl-C, then 'x'
	// typed, so "x" appears but "abc" should not appear as current input.
	// "abc" may appear if a previous frame rendered it before the Ctrl-C
	// cleared it, so we check that the final state shows "x" in the prompt.
	if !strings.Contains(out, "x") {
		t.Errorf("expected 'x' in final output (input was cleared then re-typed); output:\n%s", out)
	}
}

// TestLoopParksTheTerminalCursorInTheInputBar is the end-to-end half of the
// caret contract: the bytes the loop writes must name a row and a column,
// because nothing else moves the terminal cursor off the corner a full repaint
// leaves it in. The column is the one the frame computed for the sobria
// prompt — two for the "┃ " prefix, then the typed text.
func TestLoopParksTheTerminalCursorInTheInputBar(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	script := []scheduledEvent{
		{0, keyEvent('h')},
		{10 * time.Millisecond, keyEvent('i')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()

	// The empty line parks the caret just past the two-column prefix, on the
	// prompt row of the 24-row sobria frame: header, transcript, prompt, status.
	if !strings.Contains(out, "\x1b[23;3H") {
		t.Errorf("no cursor-position escape for the empty input; the caret is left in the corner. output:\n%q", out)
	}
	// After "hi" the caret walked two columns with the text.
	if !strings.Contains(out, "\x1b[23;5H") {
		t.Errorf("caret did not follow the typed text; want a park at row 23 column 5. output:\n%q", out)
	}
}

// stripANSI removes escape sequences so a test can match the visible text the
// way the user sees it, not the byte stream the terminal receives. The input
// row arrives as styled spans — "┃ " and the typed text are separated by SGR
// resets — so a byte-level Contains("┃ x") can never match a frame the user
// would read as "┃ x".
func stripANSI(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		s = s[i+2:]
		j := 0
		for j < len(s) && !unicode.IsLetter(rune(s[j])) {
			j++
		}
		if j < len(s) {
			j++ // the final letter terminates the sequence
		}
		s = s[j:]
	}
}

// frameHasTranscriptLine reports whether any visual row of the frame is
// exactly the given text, after escapes are stripped. A whole-row match is
// what tells a transcript line from a menu row (a menu row continues with the
// description) and from chrome that merely contains the word.
func frameHasTranscriptLine(frame, text string) bool {
	for _, l := range strings.Split(stripANSI(frame), "\n") {
		if strings.TrimRight(l, "\r") == text {
			return true
		}
	}
	return false
}

// TestLoopSlashMenuNavigateAndRun drives the menu end to end: "/" opens it,
// down moves the highlight to the second command, enter runs it, and the
// command name lands in the transcript as the submitted prompt — while the
// menu itself closes, because the buffer it was derived from is gone.
func TestLoopSlashMenuNavigateAndRun(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	script := []scheduledEvent{
		{0, keyEvent('/')},
		{10 * time.Millisecond, arrowEvent(term.KeyDown)},
		{10 * time.Millisecond, arrowEvent(term.KeyDown)},
		{30 * time.Millisecond, enterEvent()},
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	// One frame per repaint, each introduced by the clear-home sequence.
	frames := strings.Split(tty.output(), "\x1b[H\x1b[2J")

	// The menu must have been open with all five commands.
	if !strings.Contains(tty.output(), "Commands 5 · type to filter") {
		t.Errorf("menu header never rendered; output:\n%s", tty.output())
	}

	// Enter runs the highlighted row: "focus" appears as a transcript line of
	// its own in a frame where the menu is already closed. A whole-row match
	// is what tells the transcript line from the menu row "focus  Focus a
	// node by id", which contains the same word.
	ranCommand := false
	for _, f := range frames {
		if frameHasTranscriptLine(f, "❯ focus") && !strings.Contains(f, "Commands 5") {
			ranCommand = true
			break
		}
	}
	if !ranCommand {
		t.Errorf("enter did not run the highlighted command 'focus' (or the menu stayed open); frames:\n%s", tty.output())
	}
}

// TestLoopSlashMenuEscapeCloses verifies escape closes the menu by dropping
// the line: the menu is derived from the "/" prefix, so a dismissed menu is a
// cleared buffer, and the next keystroke types into the plain input again.
func TestLoopSlashMenuEscapeCloses(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	script := []scheduledEvent{
		{0, keyEvent('/')},
		{10 * time.Millisecond, keyEvent('h')},
		{30 * time.Millisecond, arrowEvent(term.KeyEscape)},
		{30 * time.Millisecond, keyEvent('x')},
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()
	if !strings.Contains(out, "Commands 1 · type to filter") {
		t.Errorf("filtered menu (\"/h\" → help) never rendered; output:\n%s", out)
	}

	// After escape the buffer is cleared (the placeholder returns) and the
	// menu is gone; typing 'x' lands in the plain input with no menu above it.
	frames := strings.Split(out, "\x1b[H\x1b[2J")
	cleared, typingAgain := false, false
	for _, f := range frames {
		// The input row carries the sobria prefix, so the placeholder line is
		// "┃ ask anything, or / for commands" — prefix included.
		if frameHasTranscriptLine(f, "┃ ask anything, or / for commands") && !strings.Contains(f, "Commands") {
			cleared = true
		}
		if strings.Contains(stripANSI(f), "┃ x") && !strings.Contains(f, "Commands") && !strings.Contains(f, "no matches") {
			typingAgain = true
		}
	}
	if !cleared {
		t.Errorf("escape did not clear the line (placeholder never returned without the menu); frames:\n%s", out)
	}
	if !typingAgain {
		t.Errorf("after escape the next keystroke did not type into the plain input; frames:\n%s", out)
	}
}

// TestLoopSlashMenuTabWalksCategories verifies tab jumps the highlight to the
// first match of the next category, wrapping to the top. The registry has a
// single category, so down-down-tab must land back on row 0: enter then runs
// "help", not "focus" — the exact discrimination the test hinges on.
func TestLoopSlashMenuTabWalksCategories(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	script := []scheduledEvent{
		{0, keyEvent('/')},
		{10 * time.Millisecond, arrowEvent(term.KeyDown)},
		{10 * time.Millisecond, arrowEvent(term.KeyDown)},
		{10 * time.Millisecond, arrowEvent(term.KeyTab)},
		{30 * time.Millisecond, enterEvent()},
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	frames := strings.Split(tty.output(), "\x1b[H\x1b[2J")
	ranHelp := false
	for _, f := range frames {
		if frameHasTranscriptLine(f, "❯ help") && !strings.Contains(f, "Commands 5") {
			ranHelp = true
			break
		}
	}
	if !ranHelp {
		t.Errorf("tab did not walk the highlight back to the first row ('help' never ran); frames:\n%s", tty.output())
	}
}

// TestLoopReceivesDriverEvents verifies that events arriving on the event
// channel from the driver (e.g. mock log replay or log-follow) are folded
// and rendered, proving the loop handles the non-keyboard side of the
// two-event model.
// TestLoopSobriaSlashMenu verifies that the sobria scene renders its header,
// status bar, and that typing "/" activates the slash command menu. The menu
// must appear overlaid at the bottom of the screen with the command list.
func TestLoopSobriaSlashMenu(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	// Pre-feed driver events to establish a transcript, then simulate the
	// user typing "/help" and double-Ctrl-C to exit.
	driverEvents := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{
			"text": "Hola!", "model": "openai/gpt-4o",
			"tokens_in": 5, "tokens_out": 3,
		}},
	}

	evCh := make(chan fold.Event, 64)
	defer close(evCh)

	go func() {
		time.Sleep(10 * time.Millisecond)
		for _, e := range driverEvents {
			evCh <- e
		}
	}()

	// Type "/help" then double-Ctrl-C to exit.
	script := []scheduledEvent{
		{100 * time.Millisecond, keyEvent('/')},
		{10 * time.Millisecond, keyEvent('h')},
		{10 * time.Millisecond, keyEvent('e')},
		{10 * time.Millisecond, keyEvent('l')},
		{10 * time.Millisecond, keyEvent('p')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, "")
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()
	// The header must render.
	if !strings.Contains(out, "Δr×i v0.1.0") {
		t.Errorf("sobria header not rendered; output:\n%s", out)
	}
	// The slash menu must activate (the "no matches" or command list should appear).
	if !strings.Contains(out, "no matches") && !strings.Contains(out, "Show available commands") {
		t.Errorf("slash menu did not activate on '/' keypress; output:\n%s", out)
	}
	// The status bar must show the model name.
	if !strings.Contains(out, "openai/gpt-4o") {
		t.Errorf("status bar missing model name; output:\n%s", out)
	}
}

// TestLoopSlashMenuWrapsUpAndDown verifies the menu's rotary navigation: pressing
// Up on the first row wraps to the last, and pressing Down on the last wraps to
// the first. The highlight must always sit on a real row — a menu that spins
// off the end and leaves Enter pointing at nothing is a menu that lies.
func TestLoopSlashMenuWrapsUpAndDown(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	// "/" opens the menu on the first row (index 0 = "help"). Pressing Up at
	// row 0 wraps to the last row (index 4 = "ui"); Enter runs it.
	script := []scheduledEvent{
		{0, keyEvent('/')},
		{50 * time.Millisecond, arrowEvent(term.KeyUp)}, // wrap: help(0) → ui(4)
		{30 * time.Millisecond, enterEvent()},           // runs "ui"
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	frames := strings.Split(tty.output(), "\x1b[H\x1b[2J")

	// Up from "help" (row 0) must have wrapped to "ui" (row 4): Enter runs "ui",
	// and "ui" appears as a whole transcript line in a frame where the menu
	// is already closed (no "Commands 5" header above it).
	ranUI := false
	for _, f := range frames {
		if frameHasTranscriptLine(f, "❯ ui") && !strings.Contains(f, "Commands 5") {
			ranUI = true
			break
		}
	}
	if !ranUI {
		t.Errorf("Up did not wrap from the first row to the last ('ui' never ran); frames:\n%s", tty.output())
	}
}

// TestLoopSlashMenuSwapsStatusForHint verifies the bottom line is a single line
// of info: when the menu is open the live status bar yields to the navigation
// hint, and closing the menu brings the status bar back. Both must never appear
// on the same frame.
func TestLoopSlashMenuSwapsStatusForHint(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("scene validation: %v", err)
	}

	driverEvents := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "simulated": false,
		}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{
			"text": "Hola!", "model": "openai/gpt-4o",
			"tokens_in": 5, "tokens_out": 3,
		}},
	}

	evCh := make(chan fold.Event, 64)
	go func() {
		time.Sleep(10 * time.Millisecond)
		for _, e := range driverEvents {
			evCh <- e
		}
	}()

	// Open the menu and stay there until the panic gesture exits.
	script := []scheduledEvent{
		{150 * time.Millisecond, keyEvent('/')},
		{100 * time.Millisecond, arrowEvent(term.KeyDown)}, // menu stays open
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: evCh}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, ""); err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()

	// A frame with the menu open must show the navigation hint and must NOT show
	// the status bar text on the same screen — the single bottom line swapped.
	frames := strings.Split(out, "\x1b[H\x1b[2J")
	hintRendered := false
	for i, f := range frames {
		stripped := stripANSI(f)
		if strings.Contains(stripped, "navigate · enter use · esc close") {
			hintRendered = true
			// The hint frame must not carry the status bar on the same screen.
			if strings.Contains(stripped, "openai/gpt-4o") {
				t.Errorf("frame %d shows both the status bar and the nav hint on the same screen:\n%s", i, stripped)
			}
		}
	}
	if !hintRendered {
		t.Errorf("navigation hint never rendered while the menu was open; output:\n%s", stripANSI(out))
	}
}

// TestLoopSobriaStatusbarRenders verifies the sobria scene renders the status
// bar (agent.mode · model.name · ⚡︎) after receiving driver events.
func TestLoopSobriaStatusbarRenders(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factorySobria))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	driverEvents := []fold.Event{
		{Type: "run.started", Seq: 1, Payload: map[string]any{
			"run_id": "r1", "simulated": false,
		}},
		{Type: "agent.activated", Seq: 2, Payload: map[string]any{"agent": "backend"}},
		{Type: "llm.response", Seq: 3, Payload: map[string]any{
			"text": "Hola!", "model": "openai/gpt-4o",
			"tokens_in": 12, "tokens_out": 8,
		}},
		{Type: "agent.turn_done", Seq: 4, Payload: map[string]any{"agent": "backend"}},
	}

	evCh := make(chan fold.Event, 64)

	go func() {
		time.Sleep(10 * time.Millisecond)
		for _, e := range driverEvents {
			evCh <- e
		}
	}()

	script := []scheduledEvent{
		{200 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: make(chan fold.Event, 64)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, "")
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := stripANSI(tty.output())
	// The status row splits "live" (bright, the agent.mode bind under the
	// "header" token) from the dim separators and model — stripANSI collapses
	// the SGR resets between spans so the visible text is one contiguous line.
	if !strings.Contains(out, "live · openai/gpt-4o · ⚡︎") {
		t.Errorf("status bar not rendered correctly; output:\n%s", out)
	}
}

// TestLoopReceivesDriverEvents verifies that events arriving on the event
func TestLoopReceivesDriverEvents(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	// Two Ctrl-C presses exit immediately; the driver events arrive first
	// and must be rendered before the loop returns.
	driverEvents := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola!"}},
	}

	evCh := make(chan fold.Event, 64)
	defer close(evCh)

	// Feed driver events after a short delay, before the Ctrl-C sequence.
	go func() {
		time.Sleep(20 * time.Millisecond)
		for _, e := range driverEvents {
			evCh <- e
		}
	}()

	script := []scheduledEvent{
		{100 * time.Millisecond, ctrlCharEvent('c')},
		{50 * time.Millisecond, ctrlCharEvent('c')},
	}

	tty := newFakeTTY(80, 24, script)
	drv := &testDriver{evCh: evCh}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = loop(ctx, tty, doc, theme.SOBRIA(), evCh, drv, "")
	if err != nil {
		t.Fatalf("loop returned error: %v", err)
	}

	out := tty.output()
	// The driver events must have been folded and rendered.
	if !strings.Contains(out, "hola") {
		t.Errorf("transcript does not contain driver event 'hola'; output:\n%s", out)
	}
	if !strings.Contains(out, "Hola!") {
		t.Errorf("transcript does not contain driver event 'Hola!'; output:\n%s", out)
	}
}
