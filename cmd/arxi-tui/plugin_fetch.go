package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// httpManifestFetcher is the host's implementation of patch.Fetcher: the one
// real network call the /ui surface makes, for `/ui plugin add <url>` (H6).
//
// # Why the fetch lives here and not in internal/patch
//
// internal/patch is a pure source-to-source transform everywhere else, and
// internal/eval already set the precedent that the only real HTTP client lives
// at the edge, not in the shared logic — that is what keeps the mutation
// surface and its tests provable offline. patch.Fetcher is the seam: the loop
// injects this, a test injects a fake, and neither the parser nor the mounter
// ever imports net/http.
//
// # What it refuses, and why the refusals are not paranoia
//
// A declarative manifest runs zero code (that is the whole H2 guarantee), so a
// fetched manifest is data the scene and token validators check before a byte
// of it mounts. But the fetch itself is network egress to a URL the user typed,
// so this bounds it:
//
//   - scheme must be http or https — a `file://` or bare path is refused,
//     because `/ui plugin add` is documented as taking a URL and a silent local
//     read would be a different, unexpected capability;
//   - the response is read under a size cap, so a manifest that is actually a
//     multi-gigabyte stream cannot exhaust memory before validation rejects it;
//   - the client carries a timeout, so a hung endpoint cannot wedge the loop.
//
// None of these makes a hostile *declarative* manifest dangerous — validation
// does that — they keep a hostile or broken *endpoint* from harming the host
// before validation runs.
type httpManifestFetcher struct {
	client *http.Client
	// maxBytes caps the manifest body. A declarative manifest is a small JSON
	// document (Scene 6's ticker is well under a kilobyte); the cap is generous
	// enough for a large legitimate manifest and small enough that a stream
	// disguised as one is cut off long before it matters.
	maxBytes int64
}

// newHTTPManifestFetcher builds the fetcher with the host's chosen limits. They
// are set here, in one place, rather than defaulted at each use so a later
// change to the timeout or the cap is a single edit the reviewer sees.
func newHTTPManifestFetcher() *httpManifestFetcher {
	return &httpManifestFetcher{
		client:   &http.Client{Timeout: 15 * time.Second},
		maxBytes: 1 << 20, // 1 MiB
	}
}

// Fetch retrieves a manifest's bytes from rawURL, returning the URL as the name
// the manifest is addressed under so a refusal inside it still names its origin.
func (f *httpManifestFetcher) Fetch(rawURL string) (string, []byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("/ui plugin add: %q is not a valid URL: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", nil, fmt.Errorf("/ui plugin add: %q must be an http or https URL (got scheme %q); a plugin is fetched from a URL, not read from a local path", rawURL, u.Scheme)
	}

	resp, err := f.client.Get(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("/ui plugin add: could not fetch %q: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("/ui plugin add: %q returned HTTP %d %s; a manifest URL must return 200 with the manifest body", rawURL, resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	// Read one byte past the cap so a body that is exactly maxBytes is accepted
	// and one larger is refused, rather than silently truncated into a manifest
	// that parses to something the author did not write.
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return "", nil, fmt.Errorf("/ui plugin add: could not read %q: %w", rawURL, err)
	}
	if int64(len(data)) > f.maxBytes {
		return "", nil, fmt.Errorf("/ui plugin add: %q is larger than the %d-byte manifest limit; a declarative manifest is a small JSON document, and a stream this size is refused rather than parsed", rawURL, f.maxBytes)
	}
	return rawURL, data, nil
}
