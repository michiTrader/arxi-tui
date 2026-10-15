package main

import "github.com/michiTrader/arxi_tui/internal/fold"

// addEvent puts one event at the end of the log the loop folds on every keystroke.
//
// The log is folded from the start each frame, so its length is the cost of pressing a
// key. A model that thinks streams hundreds of small chat.thinking fragments per answer,
// each only a step in the one-line marquee, and kept one by one they made a long
// conversation slower with every turn (125 ms a key after 40 turns, in the measurement
// in lag_test.go). Saved conversations never held them, which is why /resume made the
// lag go away. So a fragment that follows another is merged into it: the merged text is
// what the fold would have built from the two, bounded to the marquee's own size.
func addEvent(log []fold.Event, e fold.Event) []fold.Event {
	if e.Type != "chat.thinking" || len(log) == 0 || log[len(log)-1].Type != "chat.thinking" {
		return append(log, e)
	}
	frag, _ := e.Payload["text"].(string)
	if frag == "" {
		return log
	}
	last := log[len(log)-1]
	prev, _ := last.Payload["text"].(string)
	merged := e
	merged.Payload = map[string]any{"text": fold.AppendThinking(prev, frag)}
	log[len(log)-1] = merged
	return log
}

// logLenProbe is set only by a test, to see how long the loop's log is after each event.
var logLenProbe func(n int)
