package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const spaPage = `<!doctype html><html lang="en"><head><meta charset="UTF-8" />` +
	`<title>Vyce AI - Affordable AI API Proxy</title></head><body><div id="root"></div></body></html>`

const cfPage = `<!DOCTYPE html><!--[if lt IE 7]> <html class="no-js ie6 oldie"> <![endif]--><html>` +
	`<head><title>Attention Required! | Cloudflare</title></head><body>blocked</body></html>`

func serve(t *testing.T, status int, ctype, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WEBREPLY_KEY", "k")
	return &Client{BaseURL: srv.URL + "/app", APIKeyEnv: "WEBREPLY_KEY", HTTP: srv.Client()}
}

func TestAWebsiteReplyNamesTheBaseURLAndNotTheMarkup(t *testing.T) {
	c := serve(t, 200, "text/html", spaPage)
	_, err := c.Complete(context.Background(), chatRequest{Model: "m"})
	if err == nil {
		t.Fatal("an HTML reply must be an error")
	}
	msg := err.Error()
	for _, want := range []string{"points at its website", "/v1", "arxi provider update", "/app/v1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message must contain %q; got: %s", want, msg)
		}
	}
	for _, bad := range []string{"<html", "<meta", "invalid character", "not JSON"} {
		if strings.Contains(msg, bad) {
			t.Errorf("the message must not repeat %q; got: %s", bad, msg)
		}
	}
	var api *APIError
	if !errors.As(err, &api) || api.Retryable() {
		t.Errorf("a website reply is an APIError that must not be retried: %v", err)
	}
}

func TestABotWallIsNamedAndIsNotBlamedOnTheConfiguration(t *testing.T) {
	c := serve(t, 403, "text/html", cfPage)
	_, err := c.Complete(context.Background(), chatRequest{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "bot-protection") ||
		!strings.Contains(err.Error(), "nothing was sent or billed") || strings.Contains(err.Error(), "<html") {
		t.Fatalf("a Cloudflare page must be named as bot protection, with no markup: %v", err)
	}
	if strings.Contains(err.Error(), "points at its website") {
		t.Fatalf("a bot wall is not a wrong base URL: %v", err)
	}
}

func TestTheStreamingAndModelListingPathsSayItToo(t *testing.T) {
	c := serve(t, 200, "text/html", spaPage)
	if _, err := c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil); err == nil ||
		!strings.Contains(err.Error(), "points at its website") {
		t.Errorf("the stream path: %v", err)
	}
	if _, err := c.ListModels(context.Background(), ""); err == nil ||
		!strings.Contains(err.Error(), "points at its website") {
		t.Errorf("the model listing: %v", err)
	}
}

// The counterfactual: a JSON error body must keep its own message, and a base
// URL that already ends in /v1 must not be told to add it.
func TestJSONErrorsAndV1URLsAreLeftAlone(t *testing.T) {
	if describeWebReply("https://h/v1/chat/completions", 401, []byte(`{"error":{"message":"bad key"}}`)) != "" {
		t.Error("JSON is not a web page")
	}
	msg := describeWebReply("https://h/v1/chat/completions", 200, []byte(spaPage))
	if strings.Contains(msg, "try https://h/v1/v1") || strings.Contains(msg, "you configured") {
		t.Errorf("must not suggest /v1 twice: %s", msg)
	}
}

func TestEveryProviderCallIsNamed(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}],"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, _ = c.Complete(context.Background(), chatRequest{Model: "m"})
	_, _ = c.CompleteStream(context.Background(), chatRequest{Model: "m"}, nil)
	_, _ = c.ListModels(context.Background(), "")
	if len(got) != 3 {
		t.Fatalf("expected 3 calls, saw %d", len(got))
	}
	for i, ua := range got {
		if !strings.HasPrefix(ua, "arxi/") {
			t.Errorf("call %d went out as %q; the default Go agent is what bot walls block", i, ua)
		}
	}
}
