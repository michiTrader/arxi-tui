package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

func drawn(r *bridgeRig, turns int) string {
	var evs []fold.Event
	for i := 0; i < turns; i++ {
		evs = append(evs, fold.Event{Type: "run.prompt", Seq: int64(i + 1), Payload: map[string]any{"text": "msg"}})
	}
	rd := &engine.Renderer{Width: 60, Height: 14}
	return rd.RenderFrame(r.doc, fold.Fold(evs)).Plain()
}

// The three things the user asked their agent for and could not get: replace the factory
// banner, show an ASCII banner with every row, and have a banner that floats and one that
// scrolls away. Each is done here with the commands the guide teaches, through ui_edit.
func TestTheBannerRecipesInTheGuideWorkEndToEnd(t *testing.T) {
	r := newBridgeRig(t, "allow")
	if !strings.Contains(drawn(r, 0), "Run /help for commands") {
		t.Fatal("the factory banner is not on screen to start with")
	}
	// Replace the factory banner with a three-row art banner (the \n is JSON-escaped twice:
	// once for the command string, once inside the fragment).
	res := r.edit(t, `{"commands":["/ui remove banner","/ui remove banner_gap",
	  "/ui add node above chat {\"id\":\"art\",\"type\":\"text\",\"text\":\"╔════╗\\n║ ARXI ║\\n╚════╝\"}"]}`, true)
	if !res.OK {
		t.Fatalf("refused: %s", res.Text)
	}
	out := drawn(r, 0)
	for _, want := range []string{"╔════╗", "║ ARXI ║", "╚════╝"} {
		if !strings.Contains(out, want) {
			t.Errorf("art row %q is missing:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Run /help for commands") || strings.Contains(out, "v0.1.0") {
		t.Errorf("the factory banner is still there:\n%s", out)
	}

	// The loop republishes the document after every applied edit.
	r.b.publish(r.doc, theme.SOBRIA(), nil, nil, behaviour{})
	// A second banner that scrolls away with the conversation, next to the floating one.
	res = r.edit(t, `{"commands":["/ui add node below art {\"id\":\"hello\",\"type\":\"text\",\"text\":\"WELCOME-SCROLLS\",\"in_chat\":true}"]}`, true)
	if !res.OK {
		t.Fatalf("refused: %s", res.Text)
	}
	if few := drawn(r, 1); !strings.Contains(few, "WELCOME-SCROLLS") || !strings.Contains(few, "ARXI") {
		t.Errorf("both banners must show at first:\n%s", few)
	}
	many := drawn(r, 40)
	if strings.Contains(many, "WELCOME-SCROLLS") || !strings.Contains(many, "ARXI") {
		t.Errorf("the scrolling banner must go, the floating one stay:\n%s", many)
	}
}

// Only the words of the factory banner can be changed without touching the logo.
func TestTheFactoryBannerWordsHaveAnId(t *testing.T) {
	r := newBridgeRig(t, "allow")
	if res := r.edit(t, `{"commands":["/ui set banner_words text  my own words"]}`, true); !res.OK {
		t.Fatalf("refused: %s", res.Text)
	}
	out := drawn(r, 0)
	if !strings.Contains(out, "my own words") || strings.Contains(out, "Run /help") {
		t.Errorf("the words were not replaced:\n%s", out)
	}
}

func TestTheGuideTeachesTheBannerRecipes(t *testing.T) {
	g := uiGuide(builtinDoc(t), theme.SOBRIA())
	for _, want := range []string{"BANNER.", "banner_words", "/ui remove banner", "in_chat", "FLOATING", "never search files"} {
		if !strings.Contains(g, want) {
			t.Errorf("the guide lacks %q", want)
		}
	}
}
