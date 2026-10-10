package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/engine"
)

// The failure that started this: asked to put a tool's result on the call's row, the
// agent said the format was not in this project and only colours could change. The
// format is now keys the agent can read in ui_guide and change with ui_edit.
func TestTheGuideTellsTheAgentTheChatFormatIsEditable(t *testing.T) {
	r := newBridgeRig(t, "allow")
	guide := uiGuide(r.doc, r.b.below)
	for _, want := range []string{"CHAT FORMAT", "tool.inline", "tool.separator", "chat.user.max_lines", "chat.turn_gap", "chat.usage.format", "tool.title.read"} {
		if !strings.Contains(guide, want) {
			t.Errorf("ui_guide never mentions %q; the agent will say the chat format cannot be changed", want)
		}
	}
}

func TestEditingTheChatFormatWithUiEdit(t *testing.T) {
	setActiveTexts(nil)
	t.Cleanup(func() { setActiveTexts(nil) })
	u, err := applyTexts(nil, map[string]string{
		"tool.inline": "no", "tool.separator": " → ", "chat.user.max_lines": "5",
		"chat.turn_gap": "0", "tool.title.read": "Open",
	})
	if err != nil {
		t.Fatal(err)
	}
	setActiveTexts(u)
	l := chatLook()
	if l.ToolInline || l.ToolSep != " → " || l.UserMaxLines != 5 || l.TurnGap != 0 || l.ToolTitles["read"] != "Open" {
		t.Fatalf("look = %+v", l)
	}
}

func TestTheFactoryChatLookIsTheEnginesDefault(t *testing.T) {
	setActiveTexts(nil)
	got, want := chatLook(), engine.DefaultChatLook()
	if got.ToolDot != want.ToolDot || got.ToolElbow != want.ToolElbow || !got.ToolInline || got.ToolSep != want.ToolSep ||
		got.UserMaxLines != 3 || got.TurnGap != 1 || got.UsageGap != 1 || got.AssistantIndent != want.AssistantIndent ||
		got.UsageFormat != want.UsageFormat || got.Asks["run"] != want.Asks["run"] || got.OutputRows != want.OutputRows {
		t.Fatalf("factory look drifted from the engine's: %+v vs %+v", got, want)
	}
	for _, tool := range chatToolNames {
		if got.ToolTitles[tool] != want.Title(tool) {
			t.Errorf("title of %s = %q, want %q", tool, got.ToolTitles[tool], want.Title(tool))
		}
	}
}

func TestANumberKeyRefusesWordsAndAYesNoKeyRefusesMaybe(t *testing.T) {
	for _, tc := range []map[string]string{
		{"chat.user.max_lines": "many"}, {"chat.turn_gap": "-1"}, {"chat.turn_gap": "99"},
		{"tool.output_rows": "3.5"}, {"tool.inline": "maybe"},
	} {
		if _, err := applyTexts(nil, tc); err == nil {
			t.Errorf("%v was accepted", tc)
		}
	}
	if _, err := applyTexts(nil, map[string]string{"chat.user.max_lines": "0", "tool.inline": "yes"}); err != nil {
		t.Errorf("valid values refused: %v", err)
	}
}
