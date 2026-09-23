package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The render half of G2: a marquee with scroll advances a window over its
// content as the host clock's tick count rises, and the frame is a pure
// function of that tick count (ADR-0005) so a golden can pin it. These pin the
// window at t=0, a mid-scroll tick, and a full wrap, and prove the offset comes
// from the phase rather than being a static truncation.
//
// The content is ASCII so display width equals byte length and the expected
// windows can be written out by hand; wide-character correctness is truncateText
// and ansi.Cut's contract, exercised elsewhere.

const marqueeContent = "ABCDEFGHIJKLMNOP" // 16 columns wide

func scrollDoc(t *testing.T, speed int, pauseWhen string) *scene.Document {
	t.Helper()
	pw := ""
	if pauseWhen != "" {
		pw = `, "pause_when": "` + pauseWhen + `"`
	}
	body := `{ "root": { "id": "mq", "type": "marquee", "bind": "thinking.text",
	  "scroll": { "speed": ` + itoa(speed) + pw + ` } } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: scroll doc must parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: scroll doc must validate; got %v", verr)
	}
	return doc
}

func scrollState() fold.State {
	s := fold.Fold(nil)
	s.ThinkingText = marqueeContent
	return s
}

// The window slides with the tick count. speed 2, budget 10, content 16 (+4 gap
// = cycle 20): tick 0 shows the head, tick 3 shows a six-column shift, and the
// window is exactly ten columns wide throughout.
func TestScrollWindowsWithTheTickCount(t *testing.T) {
	doc := scrollDoc(t, 2, "")
	state := scrollState()

	cases := []struct {
		ticks int
		want  string
	}{
		{ticks: 0, want: "ABCDEFGHIJ"},  // offset 0
		{ticks: 1, want: "CDEFGHIJKL"},  // offset 2
		{ticks: 3, want: "GHIJKLMNOP"},  // offset 6
		{ticks: 10, want: "ABCDEFGHIJ"}, // offset 20 mod 20 == 0: a full wrap
	}
	for _, tc := range cases {
		r := Renderer{Width: 10, Height: 1, AnimTicks: map[string]int{"mq": tc.ticks}}
		got := strings.TrimRight(r.RenderFrame(doc, state).Plain(), " ")
		if got != tc.want {
			t.Errorf("tick %d: window is %q, want %q\n"+
				"consequence: the marquee's horizontal offset is not being taken from the host\n"+
				"clock's tick count, so either the phase is ignored (a static truncation) or the\n"+
				"offset math is wrong. Either way the frame does not move as ADR-0005 signs it.\n"+
				"remedy: renderMarquee must slice a %d-wide window at column (ticks*speed) mod\n"+
				"(width+gap).", tc.ticks, got, tc.want, 10)
		}
	}
}

// A short marquee — content narrower than its budget — does not scroll and does
// not report activity, so a scene that fits arms no ticker (ADR-0005: the ticker
// runs only while a visible node animates). This is the control that keeps the
// test above from passing on a renderer that windows everything unconditionally.
func TestAShortMarqueeDoesNotScrollOrReportActivity(t *testing.T) {
	doc := scrollDoc(t, 2, "")
	state := fold.Fold(nil)
	state.ThinkingText = "short" // 5 columns, well under the 40 budget

	r := Renderer{Width: 40, Height: 1, AnimTicks: map[string]int{"mq": 7}}
	frame, active := r.RenderFrameActive(doc, state)
	if got := strings.TrimRight(frame.Plain(), " "); got != "short" {
		t.Errorf("a marquee that fits its budget was windowed anyway: %q; a marquee with nothing to\n"+
			"scroll must draw its content whole regardless of the tick count", got)
	}
	if len(active) != 0 {
		t.Errorf("a marquee that fits reported %d active animation(s); it is not moving, so it must\n"+
			"report none — otherwise the loop keeps a ticker running to repaint a frame that never\n"+
			"changes (ADR-0005).", len(active))
	}
}

// A scrolling marquee reports itself active so the loop keeps the clock running,
// and carries pause_when's current truthiness so the loop can freeze the offset
// without the renderer owning a pause policy (ADR-0005 / G-B).
func TestAScrollingMarqueeReportsActivityAndPauseState(t *testing.T) {
	doc := scrollDoc(t, 2, "agent.working")

	t.Run("running when pause bind is false", func(t *testing.T) {
		state := scrollState() // agent.working defaults false
		if evalWhen("agent.working", state) {
			t.Fatalf("premise broken: agent.working must be false in this state, or the pause\n" +
				"assertion below is inverted")
		}
		r := Renderer{Width: 10, Height: 1, AnimTicks: map[string]int{"mq": 1}}
		_, active := r.RenderFrameActive(doc, state)
		if len(active) != 1 || active[0].NodeID != "mq" {
			t.Fatalf("a scrolling marquee must report exactly itself active; got %+v", active)
		}
		if active[0].Paused {
			t.Errorf("the marquee reports paused while agent.working is false; pause_when only holds\n" +
				"the offset while its bind is truthy")
		}
	})

	t.Run("paused when pause bind is true", func(t *testing.T) {
		state := scrollState()
		state.AgentWorking = true
		if !evalWhen("agent.working", state) {
			t.Fatalf("premise broken: agent.working must be true here")
		}
		r := Renderer{Width: 10, Height: 1, AnimTicks: map[string]int{"mq": 1}}
		_, active := r.RenderFrameActive(doc, state)
		if len(active) != 1 {
			t.Fatalf("a scrolling marquee must report active whether paused or not (the loop needs to\n"+
				"know it is present); got %+v", active)
		}
		if !active[0].Paused {
			t.Errorf("the marquee does not report paused while agent.working is true; the loop would\n" +
				"keep advancing the offset and pause_when would do nothing")
		}
	})
}
