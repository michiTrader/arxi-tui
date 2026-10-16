package engine

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/fold"
)

// The info rows used to be cut by the terminal at the last column, in the middle of a word
// ("arxi-tui" -> "arxi-ti"), with nothing saying a letter was lost. A row too wide now either
// wraps its words onto the next row (the chat's way) or drops whole segments; it never cuts a
// word, and it never draws wider than the frame.

const narrowScene = `{"root":{"type":"stack","children":[
 {"id":"where","type":"row","children":[
  {"type":"text","bind":"host.cwd"},
  {"type":"text","text":" · ","when":"host.branch"},
  {"type":"text","bind":"host.branch","when":"host.branch"},
  {"type":"text","text":" · ","when":"host.scroll"},
  {"type":"text","bind":"host.scroll","when":"host.scroll"}]}]}}`

var narrowState = fold.State{HostCwd: "~/projects/arxi-tui", HostBranch: "master", HostScroll: "scrolled up"}

func narrowRows(t *testing.T, mode string, width int) []string {
	t.Helper()
	r := &Renderer{Width: width, Height: 10, Narrow: mode}
	f := r.RenderFrame(mustDoc(t, narrowScene), narrowState)
	if o := f.Overflow(); len(o) > 0 {
		t.Fatalf("mode %q width %d: rows %v are wider than the frame", mode, width, o)
	}
	var out []string
	for _, l := range f.Live {
		out = append(out, strings.TrimRight(l.Text(), " "))
	}
	return out
}

func TestARowThatFitsIsDrawnTheSameInEveryMode(t *testing.T) {
	want := "~/projects/arxi-tui · master · scrolled up"
	for _, mode := range []string{"", NarrowWrap, NarrowHide} {
		got := narrowRows(t, mode, 60)
		if len(got) != 1 || got[0] != want {
			t.Errorf("mode %q: %q, want one row %q", mode, got, want)
		}
	}
}

func TestAWideRowWrapsItsWordsOntoTheNextRowAndLosesNone(t *testing.T) {
	const whole = "~/projects/arxi-tui · master · scrolled up"
	longest := len("~/projects/arxi-tui")
	for _, mode := range []string{NarrowWrap} {
		for width := 40; width >= 6; width-- {
			rows := narrowRows(t, mode, width)
			if len(rows) < 2 {
				t.Fatalf("mode %q width %d: the row did not wrap: %q", mode, width, rows)
			}
			if width >= longest {
				// Room for every word: they move whole, none is cut.
				if strings.Join(strings.Fields(strings.Join(rows, " ")), " ") != whole {
					t.Errorf("mode %q width %d lost or cut a word: %q", mode, width, rows)
				}
				continue
			}
			// A word wider than the whole terminal has to go on over the next row (as it
			// does in the chat); every letter is still on screen, in order.
			if strings.ReplaceAll(strings.Join(rows, ""), " ", "") != strings.ReplaceAll(whole, " ", "") {
				t.Errorf("mode %q width %d lost a letter: %q", mode, width, rows)
			}
		}
	}
}

// 'arxi-tui' is exactly what the owner saw cut to 'arxi-ti' in a narrowing PowerShell.
func TestTheFolderNameIsNeverCutShortInEitherMode(t *testing.T) {
	for _, mode := range []string{NarrowWrap, NarrowHide} {
		for width := 24; width >= 20; width-- {
			text := strings.Join(narrowRows(t, mode, width), "\n")
			if strings.Contains(text, "arxi-ti") && !strings.Contains(text, "arxi-tui") {
				t.Errorf("mode %q width %d cut the name:\n%s", mode, width, text)
			}
		}
	}
}

func TestHideDropsWholeSegmentsFromTheRightAndNeverLeavesASeparatorDangling(t *testing.T) {
	cases := map[int]string{
		60: "~/projects/arxi-tui · master · scrolled up",
		40: "~/projects/arxi-tui · master",
		28: "~/projects/arxi-tui · master",
		27: "~/projects/arxi-tui",
		19: "~/projects/arxi-tui",
		18: "",
		6:  "",
	}
	for width, want := range cases {
		rows := narrowRows(t, NarrowHide, width)
		if len(rows) != 1 || rows[0] != want {
			t.Errorf("width %d: %q, want one row %q", width, rows, want)
		}
		if strings.HasSuffix(strings.TrimSpace(strings.Join(rows, "")), "·") {
			t.Errorf("width %d: a separator is left dangling: %q", width, rows)
		}
	}
}

// A row with a weighted child divides the width it has; the narrow mode leaves it alone.
func TestAWeightedRowIsNotTouchedByTheNarrowMode(t *testing.T) {
	js := `{"root":{"type":"row","children":[
	 {"type":"text","text":"aaaaaaaaaa","weight":1},{"type":"text","text":"bbbbbbbbbb","weight":1}]}}`
	r := &Renderer{Width: 12, Height: 5, Narrow: NarrowHide}
	f := r.RenderFrame(mustDoc(t, js), fold.State{})
	// The author's columns stay the author's: two equal shares of the width, as before.
	if len(f.Live) != 1 || f.Live[0].Text() != "aaaaaabbbbbb" {
		t.Errorf("a weighted row changed: %q", f.Live)
	}
	for _, mode := range []string{NarrowWrap} {
		r := &Renderer{Width: 12, Height: 5, Narrow: mode}
		if f := r.RenderFrame(mustDoc(t, js), fold.State{}); len(f.Live) != 1 || f.Live[0].Text() != "aaaaaabbbbbb" {
			t.Errorf("mode %q changed a weighted row: %q", mode, f.Live)
		}
	}
}

// A column that is itself several rows (a menu beside a text) is not treated as running text.
func TestARowOfSeveralLineColumnsIsNotFlattened(t *testing.T) {
	js := `{"root":{"type":"row","children":[
	 {"type":"text","text":"one\ntwo"},{"type":"text","text":"xxxxxxxxxxxxxxxxxxxx"}]}}`
	r := &Renderer{Width: 10, Height: 5, Narrow: NarrowWrap}
	f := r.RenderFrame(mustDoc(t, js), fold.State{})
	if len(f.Live) != 2 || !strings.HasPrefix(f.Live[0].Text(), "one") || !strings.HasPrefix(f.Live[1].Text(), "two") {
		t.Errorf("a two-row column was merged into running text: %q", f.Live)
	}
}

func TestTheWrappedRowKeepsEachSegmentsStyle(t *testing.T) {
	js := `{"root":{"type":"row","children":[
	 {"type":"text","text":"alpha beta","style":{"style":"dim"}},{"type":"text","text":" gamma delta","style":{"style":"header"}}]}}`
	r := &Renderer{Width: 12, Height: 5, Narrow: NarrowWrap}
	f := r.RenderFrame(mustDoc(t, js), fold.State{})
	if len(f.Live) < 2 {
		t.Fatalf("did not wrap: %q", f.Live)
	}
	styles := map[string]bool{}
	for _, l := range f.Live {
		for _, s := range l {
			if strings.TrimSpace(s.Text) != "" {
				styles[s.Style] = true
			}
		}
	}
	if !styles["dim"] || !styles["header"] {
		t.Errorf("the wrap flattened the styles: %v", styles)
	}
}

// A lone text in a stack (a notice, the "press ctrl+c again" hint) obeys the same rule.
func TestALoneWideTextWrapsOrDisappearsButIsNeverCut(t *testing.T) {
	js := `{"root":{"type":"stack","children":[{"type":"text","text":"press ctrl+c again to exit"}]}}`
	r := &Renderer{Width: 24, Height: 5, Narrow: NarrowWrap}
	f := r.RenderFrame(mustDoc(t, js), fold.State{})
	if o := f.Overflow(); len(o) > 0 {
		t.Fatalf("wrap: rows %v too wide", o)
	}
	var rows []string
	for _, l := range f.Live {
		rows = append(rows, strings.TrimSpace(l.Text()))
	}
	if strings.Join(rows, " ") != "press ctrl+c again to exit" {
		t.Errorf("wrap lost or cut a word: %q", rows)
	}

	r = &Renderer{Width: 24, Height: 5, Narrow: NarrowHide}
	f = r.RenderFrame(mustDoc(t, js), fold.State{})
	if o := f.Overflow(); len(o) > 0 {
		t.Fatalf("hide: rows %v too wide", o)
	}
	if got := strings.TrimSpace(f.Plain()); got != "" {
		t.Errorf("hide left part of the text on screen: %q", got)
	}

	r = &Renderer{Width: 40, Height: 5, Narrow: NarrowHide}
	if got := strings.TrimSpace(r.RenderFrame(mustDoc(t, js), fold.State{}).Plain()); got != "press ctrl+c again to exit" {
		t.Errorf("a text that fits was changed: %q", got)
	}
}

// Every row the wrap makes fills the frame's width, like any other row of the frame.
func TestWrappedRowsAreAsWideAsTheFrame(t *testing.T) {
	r := &Renderer{Width: 20, Height: 10, Narrow: NarrowWrap}
	f := r.RenderFrame(mustDoc(t, narrowScene), narrowState)
	if len(f.Live) < 2 {
		t.Fatalf("did not wrap: %q", f.Live)
	}
	for i, l := range f.Live {
		if l.Width() != 20 {
			t.Errorf("row %d is %d wide, want 20: %q", i, l.Width(), l.Text())
		}
	}
}

// The mode reaches a row that sits inside a box (the renderer hands a child renderer down).
func TestTheModeReachesARowInsideABox(t *testing.T) {
	js := `{"root":{"type":"box","border":"round","children":[
	 {"type":"row","children":[{"type":"text","text":"alpha "},{"type":"text","text":"beta "},{"type":"text","text":"gamma delta epsilon"}]}]}}`
	hide := &Renderer{Width: 20, Height: 8, Narrow: NarrowHide}
	got := hide.RenderFrame(mustDoc(t, js), fold.State{}).Plain()
	if strings.Contains(got, "gamma") || strings.Contains(got, "epsilon") || !strings.Contains(got, "alpha") {
		t.Errorf("hide did not reach the row in the box:\n%s", got)
	}
	wrap := &Renderer{Width: 20, Height: 8, Narrow: NarrowWrap}
	got = wrap.RenderFrame(mustDoc(t, js), fold.State{}).Plain()
	if !strings.Contains(got, "epsilon") || !strings.Contains(got, "gamma") {
		t.Errorf("wrap lost words in the box:\n%s", got)
	}
}

// The zero value leaves rows exactly as the author wrote them: previews and the golden
// frames render with no policy, and only the live loop picks one.
func TestWithNoNarrowPolicyARowIsLeftAlone(t *testing.T) {
	r := &Renderer{Width: 20, Height: 5}
	f := r.RenderFrame(mustDoc(t, narrowScene), narrowState)
	if len(f.Live) != 1 {
		t.Errorf("a row with no policy was rewritten into %d rows: %q", len(f.Live), f.Live)
	}
}
