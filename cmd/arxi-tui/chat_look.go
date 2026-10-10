package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/engine"
)

// This file is where the way the CONVERSATION is drawn becomes the user's. The colours of
// the chat were always tokens, but its format (the dot before a tool call, whether the
// result sits beside the call or under it, how many lines of what you wrote are shown,
// the gap between turns, the shape of the usage line) lived as constants in the engine.
// A user who asked their agent for "the result on the same row as the tool" was told it
// was impossible, because it was: it was not in anything the agent could reach.
//
// Every one of those is now a text key (ui_texts.go registry), so /ui text <key> <value>
// and the agent's ui_edit change it, /ui undo takes it back, and ui_guide lists it with
// its current value. chatLook() reads them into the engine's ChatLook once per repaint.
// A value that does not parse (a number that is not one) is the factory value, never an
// error on screen: the setting is checked when it is written, not when it is drawn.

// chatLookKeys declares the keys; the registry in ui_texts.go calls it.
func chatLookKeys(add func(key, def, role string, paragraph bool)) {
	d := engine.DefaultChatLook()
	add("tool.dot", d.ToolDot, "chat: the glyph before a tool call, e.g. \"● \"", false)
	add("tool.elbow", d.ToolElbow, "chat: the glyph before a tool result that has its own row, e.g. \"└ \"", false)
	add("tool.inline", "yes", "chat: \"yes\" puts a one-row tool result on the call's own row (Read(a.go) - Read 12 lines), \"no\" always puts it under the call", false)
	add("tool.separator", d.ToolSep, "chat: what sits between a tool call and its result when they share a row", false)
	for _, t := range chatToolNames {
		add("tool.title."+t, chatToolTitle(t), "chat: the name shown for the "+t+" tool", false)
	}
	add("tool.expand_hint", d.ExpandHint, "chat: the hint at the end of a cut block (ctrl+o is what opens it)", false)
	add("tool.output_rows", strconv.Itoa(d.OutputRows), "chat: rows of a command's output shown before \"… +N lines\"", false)
	add("tool.diff_rows", strconv.Itoa(d.DiffRows), "chat: rows of a change (diff) shown in the conversation", false)
	add("tool.approval_rows", strconv.Itoa(d.ApprovalRows), "chat: rows of a change shown while it waits for your answer", false)
	add("ask.change", d.Asks[""], "chat: the question under a file change that waits for you", false)
	add("ask.run", d.Asks["run"], "chat: the question under a command that waits for you", false)
	add("ask.web_fetch", d.Asks["web_fetch"], "chat: the question under a page the agent wants to read", false)
	add("ask.web_search", d.Asks["web_search"], "chat: the question under a web search that waits for you", false)
	add("ask.ui_edit", d.Asks["ui_edit"], "chat: the question under a change to the interface", false)
	add("ask.keys", d.AskKeys, "chat: the keys that answer those questions", false)
	add("chat.user.marker", d.UserMarker, "chat: the mark before every row of your messages, e.g. \"┃ \"", false)
	add("chat.user.max_lines", strconv.Itoa(d.UserMaxLines), "chat: how many rows of what you wrote the conversation shows (0 = all); ctrl+o shows it whole", false)
	add("chat.user.more", d.UserMore, "chat: the row that says a message of yours was cut; {n} is the rows hidden, {hint} the expand hint", false)
	add("chat.error.marker", d.ErrorMarker, "chat: the mark before an error", false)
	add("chat.warn.marker", d.WarnMarker, "chat: the mark before a warning", false)
	add("chat.answer.indent", strconv.Itoa(d.AssistantIndent), "chat: columns the agent's answers are indented", false)
	add("chat.turn_gap", strconv.Itoa(d.TurnGap), "chat: blank rows between one turn and the next (0 packs the conversation)", false)
	add("chat.usage_gap", strconv.Itoa(d.UsageGap), "chat: blank rows between an answer and its time/tokens line", false)
	add("chat.usage.format", d.UsageFormat, "chat: the line under an answer; {time} and {tokens} are filled in; blank spaces hide it", false)
	add("chat.usage.tokens", d.UsageTokens, "chat: how {tokens} reads; {in} and {out} are the token counts", false)
}

// chatToolNames are the tools whose displayed name can be changed.
var chatToolNames = []string{"list", "read", "grep", "edit", "write", "run", "web_fetch", "web_search", "ui_guide", "ui_edit"}

func chatToolTitle(tool string) string {
	return engine.DefaultChatLook().Title(tool)
}

// chatLook builds the engine's look from the active words.
func chatLook() *engine.ChatLook {
	n := func(text string, def int) int {
		v, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || v < 0 || v > 1000 {
			return def
		}
		return v
	}
	d := engine.DefaultChatLook()
	l := engine.ChatLook{
		ToolDot:    uiText("tool.dot"),
		ToolElbow:  uiText("tool.elbow"),
		ToolInline: !isNo(uiText("tool.inline")),
		ToolSep:    uiText("tool.separator"),
		ToolTitles: map[string]string{},
		ExpandHint: uiText("tool.expand_hint"),
		Asks: map[string]string{
			"":           uiText("ask.change"),
			"run":        uiText("ask.run"),
			"web_fetch":  uiText("ask.web_fetch"),
			"web_search": uiText("ask.web_search"),
			"ui_edit":    uiText("ask.ui_edit"),
		},
		AskKeys:         uiText("ask.keys"),
		UserMarker:      uiText("chat.user.marker"),
		UserMore:        uiText("chat.user.more"),
		ErrorMarker:     uiText("chat.error.marker"),
		WarnMarker:      uiText("chat.warn.marker"),
		UsageFormat:     uiText("chat.usage.format"),
		UsageTokens:     uiText("chat.usage.tokens"),
		OutputRows:      n(uiText("tool.output_rows"), d.OutputRows),
		DiffRows:        n(uiText("tool.diff_rows"), d.DiffRows),
		ApprovalRows:    n(uiText("tool.approval_rows"), d.ApprovalRows),
		UserMaxLines:    n(uiText("chat.user.max_lines"), d.UserMaxLines),
		AssistantIndent: n(uiText("chat.answer.indent"), d.AssistantIndent),
		TurnGap:         n(uiText("chat.turn_gap"), d.TurnGap),
		UsageGap:        n(uiText("chat.usage_gap"), d.UsageGap),
	}
	for _, t := range chatToolNames {
		l.ToolTitles[t] = uiText("tool.title." + t)
	}
	return &l
}

func isNo(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "no", "false", "off", "0", "n":
		return true
	}
	return false
}

// chatLookNumbers are the keys that hold a whole number, with the most each may be.
var chatLookNumbers = map[string]int{
	"tool.output_rows": 1000, "tool.diff_rows": 1000, "tool.approval_rows": 1000,
	"chat.user.max_lines": 1000, "chat.answer.indent": 40, "chat.turn_gap": 5, "chat.usage_gap": 5,
}

// validChatLook refuses a value that would not mean anything to the chat, at the moment
// it is written, so the user (and the model's repair turn) is told instead of a setting
// that silently does nothing.
func validChatLook(key, val string) error {
	if max, ok := chatLookNumbers[key]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n < 0 || n > max {
			return fmt.Errorf("text %q is a whole number from 0 to %d (for example 3); %q is not", key, max, val)
		}
	}
	if key == "tool.inline" {
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "yes", "no", "true", "false", "on", "off":
		default:
			return fmt.Errorf("text %q is yes or no; %q is not", key, val)
		}
	}
	return nil
}
