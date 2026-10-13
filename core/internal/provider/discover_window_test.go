package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func listingClient(t *testing.T, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Provider: "p", Secret: func(string) (string, bool, error) { return "", false, nil }}
}

func windows(t *testing.T, body string) map[string]int {
	t.Helper()
	infos, err := listingClient(t, body).ListModelInfo(context.Background(), "openai-chat-completions/v1")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, m := range infos {
		out[m.ID] = m.ContextWindow
	}
	return out
}

func TestListModelInfoReadsTheWindowUnderEachNameInUse(t *testing.T) {
	got := windows(t, `{"data":[
		{"id":"a","max_input_tokens":1000000},
		{"id":"b","context_length":128000},
		{"id":"c","context_window":200000},
		{"id":"d","max_model_len":32768},
		{"id":"e","context_length":"262144"},
		{"id":"none"}],
		"models":[{"name":"models/g","inputTokenLimit":1048576}]}`)
	want := map[string]int{"a": 1_000_000, "b": 128_000, "c": 200_000, "d": 32_768, "e": 262_144, "none": 0, "g": 1_048_576}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s = %d; want %d", id, got[id], w)
		}
	}
}

func TestListModelInfoIgnoresWindowsThatAreNotWindows(t *testing.T) {
	got := windows(t, `{"data":[
		{"id":"tiny","context_length":12},
		{"id":"huge","context_length":99999999999},
		{"id":"frac","context_length":1000.5},
		{"id":"text","context_length":"lots"},
		{"id":"neg","context_length":-5}]}`)
	for id, w := range got {
		if w != 0 {
			t.Errorf("%s = %d; an implausible window must read as unknown", id, w)
		}
	}
}
