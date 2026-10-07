package engine

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const wrapQuestion = "una pregunta, cuando hablo con la IA en arxi-tui hablo con que agente o con cual parte del team? hay blueprint por defecto?"

// The reported bug: "defecto" was cut in the middle at the edge of the row. The
// word has to move down whole.
func TestInputWrapKeepsAWordWhole(t *testing.T) {
	idx := strings.Index(wrapQuestion, "defecto")
	// A row that ends two letters into "defecto" cut it as "de|fecto".
	room := idx + 2
	rows := inputVisualRows(wrapQuestion, room)
	for _, r := range rows {
		if strings.HasSuffix(r, "de") || strings.HasPrefix(r, "fecto") {
			t.Fatalf("a word was cut in the middle: %q", rows)
		}
	}
	if last := rows[len(rows)-1]; last != "defecto?" {
		t.Fatalf("the word must start the next row, rows = %q", rows)
	}
	if !strings.HasSuffix(rows[len(rows)-2], "por ") && !strings.HasSuffix(rows[len(rows)-2], "por") {
		t.Fatalf("the row before must end after 'por': %q", rows)
	}
	for _, r := range rows {
		if ansi.StringWidth(r) > room {
			t.Fatalf("row %q is wider than %d", r, room)
		}
	}
}

func TestInputWrapOnlyCutsAWordWiderThanTheRow(t *testing.T) {
	rows := inputVisualRows("ab "+strings.Repeat("x", 25), 10)
	want := []string{"ab ", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", rows, want)
	}
}

func TestInputWrapSpaceAtTheEdgeHangsWithoutTakingACell(t *testing.T) {
	rows := inputVisualRows("abcde fghij", 5)
	if strings.Join(rows, "|") != "abcde|fghij|" {
		t.Fatalf("rows = %q", rows)
	}
	if r, c := inputCaretRowCol("abcde fghij", 6, 5); r != 1 || c != 0 {
		t.Fatalf("caret before 'f' = (%d,%d), want (1,0)", r, c)
	}
}

// The caret and the drawn rows come from one layout, so for any text the cell a
// caret reports holds the rune that follows it.
func TestInputCaretAlwaysSitsOnTheRuneItPrecedes(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabet := []rune("ab cd  efg\nhij界 klmnop")
	for n := 0; n < 400; n++ {
		var b strings.Builder
		for i, l := 0, rng.Intn(60); i < l; i++ {
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		text, room := b.String(), 2+rng.Intn(14)
		rows := inputVisualRows(text, room)
		rs := []rune(text)
		for i, r := range rs {
			if r == ' ' || r == '\n' {
				continue
			}
			row, col := inputCaretRowCol(text, i, room)
			if row >= len(rows) {
				t.Fatalf("%q room %d: caret %d on row %d of %d", text, room, i, row, len(rows))
			}
			tail := ansi.Cut(rows[row], col, ansi.StringWidth(rows[row]))
			if !strings.HasPrefix(tail, string(r)) {
				t.Fatalf("%q room %d: caret %d at (%d,%d) is not on %q; rows = %q", text, room, i, row, col, string(r), rows)
			}
		}
		for _, r := range rows {
			if ansi.StringWidth(r) > room {
				t.Fatalf("%q room %d: row %q too wide", text, room, r)
			}
		}
		// Nothing is lost: the visible text is the input minus hanging spaces and newlines.
		joined := strings.Join(rows, "")
		if strings.ReplaceAll(strings.ReplaceAll(joined, " ", ""), "\n", "") != strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), "\n", "") {
			t.Fatalf("%q room %d lost characters: %q", text, room, rows)
		}
	}
}

func TestInputVerticalMoveFollowsWordWrappedRows(t *testing.T) {
	text := "hello wonderful world"
	// room 10: "hello " / "wonderful " / "world"
	if rows := inputVisualRows(text, 10); strings.Join(rows, "|") != "hello |wonderful |world" {
		t.Fatalf("rows = %q", rows)
	}
	down := InputCaretVerticalMove(text, 2, 10, +1)
	if r, c := inputCaretRowCol(text, down, 10); r != 1 || c != 2 {
		t.Fatalf("down landed at (%d,%d)", r, c)
	}
	if up := InputCaretVerticalMove(text, down, 10, -1); up != 2 {
		t.Fatalf("up = %d, want 2", up)
	}
}
