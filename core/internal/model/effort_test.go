package model

import (
	"strings"
	"testing"
)

func TestEffortLevelsPerModel(t *testing.T) {
	oa, an := ProtocolOpenAIChatCompletions, ProtocolAnthropicMessages
	cases := []struct {
		protocol, id string
		want         string
	}{
		// DeepSeek V4: a thinking switch plus low / high / max; no medium.
		{oa, "deepseek-v4.1-flash", "off low high max"},
		{oa, "tokenharbor/deepseek-v4.1-flash:free", "off low high max"},
		{oa, "deepseek-v4-pro", "off low high max"},
		// OpenAI by generation.
		{oa, "gpt-5", "minimal low medium high"},
		{oa, "gpt-5-mini", "minimal low medium high"},
		{oa, "gpt-5.1", "off low medium high"},
		{oa, "gpt-5.1-mini", "off low medium high"},
		{oa, "gpt-5.2", "off low medium high xhigh"},
		{oa, "gpt-5.6", "off low medium high xhigh max"},
		{oa, "gpt-6", "low medium high xhigh"},
		{oa, "o3", "low medium high"},
		{oa, "o4-mini", "low medium high"},
		{oa, "gpt-4o", ""},
		{oa, "gpt-5-chat-latest", ""},
		// Anthropic: effort only from Opus 4.5 / Sonnet 4.6; max and xhigh by model.
		{an, "claude-opus-4-1", ""},
		{an, "claude-opus-4-20250514", ""},
		{an, "claude-sonnet-4-5", ""},
		{an, "claude-haiku-4-5", ""},
		{an, "claude-opus-4-5-20251101", "low medium high"},
		{an, "claude-sonnet-4-6", "low medium high max"},
		{an, "claude-opus-4-6", "low medium high max"},
		{an, "claude-opus-4-7", "low medium high xhigh max"},
		{an, "claude-sonnet-5", "low medium high xhigh max"},
		{an, "claude-haiku-5-5", "off low medium high xhigh max"},
		// A Claude behind an OpenAI-style gateway has nothing to choose here.
		{oa, "anthropic/claude-sonnet-4-6", ""},
		// Switch-only models, and the default for everything else.
		{oa, "kimi-k2.6", "off on"},
		{oa, "gemini-3-pro", "low high"},
		{oa, "llama3.1", "low medium high"},
		{oa, "qwen3-coder", "low medium high"},
	}
	for _, c := range cases {
		got := strings.Join(EffortLevels(c.protocol, c.id), " ")
		if got != c.want {
			t.Errorf("EffortLevels(%q, %q) = %q, want %q", c.protocol, c.id, got, c.want)
		}
	}
}

func TestEffortAllowed(t *testing.T) {
	oa := ProtocolOpenAIChatCompletions
	if EffortAllowed(oa, "deepseek-v4.1-flash", EffortMedium) {
		t.Error("deepseek v4 has no distinct medium")
	}
	if !EffortAllowed(oa, "deepseek-v4.1-flash", EffortMax) {
		t.Error("deepseek v4 takes max")
	}
	if EffortAllowed(oa, "gpt-5", EffortOff) {
		t.Error("gpt-5 cannot turn thinking off")
	}
}

func TestOpenAIWire(t *testing.T) {
	cases := []struct {
		id, level string
		want      OpenAIEffort
	}{
		{"deepseek-v4.1-flash", "", OpenAIEffort{}},
		{"deepseek-v4.1-flash", "off", OpenAIEffort{Thinking: "disabled"}},
		{"deepseek-v4.1-flash", "max", OpenAIEffort{Reasoning: "max"}},
		{"kimi-k2.6", "off", OpenAIEffort{Thinking: "disabled"}},
		{"kimi-k2.6", "on", OpenAIEffort{Thinking: "enabled"}},
		{"gpt-5.1", "off", OpenAIEffort{Reasoning: "none"}},
		{"gpt-5.1", "medium", OpenAIEffort{Reasoning: "medium"}},
		{"llama3.1", "high", OpenAIEffort{Reasoning: "high"}},
	}
	for _, c := range cases {
		if got := OpenAIWire(c.id, c.level); got != c.want {
			t.Errorf("OpenAIWire(%q, %q) = %+v, want %+v", c.id, c.level, got, c.want)
		}
	}
}

func TestAnthropicWire(t *testing.T) {
	if got := AnthropicWire("max"); got != (AnthropicEffort{Effort: "max"}) {
		t.Errorf("max = %+v", got)
	}
	if got := AnthropicWire("off"); got != (AnthropicEffort{ThinkingOff: true}) {
		t.Errorf("off = %+v", got)
	}
	if got := AnthropicWire(""); got != (AnthropicEffort{}) {
		t.Errorf("empty = %+v", got)
	}
}
