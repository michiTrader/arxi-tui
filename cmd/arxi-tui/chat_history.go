package main

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// What the model is told about a conversation is the history, and the history used to
// hold only questions that got an answer. A request that failed (a rate limit, a dead
// connection) or was cancelled (the user switched model in the middle of the wait) was
// forgotten, so the next model, asked to "reintenta" / "retry", received that one word
// and nothing to retry. Tool calls were forgotten too, so it could not tell what had
// already been done. Now:
//
//   - a request that got no answer stays in the history with a note saying so, and the
//     tools that did run before it stopped;
//   - the tools that produced an answer are listed in the system prompt of the next
//     turns (toolRecord), not pasted after the answer. A bracketed footer on every
//     earlier reply is a pattern the model copies into its own replies; a record the
//     app keeps is read as what it is.
//
// Both are small, and both are what lets a different model, or the same one a minute
// later, carry on instead of starting from nothing.

const (
	digestMaxTools = 12
	digestArgMax   = 70
)

// toolDigestEntry is one tool call as a short word: read(main.go), run(ren a b), a failed
// one marked. The interface guide is left out: it is reading, not doing.
func toolDigestEntry(t driver.ToolCall) string {
	if t.Name == uiToolGuide {
		return ""
	}
	arg := strings.Join(strings.Fields(t.Arg), " ")
	if len([]rune(arg)) > digestArgMax {
		arg = string([]rune(arg)[:digestArgMax]) + "…"
	}
	e := t.Name + "(" + arg + ")"
	if !t.OK {
		e += " failed"
	}
	return e
}

// toolDigest joins the entries; "" when none.
func toolDigest(tools []string) string {
	if len(tools) == 0 {
		return ""
	}
	if len(tools) > digestMaxTools {
		tools = append(tools[:digestMaxTools:digestMaxTools], "…")
	}
	return strings.Join(tools, "; ")
}

// toolRecord is what one answered turn did: the request, shortened, and the tools used.
type toolRecord struct {
	ask   string
	tools []string
}

const (
	toolRecordsMax = 12 // answered turns remembered; the oldest go first
	toolRecordAsk  = 60 // runes of the request kept
)

// newToolRecord shortens the request and keeps the tools. It is empty when the turn used
// none, which is what most turns are.
func newToolRecord(ask string, tools []string) (toolRecord, bool) {
	if len(tools) == 0 {
		return toolRecord{}, false
	}
	ask = strings.Join(strings.Fields(ask), " ")
	if r := []rune(ask); len(r) > toolRecordAsk {
		ask = string(r[:toolRecordAsk]) + "…"
	}
	return toolRecord{ask: ask, tools: append([]string(nil), tools...)}, true
}

// toolRecordsText is the paragraph the system prompt carries, "" when there is nothing
// to say. It says what it is, so the model neither repeats it in its replies nor takes
// it for something the user wrote.
func toolRecordsText(recs []toolRecord) string {
	if len(recs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Tools used in earlier turns of this conversation, kept by the app (not part of your replies; do not copy this list into them):")
	for _, r := range recs {
		b.WriteString("\n- \"" + r.ask + "\": " + toolDigest(r.tools))
	}
	return b.String()
}

// unansweredText is the assistant's history entry for a request that got no answer. The
// words tell the next model to take the request up again when the user asks for it.
func unansweredText(why string, tools []string) string {
	t := "[No reply was given to this request: " + why + "."
	if d := toolDigest(tools); d != "" {
		t += " Tools that ran before it stopped: " + d + "."
	}
	return t + " If the user asks to retry or continue, do this request.]"
}

const (
	whyCancelled = "it was cancelled"
	whyFailed    = "the request failed"
)
