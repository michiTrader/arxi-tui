package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// This file is /compact: replacing the older part of the conversation with a summary,
// so the model carries the substance of it in a few hundred tokens instead of the
// words.
//
// What is and is not in the history matters here, so it is said once. The history the
// TUI sends with a question is the questions and the FINAL answers of the earlier
// turns, capped by trimHistory at chatMaxHistory messages and chatMaxHistoryBytes. The
// files a tool read and the commands it ran inside a turn are not in it: they live
// only for the length of that turn. Two consequences:
//
//   - the size the status bar calls "ctx" (the last request of the last turn) can be
//     far larger than what the NEXT turn will carry, because most of it was tool output
//     of the turn that just ended;
//   - what trimHistory drops (anything older than the cap) is dropped whole and
//     silently. Compacting first turns that loss into a summary.
//
// So compacting is worth doing before the cap bites, and pointless on a short history.

const (
	// compactKeepTurns is how many of the most recent messages stay word for word. An
	// even number, so a kept window starts on a question.
	compactKeepTurns = 4
	// compactMinMessages is the fewest messages worth summarising: below it the summary
	// would be about as long as what it replaces.
	compactMinMessages = 6
	// compactMinBytes is the least old text worth a model call.
	compactMinBytes = 4_000
	// compactSendBytes caps the transcript handed to the model to summarise. A request
	// that is itself too big is how a context gets stuck; the oldest text is cut first.
	compactSendBytes = 120_000
	// compactTurnBytes caps one message inside that transcript (a pasted file, a long
	// answer): its head and tail are kept.
	compactTurnBytes = 6_000
	// compactSummaryBytes caps the summary the model writes, as kept in the history.
	compactSummaryBytes = 12_000
)

// compactSystem is the instruction the summary is written under.
const compactSystem = "You write the memory of a conversation between a user and an assistant in a terminal. " +
	"Write a summary the assistant can carry on from: the user's goals, decisions taken and why, " +
	"files and commands that matter (exact names), what was done, what is still open, and anything " +
	"the user said they prefer. Keep exact identifiers, paths and numbers. Do not greet, do not " +
	"comment on the summary, do not invent. Plain text, short sections, no more than 600 words."

// estimateTokens is the rough size of a text in tokens: four bytes make about one.
// It is only ever shown as an estimate (with a "~"), never as a count the provider gave.
func estimateTokens(bytes int) int { return (bytes + 3) / 4 }

// historyBytes is the text the history carries.
func historyBytes(h []driver.ChatTurn) int {
	n := 0
	for _, t := range h {
		n += len(t.Text)
	}
	return n
}

// splitForCompact divides the history into what gets summarised and what stays word for
// word. The kept part is the last compactKeepTurns messages, moved earlier if needed so
// it starts on a question. ok is false when there is not enough old text to be worth it.
func splitForCompact(h []driver.ChatTurn) (old, keep []driver.ChatTurn, ok bool) {
	cut := len(h) - compactKeepTurns
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && cut < len(h) && h[cut].Role != "user" {
		cut--
	}
	old, keep = h[:cut], h[cut:]
	if len(old) < compactMinMessages-compactKeepTurns || historyBytes(old) < compactMinBytes {
		return nil, h, false
	}
	return old, keep, true
}

// clipTurn keeps the head and the tail of a long message.
func clipTurn(s string, max int) string {
	if len(s) <= max {
		return s
	}
	half := max / 2
	return strings.ToValidUTF8(s[:half], "") + "\n[… " + fmt.Sprint(len(s)-max) + " bytes left out …]\n" + strings.ToValidUTF8(s[len(s)-half:], "")
}

// compactTranscript renders the messages to summarise as a transcript, newest last,
// within compactSendBytes. When it must cut, it cuts the oldest messages and says so.
func compactTranscript(old []driver.ChatTurn) string {
	lines := make([]string, len(old))
	for i, t := range old {
		who := "USER"
		if t.Role == "assistant" {
			who = "ASSISTANT"
		}
		lines[i] = who + ": " + clipTurn(strings.TrimSpace(t.Text), compactTurnBytes)
	}
	total, start := 0, len(lines)
	for start > 0 && total+len(lines[start-1]) <= compactSendBytes {
		start--
		total += len(lines[start]) + 2
	}
	out := strings.Join(lines[start:], "\n\n")
	if start > 0 {
		out = fmt.Sprintf("[the %d oldest messages are not shown]\n\n", start) + out
	}
	return out
}

// compactPrompt is the question the model answers to write the summary. focus is what
// the user asked it to keep (/compact <focus>), "" for nothing in particular.
func compactPrompt(old []driver.ChatTurn, focus string) string {
	p := "Summarise this conversation.\n"
	if focus = strings.TrimSpace(focus); focus != "" {
		p += "The user asks you to keep especially: " + focus + "\n"
	}
	return p + "\n--- conversation ---\n" + compactTranscript(old)
}

// fallbackSummary is what stands in for the model's summary when the model cannot be
// asked (the request failed, the context was the problem): the user's requests, short,
// in order, and the tools the earlier turns used. It is poorer than a real summary and
// says it is one, but it is never empty and it needs no network.
func fallbackSummary(old []driver.ChatTurn, tools string) string {
	var b strings.Builder
	b.WriteString("(Automatic outline: a summary could not be written, so this lists what the user asked, in order.)\n")
	n := 0
	for _, t := range old {
		if t.Role != "user" {
			continue
		}
		q := strings.Join(strings.Fields(t.Text), " ")
		if len([]rune(q)) > 160 {
			q = string([]rune(q)[:160]) + "…"
		}
		if q == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, q)
	}
	if tools = strings.TrimSpace(tools); tools != "" {
		b.WriteString("\nTools used: " + tools + "\n")
	}
	return strings.TrimSpace(b.String())
}

// summaryHeader introduces the summary in the history, so the model reads it as memory
// and not as something the user said.
const summaryHeader = "[Summary of the earlier conversation, written by the assistant to keep it short]\n"

// compactedHistory is the new history: the summary as a question/answer pair (a history
// must start on a user turn and alternate), then the kept messages.
func compactedHistory(summary string, keep []driver.ChatTurn) []driver.ChatTurn {
	if len(summary) > compactSummaryBytes {
		summary = strings.ToValidUTF8(summary[:compactSummaryBytes], "") + "…"
	}
	out := []driver.ChatTurn{
		{Role: "user", Text: summaryHeader + summary},
		{Role: "assistant", Text: "Understood. I will carry on from this summary."},
	}
	return append(out, keep...)
}

// compactReport is the line the conversation shows after a compaction.
func compactReport(beforeBytes, afterBytes, summarised int, viaModel bool) string {
	how := "summarised by the model"
	if !viaModel {
		how = "outlined without the model (it could not be asked)"
	}
	return fmt.Sprintf("compacted: %d earlier messages %s. The history the next question carries went from ~%s to ~%s tokens (estimated).",
		summarised, how, humanTokens(uint64(estimateTokens(beforeBytes))), humanTokens(uint64(estimateTokens(afterBytes))))
}

// autoCompactSetting is the user's choice for compacting without being asked.
type autoCompactSetting struct {
	On      bool
	Percent int // of the model's context window
}

const (
	defaultAutoCompactPercent = 80
	minAutoCompactPercent     = 10
	maxAutoCompactPercent     = 95
)

// clampPercent keeps a percentage in the range the setting accepts.
func clampPercent(p int) int {
	if p < minAutoCompactPercent {
		return minAutoCompactPercent
	}
	if p > maxAutoCompactPercent {
		return maxAutoCompactPercent
	}
	return p
}

// shouldAutoCompact decides, after a turn, whether to compact now. It needs the window
// (without it a percentage means nothing, so nothing is done) and a history that is
// worth compacting: a long tool loop can fill the window of a turn while the history is
// three short messages, and compacting that every turn would spend a model call each
// time to change nothing.
func shouldAutoCompact(s autoCompactSetting, lastContext uint64, window int, hist []driver.ChatTurn) bool {
	if !s.On || window <= 0 || lastContext == 0 {
		return false
	}
	if lastContext*100 < uint64(window)*uint64(clampPercent(s.Percent)) {
		return false
	}
	_, _, worth := splitForCompact(hist)
	return worth
}

// contextPercent is how full the window is, 0 when either figure is unknown.
func contextPercent(ctx uint64, window int) int {
	if ctx == 0 || window <= 0 {
		return 0
	}
	p := int(ctx * 100 / uint64(window))
	if p == 0 {
		p = 1 // something is held: never show a window that looks empty
	}
	return p
}

// compactTimeout bounds the summary request. The core has its own, longer limits; a
// summary that takes longer than this is not worth waiting for, and the outline is.
const compactTimeout = 4 * time.Minute

// historySize is the number of messages and bytes of the conversation held now.
func (c *chatSession) historySize() (msgs, bytes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.history), historyBytes(c.history)
}

// compact replaces the older part of the conversation with a summary. It answers
// at once for what can be decided up front (a turn in flight, an old core); the work
// itself runs on its own goroutine and reports in the conversation, like a turn.
//
// The model is asked with no tools, no history and no thinking: the whole point is a
// small request, and a request that carries the very conversation that is too big
// would fail for the same reason. If it cannot be asked (an error, an empty reply) the
// outline of fallbackSummary stands in, and the report says so.
func (c *chatSession) compact(ctx context.Context, focus string) error {
	if err := chatRequires(c.core.Hello()); err != nil {
		return err
	}
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return errChatBusy
	}
	old, keep, worth := splitForCompact(c.history)
	gen := c.gen
	if !worth {
		n, b := len(c.history), historyBytes(c.history)
		c.mu.Unlock()
		c.post(ctx, gen, "chat.note", map[string]any{"text": fmt.Sprintf(
			"nothing to compact: the history the next question carries is %d messages, ~%s tokens. "+
				"/compact is for a long conversation; /clear starts a new one.", n, humanTokens(uint64(estimateTokens(b))))})
		return nil
	}
	c.busy, c.compacting = true, true
	c.turn++
	turn := c.turn
	turnCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	before := historyBytes(c.history)
	recs := append([]toolRecord(nil), c.toolLog...)
	c.mu.Unlock()
	go c.runCompact(turnCtx, focus, old, keep, before, toolRecordsText(recs), gen, turn)
	return nil
}

func (c *chatSession) runCompact(ctx context.Context, focus string, old, keep []driver.ChatTurn, before int, tools string, gen, turn int64) {
	defer func() {
		c.mu.Lock()
		if c.gen == gen && c.turn == turn {
			c.busy, c.compacting, c.cancel = false, false, nil
		}
		c.mu.Unlock()
	}()
	c.emit(ctx, gen, "agent.activated", map[string]any{"agent": "assistant"})
	core := c.core
	if c.dial != nil {
		if conn, closeConn, err := c.dial(ctx); err == nil {
			defer closeConn()
			core = conn
		}
	}
	summary, viaModel := "", false
	reqCtx, stop := context.WithTimeout(ctx, compactTimeout)
	res, err := core.SubmitChatSend(reqCtx, driver.ChatSendParams{Prompt: compactPrompt(old, focus), System: compactSystem})
	stop()
	if ctx.Err() != nil {
		return // cancelled: cancelTurn already told the conversation and left the history alone
	}
	if err == nil && res != nil {
		if t := strings.TrimSpace(res.Text); t != "" && t != "(the model sent an empty reply)" {
			summary, viaModel = t, true
		}
	}
	if !viaModel {
		summary = fallbackSummary(old, tools)
	}
	next := compactedHistory(summary, keep)
	c.mu.Lock()
	if c.gen != gen || c.turn != turn {
		c.mu.Unlock()
		return
	}
	c.history = next
	c.mu.Unlock()
	after := historyBytes(next)
	report := compactReport(before, after, len(old), viaModel)
	if !viaModel && err != nil {
		report += " Reason: " + chatErrorText(err)
	}
	c.emit(ctx, gen, "chat.compacted", map[string]any{
		"summary": summary, "keep": float64(len(keep)),
		"context_tokens": float64(estimateTokens(after)),
	})
	c.emit(ctx, gen, "chat.note", map[string]any{"text": report})
	c.emit(ctx, gen, "agent.turn_done", map[string]any{"agent": "assistant"})
}

// compactLine reports whether Enter on this line (or on the highlighted menu row) is
// `/compact`, with what the user asked to keep after it ("" for nothing in particular).
// `/compact` takes free text, unlike `/clear`: the text is a hint for the summary, and
// a hint can do no harm, since compacting never discards the lines on screen.
func compactLine(input string, sel int, cat string) (focus string, ok bool) {
	if menuCommand("compact", input, sel, cat) {
		return "", true
	}
	if rest, matched := stripLeadingVerb(strings.TrimSpace(input), "compact"); matched && strings.HasPrefix(strings.TrimSpace(input), "/") {
		return rest, true
	}
	return "", false
}

// sessionCompactor is the optional capability a Driver has when it owns a conversation
// that can be compacted (serveDriver).
type sessionCompactor interface {
	Compact(ctx context.Context, focus string) error
}

// historyReader is the optional capability a Driver has when it can say what the next
// question will carry: the autocompact decision reads it.
type historyReader interface {
	ChatHistory() []driver.ChatTurn
}

// noCompactNotice is the answer to /compact on a connection with no chat to compact.
const noCompactNotice = "/compact: there is no live conversation with a model to compact; set ARXI_BIN to an arxi binary and restart"
