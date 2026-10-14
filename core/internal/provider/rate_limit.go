package provider

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimit is what a provider says about its request budget in the headers of an
// answer: how many requests are left in the current window and how long until the
// window opens again. It is read from successful answers too, which is the point:
// a caller that knows the budget is spent can wait for the window instead of
// spending a request on a 429.
type RateLimit struct {
	// Remaining is the requests still allowed in the window; only meaningful when
	// Known is true (a header that is absent is not the same as zero).
	Remaining int
	Known     bool
	// Reset is how long until the window opens again, zero when unknown.
	Reset time.Duration
}

// Spent reports whether the budget is used up and the provider said when it comes
// back, i.e. the next request would be refused and there is a time to wait for.
func (r RateLimit) Spent() bool { return r.Known && r.Remaining <= 0 && r.Reset > 0 }

// maxRateReset bounds what a header may ask us to wait for; it matches the ceiling
// on Retry-After, for the same reason.
const maxRateReset = maxRetryAfter

// rateLimitFrom reads the common rate-limit headers: the de-facto
// X-RateLimit-Remaining / X-RateLimit-Reset, the IETF RateLimit-* draft, and the
// per-requests variants OpenAI sends (x-ratelimit-remaining-requests,
// x-ratelimit-reset-requests). The first header of each kind that parses wins.
// The second return is false when the answer says nothing usable about requests.
func rateLimitFrom(h http.Header, now time.Time) (RateLimit, bool) {
	var out RateLimit
	for _, k := range []string{"X-Ratelimit-Remaining", "Ratelimit-Remaining", "X-Ratelimit-Remaining-Requests"} {
		v := strings.TrimSpace(h.Get(k))
		if v == "" {
			continue
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			continue
		}
		out.Remaining, out.Known = int(n), true
		break
	}
	for _, k := range []string{"X-Ratelimit-Reset", "Ratelimit-Reset", "X-Ratelimit-Reset-Requests"} {
		if d, ok := resetWait(h.Get(k), now); ok {
			out.Reset = d
			break
		}
	}
	if !out.Known {
		return RateLimit{}, false
	}
	return out, true
}

// resetWait turns a reset header into a wait. Providers disagree on what it is:
// a unix time in seconds (1718000000) or milliseconds, a number of seconds to go
// (59), or a Go-style duration ("1m30s", "250ms"). Anything else reads as unknown.
func resetWait(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	var d time.Duration
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		switch {
		case math.IsNaN(n) || math.IsInf(n, 0) || n < 0:
			return 0, false
		case n >= 1e11: // unix milliseconds
			d = time.UnixMilli(int64(n)).Sub(now)
		case n >= 1e8: // unix seconds
			d = time.Unix(int64(n), 0).Sub(now)
		default: // seconds to go
			d = time.Duration(n * float64(time.Second))
		}
	} else if pd, err := time.ParseDuration(v); err == nil {
		d = pd
	} else {
		return 0, false
	}
	if d < 0 {
		d = 0
	}
	if d > maxRateReset {
		d = maxRateReset
	}
	return d, true
}

// noteRateLimit tells OnRateLimit what an answer's headers say about the budget.
func (c *Client) noteRateLimit(h http.Header) {
	if c.OnRateLimit == nil {
		return
	}
	if rl, ok := rateLimitFrom(h, time.Now()); ok {
		c.OnRateLimit(rl)
	}
}
