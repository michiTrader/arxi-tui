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
//	ctx 34k/270k (13%) · ↑929k ↓13k · autocompact 80%
//
// ctx is what the last request held (its input, cache and answer) against the model's
// context window, with the share in brackets: a bare count says nothing about how close
// the model is to refusing, a bare share hides how big the window is believed to be. The
// upload total (↑) is the sum over every ask, so it grows far past the window and is not
// the window. When the window is not known the bar says "ctx 34k/?" -- the held size is
// real, the window is not, and no share is invented; the providers screen (/provider ->
// model -> Set context window) tells arxi the window. The autocompact part says "off"
// once there is something else on the bar, and stays quiet on a fresh screen with
// autocompact off, so the bar starts empty.
func statusUsage(ctx, in, out uint64, window int, ac autoCompactSetting) string {
	return statusUsageHeld(ctx, false, in, out, window, ac)
}

// statusUsageHeld is statusUsage, with the held size optionally marked as an estimate
// ("ctx ~3k/270k"): a conversation brought back by /resume whose log has no figure the
// provider reported is sized from its text, and the bar says so rather than show nothing
// or pass a guess off as a count.
func statusUsageHeld(ctx uint64, estimated bool, in, out uint64, window int, ac autoCompactSetting) string {
	var parts []string
	if in != 0 || out != 0 || ctx != 0 {
		held := humanTokens(ctx)
		if estimated {
			held = "~" + held
		}
		share := ""
		size := "?"
		if window > 0 {
			size = humanTokens(uint64(window))
			if p := contextPercent(ctx, window); p > 0 {
				share = fmt.Sprintf(" (%d%%)", p)
			}
		}
		parts = append(parts, fmt.Sprintf("ctx %s/%s%s · ↑%s ↓%s", held, size, share, humanTokens(in), humanTokens(out)))
	}
	switch {
	case ac.On:
		parts = append(parts, fmt.Sprintf("autocompact %d%%", clampPercent(ac.Percent)))
	case len(parts) > 0:
		parts = append(parts, "autocompact off")
	}
	return strings.Join(parts, " · ")
}
