package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
)

// The render half of G3: a text node with reveal draws a growing prefix of its
// content as the host clock's phase rises (SCENES.md Scene 4, G-B — the
// character-count axis), and the frame is a pure function of that phase
// (ADR-0005) so these can pin it at the start, mid-reveal and settled. The
// content is ASCII so display width equals length and the expected prefixes are
// written by hand; grapheme-safe cutting is ansi.Cut's contract, exercised
// elsewhere.
//
// Counterfactual, run rather than argued: reverting revealPrefix so it returns
// the whole text regardless of phase leaves the settled and nil cases green and
// fails the start (phase 0) and mid cases — exactly the phase→prefix mapping
// this pins. A version that read a fixed truncation instead of AnimPhase[id]
// fails the mid case the same way.

const revealContent = "HELLOWORLD" // 10 columns, no space so a prefix has no ambiguous trailing pad

func revealDoc(t *testing.T) *scene.Document {
	t.Helper()
	body := `{ "root": { "id": "rv", "type": "text", "text": "HELLOWORLD",
	  "reveal": { "anim": "reveal.fast" } } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: reveal doc must parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: reveal doc must validate; got %v", verr)
	}
	return doc
}

// The prefix grows with the phase: phase 0 shows nothing, a mid phase shows a
// width-proportional prefix cut at a grapheme boundary, and phase 1 shows the
// whole string. The phase is host-computed and eased before it arrives, so this
// asserts only the character-count mapping, not the easing.
func TestRevealShowsAGrowingPrefixWithThePhase(t *testing.T) {
	doc := revealDoc(t)
	state := fold.Fold(nil)

	cases := []struct {
		phase float64
		want  string
	}{
		{phase: 0.0, want: ""},           // the start of the reveal
		{phase: 0.5, want: "HELLO"},      // round(0.5*10) = 5 columns
		{phase: 1.0, want: "HELLOWORLD"}, // settled: the whole string
	}
	for _, tc := range cases {
		r := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{"rv": tc.phase}}
		got := strings.TrimRight(r.RenderFrame(doc, state).Plain(), " ")
		if got != tc.want {
			t.Errorf("phase %v: prefix is %q, want %q\n"+
				"consequence: the reveal's visible length is not being taken from the host clock's\n"+
				"phase, so either the phase is ignored (the whole string draws) or the character-count\n"+
				"math is wrong. Either way the typewriter does not grow as ADR-0005/G-B sign it.\n"+
				"remedy: renderText must clip the content to round(phase*width) columns via ansi.Cut.",
				tc.phase, got, tc.want)
		}
	}
}

// A nil AnimPhase is the pure/golden path with no clock, and a reveal there
// draws its whole text — the no-op guarantee that keeps a document with a reveal
// rendering fully when nothing is driving time, so no non-motion golden moves. A
// non-nil map that has no entry for the node is a different state: its first
// appearance in the live loop, the start of the reveal, phase 0. The map's own
// nil-ness is the only thing that distinguishes them, so this pins both.
func TestRevealNilPhaseIsSettledButAbsentEntryIsTheStart(t *testing.T) {
	doc := revealDoc(t)
	state := fold.Fold(nil)

	// nil map: no clock, settled, whole string.
	settled := Renderer{Width: 40, Height: 1}
	if got := strings.TrimRight(settled.RenderFrame(doc, state).Plain(), " "); got != revealContent {
		t.Errorf("a reveal rendered with no clock (nil AnimPhase) drew %q, want the whole %q; a\n"+
			"document with a reveal must render fully when nothing is animating it, or every\n"+
			"non-motion golden would move", got, revealContent)
	}

	// non-nil map, no entry for "rv": first appearance, phase 0, nothing shown.
	started := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{}}
	if got := strings.TrimRight(started.RenderFrame(doc, state).Plain(), " "); got != "" {
		t.Errorf("a reveal on its first live-loop frame (non-nil map, no entry) drew %q, want nothing;\n"+
			"the clock has not seen it yet, which is the start of the reveal, not the settled end —\n"+
			"the two absent states are told apart by whether the map itself is nil", got)
	}
}

// A revealing node reports itself active with its one-shot marker and token, so
// the loop drives the clock and resolves the token's timing against the theme.
// A reveal whose content is empty reports nothing: there is no prefix to grow
// through, so arming a ticker for it would repaint a frame that never changes.
func TestRevealReportsOneShotActivity(t *testing.T) {
	doc := revealDoc(t)
	state := fold.Fold(nil)

	r := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{"rv": 0.3}}
	_, active := r.RenderFrameActive(doc, state)
	if len(active) != 1 || active[0].NodeID != "rv" {
		t.Fatalf("a revealing text node must report exactly itself active; got %+v", active)
	}
	if !active[0].OneShot {
		t.Errorf("the reveal does not report OneShot; the loop would drive it as a continuous scroll\n" +
			"(a tick count) instead of a phase that settles")
	}
	if active[0].Token != "reveal.fast" {
		t.Errorf("the reveal reports token %q, want %q; the loop resolves the token's duration and\n"+
			"curve by this name", active[0].Token, "reveal.fast")
	}

	// Empty content: nothing to reveal, nothing to animate.
	emptyBody := `{ "root": { "id": "rv", "type": "text", "bind": "model.name",
	  "reveal": { "anim": "reveal.fast" } } }`
	emptyDoc, err := scene.ParseDocument([]byte(emptyBody))
	if err != nil {
		t.Fatalf("premise broken: empty reveal doc must parse; got %v", err)
	}
	empty := Renderer{Width: 40, Height: 1, AnimPhase: map[string]float64{}}
	_, emptyActive := empty.RenderFrameActive(emptyDoc, fold.Fold(nil))
	if len(emptyActive) != 0 {
		t.Errorf("a reveal with empty content reported %d active animation(s); it has no prefix to\n"+
			"grow through, so it must report none or the loop keeps a ticker running for a frame\n"+
			"that never changes", len(emptyActive))
	}
}

// When the revealed text grows wider than the pane, the reveal scrolls
// horizontally to keep the writing edge on screen: the visible line is the last
// r.Width columns of the revealed prefix, so the newest graphemes are always
// shown instead of overflowing a single-line node and churning the last column
// (which, with auto-wrap off, is all the reader would see). This is the reported
// fix — before it, a long reveal only animated its final cell and the rest had
// to be read by widening the terminal.
//
// Counterfactual, run rather than argued: dropping the window in renderText
// leaves the visible line at the full revealed width (38 > 10 here) and keeps
// the head ("HEAD") on screen while the edge ("EDGE") overflows off the right —
// so both assertions below flip.
func TestRevealScrollsToFollowTheWritingEdge(t *testing.T) {
	// A distinctive head, a run of filler, and a distinctive writing edge.
	const content = "HEAD" + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" + "EDGE" // 4 + 30 + 4 = 38 columns
	body := `{ "root": { "id": "rv", "type": "text", "text": "` + content + `",
	  "reveal": { "anim": "reveal.fast" } } }`
	doc, err := scene.ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: reveal doc must parse; got %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Fatalf("premise broken: reveal doc must validate; got %v", verr)
	}

	const width = 10
	// Phase 1.0: the whole 38-column string is revealed, well past the 10-column
	// pane, so the window must show the tail.
	r := Renderer{Width: width, Height: 1, AnimPhase: map[string]float64{"rv": 1.0}}
	got := strings.TrimRight(r.RenderFrame(doc, fold.Fold(nil)).Plain(), " ")

	if len([]rune(got)) > width {
		t.Errorf("the revealed line is %d columns wide, wider than the %d-column pane; the reveal is\n"+
			"not being windowed, so it overflows the single-line node and the last cell churns.\n"+
			"line: %q", len([]rune(got)), width, got)
	}
	if !strings.Contains(got, "EDGE") {
		t.Errorf("the writing edge %q is not on screen; the reveal must scroll to follow the newest\n"+
			"graphemes, not freeze on the head. line: %q", "EDGE", got)
	}
	if strings.Contains(got, "HEAD") {
		t.Errorf("the head %q is still on screen at width %d; a followed edge means the start has\n"+
			"scrolled off. line: %q", "HEAD", width, got)
	}
}
