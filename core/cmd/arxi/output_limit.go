package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/michiTrader/arxi/internal/turn"
)

const (
	// defaultMaxOutput is how many tokens one reply of the model may take. A reasoning
	// model spends part of it on thinking before it writes a word, so a small figure
	// (the old 4096) is used up by the thinking alone and the answer comes back empty.
	// It is a ceiling, not a cost: the model is billed for what it writes.
	defaultMaxOutput = 16384
	maxOutputEnv     = "ARXI_MAX_OUTPUT_TOKENS"
	minMaxOutput     = 256
	maxMaxOutput     = 1 << 20
)

// chatMaxOutput is the reply ceiling sent to the provider: the default, or what
// ARXI_MAX_OUTPUT_TOKENS says when that is a whole number in range. A value that is
// not is ignored (the default is used), because a bad figure here must not stop chat.
func chatMaxOutput() int {
	v := strings.TrimSpace(os.Getenv(maxOutputEnv))
	if v == "" {
		return defaultMaxOutput
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < minMaxOutput || n > maxMaxOutput {
		return defaultMaxOutput
	}
	return n
}

var outputLimitPatterns = []*regexp.Regexp{
	// "the valid range of max_tokens is [1, 8192]"
	regexp.MustCompile(`\[\s*1\s*,\s*(\d+)\s*\]`),
	// "max_tokens: 20000 > 8192, which is the maximum allowed"
	regexp.MustCompile(`>\s*(\d{3,7})\s*,\s*which is the maximum`),
	// "at most 8192 tokens", "maximum allowed is 8192", "limit of 8192"
	regexp.MustCompile(`(?i)(?:at most|maximum|max(?:imum)? allowed|up to|limit of|limit is)\D{0,40}?(\d{3,7})`),
}

// outputLimitFrom reads the output ceiling a provider names when it refuses a request
// for asking too many reply tokens. Only a 400 that talks about tokens is read, and only
// a figure that is a believable ceiling: anything else is not a limit to learn.
func outputLimitFrom(r *turn.Refusal) (int, bool) {
	if r == nil || r.Code != "http_400" || !strings.Contains(strings.ToLower(r.Message), "token") {
		return 0, false
	}
	for _, re := range outputLimitPatterns {
		m := re.FindStringSubmatch(r.Message)
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n >= minMaxOutput {
			return n, true
		}
	}
	return 0, false
}
