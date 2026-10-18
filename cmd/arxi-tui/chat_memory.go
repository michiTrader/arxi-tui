package main

import (
	"strings"

	"github.com/michiTrader/arxi_tui/internal/driver"
	"github.com/michiTrader/arxi_tui/internal/fold"
)

// Between two questions the history keeps the user's text and the model's final answer,
// and nothing of what the model read to write it. So the next question starts from a
// model that has forgotten the file it opened a minute ago, and it reads it again: one
// whole file per repeat, paid for in every ask that follows. It also makes the context
// figure fall after a long turn, because the next ask starts from the short history.
//
// toolMemory is what the app keeps instead: what the reading tools (read, list, grep)
// returned in earlier answered turns, newest first within a byte budget, in the system
// prompt of the next turns. It is as stale as the files are, so it is dropped as soon as
// something could have changed them (a write, an edit, a command), it says it may be out
// of date, and it is emptied by /clear and by a compaction, whose point is a smaller
// context.

const (
	// memoryBudget is how much remembered tool output the system prompt carries: about
	// 8k tokens, a few files, never a window's worth.
	memoryBudget = 32 * 1024
	// memoryMaxEntries bounds the list however small the results are.
	memoryMaxEntries = 40
)

// remembers says which tools' results are worth keeping: the ones that only look. A
// command's output is not the state of anything, and a write's result is a receipt.
func remembers(name string) bool {
	switch name {
	case "read", "list", "grep":
		return true
	}
	return false
}

// changes says which tools could have changed what the remembered results describe.
func changesFiles(name string) bool {
	switch name {
	case "write", "edit", "run":
		return true
	}
	return false
}

// remember folds the calls of an answered turn into the memory, in the order they ran:
// a result is kept, a later look at the same thing replaces it, and a call that could
// have changed the files drops what it could have changed. A write or edit names its
// file, so only that file's reading and the listings and searches (which may now differ)
// go; a command names nothing, so everything goes.
func remember(mem []toolEcho, calls []toolEcho) []toolEcho {
	return foldCalls(mem, calls, true)
}

// invalidate is remember for a turn that got no answer: what it read is already in the
// note the history keeps for it, so only what it could have changed is acted on.
func invalidate(mem []toolEcho, calls []toolEcho) []toolEcho {
	return foldCalls(mem, calls, false)
}

func foldCalls(mem []toolEcho, calls []toolEcho, keep bool) []toolEcho {
	for _, c := range calls {
		switch {
		case c.name == "run":
			mem = nil
		case changesFiles(c.name):
			kept := mem[:0:0]
			for _, m := range mem {
				if m.name == "read" && m.arg != c.arg {
					kept = append(kept, m)
				}
			}
			mem = kept
		case keep && remembers(c.name) && c.ok && c.output != "" && !cutOutput(c.output):
			kept := mem[:0:0]
			for _, m := range mem {
				if m.entry != c.entry {
					kept = append(kept, m)
				}
			}
			mem = append(kept, c)
		}
	}
	if len(mem) > memoryMaxEntries {
		mem = mem[len(mem)-memoryMaxEntries:]
	}
	return append([]toolEcho(nil), mem...)
}

// memoryFromEvents rebuilds what the live session keeps in memory from a saved
// conversation: the calls of each question are folded in when it is answered, or
// invalidate what they changed when it was not, and a compaction empties it.
func memoryFromEvents(evs []fold.Event) []toolEcho {
	var mem, calls []toolEcho
	for _, e := range evs {
		switch e.Type {
		case "run.prompt":
			mem = invalidate(mem, calls) // the previous question never got an answer
			calls = nil
		case "chat.tool":
			var t driver.ToolCall
			t.Name, _ = e.Payload["name"].(string)
			t.Arg, _ = e.Payload["arg"].(string)
			t.OK, _ = e.Payload["ok"].(bool)
			t.Output, _ = e.Payload["output"].(string)
			if d := toolDigestEntry(t); d != "" {
				calls = append(calls, echoOf(t, d))
			}
		case "llm.response":
			// A streamed answer arrives in pieces: only the first finds calls to fold.
			mem = remember(mem, calls)
			calls = nil
		case "chat.error", "chat.cancelled":
			mem = invalidate(mem, calls)
			calls = nil
		case "chat.compacted":
			mem, calls = nil, nil
		}
	}
	return mem
}

// toolMemoryText is the paragraph the system prompt carries, "" when there is nothing to
// say. Newest first until the budget is spent; what does not fit is left out whole.
func toolMemoryText(mem []toolEcho) string {
	left := memoryBudget
	keep := make([]bool, len(mem))
	n := 0
	for i := len(mem) - 1; i >= 0; i-- {
		if size := len(mem[i].output); size <= left {
			keep[i], left = true, left-size
			n++
		}
	}
	if n == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("What you read in earlier turns of this conversation, kept by the app so you need not read it again. " +
		"It is as it was when read: a file may have changed since, so read it again if the user says it changed or something may have changed it. " +
		"(not part of your replies; do not copy it into them):")
	for i, m := range mem {
		if keep[i] {
			b.WriteString("\n- " + m.entry + " returned:\n" + m.output)
		}
	}
	return b.String()
}

// cutOutput says whether a result is only part of what was asked: the core marks a
// clipped output, the read tool marks a page of a long file ("[showing lines 1-2000 of
// 5000...]"), and a stored conversation marks a field it cut. Part of a file remembered as
// "what you read" would make the model believe that is all there is, so it is never kept.
func cutOutput(s string) bool {
	return strings.HasSuffix(s, "[output cut]") || strings.Contains(s, "\n[showing lines ") ||
		(len(s) > sessionFieldM && strings.HasSuffix(s, "…"))
}

// echoOf is one tool call of a turn as the memory sees it.
func echoOf(t driver.ToolCall, entry string) toolEcho {
	return toolEcho{entry: entry, output: t.Output, name: t.Name, arg: t.Arg, ok: t.OK}
}
