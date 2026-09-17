package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

const factoryRAWScene = `{ "root": { "type": "stack", "children": [
  { "id": "chat",   "type": "markdown", "bind": "chat.history", "grow": 1 },
  { "id": "prompt", "type": "input",    "bind": "user.input", "placeholder": "> " }
]}}`

// TestRawSceneRendersCorrectly renders RAW.scene with the Phase 0 mock events
// and verifies that chat.history shows the transcript and user.input shows the prompt.
func TestRawSceneRendersCorrectly(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 80, Height: 24}

	events := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola!"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}
	state := fold.Fold(events)

	f := r.RenderFrame(doc, state)
	got := f.Plain()

	if strings.Contains(got, "UNKNOWN NODE TYPE") {
		t.Errorf("render produced unknown node type; output:\n%s", got)
	}
	if !strings.Contains(got, "hola") {
		t.Errorf("expected 'hola' in chat history; got:\n%s", got)
	}
	if !strings.Contains(got, "Hola!") {
		t.Errorf("expected assistant response; got:\n%s", got)
	}
	if !strings.HasSuffix(got, "> ") {
		t.Errorf("expected output to end with '> '; got:\n%s", got)
	}
	t.Logf("rendered frame:\n%s", got)
}

// TestRawSceneInputNeverContracts is the Q6 property in executable form:
// the input row is always exactly one line at the bottom, no matter how much
// transcript fills the frame. A community scene that overflows must contract
// the elastic panes first and never the typing flow.
func TestRawSceneInputNeverContracts(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 6}

	// Ten turns: 2 lines each = 20 lines, but the frame is only 6 tall.
	events := []fold.Event{}
	for i := 1; i <= 10; i++ {
		events = append(events, fold.Event{
			Type:    "run.prompt",
			Seq:     int64(i) * 2,
			Payload: map[string]any{"text": fmtShort(i)},
		})
		events = append(events, fold.Event{
			Type:    "llm.response",
			Seq:     int64(i)*2 + 1,
			Payload: map[string]any{"text": fmtShort(i * 2)},
		})
	}
	state := fold.Fold(events)

	f := r.RenderFrame(doc, state)
	lines := f.Live

	// Exactly 6 lines: 4 scrollback + 1 blank + 1 prompt. Never 5 or 7.
	if len(lines) != r.Height {
		t.Errorf("expected %d lines in a %d-tall frame, got %d", r.Height, r.Height, len(lines))
	}
	// The last line must be the input prompt.
	last := lines[len(lines)-1].Text()
	if !strings.Contains(last, "> ") {
		t.Errorf("expected last line to be the prompt '> '; got %q", last)
	}
}

// TestRawSceneTranscriptFillsFromTail guarantees new lines arrive at the
// bottom, above the input — the user does not have to scroll to see the
// reply they are waiting for.
func TestRawSceneTranscriptFillsFromTail(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	r := Renderer{Width: 80, Height: 4}
	state := fold.Fold([]fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "first"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "second"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "third"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "fourth"}},
	})

	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// "fourth" is the newest line; it must not have been clipped by overflow.
	if !strings.Contains(got, "fourth") {
		t.Errorf("newest transcript line 'fourth' was clipped; got:\n%s", got)
	}
}

// TestInputPlaceholderIsAHintNotText pins both halves of the input contract
// that a human notices the moment they are wrong: the placeholder is drawn
// only while the line is empty, under its own token so a theme can dim it, and
// it never concatenates itself in front of what was typed.
func TestInputPlaceholderIsAHintNotText(t *testing.T) {
	const sceneJSON = `{ "root": { "type": "stack", "children": [
	  { "id": "prompt", "type": "input", "bind": "user.input",
	    "prefix": "┃ ", "placeholder": "ask anything" }
	]}}`
	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 3}

	empty := r.RenderFrame(doc, fold.State{})
	if !strings.Contains(empty.Plain(), "ask anything") {
		t.Fatalf("empty input does not draw its placeholder; the human has no hint:\n%s", empty.Plain())
	}
	line := empty.Live[0]
	if len(line) == 0 {
		t.Fatal("empty input drew no cells at all")
	}
	if last := line[len(line)-1]; last.Style != "input.placeholder" {
		t.Errorf("placeholder drawn under style %q, want \"input.placeholder\"; the hint reads as text the human typed", last.Style)
	}

	typed := r.RenderFrame(doc, fold.State{UserInput: "hi"})
	got := typed.Plain()
	if strings.Contains(got, "ask anything") {
		t.Errorf("placeholder survived the first keystroke and glued itself to the typed text:\n%s", got)
	}
	if !strings.Contains(got, "┃ hi") {
		t.Errorf("typed text not drawn after the prefix; got:\n%s", got)
	}
}

// TestInputFrameReportsWhereTheCaretGoes is what the terminal cursor needs to
// exist at all: a row and a column measured in the frame, not in the input.
// Without it the host writes a full repaint and leaves the caret wherever the
// last byte put it, which is the bottom-right corner.
func TestInputFrameReportsWhereTheCaretGoes(t *testing.T) {
	const sceneJSON = `{ "root": { "type": "stack", "children": [
	  { "type": "text", "text": "above" },
	  { "id": "prompt", "type": "input", "bind": "user.input",
	    "prefix": "┃ ", "placeholder": "ask anything" }
	]}}`
	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 4}

	f := r.RenderFrame(doc, fold.State{})
	if f.Cursor.Hidden {
		t.Fatal("frame reports no caret; the terminal cursor would stay in the corner")
	}
	if f.Cursor.Line != 1 {
		t.Errorf("caret on frame row %d, want 1 (the row below the text node); the row is counted from the frame, not from the input", f.Cursor.Line)
	}
	if f.Cursor.Col != 2 {
		t.Errorf("empty caret at column %d, want 2 (past the two-column \"┃ \" prefix)", f.Cursor.Col)
	}

	f = r.RenderFrame(doc, fold.State{UserInput: "hi"})
	if want := 4; f.Cursor.Col != want {
		t.Errorf("caret at column %d after typing \"hi\", want %d; the caret must walk with the text", f.Cursor.Col, want)
	}
}

// TestFramesWithoutAnInputCarryNoCaret keeps the other renderers honest. The
// caret belongs to the input, and a text-only frame that reported one would
// park the terminal on a row that means nothing.
func TestFramesWithoutAnInputCarryNoCaret(t *testing.T) {
	const sceneJSON = `{ "root": { "type": "stack", "children": [
	  { "type": "text", "text": "no caret here" }
	]}}`
	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 40, Height: 3}
	f := r.RenderFrame(doc, fold.State{})
	if !f.Cursor.Hidden {
		t.Errorf("frame with no input reports a caret at %+v; the terminal would park on a meaningless row", f.Cursor)
	}
}

// TestRawSceneStyledGolden compares the rendered RAW scene with token annotations
// against the golden file. UPDATE_GOLDEN=1 regenerates the fixture.
func TestRawSceneStyledGolden(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAWScene))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	events := []fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "Hola!"}},
		{Type: "run.prompt", Seq: 3, Payload: map[string]any{"text": "gracias"}},
		{Type: "llm.response", Seq: 4, Payload: map[string]any{"text": "De nada."}},
	}
	state := fold.Fold(events)

	r := Renderer{Width: 80, Height: 24}
	f := r.RenderFrame(doc, state)
	got := f.Styled()

	goldenPath := "../../testdata/RAW.styled"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if got != string(want) {
		t.Errorf("raw scene styled output does not match golden:\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

// TestOverlayBottomAppearsAfterInput verifies that an overlay with anchor="bottom"
// appears AFTER the input node on screen (below it), not before or over it.
// This floats the slash menu beneath the input bar where the user can see both.
func TestOverlayBottomAppearsAfterInput(t *testing.T) {
	sceneJSON := `{ "root": { "type": "stack", "children": [
	  { "id": "chat", "type": "text", "text": "Chat content", "grow": 1 },
	  { "id": "input", "type": "input", "bind": "user.input", "placeholder": "type here" },
	  { "id": "overlay", "type": "overlay", "anchor": "bottom", "when": "slash.active",
	    "children": [
	      { "type": "text", "text": "OVERLAY LINE 1" },
	      { "type": "text", "text": "OVERLAY LINE 2" }
	    ]}
	]}}`

	doc, err := scene.ParseDocument([]byte(sceneJSON))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	r := Renderer{Width: 80, Height: 10}

	// State with slash.active=true to trigger the overlay.
	events := []fold.Event{
		{Type: "ui.state", Seq: 1, Payload: map[string]any{"slash.active": true}},
		{Type: "ui.state", Seq: 2, Payload: map[string]any{"user.input": "/"}},
	}
	state := fold.Fold(events)

	f := r.RenderFrame(doc, state)
	got := f.Plain()

	// The overlay lines should be present.
	if !strings.Contains(got, "OVERLAY LINE 1") {
		t.Errorf("overlay line 1 missing.\nGot:\n%s", got)
	}
	if !strings.Contains(got, "OVERLAY LINE 2") {
		t.Errorf("overlay line 2 missing.\nGot:\n%s", got)
	}

	// The input line should still be visible, showing the typed "/".
	// The input node renders as "┃ /" (prefix + text).
	if !strings.Contains(got, "/") {
		t.Errorf("input text missing; overlay hid it instead of appearing after it.\nGot:\n%s", got)
	}

	// Verify order: overlay should come AFTER input in the output (below on screen).
	lines := strings.Split(got, "\n")
	overlayIdx := -1
	inputIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "OVERLAY LINE 1") {
			overlayIdx = i
		}
		// The input line is the one containing the slash we typed.
		if strings.Contains(line, "/") && !strings.Contains(line, "OVERLAY") {
			inputIdx = i
		}
	}
	if overlayIdx == -1 || inputIdx == -1 {
		t.Fatalf("could not find overlay (idx=%d) or input (idx=%d) in output", overlayIdx, inputIdx)
	}
	if overlayIdx <= inputIdx {
		t.Errorf("overlay should appear AFTER input (below on screen); overlay at line %d, input at line %d.\nGot:\n%s",
			overlayIdx, inputIdx, got)
	}

	t.Logf("rendered frame with overlay:\n%s", got)
}

func fmtShort(n int) string {
	return string(rune('A' + n - 1))
}
