package main

import (
	"fmt"
	"strings"
)

// The model remembers a conversation only through the history the app sends with each
// question, and that history used to hold the words of each question and of each final
// answer and nothing else. What the model DID in a turn (the files it read, the change
// it proposed, the proposal that was refused and why) was gone by the next question, so
// it said "done" about a change that had been refused and could not tell what it had
// already tried. The trace below is that missing part: one short line per tool call, kept
// in the assistant's side of the history, written by the app and not by the model.

// traceMaxLines and traceMaxLine bound the record, so a long tool loop cannot grow the
// history without limit: the newest calls are the ones that matter.
const (
	traceMaxLines = 12
	traceMaxLine  = 200
)

// traceMarker opens the record. The next line tells the model what it is, so that it
// reads it as a fact and does not imitate the format in its own answers.
const traceMarker = "[Record kept by the app of what you did with tools in this turn (it is not text you wrote; do not copy this format):"

// interruptedNote ends a turn that did not finish.
const interruptedNote = "[This turn was cut short before you finished it (the user cancelled or interrupted it, or it failed): what is listed above happened, and nothing after it did.]"

// traceLine is one tool call as the record says it.
func traceLine(name, arg, summary string, ok bool) string {
	var b strings.Builder
	b.WriteString("- " + name)
	if arg != "" {
		b.WriteString("(" + oneLine(arg, 80) + ")")
	}
	if summary != "" {
		b.WriteString(": " + oneLine(summary, traceMaxLine))
	}
	if !ok {
		b.WriteString(" [did not succeed]")
	}
	return b.String()
}

// oneLine collapses whitespace and cuts at n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// traceText renders the lines of a turn, newest last, or "" when the turn used no tool.
func traceText(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	skipped := 0
	if len(lines) > traceMaxLines {
		skipped = len(lines) - traceMaxLines
		lines = lines[skipped:]
	}
	var b strings.Builder
	b.WriteString(traceMarker + "\n")
	if skipped > 0 {
		fmt.Fprintf(&b, "- (%d earlier calls not listed)\n", skipped)
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("]")
	return b.String()
}

// withTrace puts the record in front of what the assistant said (or, for a turn that
// never answered, instead of it).
func withTrace(lines []string, answer string) string {
	t := traceText(lines)
	switch {
	case t == "":
		return answer
	case strings.TrimSpace(answer) == "":
		return t
	}
	return t + "\n\n" + answer
}

// interruptedSuffix is the closing note of a turn that never answered.
func interruptedSuffix(trace []string) string {
	if len(trace) == 0 {
		return interruptedNote
	}
	return "\n\n" + interruptedNote
}
