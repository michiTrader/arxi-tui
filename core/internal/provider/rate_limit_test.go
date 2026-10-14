package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

func TestRateLimitFromReadsTheCommonHeaders(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := []struct {
		name      string
		h         http.Header
		known     bool
		remaining int
		reset     time.Duration
	}{
		{"unix seconds", hdr("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "1800000042"), true, 0, 42 * time.Second},
		{"unix milliseconds", hdr("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "1800000007000"), true, 0, 7 * time.Second},
		{"seconds to go", hdr("RateLimit-Remaining", "2", "RateLimit-Reset", "59"), true, 2, 59 * time.Second},
		{"openai duration", hdr("x-ratelimit-remaining-requests", "0", "x-ratelimit-reset-requests", "1m30s"), true, 0, 90 * time.Second},
		{"reset in the past", hdr("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "1799999000"), true, 0, 0},
		{"clamped", hdr("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "99999999"), true, 0, maxRateReset},
		{"no remaining", hdr("X-RateLimit-Reset", "30"), false, 0, 0},
		{"garbage remaining", hdr("X-RateLimit-Remaining", "lots"), false, 0, 0},
		{"garbage reset", hdr("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "soon"), true, 0, 0},
		{"none", http.Header{}, false, 0, 0},
	}
	for _, c := range cases {
		got, ok := rateLimitFrom(c.h, now)
		if ok != c.known || (ok && (got.Remaining != c.remaining || got.Reset != c.reset)) {
			t.Errorf("%s: got %+v ok=%v, want remaining=%d reset=%v known=%v", c.name, got, ok, c.remaining, c.reset, c.known)
		}
	}
}

func TestSpentNeedsAnEmptyBudgetAndAWindowToWaitFor(t *testing.T) {
	for _, c := range []struct {
		rl   RateLimit
		want bool
	}{
		{RateLimit{Known: true, Remaining: 0, Reset: time.Second}, true},
		{RateLimit{Known: true, Remaining: 1, Reset: time.Second}, false},
		{RateLimit{Known: true, Remaining: 0}, false},
		{RateLimit{Remaining: 0, Reset: time.Second}, false},
	} {
		if got := c.rl.Spent(); got != c.want {
			t.Errorf("%+v Spent() = %v, want %v", c.rl, got, c.want)
		}
	}
}

func TestEveryClientPathReportsTheRateLimitHeaders(t *testing.T) {
	for _, status := range []int{200, 429} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "12")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			w.Write([]byte(`{"id":"x","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}],"content":[{"type":"text","text":"hi"}],"type":"message","role":"assistant","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		}))
		var got []RateLimit
		c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Getenv: func(string) string { return "" },
			OnRateLimit: func(r RateLimit) { got = append(got, r) }}
		c.Complete(context.Background(), chatRequest{Model: "m"})
		c.CompleteStream(context.Background(), chatRequest{Model: "m"}, func(string) {})
		c.CompleteAnthropic(context.Background(), anthropicRequest{Model: "m"})
		srv.Close()
		if len(got) != 3 {
			t.Fatalf("status %d: %d reports for 3 calls", status, len(got))
		}
		for _, r := range got {
			if !r.Spent() || r.Reset != 12*time.Second {
				t.Errorf("status %d: report %+v", status, r)
			}
		}
	}
}
