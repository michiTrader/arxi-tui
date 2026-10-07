package model

import (
	"strconv"
	"strings"
)

// This file answers one question: what can a caller ask of THIS model about
// thinking, and what goes on the wire when they ask it?
//
// The answer differs per model and per vendor, so one global list of levels is
// wrong for most models: some take `max`, some have no `medium`, some only turn
// thinking off and on. The table below follows each vendor's documentation
// (OpenAI "Reasoning models", Anthropic "Effort", DeepSeek "Thinking mode",
// Moonshot "Thinking models"). A model the table does not know takes the three
// levels every OpenAI-style server understands: low, medium, high.
//
// One vocabulary is used everywhere (the TUI menu, `chat send --effort`): off, on,
// minimal, low, medium, high, xhigh, max. "off" turns thinking off, "on" turns it
// on at the model's own default depth; the rest are depths. A model lists only the
// words it really takes, in menu order.

// Effort level words.
const (
	EffortOff     = "off"
	EffortOn      = "on"
	EffortMinimal = "minimal"
	EffortLow     = "low"
	EffortMedium  = "medium"
	EffortHigh    = "high"
	EffortXHigh   = "xhigh"
	EffortMax     = "max"
)

// EffortWords is every word the vocabulary has, in menu order.
var EffortWords = []string{EffortOff, EffortOn, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// IsEffortWord reports whether level belongs to the vocabulary at all.
func IsEffortWord(level string) bool {
	for _, w := range EffortWords {
		if w == level {
			return true
		}
	}
	return false
}

// EffortLevels lists the thinking levels a model takes, in menu order. An empty
// list means the model has nothing to choose. protocol is the wire the provider
// speaks (EffectiveProtocol); id may carry a gateway prefix ("openai/gpt-5.1") or a
// tag (":free"), neither of which changes the model.
func EffortLevels(protocol, id string) []string {
	m := bareID(id)
	switch {
	case isClaude(m):
		if protocol != ProtocolAnthropicMessages {
			return nil // a Claude behind an OpenAI-style gateway: the gateway decides
		}
		return claudeLevels(m)
	case strings.HasPrefix(m, "gpt-"), isOSeries(m):
		return openAILevels(m)
	case isDeepSeekV4(m):
		return []string{EffortOff, EffortLow, EffortHigh, EffortMax}
	case strings.HasPrefix(m, "kimi-k2"):
		return []string{EffortOff, EffortOn}
	case strings.HasPrefix(m, "gemini-3") && strings.Contains(m, "pro"):
		return []string{EffortLow, EffortHigh}
	}
	return []string{EffortLow, EffortMedium, EffortHigh}
}

// EffortAllowed reports whether level is one the model takes.
func EffortAllowed(protocol, id, level string) bool {
	for _, l := range EffortLevels(protocol, id) {
		if l == level {
			return true
		}
	}
	return false
}

// OpenAIEffort is what one level becomes on the Chat Completions wire.
type OpenAIEffort struct {
	// Reasoning is the `reasoning_effort` value; empty sends nothing.
	Reasoning string
	// Thinking is the `thinking.type` value ("enabled" or "disabled") for the
	// models that have a switch; empty sends nothing.
	Thinking string
}

// OpenAIWire translates a level for the Chat Completions wire. The level has
// already been checked against EffortLevels; an unknown pair sends the level as
// it is, the way every call did before levels were per model.
func OpenAIWire(id, level string) OpenAIEffort {
	m := bareID(id)
	switch {
	case level == "":
		return OpenAIEffort{}
	case isDeepSeekV4(m):
		if level == EffortOff {
			return OpenAIEffort{Thinking: "disabled"}
		}
		return OpenAIEffort{Reasoning: level}
	case strings.HasPrefix(m, "kimi-k2"):
		if level == EffortOff {
			return OpenAIEffort{Thinking: "disabled"}
		}
		return OpenAIEffort{Thinking: "enabled"}
	case level == EffortOff:
		return OpenAIEffort{Reasoning: "none"} // OpenAI's word for "do not think"
	case level == EffortOn:
		return OpenAIEffort{}
	}
	return OpenAIEffort{Reasoning: level}
}

// AnthropicEffort is what one level becomes on the Messages wire.
type AnthropicEffort struct {
	// Effort is the `output_config.effort` value; empty sends nothing.
	Effort string
	// ThinkingOff sends `thinking: {type: disabled}`.
	ThinkingOff bool
}

// AnthropicWire translates a level for the Messages wire.
func AnthropicWire(level string) AnthropicEffort {
	switch level {
	case "", EffortOn:
		return AnthropicEffort{}
	case EffortOff:
		return AnthropicEffort{ThinkingOff: true}
	}
	return AnthropicEffort{Effort: level}
}

// bareID drops a gateway prefix and a ":tag" and lower-cases, so
// "tokenharbor/DeepSeek-V4.1-Flash:free" is "deepseek-v4.1-flash".
func bareID(id string) string {
	m := strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if i := strings.Index(m, ":"); i >= 0 {
		m = m[:i]
	}
	return m
}

func isClaude(m string) bool { return strings.HasPrefix(m, "claude-") }

func isOSeries(m string) bool {
	for _, p := range []string{"o1", "o3", "o4"} {
		if m == p || strings.HasPrefix(m, p+"-") {
			return true
		}
	}
	return false
}

// isDeepSeekV4 is true for DeepSeek V4 and later, the generation with the
// thinking switch and the low / high / max depths.
func isDeepSeekV4(m string) bool {
	rest, ok := strings.CutPrefix(m, "deepseek-v")
	if !ok {
		return false
	}
	major, _, _ := splitVersion(rest)
	return major >= 4
}

// splitVersion reads the leading numbers of "5.1-mini" or "4-8-20260101" as major
// and minor (5 and 1; 4 and 8). It stops at the first part that is not a number,
// and a date stamp (six digits or more) is not a version part.
func splitVersion(s string) (major, minor int, ok bool) {
	var nums []int
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' }) {
		n, err := strconv.Atoi(part)
		if err != nil || len(part) >= 6 || len(nums) == 2 {
			break
		}
		nums = append(nums, n)
	}
	switch len(nums) {
	case 0:
		return 0, 0, false
	case 1:
		return nums[0], 0, true
	}
	return nums[0], nums[1], true
}

// version compares as major*100+minor.
func version(major, minor int) int { return major*100 + minor }

// openAILevels covers OpenAI's reasoning models by generation:
//
//	o-series            low, medium, high
//	gpt-5, -mini, -nano minimal, low, medium, high  (cannot turn thinking off)
//	gpt-5.1             off, low, medium, high
//	gpt-5.2 to 5.5      off, low, medium, high, xhigh
//	gpt-5.6 and later 5 off, low, medium, high, xhigh, max
//	gpt-6               low, medium, high, xhigh  (no "none")
//
// gpt-4 and earlier have no thinking to ask for.
func openAILevels(m string) []string {
	if isOSeries(m) {
		return []string{EffortLow, EffortMedium, EffortHigh}
	}
	rest := strings.TrimPrefix(m, "gpt-")
	major, minor, ok := splitVersion(rest)
	if !ok || major < 5 {
		return nil
	}
	v := version(major, minor)
	switch {
	case major >= 6:
		return []string{EffortLow, EffortMedium, EffortHigh, EffortXHigh}
	case v >= version(5, 6):
		return []string{EffortOff, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	case v >= version(5, 2):
		return []string{EffortOff, EffortLow, EffortMedium, EffortHigh, EffortXHigh}
	case v >= version(5, 1):
		return []string{EffortOff, EffortLow, EffortMedium, EffortHigh}
	}
	if strings.Contains(m, "chat") {
		return nil // gpt-5-chat does not reason
	}
	return []string{EffortMinimal, EffortLow, EffortMedium, EffortHigh}
}

// claudeLevels covers Anthropic's `output_config.effort`. Which models take it, and
// which of them take xhigh and max, is a table by family and version; the models
// before Opus 4.5 have no effort at all.
func claudeLevels(m string) []string {
	rest := strings.TrimPrefix(m, "claude-")
	family, ver, _ := strings.Cut(rest, "-")
	major, minor, ok := splitVersion(ver)
	if !ok {
		return nil
	}
	v := version(major, minor)
	var effort, xhigh, max, off bool
	switch family {
	case "opus":
		effort, max, xhigh = v >= version(4, 5), v >= version(4, 6), v >= version(4, 7)
	case "sonnet":
		effort, max, xhigh = v >= version(4, 6), v >= version(4, 6), v >= version(5, 0)
	case "haiku":
		effort, max, xhigh, off = v >= version(5, 5), v >= version(5, 5), v >= version(5, 5), v >= version(5, 5)
	case "fable":
		effort, max, xhigh = true, true, true
	case "mythos":
		effort, max, xhigh = true, true, !strings.Contains(m, "preview")
	}
	if !effort {
		return nil
	}
	var out []string
	if off {
		out = append(out, EffortOff)
	}
	out = append(out, EffortLow, EffortMedium, EffortHigh)
	if xhigh {
		out = append(out, EffortXHigh)
	}
	if max {
		out = append(out, EffortMax)
	}
	return out
}
