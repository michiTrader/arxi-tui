package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/michiTrader/arxi/internal/provider"
)

// walledHost is a gateway like the one that started this: its OpenAI route answers
// every client with a firewall page, its Anthropic route works. The {} probe is
// answered like a live API answers an empty request and is not counted as a hit.
type walledHost struct {
	srv         *httptest.Server
	chat, wired atomic.Int32
}

func newWalledHost(t *testing.T) *walledHost {
	t.Helper()
	h := &walledHost{}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		probe := strings.TrimSpace(string(body)) == "{}"
		switch r.URL.Path {
		case "/v1/chat/completions":
			if !probe {
				h.chat.Add(1)
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(403)
			io.WriteString(w, "<!DOCTYPE html><html><title>Attention Required! | Cloudflare</title></html>")
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			if probe {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":{"message":"missing fields"}}`)
				return
			}
			h.wired.Add(1)
			io.WriteString(w, `{"id":"1","type":"message","role":"assistant","model":"m","stop_reason":"end_turn",
"content":[{"type":"text","text":"pong"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// realDetect swaps the test stub for the real detector.
func realDetect(t *testing.T) {
	t.Helper()
	saved := detectEndpoint
	detectEndpoint = func(base, key string) (string, string) {
		return provider.DetectEndpoint(context.Background(), base, key, nil)
	}
	t.Cleanup(func() { detectEndpoint = saved })
}

func TestAddingAWalledHostPicksTheWireThatAnswers(t *testing.T) {
	isolate(t)
	realDetect(t)
	h := newWalledHost(t)
	got, err := registerProviderWire("gate", h.srv.URL+"/v1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider.Protocol != "anthropic-messages/v1" {
		t.Fatalf("protocol = %q; the OpenAI route is walled, the Anthropic one is open", got.Provider.Protocol)
	}
}

func TestAnExplicitWireIsNeverOverriddenAndAnUnknownOneNamesTheChoices(t *testing.T) {
	isolate(t)
	realDetect(t)
	h := newWalledHost(t)
	got, err := registerProviderWire("gate", h.srv.URL+"/v1", "openai", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider.Protocol != "openai-chat-completions/v1" {
		t.Fatalf("protocol = %q; the user chose it", got.Provider.Protocol)
	}
	_, err = registerProviderWire("gate2", h.srv.URL+"/v1", "grpc", "", "")
	if err == nil || !strings.Contains(err.Error(), "openai, anthropic, auto") {
		t.Fatalf("err = %v; must list the accepted words", err)
	}
}

func TestAnAddressWithoutV1IsCompletedWhereTheAPILives(t *testing.T) {
	isolate(t)
	realDetect(t)
	h := newWalledHost(t)
	got, err := registerProviderWire("gate", h.srv.URL, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider.BaseURL != h.srv.URL+"/v1" || got.Provider.Protocol != "anthropic-messages/v1" {
		t.Fatalf("got %q %q", got.Provider.BaseURL, got.Provider.Protocol)
	}
}

func TestAProviderStoredOnTheWalledWireStillAnswersAndRemembersTheOpenOne(t *testing.T) {
	isolate(t)
	h := newWalledHost(t)
	// stored as OpenAI on purpose: this is what the TUI did before detection existed
	if _, err := registerProviderWire("gate", h.srv.URL+"/v1", "openai", "", ""); err != nil {
		t.Fatal(err)
	}
	store, err := providerStore()
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Load("gate")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.AddModel("m", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		res, err := chatSend(context.Background(), "ping", "", "", "gate/m")
		if err != nil || !strings.Contains(res.Text, "pong") {
			t.Fatalf("ask %d: %+v %v", i, res, err)
		}
	}
	if h.chat.Load() != 1 || h.wired.Load() != 2 {
		t.Fatalf("walled hits=%d open hits=%d; want 1 and 2: the second ask must go straight to the wire that worked", h.chat.Load(), h.wired.Load())
	}
}
