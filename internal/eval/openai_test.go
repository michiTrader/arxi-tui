package eval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A 504 that clears on a later attempt is the measured vyceai incident: the
// gateway times out on the big scene-patch prompts intermittently, so a run
// that retries rides it out while a single-shot run reports model_error.
func TestPatchRetriesTransientThenSucceeds(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			http.Error(w, "upstream timed out", http.StatusGatewayTimeout)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()

	m := &OpenAIModel{
		APIKey:      "test",
		BaseURL:     srv.URL,
		Model:       "scripted",
		Client:      srv.Client(),
		MaxAttempts: 5,
		Backoff:     func(int) time.Duration { return 0 }, // do not sleep in a test
	}

	body, err := m.Patch(context.Background(), PatchRequest{Order: "x", Base: []byte("{}")})
	if err != nil {
		t.Fatalf("Patch after transient 504s: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != `{"ok":true}` {
		t.Fatalf("body = %q, want the model's document", got)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (two 504s then success)", calls)
	}
}

// A 400 is the operator's answer (bad key, bad model), not transport. Retrying
// it would hide a configuration mistake behind a delay, so it must surface on
// the first attempt.
func TestPatchDoesNotRetryClientError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, `{"error":{"message":"insufficient_user_quota"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	m := &OpenAIModel{
		APIKey:      "test",
		BaseURL:     srv.URL,
		Model:       "scripted",
		Client:      srv.Client(),
		MaxAttempts: 5,
		Backoff:     func(int) time.Duration { return 0 },
	}

	_, err := m.Patch(context.Background(), PatchRequest{Order: "x", Base: []byte("{}")})
	if err == nil {
		t.Fatal("Patch on 400: want error, got nil")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (a 400 is not retried)", calls)
	}
}

// Exhausting the budget reports what the gateway said, not a bare attempt
// count, so an operator reads the proxy's own message.
func TestPatchExhaustsOnPersistentTransient(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "upstream timed out", http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	m := &OpenAIModel{
		APIKey:      "test",
		BaseURL:     srv.URL,
		Model:       "scripted",
		Client:      srv.Client(),
		MaxAttempts: 3,
		Backoff:     func(int) time.Duration { return 0 },
	}

	_, err := m.Patch(context.Background(), PatchRequest{Order: "x", Base: []byte("{}")})
	if err == nil {
		t.Fatal("Patch on persistent 504: want error, got nil")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (the full budget)", calls)
	}
	if !strings.Contains(err.Error(), "504") {
		t.Fatalf("error %q should carry the gateway status", err.Error())
	}
}
