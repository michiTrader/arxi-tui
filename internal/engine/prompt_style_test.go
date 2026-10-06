package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/ui"
)

func styledChat(style string) ui.Frame {
	state := fold.State{History: []fold.ChatLine{
		{Role: "user", Text: "hello there"},
		{Role: "assistant", Text: "hi"},
	}}
	r := Renderer{Width: 30, PromptStyle: style}
	return r.renderMarkdown(&scene.Node{Bind: "chat.history"}, state, -1)
}

func TestPromptStyleBarIsTheDefault(t *testing.T) {
	for _, style := range []string{"", PromptBar} {
		got := styledChat(style).Plain()
		if !strings.HasPrefix(got, "┃ hello there") {
			t.Errorf("style %q: want the bar marker, got:\n%s", style, got)
		}
	}
}

func TestPromptStylePlainDropsTheMarker(t *testing.T) {
	got := styledChat(PromptPlain).Plain()
	if strings.Contains(got, "┃") {
		t.Errorf("plain style still draws the marker:\n%s", got)
	}
	if !strings.HasPrefix(got, "hello there") {
		t.Errorf("plain style should start with the words:\n%s", got)
	}
}

func TestPromptStyleBandShadesTheWholeRowToTheEdge(t *testing.T) {
	f := styledChat(PromptBand)
	row := f.Live[0]
	if row.Width() != 30 {
		t.Errorf("banded row is %d columns, want the full 30", row.Width())
	}
	for _, sp := range row {
		if sp.Fill != promptBandToken {
			t.Errorf("span %q has fill %q, want %q", sp.Text, sp.Fill, promptBandToken)
		}
	}
	// Only the user's own message is banded: the answer stays unshaded.
	for _, l := range f.Live[1:] {
		for _, sp := range l {
			if sp.Fill != "" {
				t.Errorf("the answer picked up the band: %q", sp.Text)
			}
		}
	}
}

func TestPinnedQuestionKeepsTheBandWhenScrolled(t *testing.T) {
	state := longChat()
	r := Renderer{Width: 60, ChatScroll: 14, PromptStyle: PromptBand}
	f := r.renderMarkdown(&scene.Node{Bind: "chat.history"}, state, 8)
	first := f.Live[0]
	if !strings.Contains(first.Text(), "FIRSTQ") {
		t.Fatalf("expected the pinned question on the first row, got %q", first.Text())
	}
	if first.Width() != 60 {
		t.Errorf("pinned banded row is %d columns, want 60", first.Width())
	}
	for _, sp := range first {
		if sp.Fill != promptBandToken {
			t.Errorf("pinned span %q lost the band", sp.Text)
		}
	}
}
