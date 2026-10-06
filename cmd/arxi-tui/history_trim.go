package main

import "github.com/michiTrader/arxi_tui/internal/driver"

// trimHistory returns a copy of hist holding only its most recent turns: at most
// maxMsgs of them and at most maxBytes of text. Older turns are dropped whole, never
// cut in the middle, and the newest turn always survives even if it alone is over the
// byte budget (dropping it would make the model answer a question it cannot see).
// The kept window never starts on an assistant turn, since a reply with no question
// before it is rejected by some providers and confuses the rest.
func trimHistory(hist []driver.ChatTurn, maxMsgs, maxBytes int) []driver.ChatTurn {
	start := len(hist)
	total := 0
	for start > 0 {
		t := hist[start-1]
		if len(hist)-start >= maxMsgs {
			break
		}
		if start < len(hist) && total+len(t.Text) > maxBytes {
			break
		}
		total += len(t.Text)
		start--
	}
	for start < len(hist) && hist[start].Role == "assistant" {
		start++
	}
	return append([]driver.ChatTurn(nil), hist[start:]...)
}
