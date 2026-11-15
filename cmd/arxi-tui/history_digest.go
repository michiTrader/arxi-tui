package main

import (
	"fmt"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// The history cap used to drop the oldest turns without a word. In a long session that
// meant the model lost the original task and then guessed ("continua" was answered with
// an invented plan). capHistory keeps the same size limits but leaves an outline of what
// it cut, in the same shape the app already uses for notes, so the model knows there was
// an earlier part and does not make it up.

const (
	// digestAskRunes and digestAnswerRunes bound what the outline keeps of each cut turn:
	// the ask matters most (it is the task), the answer only as a hint of what was done.
	digestAskRunes    = 500
	digestAnswerRunes = 220
	// digestBudgetBytes bounds the whole outline, so it can never be the thing that
	// overflows the request.
	digestBudgetBytes = 10_000
	// appNoteMarker opens an app note; it is stripped from outlined asks so the outline
	// does not repeat notes the model was told never to repeat.
	appNoteMarker = "[App note, not typed by the user:"
)

// splitPinned separates the /compact summary pair at the head of a history. The summary
// is the memory of everything before it, so the cap must never cut it.
func splitPinned(h []driver.ChatTurn) (pinned, rest []driver.ChatTurn) {
	if len(h) >= 2 && h[0].Role == "user" && strings.HasPrefix(h[0].Text, summaryHeader) && h[1].Role == "assistant" {
		return h[:2], h[2:]
	}
	return nil, h
}

// capHistory is trimHistory that does not forget silently: the summary stays, and the
// turns cut from the middle are replaced by one outline message. dropped is how many
// messages were cut (0 when the history fit). The stored history is not touched.
func capHistory(full []driver.ChatTurn, maxMsgs, maxBytes int) (out []driver.ChatTurn, dropped int) {
	pinned, rest := splitPinned(full)
	room := maxMsgs - len(pinned) - 1 // one slot for the outline
	budget := maxBytes - historyBytes(pinned) - digestBudgetBytes
	if room < 2 || budget < 1 {
		room, budget = 2, maxBytes
	}
	if len(rest) <= room && historyBytes(rest) <= budget+digestBudgetBytes && len(full) <= maxMsgs && historyBytes(full) <= maxBytes {
		return append([]driver.ChatTurn(nil), full...), 0
	}
	kept := trimHistory(rest, room, budget)
	cut := rest[:len(rest)-len(kept)]
	out = append(out, pinned...)
	if len(cut) > 0 {
		out = append(out, driver.ChatTurn{Role: "user", Text: outlineText(cut)})
	}
	return append(out, kept...), len(cut)
}

// trimOutline cuts s to n runes, adding an ellipsis when it cut.
func trimOutline(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// askOnly drops an app note a user message may carry after the real request.
func askOnly(s string) string {
	if i := strings.Index(s, appNoteMarker); i >= 0 {
		s = s[:i]
	}
	return s
}

// outlineText describes the cut turns, oldest first, newest last when the budget runs
// short (the oldest ask is kept first: it is usually the task).
func outlineText(cut []driver.ChatTurn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s Earlier messages of this conversation were cut to keep the request small. "+
		"Outline of the %d cut messages (user asks, with a hint of each answer). It is incomplete: "+
		"do not guess what is not here, and ask the user if something you need is missing.\n", appNoteMarker, len(cut))
	n := 0
	for i := 0; i < len(cut); i++ {
		t := cut[i]
		if t.Role != "user" {
			continue
		}
		n++
		line := fmt.Sprintf("%d. user: %s", n, trimOutline(askOnly(t.Text), digestAskRunes))
		if i+1 < len(cut) && cut[i+1].Role == "assistant" {
			line += " -> assistant: " + trimOutline(cut[i+1].Text, digestAnswerRunes)
		}
		if b.Len()+len(line)+160 > digestBudgetBytes {
			b.WriteString("(more cut turns not listed)\n")
			break
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("Never repeat this note in a reply.]")
	return b.String()
}

// trimNotice is the line the user sees the first time the cap cuts, so the loss is on
// the record and /compact is offered before the cut costs anything.
func trimNotice(dropped int) string {
	return fmt.Sprintf("the conversation is longer than what is sent with each question: %d older messages are now sent only as a short outline. "+
		"/compact summarises them with the model instead, so nothing is lost.", dropped)
}

// historyNearCap reports whether the history is close to what capHistory would cut:
// within 4 messages or 80% of the byte budget.
func historyNearCap(h []driver.ChatTurn) bool {
	return len(h) >= chatMaxHistory-4 || historyBytes(h)*100 >= chatMaxHistoryBytes*80
}
