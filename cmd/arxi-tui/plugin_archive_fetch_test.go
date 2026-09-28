package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This file covers httpArchiveFetcher, the behavioral install path's one network
// call (DESIGN-BLOCK-I §I-I step 1). It is the same shape as the manifest fetch,
// so the guarantees under test are the same — scheme, status, and the size cap
// that keeps a hostile endpoint from harming the host before extraction and
// PackageDigest ever run — but the cap is the bundle cap, not the manifest cap,
// and these tests exercise that boundary rather than re-proving the manifest
// fetch. The bytes are never a real archive here: this layer's job is to bound
// the fetch, and whether the body is valid gzip is Installer.Extract's concern,
// proven offline in internal/ext.

// TestArchiveFetchReturnsBodyAndNamesTheURL is the positive control: a 200 with a
// body yields the bytes and the URL as the name a refusal inside the extracted
// manifest would be addressed under.
func TestArchiveFetchReturnsBodyAndNamesTheURL(t *testing.T) {
	body := []byte("\x1f\x8b\x08 pretend gzip bundle")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	f := newHTTPArchiveFetcher()
	name, data, err := f.Fetch(srv.URL)
	if err != nil {
		t.Fatalf("Fetch of a 200 endpoint failed: %v", err)
	}
	if name != srv.URL {
		t.Errorf("Fetch named the bundle %q, want the URL %q; a refusal inside the extracted manifest must name where the package came from", name, srv.URL)
	}
	if string(data) != string(body) {
		t.Errorf("Fetch returned %q, want the served body %q", data, body)
	}
}

// TestArchiveFetchRefusesANonHTTPScheme covers the one capability boundary: the
// install command takes a URL, so a `file://` path is refused rather than
// silently reading a local file, which would be a capability the command never
// advertised.
func TestArchiveFetchRefusesANonHTTPScheme(t *testing.T) {
	f := newHTTPArchiveFetcher()
	_, _, err := f.Fetch("file:///etc/passwd")
	if err == nil {
		t.Fatal("Fetch accepted a file:// URL; a plugin bundle is fetched from the network, so a local-path scheme must be refused")
	}
	if !strings.Contains(err.Error(), "scheme") && !strings.Contains(err.Error(), "http") {
		t.Errorf("the scheme refusal must say http/https is required.\n  got: %v", err)
	}
}

// TestArchiveFetchRefusesANon200 covers an endpoint that answers but not with a
// bundle: a 404 is refused with its status, so the user sees the endpoint's own
// answer rather than a gzip error on an error page's HTML.
func TestArchiveFetchRefusesANon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	f := newHTTPArchiveFetcher()
	_, _, err := f.Fetch(srv.URL)
	if err == nil {
		t.Fatal("Fetch accepted a 404; a bundle URL must return 200 with the archive body, or the fetch is refused")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the non-200 refusal must name the status so the user knows the endpoint answered.\n  got: %v", err)
	}
}

// TestArchiveFetchRefusesAnOversizeBody is the size-cap counterfactual: a body one
// byte past the cap is refused, not read into memory unbounded. It builds the
// fetcher with a tiny cap so the test need not serve 128 MiB, and serves exactly
// cap+1 bytes so the boundary itself is exercised.
func TestArchiveFetchRefusesAnOversizeBody(t *testing.T) {
	const cap = 16
	big := strings.Repeat("x", cap+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	f := &httpArchiveFetcher{client: &http.Client{Timeout: 5 * time.Second}, maxBytes: cap}
	_, _, err := f.Fetch(srv.URL)
	if err == nil {
		t.Fatal("Fetch accepted a body past the cap; an oversize response must be refused rather than read unbounded, or a hostile endpoint can exhaust memory before extraction runs")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("the oversize refusal must say the bundle exceeded the limit.\n  got: %v", err)
	}
}

// TestArchiveFetchAcceptsABodyAtExactlyTheCap is the other side of the boundary: a
// body of exactly maxBytes is legal, so the cap rejects only what is genuinely
// larger. Without this the size check could be off by one in the safe direction
// and go unnoticed — a bundle at the documented limit would be wrongly refused.
func TestArchiveFetchAcceptsABodyAtExactlyTheCap(t *testing.T) {
	const cap = 16
	exact := strings.Repeat("x", cap)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(exact))
	}))
	defer srv.Close()

	f := &httpArchiveFetcher{client: &http.Client{Timeout: 5 * time.Second}, maxBytes: cap}
	_, data, err := f.Fetch(srv.URL)
	if err != nil {
		t.Fatalf("Fetch refused a body of exactly the cap (%d bytes): %v; the limit rejects larger, not equal", cap, err)
	}
	if len(data) != cap {
		t.Errorf("Fetch returned %d bytes, want the full %d served at the cap", len(data), cap)
	}
}
