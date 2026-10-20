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
	if t.Name == uiToolGuide || t.Name == uiToolPreview {
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

// unansweredText is the app's note about a request that got no answer. The words tell the
// next model to take the request up again when the user asks for it.
//
// It is the APP speaking, so it is never put in the assistant's mouth (see
// unansweredTurn): a model shown its own earlier reply as "[No reply was given...]" copied
// it, and the user read that sentence as the answer to "reintenta", three times in a row.
func unansweredText(why string, tools []string) string {
	t := "[App note, not typed by the user: No reply was given to the request above: " + why + "."
	if d := toolDigest(tools); d != "" {
		t += " Tools that ran before it stopped: " + d + "."
	}
	return t + " If the user asks to retry or continue, do the request above. Never repeat this note in a reply.]"
}

// unansweredTurn is what the history keeps of a request that got no answer: the request
// and, in the same USER message, the app's note about it. There is no assistant message
// for it, because there was no reply; the core joins the user messages that follow each
// other (mergeUserRuns in core/cmd/arxi), so the model reads one message and has no earlier "reply" of its
// own to imitate.
func unansweredTurn(request, why string, echoes []toolEcho) driver.ChatTurn {
	return driver.ChatTurn{Role: "user", Text: request + "\n\n" + unansweredWith(why, echoes)}
}

// toolEcho is one tool call of a turn with what it returned.
type toolEcho struct {
	entry  string // read(main.go), as toolDigestEntry words it
	output string // what the tool returned, as far as the core reported it
	// name, arg and ok are the call itself; the memory of earlier turns (chat_memory.go)
	// needs them to tell a read from a command and one file from another.
	name, arg string
	ok        bool
}

const (
	// echoBudget is how much tool output a turn that got no answer leaves in the history.
	// Without it, a turn that read a dozen files and then timed out left only their names,
	// so "continue" started from nothing and read them all again -- often into the same
	// timeout. About 12k tokens: enough to keep what was found, small enough that a failed
	// turn cannot make the next request the size of the window.
	echoBudget = 48 * 1024
)

// unansweredWith is unansweredText plus what the tools returned, newest first until the
// budget is spent. Older results are named but not repeated, and say so.
func unansweredWith(why string, echoes []toolEcho) string {
	var names []string
	for _, e := range echoes {
		names = append(names, e.entry)
	}
	text := unansweredText(why, names)
	kept := make([]bool, len(echoes))
	left := echoBudget
	for i := len(echoes) - 1; i >= 0; i-- {
		n := len(echoes[i].output)
		if n == 0 || n > left {
			continue
		}
		kept[i], left = true, left-n
	}
	var b strings.Builder
	dropped := 0
	for i, e := range echoes {
		switch {
		case kept[i]:
			b.WriteString("\n- " + e.entry + " returned:\n" + e.output)
		case e.output != "":
			dropped++
		}
	}
	if b.Len() == 0 {
		return text
	}
	head := "\nWhat those tools returned, kept by the app so the work need not be repeated (not part of your replies; do not copy it into them)"
	if dropped > 0 {
		head += "; " + plural(dropped, "older result", "older results") + " did not fit and are left out"
	}
	return text + head + ":" + b.String()
}

const (
	whyCancelled = "it was cancelled"
	whyFailed    = "the request failed"
)
