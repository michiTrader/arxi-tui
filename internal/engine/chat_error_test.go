package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

func mustDoc(t *testing.T, js string) *scene.Document {
	t.Helper()
	doc, err := scene.ParseDocument([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// A failed request is drawn inside the transcript, in order, as a normal turn.
func TestFailedRequestRendersInTheChatFlow(t *testing.T) {
	doc := mustDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"chat","type":"markdown","bind":"chat.history","grow":1}]}}`)
	state := fold.Fold([]fold.Event{
		{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "hello"}},
		{Type: "chat.error", Seq: 2, Payload: map[string]any{"text": "no provider is set up yet: add one with /provider"}},
	})
	r := Renderer{Width: 80, Height: 10}
	got := r.RenderFrame(doc, state).Plain()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected the prompt, a gap and the error, got:\n%s", got)
	}
	if !strings.Contains(lines[0], "hello") {
		t.Errorf("first row should be the user's line: %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "✗ ") || !strings.Contains(lines[2], "/provider") {
		t.Errorf("the error should follow the prompt in the flow, got %q", lines[2])
	}
}

// The working indicator turns through the Braille frames as the host clock ticks,
// and is absent when nothing is working.
func TestWorkingSpinnerCyclesBrailleFrames(t *testing.T) {
	doc := mustDoc(t, `{"root":{"type":"stack","children":[
	  {"id":"working","type":"row","when":"agent.working","children":[
	    {"id":"working_spin","type":"spinner","bind":"agent.working"},
	    {"type":"text","text":" working"}]}]}}`)
	busy := fold.Fold([]fold.Event{{Type: "agent.activated", Seq: 1, Payload: map[string]any{"agent": "a"}}})

	seen := map[string]bool{}
	for tick := 0; tick < len(spinnerFrames); tick++ {
		r := Renderer{Width: 40, Height: 5, AnimTicks: map[string]int{"working_spin": tick}}
		frame, act := r.RenderFrameActive(doc, busy)
		got := frame.Plain()
		want := spinnerFrames[tick]
		if !strings.HasPrefix(got, want+" working") {
			t.Fatalf("tick %d: want %q then \" working\", got %q", tick, want, got)
		}
		seen[want] = true
		found := false
		for _, a := range act {
			found = found || a.NodeID == "working_spin"
		}
		if !found {
			t.Errorf("tick %d: the spinner did not report itself as animating", tick)
		}
	}
	if len(seen) != 10 {
		t.Errorf("expected 10 distinct frames, saw %d", len(seen))
	}

	idle := fold.Fold(nil)
	r := Renderer{Width: 40, Height: 5}
	if got := r.RenderFrame(doc, idle).Plain(); strings.Contains(got, "working") {
		t.Errorf("nothing is working, yet the indicator shows: %q", got)
	}
}
