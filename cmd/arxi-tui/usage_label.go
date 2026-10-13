package main

import (
	"fmt"
	"strings"
)

// humanTokens renders a token count compactly: 850, 1.2k, 15k, 1.3M.
func humanTokens(n uint64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return trimZero(fmt.Sprintf("%.1fk", float64(n)/1000))
	case n < 999_500:
		return fmt.Sprintf("%dk", (n+500)/1000)
	default:
		return trimZero(fmt.Sprintf("%.1fM", float64(n)/1_000_000))
	}
}

// trimZero turns "2.0k" into "2k".
func trimZero(s string) string {
	if len(s) > 3 && s[len(s)-3:len(s)-1] == ".0" {
		return s[:len(s)-3] + s[len(s)-1:]
	}
	return s
}

// usageLabel is the status-bar segment: the size of the last request (what the
// model has to carry) and the session's running input/output totals. It is
// empty before the first answer so a fresh screen stays quiet. The core
// reports tokens only, so no money figure is shown.
func usageLabel(ctx, in, out uint64) string {
	if ctx == 0 && in == 0 && out == 0 {
		return ""
	}
	return fmt.Sprintf("ctx %s · ↑%s ↓%s", humanTokens(ctx), humanTokens(in), humanTokens(out))
}

// statusUsage is the usage segment with what the window and autocompact add:
//
//	ctx 37k 4% · ↑929k ↓13k · autocompact 80%
//
// The percentage is how much of the model's context window the conversation holds, and
// it is left out when the window is not known: a share of an unknown size would be a
// guess. The autocompact part says "off" once there is something else on the bar, and
// stays quiet on a fresh screen with autocompact off, so the bar starts empty.
func statusUsage(ctx, in, out uint64, window int, ac autoCompactSetting) string {
	var parts []string
	if base := usageLabel(ctx, in, out); base != "" {
		if p := contextPercent(ctx, window); p > 0 {
			base = fmt.Sprintf("ctx %s %d%% · ↑%s ↓%s", humanTokens(ctx), p, humanTokens(in), humanTokens(out))
		}
		parts = append(parts, base)
	}
	switch {
	case ac.On:
		parts = append(parts, fmt.Sprintf("autocompact %d%%", clampPercent(ac.Percent)))
	case len(parts) > 0:
		parts = append(parts, "autocompact off")
	}
	return strings.Join(parts, " · ")
}
