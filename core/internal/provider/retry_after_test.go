package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryAfterTextReadsTheWaitAGatewayWrites(t *testing.T) {
	for msg, want := range map[string]time.Duration{
		"rate_limit_error: Rate limit exceeded. Max 5 requests per minute for this account. Retry in 27s.": 27 * time.Second,
		"Please try again in 2.5 seconds": 2500 * time.Millisecond,
		"retry after 3m":                  3 * time.Minute,
		"Try again in 350ms":              350 * time.Millisecond,
		"Retry in 5":                      5 * time.Second,
		"Retry in 9999m":                  maxRetryAfter,
		"too many requests":               0,
		"retrying is not possible":        0,
		"":                                0,
	} {
		if got := retryAfterText(msg); got != want {
			t.Errorf("retryAfterText(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestRetryAfterHeaderSecondsAndDate(t *testing.T) {
	h := http.Header{}
	if retryAfterHeader(h) != 0 {
		t.Error("no header means no ask")
	}
	h.Set("Retry-After", "12")
	if got := retryAfterHeader(h); got != 12*time.Second {
		t.Errorf("seconds: %v", got)
	}
	h.Set("Retry-After", time.Now().Add(20*time.Second).UTC().Format(http.TimeFormat))
	if got := retryAfterHeader(h); got < 15*time.Second || got > 21*time.Second {
		t.Errorf("date: %v", got)
	}
	h.Set("Retry-After", "garbage")
	if retryAfterHeader(h) != 0 {
		t.Error("an unreadable header is no ask")
	}
	h.Set("Retry-After", "86400")
	if got := retryAfterHeader(h); got != maxRetryAfter {
		t.Errorf("a day must be clamped, got %v", got)
	}
}

func TestAPIErrorCarriesTheRetryAfterHeaderAndTheMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down. Retry in 27s."}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL}
	_, err := c.Complete(context.Background(), chatRequest{Model: "m"})
	var api *APIError
	if !errors.As(err, &api) {
		t.Fatalf("err = %v, want an APIError", err)
	}
	if api.RetryAfter != 7*time.Second || api.After() != 7*time.Second {
		t.Errorf("header ask = %v / %v, want 7s", api.RetryAfter, api.After())
	}
	// With no header the message is read.
	api2 := &APIError{Status: 429, Message: "Retry in 27s."}
	if api2.After() != 27*time.Second {
		t.Errorf("message ask = %v, want 27s", api2.After())
	}
}
