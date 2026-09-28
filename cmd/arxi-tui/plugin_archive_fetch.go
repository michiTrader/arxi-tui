package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// httpArchiveFetcher fetches a behavioral plugin's `.tar.gz` bundle — the one
// network call the behavioral install path makes (DESIGN-BLOCK-I §I-I step 1).
// It is httpManifestFetcher generalised from a JSON body to an archive body:
// same http/https-only, status-checked, size-capped, timed fetch, one wider
// content type. The design names it as a generalisation for exactly this reason
// — the egress class is unchanged, so the bounds that made the manifest fetch
// safe against a hostile endpoint are the bounds that make the archive fetch
// safe, and the two must not drift into different rules for the same threat.
//
// # Why a separate fetcher rather than reusing httpManifestFetcher verbatim
//
// The threat is identical but two limits legitimately differ, and each is
// wrong for the other body:
//
//   - the size cap is far larger, because a behavioral package carries a
//     compiled binary and a declarative manifest never does — the 1 MiB manifest
//     cap would reject every real bundle, and the 128 MiB bundle cap would let a
//     hostile endpoint stream 128 MiB at the declarative path that only ever
//     needs a kilobyte;
//   - the timeout is longer, because downloading a binary is a bigger transfer
//     than fetching a small JSON document, so the deadline that bounds a hung
//     endpoint has to leave room for the honest large case.
//
// Both are the *download* (compressed, over-the-wire) bound. It is deliberately
// separate from the *extraction* bound (Installer.MaxBytes, decompressed): this
// caps what a hostile endpoint can make the host read before extraction runs,
// and the extraction cap independently catches a bundle that is small on the
// wire but a decompression bomb once unpacked. Neither cap subsumes the other,
// so both are enforced.
//
// What it does NOT do is make a hostile bundle safe — extraction (the traversal,
// entry-kind and decompression-bomb refusals) and PackageDigest do that, and the
// consent gate decides whether its code ever runs. This only keeps a hostile or
// broken endpoint from harming the host before those checks get their turn.
type httpArchiveFetcher struct {
	client *http.Client
	// maxBytes caps the compressed bundle read off the wire. It mirrors the
	// extraction cap's magnitude (installMaxBytes) rather than the manifest
	// cap's: a package legitimately carries a binary, so the number that is
	// generous for a manifest would reject every real bundle.
	maxBytes int64
}

// newHTTPArchiveFetcher builds the fetcher with the host's chosen limits, set
// here in one place — the same discipline httpManifestFetcher follows — so a
// later change to the cap or the timeout is a single edit a reviewer sees rather
// than a constant defaulted at each call site.
func newHTTPArchiveFetcher() *httpArchiveFetcher {
	return &httpArchiveFetcher{
		// 120s bounds a hung endpoint while leaving room for an honest binary
		// download; the size cap is the harder bound, and the timeout only has to
		// stop a connection that never finishes.
		client:   &http.Client{Timeout: 120 * time.Second},
		maxBytes: 128 << 20, // 128 MiB, matching the extraction cap's magnitude
	}
}

// Fetch retrieves a bundle's bytes from rawURL, returning the URL as the name the
// package is addressed under so a refusal inside its manifest (read after
// extraction) still names the origin the user typed. The bytes are returned whole
// rather than streamed because the caller hands them straight to
// Installer.Extract via a bytes.Reader, and holding one bounded bundle in memory
// is simpler than threading a live HTTP body — already capped — through the
// extractor.
func (f *httpArchiveFetcher) Fetch(rawURL string) (string, []byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("/ui plugin install: %q is not a valid URL: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("/ui plugin install: %q must be an http or https URL (got scheme %q); a plugin bundle is fetched from a URL, not read from a local path", rawURL, u.Scheme)
	}

	resp, err := f.client.Get(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("/ui plugin install: could not fetch %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("/ui plugin install: %q returned HTTP %d %s; a bundle URL must return 200 with the archive body", rawURL, resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	// Read one byte past the cap so a bundle that is exactly maxBytes is accepted
	// and one larger is refused, the same off-by-one guard the manifest fetch uses
	// to tell a legal maximum from an over-large stream.
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("/ui plugin install: could not read %q: %w", rawURL, err)
	}
	if int64(len(data)) > f.maxBytes {
		return "", nil, fmt.Errorf("/ui plugin install: %q is larger than the %d-byte bundle download limit; the compressed bundle is bounded on the wire before extraction, and a stream this size is refused rather than read", rawURL, f.maxBytes)
	}
	return rawURL, data, nil
}
