//go:build !windows

package main

import (
	"context"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/defaultscene"
	"github.com/michiTrader/arxi_tui/internal/engine"
	"github.com/michiTrader/arxi_tui/internal/fold"
	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// A conversation of n turns, each with `think` thinking fragments and `tools` tool calls,
// logged the way the loop does it.
func lagLog(n, think, tools int) []fold.Event {
	var log []fold.Event
	add := func(typ string, p map[string]any) { log = addEvent(log, fold.Event{Type: typ, Payload: p}) }
	for i := 0; i < n; i++ {
		add("run.prompt", map[string]any{"text": "pregunta " + strings.Repeat("x", 80)})
		for j := 0; j < think; j++ {
			add("chat.thinking", map[string]any{"text": "pensando un fragmento corto de razonamiento "})
		}
		for j := 0; j < tools; j++ {
			add("chat.tool", map[string]any{"name": "read", "arg": "a.go", "ok": true, "summary": "Read 300 lines", "output": strings.Repeat("l\n", 300)})
		}
		add("llm.response", map[string]any{"text": strings.Repeat("respuesta con **markdown** y `codigo` ", 60), "model": "p/m", "context_tokens": float64(12000)})
		add("agent.turn_done", nil)
	}
	return log
}

// The cause of the reported lag: the log must not grow with the amount a model thinks.
func TestThinkingFragmentsDoNotAccumulateInTheLog(t *testing.T) {
	few, many := lagLog(10, 1, 3), lagLog(10, 2000, 3)
	if len(many) != len(few) {
		t.Fatalf("2000 thinking fragments per turn left %d events, one fragment per turn left %d: the log grows with the thinking, and so does the cost of every key", len(many), len(few))
	}
}

// Merging must not change what the marquee shows. Random fragments, with the newlines and
// half words real reasoning has, are folded one by one and merged, and must agree.
func TestMergedThinkingShowsWhatTheFragmentsWould(t *testing.T) {
	words := []string{"hola", "mundo", "razon", "ando", "\n", "  ", "la", "solución", "es", "x", "\n\n", "pi", "eza"}
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 200; trial++ {
		var raw, merged []fold.Event
		for i, n := 0, 1+rng.Intn(400); i < n; i++ {
			var b strings.Builder
			for k, m := 0, 1+rng.Intn(4); k < m; k++ {
				b.WriteString(words[rng.Intn(len(words))])
				if rng.Intn(2) == 0 {
					b.WriteByte(' ')
				}
			}
			e := fold.Event{Type: "chat.thinking", Payload: map[string]any{"text": b.String()}}
			raw = append(raw, e)
			merged = addEvent(merged, e)
		}
		if len(merged) != 1 {
			t.Fatalf("trial %d: %d events after merging, want 1", trial, len(merged))
		}
		if a, b := fold.Fold(raw).ThinkingText, fold.Fold(merged).ThinkingText; a != b {
			t.Fatalf("trial %d: the marquee shows %q merged but %q fragment by fragment", trial, b, a)
		}
	}
}

// The real loop keeps the log short too: this is what stops the lag in the program, not
// only in the helper. A model thinking 3000 fragments leaves one event in the loop's log.
func TestTheLoopKeepsOneEventForAWholeThought(t *testing.T) {
	var mu sync.Mutex
	longest := 0
	logLenProbe = func(n int) {
		mu.Lock()
		if n > longest {
			longest = n
		}
		mu.Unlock()
	}
	defer func() { logLenProbe = nil }()
	drv := &testDriver{evCh: make(chan fold.Event, 4096), seq: 1000}
	drv.evCh <- fold.Event{Type: "run.prompt", Seq: 1, Payload: map[string]any{"text": "piensa"}}
	for i := 0; i < 3000; i++ {
		drv.evCh <- fold.Event{Type: "chat.thinking", Seq: int64(2 + i), Payload: map[string]any{"text": "razonando "}}
	}
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	tty := newFakeTTY(100, 30, exitAfter(800))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := loop(ctx, tty, doc, theme.SOBRIA(), drv.evCh, drv, ""); err != nil {
		t.Fatalf("loop: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if longest != 2 { // the question and the one merged thought
		t.Errorf("the loop's log reached %d events for a question and 3000 thinking fragments, want 2", longest)
	}
}

// Only fragments that follow each other are merged: what separates two thinkings (a tool
// call that clears the marquee) keeps them apart, and no other event is ever merged.
func TestOnlyConsecutiveThinkingIsMerged(t *testing.T) {
	var log []fold.Event
	add := func(typ, text string) {
		log = addEvent(log, fold.Event{Type: typ, Payload: map[string]any{"text": text, "name": "read"}})
	}
	add("chat.thinking", "primero ")
	add("chat.thinking", "segundo")
	add("chat.tool", "")
	add("chat.thinking", "tercero")
	add("chat.warn", "aviso")
	add("chat.warn", "aviso")
	// A fragment with nothing in it must not disturb the one before it: folded fragment by
	// fragment it is ignored, and the space it follows still separates the next word.
	var withSpace []fold.Event
	for _, f := range []string{"uno ", "", "dos"} {
		withSpace = addEvent(withSpace, fold.Event{Type: "chat.thinking", Payload: map[string]any{"text": f}})
	}
	if got := fold.Fold(withSpace).ThinkingText; got != "uno dos" {
		t.Errorf("an empty fragment ate the space between words: %q, want \"uno dos\"", got)
	}
	var kinds []string
	for _, e := range log {
		kinds = append(kinds, e.Type)
	}
	if got := strings.Join(kinds, ","); got != "chat.thinking,chat.tool,chat.thinking,chat.warn,chat.warn" {
		t.Errorf("events = %s", got)
	}
	if st := fold.Fold(log[:1]); st.ThinkingText != "primero segundo" {
		t.Errorf("merged text = %q", st.ThinkingText)
	}
	// An empty fragment adds nothing, and does not disturb the last event.
	n := len(log)
	log = addEvent(log, fold.Event{Type: "chat.warn"})
	log = addEvent(log, fold.Event{Type: "chat.thinking", Payload: map[string]any{"text": ""}})
	if len(log) != n+2 {
		t.Errorf("an empty thinking fragment after a warning changed the log: %d events, want %d", len(log), n+2)
	}
}

// The lag itself, measured the way a key press costs it: fold the log and draw a frame.
// A conversation that thought a lot must cost about what one that did not think does.
func TestAKeyOnALongThoughtfulConversationCostsAboutAsMuchAsOnAQuietOne(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	doc, err := scene.ParseNamed(defaultscene.Name, defaultscene.JSON)
	if err != nil {
		t.Fatal(err)
	}
	cost := func(log []fold.Event) time.Duration {
		best := time.Hour
		for i := 0; i < 5; i++ {
			start := time.Now()
			st := fold.Fold(log)
			st.UserInput = "hola"
			r := engine.Renderer{Width: 120, Height: 40, PromptStyle: "band", Look: chatLook(), Scrollbar: true}
			_ = r.RenderFrame(doc, st)
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	quiet, thoughtful := cost(lagLog(40, 0, 6)), cost(lagLog(40, 200, 6))
	t.Logf("a key costs %v on a quiet conversation and %v on one that thought 200 fragments a turn", quiet, thoughtful)
	if thoughtful > quiet*2+5*time.Millisecond {
		t.Errorf("thinking made every key %v instead of %v", thoughtful, quiet)
	}
}
