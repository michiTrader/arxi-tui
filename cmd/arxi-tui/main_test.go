package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// TestRenderCarriesCarriageReturns fixes the emit contract for a raw terminal.
// Two failures this guards against, both invisible in a golden and both a
// staircase or a flash on a real terminal:
//
//   - A bare \n. Raw mode turns output processing off, so the terminal no longer
//     translates \n into \r\n; a frame emitted with bare newlines draws as a
//     staircase — one row down, one column further right per line. The in-place
//     emit path positions every row absolutely (CUP) and writes no newline at
//     all, so a stray \n reaching the terminal is a bug.
//   - A full-screen clear. The old path opened each frame with CSI 2J, blanking
//     the whole screen before repainting it, which flashes on every keystroke and
//     every animation tick. The repaint must instead be atomic (wrapped in
//     synchronized output) and erase in place, never sending CSI 2J.
func TestRenderCarriesCarriageReturns(t *testing.T) {
	doc, err := scene.ParseDocument([]byte(factoryRAW))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	state := fold.Fold([]fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hola"}},
		{Type: "llm.response", Seq: 2, Payload: map[string]any{"text": "line one\nline two"}},
	})

	var buf bytes.Buffer
	render(&buf, doc, engine.Renderer{Width: 80, Height: 24}, theme.SOBRIA(), state)
	out := buf.String()

	if !strings.HasPrefix(out, frameBegin) {
		t.Errorf("render does not open in synchronized output; a repaint seen half-drawn flickers. got prefix %q", out[:min(len(out), 12)])
	}
	if strings.Contains(out, "\033[2J") {
		t.Errorf("render clears the whole screen with CSI 2J; that blanks the screen before every repaint and flickers. Erase in place instead")
	}
	if strings.Contains(out, "\n") {
		t.Errorf("emit wrote a newline; the in-place path positions every row with CUP and a raw terminal turns a bare \\n into a staircase")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
