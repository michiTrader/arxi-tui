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
//   - an answer is followed by a one-line digest of the tools used to produce it.
//
// Both are small (a line each), and both are what lets a different model, or the same one
// a minute later, carry on instead of starting from nothing.

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

// answeredText is the assistant's history entry for a turn that got an answer.
func answeredText(answer string, tools []string) string {
	if d := toolDigest(tools); d != "" {
		return answer + "\n\n[Tools used for this reply: " + d + "]"
	}
	return answer
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
