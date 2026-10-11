package main

import (
	"strings"
	"unicode"
)

// sendUsage is the local warning a bare `/send` gets. It names the consequence
// (nothing was sent) and the remedy (give it text), per the repo's message rule.
const sendUsage = "/send needs text: nothing was sent. Type /send followed by what the model should read."

// parseSend recognises `/send <text>`, the explicit way to put a line that
// starts with "/" in front of the model. The text is everything after the verb,
// trimmed at both ends and otherwise untouched: `/send /help` sends the
// literal "/help", it does not run the command.
//
// matched reports whether the line IS a /send invocation (an empty text with
// matched=true is the bare command, which the host answers with sendUsage).
// Only the slash spelling counts: a bare "send hi" is an ordinary sentence for
// the model, and "/sendx" is a different (unknown) command.
func parseSend(line string) (text string, matched bool) {
	body := strings.TrimSpace(line)
	rest, ok := strings.CutPrefix(body, "/send")
	if !ok {
		return "", false
	}
	if rest == "" {
		return "", true
	}
	if r := []rune(rest)[0]; !unicode.IsSpace(r) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
