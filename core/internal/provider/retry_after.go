package provider

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfter bounds what a provider may ask for. A header of "86400" is a
// provider telling the caller to come back tomorrow; waiting that long in a chat
// is not a retry, it is a hang, so a longer ask is treated as a refusal to wait.
const maxRetryAfter = 10 * time.Minute

// retryAfterHeader reads the Retry-After header: whole seconds, or an HTTP date.
// It returns zero when the header is absent or unreadable.
func retryAfterHeader(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		if secs <= 0 {
			return 0
		}
		return clampRetryAfter(time.Duration(secs * float64(time.Second)))
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := time.Until(at); d > 0 {
			return clampRetryAfter(d)
		}
	}
	return 0
}

// retryInText finds the wait a gateway wrote into its message: "Retry in 27s",
// "try again in 2.5 seconds", "retry after 3m". Gateways that ship no header say it
// only here.
var retryInText = regexp.MustCompile(`(?i)(?:retry|try again)\s*(?:in|after)\s*(\d+(?:\.\d+)?)\s*(ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?)?\b`)

func retryAfterText(msg string) time.Duration {
	m := retryInText.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil || n <= 0 {
		return 0
	}
	unit := time.Second
	switch strings.ToLower(m[2]) {
	case "ms", "millisecond", "milliseconds":
		unit = time.Millisecond
	case "m", "min", "mins", "minute", "minutes":
		unit = time.Minute
	}
	return clampRetryAfter(time.Duration(n * float64(unit)))
}

func clampRetryAfter(d time.Duration) time.Duration {
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
