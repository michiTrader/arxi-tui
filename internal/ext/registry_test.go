package ext

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A minimal, well-formed registry index: one entry with a full identity, an
// HTTPS manifest_url, a description label and inline preview markdown. Every
// refusal test below is a single deviation from this, so a failure names exactly
// the field under test — the manifest_test.go discipline, ported.
const validRegistry = `{
  "version": "reg/v1",
  "entries": [
    {
      "id": "tick",
      "name": "Ticker",
      "version": "0.2.0",
      "manifest_url": "https://example.com/tick/manifest.json",
      "description": "A top-right price ticker.",
      "preview": "# Ticker\n\nStreams a sparkline."
    }
  ]
}`

// TestLoadsAValidRegistry is the positive control: the whole point of J2 is that
// a well-formed index parses and validates, so this runs first — if it fails,
// every refusal test below is measuring a validator that rejects everything.
func TestLoadsAValidRegistry(t *testing.T) {
	r, err := ParseRegistryNamed("registry.json", []byte(validRegistry))
	if err != nil {
		t.Fatalf("ParseRegistryNamed refused a well-formed index: %v; an index is data and must parse", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate refused a well-formed index: %v; J2's promise is that a good index loads", err)
	}
	if len(r.Entries) != 1 || r.Entries[0].ID != "tick" || r.Entries[0].ManifestURL != "https://example.com/tick/manifest.json" {
		t.Fatalf("parsed entries = %+v; the entry identity and manifest_url must survive the parse", r.Entries)
	}
}

// TestRefusesAnUnknownRegistryVersion is the closed-set counterfactual: the index
// schema is code on both sides, so a version the host does not recognise is
// refused, not negotiated. Run by hand, dropping the legalRegistryVersions check
// lets "reg/v99" load and the host reads a shape it does not understand as if it
// did — the exact silent-accept the closed set exists to prevent.
func TestRefusesAnUnknownRegistryVersion(t *testing.T) {
	src := strings.Replace(validRegistry, `"version": "reg/v1"`, `"version": "reg/v99"`, 1)
	r, err := ParseRegistryNamed("registry.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseRegistryNamed: %v", err)
	}
	err = r.Validate()
	if err == nil {
		t.Fatal("Validate accepted an unknown registry version; the index schema is a closed set, so an unrecognised version must be refused rather than read as if the host understood it")
	}
	if !strings.Contains(err.Error(), "reg/v1") {
		t.Fatalf("unknown-version refusal = %q; it must list the legal versions so the author knows what the host understands", err.Error())
	}
	assertAddressed(t, err)
}

// TestRefusesAMissingRegistryVersion covers the absent tag: a version is required
// so an old index that predates the field is never read as a current one — the
// same reason a manifest's protocol is required rather than defaulted.
func TestRefusesAMissingRegistryVersion(t *testing.T) {
	src := strings.Replace(validRegistry, `"version": "reg/v1",`, ``, 1)
	r, err := ParseRegistryNamed("registry.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseRegistryNamed: %v", err)
	}
	if err := r.Validate(); err == nil {
		t.Fatal("Validate accepted an index with no version; the version is a required closed-set tag, so its absence must be refused, not defaulted to the current schema")
	}
}

// TestRefusesAMalformedEntry is the per-entry identity sweep: id/name/version/
// description are each required, and a manifest_url is required, because every
// one is load-bearing for browse-and-install (identity is consent identity, the
// description is the row label, the manifest_url is the install). Each case is a
// single deletion from the valid fixture, so the refusal names the field the
// deletion removed.
func TestRefusesAMalformedEntry(t *testing.T) {
	cases := []struct {
		name string
		old  string
		want string
	}{
		{"missing id", `"id": "tick",`, "id"},
		{"missing name", `"name": "Ticker",`, "name"},
		{"missing version", `"version": "0.2.0",`, "version"},
		{"missing manifest_url", `"manifest_url": "https://example.com/tick/manifest.json",`, "manifest_url"},
		{"missing description", `"description": "A top-right price ticker.",`, "description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(validRegistry, tc.old, ``, 1)
			r, err := ParseRegistryNamed("registry.json", []byte(src))
			if err != nil {
				t.Fatalf("ParseRegistryNamed: %v", err)
			}
			err = r.Validate()
			if err == nil {
				t.Fatalf("Validate accepted an entry with no %s; that field is load-bearing for browse-and-install, so its absence must be refused", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("entry refusal = %q; it must name the missing field %q so the author knows what to add", err.Error(), tc.want)
			}
			assertAddressed(t, err)
		})
	}
}

// TestRefusesABadEntryID is the id-grammar counterfactual: an entry id must be a
// legal identifier because it is the same id the fetched manifest carries — a
// bind namespace and consent key — so an index that lists a malformed id would
// name a plugin that could never load.
func TestRefusesABadEntryID(t *testing.T) {
	src := strings.Replace(validRegistry, `"id": "tick"`, `"id": "Tick!"`, 1)
	r, err := ParseRegistryNamed("registry.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseRegistryNamed: %v", err)
	}
	err = r.Validate()
	if err == nil {
		t.Fatal("Validate accepted an entry whose id is not a legal identifier; the id is the manifest's bind namespace and consent key, so a malformed one must be refused")
	}
	assertAddressed(t, err)
}

// TestRefusesANonHTTPSManifestURL is the security counterfactual, in both wrong
// directions: a plaintext http:// url invites a man-in-the-middle to swap the
// manifest between browse and install, and a file:// url lets a public index name
// a path on the user's own disk. Run by hand, deleting the scheme check in
// validateManifestURL lets both load — the exact downgrade the design's security
// note names.
func TestRefusesANonHTTPSManifestURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"plaintext http", "http://example.com/tick/manifest.json"},
		{"local file", "file:///etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(validRegistry, "https://example.com/tick/manifest.json", tc.url, 1)
			r, err := ParseRegistryNamed("registry.json", []byte(src))
			if err != nil {
				t.Fatalf("ParseRegistryNamed: %v", err)
			}
			err = r.Validate()
			if err == nil {
				t.Fatalf("Validate accepted manifest_url %q; a non-HTTPS url lets a public index redirect the install to swapped code or a local path, so it must be refused", tc.url)
			}
			if !strings.Contains(err.Error(), "HTTPS") {
				t.Fatalf("non-HTTPS refusal = %q; it must explain the scheme is the reason so the author uses https://", err.Error())
			}
			assertAddressed(t, err)
		})
	}
}

// TestPreviewIsOptional pins the one field validateEntry deliberately does not
// require: an entry with no preview markdown renders an empty preview pane, a thin
// browse experience rather than a broken one. The counterfactual is the paired
// deletion — description removed above is refused, preview removed here is not —
// which is what proves the distinction is intentional and not an oversight.
func TestPreviewIsOptional(t *testing.T) {
	src := strings.Replace(validRegistry, `,
      "preview": "# Ticker\n\nStreams a sparkline."`, ``, 1)
	r, err := ParseRegistryNamed("registry.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseRegistryNamed: %v", err)
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("Validate refused an entry with no preview: %v; preview is optional — its absence is a thin browse view, not a broken index", err)
	}
}

// TestFetchRegistryReadsAndValidatesOverHTTPS is the positive fetch control: a
// GET of a static HTTPS file, parsed and validated. It uses httptest.NewTLSServer
// and its own trusting client through the injected seam, so it proves the wiring
// without reaching the real network and without loosening the HTTPS-only rule.
func TestFetchRegistryReadsAndValidatesOverHTTPS(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(validRegistry))
	}))
	defer ts.Close()
	r, err := fetchRegistry(ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("fetchRegistry refused a well-formed HTTPS index: %v; a GET of a good static file must load", err)
	}
	if len(r.Entries) != 1 || r.Entries[0].ID != "tick" {
		t.Fatalf("fetched entries = %+v; the fetched bytes must parse to the same index", r.Entries)
	}
}

// TestFetchRegistryRefusesNonHTTPS proves the index url itself is HTTPS-only: a
// plaintext fetch of the discovery file is the first place a man-in-the-middle
// could swap every entry at once. The check fires before any network, so this
// test dials nothing.
func TestFetchRegistryRefusesNonHTTPS(t *testing.T) {
	_, err := FetchRegistry("http://example.com/registry.json")
	if err == nil {
		t.Fatal("FetchRegistry accepted an http:// index url; a plaintext fetch of the discovery file lets a man-in-the-middle swap every entry at once, so it must be refused")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("non-HTTPS index refusal = %q; it must name the scheme as the reason", err.Error())
	}
}

// TestFetchRegistryRefusesNon200 covers a moved or missing index: the file is
// static, so anything but 200 is a dead link, not a negotiation to retry.
func TestFetchRegistryRefusesNon200(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer ts.Close()
	if _, err := fetchRegistry(ts.Client(), ts.URL); err == nil {
		t.Fatal("fetchRegistry accepted a 404 response; a non-200 for a static index means it is missing or moved and must be an error, not an empty index")
	}
}

// TestFetchRegistryValidatesTheFetchedIndex proves the fetch path runs Validate,
// not just Parse: a syntactically valid but schema-invalid index (unknown
// version) fetched over HTTPS is still refused, and the refusal carries the
// source URL as its address so the user can open the file it came from.
func TestFetchRegistryValidatesTheFetchedIndex(t *testing.T) {
	bad := strings.Replace(validRegistry, `"version": "reg/v1"`, `"version": "reg/v99"`, 1)
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(bad))
	}))
	defer ts.Close()
	_, err := fetchRegistry(ts.Client(), ts.URL)
	if err == nil {
		t.Fatal("fetchRegistry accepted an index with an unknown version; the fetch path must run Validate, not just Parse, or a hostile index bypasses every schema refusal by being served rather than read from disk")
	}
	if !strings.Contains(err.Error(), ts.URL) {
		t.Fatalf("fetched-index refusal = %q; it must carry the source url %q as its address", err.Error(), ts.URL)
	}
}
