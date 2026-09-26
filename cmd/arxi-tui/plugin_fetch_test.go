package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This file covers the host half of H6: httpManifestFetcher, the one real
// network call the /ui surface makes. The parse/mount side is proven offline in
// internal/patch; here the concern is the fetch's own guarantees — scheme,
// status, and the size cap that keeps a hostile endpoint from harming the loop
// before the manifest validators ever run.

// TestFetchReturnsBodyAndNamesTheURL is the positive control: a 200 with a body
// yields the bytes and the URL as the name a refusal would be addressed under.
func TestFetchReturnsBodyAndNamesTheURL(t *testing.T) {
	const body = `{"id":"probe"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := newHTTPManifestFetcher()
	name, data, err := f.Fetch(srv.URL)
	if err != nil {
		t.Fatalf("Fetch of a 200 endpoint failed: %v", err)
	}
	if name != srv.URL {
		t.Errorf("Fetch named the manifest %q, want the URL %q; a refusal inside a fetched manifest must name where it came from", name, srv.URL)
	}
	if string(data) != body {
		t.Errorf("Fetch returned %q, want the served body %q", data, body)
	}
}

// TestFetchRefusesANonHTTPScheme covers the one capability boundary the fetch
// draws: `/ui plugin add` takes a URL, so a `file://` path is refused rather than
// silently reading a local file, which would be a different capability the
// command never advertised.
func TestFetchRefusesANonHTTPScheme(t *testing.T) {
	f := newHTTPManifestFetcher()
	_, _, err := f.Fetch("file:///etc/passwd")
	if err == nil {
		t.Fatal("Fetch accepted a file:// URL; `/ui plugin add` fetches from the network, so a local-path scheme must be refused")
	}
	if !strings.Contains(err.Error(), "scheme") && !strings.Contains(err.Error(), "http") {
		t.Errorf("the scheme refusal must say http/https is required.\n  got: %v", err)
	}
}

// TestFetchRefusesANon200 covers an endpoint that answers but not with a
// manifest: a 404 is refused with its status, so the user sees the endpoint's
// own answer rather than a parse error on an error page's HTML.
func TestFetchRefusesANon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	f := newHTTPManifestFetcher()
	_, _, err := f.Fetch(srv.URL)
	if err == nil {
		t.Fatal("Fetch accepted a 404; a manifest URL must return 200 with the manifest body, or the fetch is refused")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the non-200 refusal must name the status so the user knows the endpoint answered.\n  got: %v", err)
	}
}

// TestFetchRefusesAnOversizeBody is the size-cap counterfactual: a body one byte
// past the cap is refused, not truncated into a manifest the author never wrote.
// It builds the fetcher directly with a tiny cap so the test need not serve a
// megabyte, and serves exactly cap+1 bytes so the boundary itself is exercised.
func TestFetchRefusesAnOversizeBody(t *testing.T) {
	const cap = 16
	big := strings.Repeat("x", cap+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	f := &httpManifestFetcher{client: &http.Client{Timeout: 5 * time.Second}, maxBytes: cap}
	_, _, err := f.Fetch(srv.URL)
	if err == nil {
		t.Fatal("Fetch accepted a body past the cap; an oversize response must be refused rather than truncated, or the parsed manifest is not what the endpoint sent")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("the oversize refusal must say the manifest exceeded the limit.\n  got: %v", err)
	}
}

// TestFetchAcceptsABodyAtExactlyTheCap is the other side of the boundary: a body
// of exactly maxBytes is legal, so the cap rejects only what is genuinely larger.
// Without this the size check could be off by one in the safe direction and go
// unnoticed — a manifest at the documented limit would be wrongly refused.
func TestFetchAcceptsABodyAtExactlyTheCap(t *testing.T) {
	const cap = 16
	exact := strings.Repeat("x", cap)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(exact))
	}))
	defer srv.Close()

	f := &httpManifestFetcher{client: &http.Client{Timeout: 5 * time.Second}, maxBytes: cap}
	_, data, err := f.Fetch(srv.URL)
	if err != nil {
		t.Fatalf("Fetch refused a body of exactly the cap (%d bytes): %v; the limit rejects larger, not equal", cap, err)
	}
	if len(data) != cap {
		t.Errorf("Fetch returned %d bytes, want the full %d served at the cap", len(data), cap)
	}
}
