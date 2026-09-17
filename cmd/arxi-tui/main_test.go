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
// Raw mode turns output processing off, so the terminal no longer translates
// \n into \r\n; a frame emitted with bare newlines draws as a staircase — one
// row down, one column further right per line. Every newline the emit path
// writes must carry its own carriage return, and the repaint must open with
// the clear-home sequence so a shorter frame cannot leave the tail of a
// longer one behind.
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

	if !strings.HasPrefix(out, "\033[H\033[2J") {
		t.Errorf("render does not open with clear-home; a shorter frame would leave the tail of a longer one on screen. got prefix %q", out[:min(len(out), 12)])
	}
	for i := 0; i < len(out); i++ {
		if out[i] == '\n' && (i == 0 || out[i-1] != '\r') {
			// Show the neighborhood so the failing line is in the message.
			lo := i - 20
			if lo < 0 {
				lo = 0
			}
			hi := i + 20
			if hi > len(out) {
				hi = len(out)
			}
			t.Errorf("bare \\n at byte %d (…%q…): on a raw terminal that is a staircase, not a line break. Every newline must be \\r\\n", i, out[lo:hi])
			break
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
