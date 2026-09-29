package ext

// The community registry is J2 of Block J (DESIGN-BLOCK-J.md): a single JSON
// index committed to a git repo and fetched over HTTPS, with no server in the
// loop. It is discovery only — it names where a plugin's manifest lives, and the
// install path is the existing H6/I5 pipeline (fetch the manifest_url, Validate,
// consent gate, patch.Mount). The registry grants nothing; every entry it lists
// is attacker-controlled data from a public file, so an entry's manifest_url
// still passes the full manifest Validate and (for a behavioral entry) the Q15
// consent gate before any process spawns. The escape hatch (invariant 6) is the
// backstop for a hostile previewed scene.
//
// This file mirrors manifest.go deliberately: the same Parse/ParseNamed/Validate
// shape, the same addressed *Error, the same closed-set-because-code version
// discipline (legalRegistryVersions is to the index what legalProtocols is to a
// manifest). Reusing those pieces rather than re-spelling them is why a registry
// refusal carries file:line for free and cannot drift from a manifest refusal on
// what an address looks like.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// legalRegistryVersions is the closed set of index-schema versions the host
// recognises. Closed for the same reason legalProtocols is (manifest.go): the
// index shape is code on both sides, so an unknown version is refused at load,
// not negotiated. A future v2 is a signed addition here, never a silent accept.
var legalRegistryVersions = map[string]bool{"reg/v1": true}

// unnamedRegistry names an index parsed from bytes with no origin, mirroring
// unnamedManifest. Angle-bracketed so it can never be mistaken for a real file.
const unnamedRegistry = "<registry>"

// Registry is a parsed community index: a schema version and the entries a
// browse view lists. The address book (src/file/offsets) is kept for the same
// reason Manifest keeps it — a refusal names the key the author wrote rather than
// the file's first byte.
type Registry struct {
	Version string          `json:"version"`
	Entries []RegistryEntry `json:"entries"`

	src     []byte
	file    string
	offsets map[string]int
}

// RegistryEntry is one row of the index. id/name/version are the entry's
// identity and must equal the fetched manifest's (checked on preview/install, not
// here — this package never fetches during Validate); manifest_url is where the
// full manifest is fetched; description is the one-line list-row label; preview
// is inline markdown for the preview pane, kept in the index so the browse view
// renders with a single fetch (DESIGN-BLOCK-J.md J2, fork 2 resolved inline).
type RegistryEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	ManifestURL string `json:"manifest_url"`
	Description string `json:"description"`
	Preview     string `json:"preview,omitempty"`
}

// ParseRegistry parses an index from bytes with no origin name.
func ParseRegistry(data []byte) (*Registry, error) {
	return ParseRegistryNamed(unnamedRegistry, data)
}

// ParseRegistryNamed is ParseRegistry for an index whose origin has a name — the
// name a refusal prints. A caller that fetched the bytes passes the source URL so
// a malformed public index points the user at the file it came from. It parses
// only; call Validate to run the refusals.
func ParseRegistryNamed(name string, data []byte) (*Registry, error) {
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, jsonError(name, data, err)
	}
	r.src = data
	r.file = name
	r.offsets = topLevelKeyOffsets(data)
	return &r, nil
}

// Validate runs the J2 refusals in order: the schema version first (closed
// because code), then each entry. It returns the first refusal, each carrying
// file:line. It never fetches: an entry's manifest_url is validated for shape
// here, but the manifest it points at is fetched and Validated only on preview or
// install, so Validate stays a pure function over the index bytes and is testable
// without a network.
func (r *Registry) Validate() error {
	if r == nil {
		return nil
	}
	if r.Version == "" {
		return &Error{Loc: r.rootLoc(), Msg: "registry index has no version; the version is a closed-set schema tag the host must recognise (DESIGN-BLOCK-J.md J2) — add \"version\": \"reg/v1\""}
	}
	if !legalRegistryVersions[r.Version] {
		return &Error{Loc: r.locAt("version"), Msg: fmt.Sprintf("unknown registry version %q; the index schema is code on both sides so it is a closed set, not negotiated (DESIGN-BLOCK-J.md J2) — the legal versions are %s", r.Version, legalRegistryVersionList())}
	}
	for i, e := range r.Entries {
		if err := r.validateEntry(i, e); err != nil {
			return err
		}
	}
	return nil
}

// validateEntry refuses an entry whose identity or manifest_url is missing or
// malformed. Every field checked here is one the browse-and-install flow cannot
// do without: id/name/version are the consent identity the install gate shows and
// the values a preview cross-checks against the fetched manifest; manifest_url is
// the only thing that turns an entry into an install; description is the list-row
// label, so an entry without one is a row the user cannot read before installing.
// preview is intentionally not required — an entry with no preview markdown
// renders an empty preview pane, which is a thin browse experience, not a broken
// one.
//
// An entry refusal points at the "entries" key rather than the exact array
// element, mirroring validateMounts: the array offset is the address the index's
// top-level offsets carry, and it is enough for the user to find the file. The
// message names the entry index and its id so the offending row is unambiguous
// inside the array.
func (r *Registry) validateEntry(i int, e RegistryEntry) error {
	if e.ID == "" {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %d has no id; the id is the plugin's identity, matched against the fetched manifest's id and used as the consent key (DESIGN-BLOCK-J.md J2) — add an \"id\" matching [a-z][a-z0-9-]{0,62}", i)}
	}
	if !idPattern.MatchString(e.ID) {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %d has id %q, which is not a legal identifier; it must match [a-z][a-z0-9-]{0,62} because it is the same id the fetched manifest must carry, and a manifest id is a bind namespace and consent key", i, e.ID)}
	}
	if e.Name == "" {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %q has no name; the name is the human-facing label the consent screen shows, and consent to an unnamed plugin is consent the user could not read (DESIGN-BLOCK-J.md J2) — add a \"name\"", e.ID)}
	}
	if e.Version == "" {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %q has no version; the version is part of the consent identity (Q15), so a browse row without one cannot tell the user what a later bump re-asks consent for — add a \"version\"", e.ID)}
	}
	if e.Description == "" {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %q has no description; the description is the one-line list-row label, and a row with no label is a plugin the user installs blind (DESIGN-BLOCK-J.md J2) — add a \"description\"", e.ID)}
	}
	if err := r.validateManifestURL(e); err != nil {
		return err
	}
	return nil
}

// validateManifestURL refuses an entry whose manifest_url is absent or not an
// HTTPS URL. HTTPS-only is a security refusal, not a style choice: the index is
// attacker-controlled public data, and a plaintext http:// URL invites a
// man-in-the-middle to swap the manifest between the index the user browsed and
// the code the install gate spawns, while a file:// or other scheme would let a
// public index name a path on the user's own disk. The scheme check here is the
// index-time half; FetchRegistry re-checks the top-level index URL for the same
// reason, so neither the index nor the thing that fetches it can introduce a
// downgrade the other trusts.
func (r *Registry) validateManifestURL(e RegistryEntry) error {
	if e.ManifestURL == "" {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %q has no manifest_url; the manifest_url is the only field that turns an entry into an install, fetched and Validated through the same pipeline as /ui plugin add (DESIGN-BLOCK-J.md J2) — add a \"manifest_url\"", e.ID)}
	}
	u, err := url.Parse(e.ManifestURL)
	if err != nil {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %q has a malformed manifest_url %q: %v", e.ID, e.ManifestURL, err)}
	}
	if u.Scheme != "https" {
		return &Error{Loc: r.locAt("entries"), Msg: fmt.Sprintf("registry entry %q has manifest_url %q with scheme %q; a manifest is fetched over HTTPS only, because a public index is attacker-controlled data and a plaintext or file:// URL would let it redirect the install to swapped code or a local path (DESIGN-BLOCK-J.md J2 security note) — use an https:// URL", e.ID, e.ManifestURL, u.Scheme)}
	}
	return nil
}

// FetchRegistry fetches an index over HTTPS, parses and validates it. It is the
// one network entry point J2 adds, and the only place the "no servers, a raw file
// URL" claim of DESIGN-BLOCK-J.md J2 becomes concrete: a GET of a static file,
// nothing negotiated. Network and HTTP errors are returned bare rather than as an
// addressed *Error — like LoadFile's os.ReadFile error, a URL the user typed has
// no file:line to carry, and the message names the URL instead. Document refusals
// (a malformed or invalid index) still carry their address, because ParseRegistry
// and Validate produce them from the fetched bytes.
func FetchRegistry(rawURL string) (*Registry, error) {
	return fetchRegistry(http.DefaultClient, rawURL)
}

// FetchRegistryWithClient is FetchRegistry with the HTTP client supplied by the
// caller, so the host can bound the fetch with a timeout the way its manifest and
// archive fetchers bound theirs (newHTTPManifestFetcher's 15s client). The
// HTTPS-only rule and every document refusal are unchanged: the client decides
// only the transport and its timeout, never what index is accepted. It exists
// because the loop opens the installer on a worker goroutine and an unbounded
// DefaultClient could leave that goroutine hung on a slow-loris registry for the
// life of the session.
func FetchRegistryWithClient(client *http.Client, rawURL string) (*Registry, error) {
	return fetchRegistry(client, rawURL)
}

// fetchRegistry is FetchRegistry with the HTTP client injected, so a test drives
// it against httptest.NewTLSServer's client without reaching the real network and
// without loosening the HTTPS-only rule the production path enforces.
func fetchRegistry(client *http.Client, rawURL string) (*Registry, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("registry url %q is malformed: %w", rawURL, err)
	}
	// HTTPS-only, the same refusal validateManifestURL makes one level down: the
	// index itself is fetched from a URL, and a plaintext fetch of the discovery
	// file is the first place a man-in-the-middle could swap every entry at once.
	if u.Scheme != "https" {
		return nil, fmt.Errorf("registry url %q has scheme %q; the index is fetched over HTTPS only, because a plaintext fetch of the discovery file lets a man-in-the-middle swap every entry at once (DESIGN-BLOCK-J.md J2 security note) — use an https:// URL", rawURL, u.Scheme)
	}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetching registry %q: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching registry %q: server returned %s; the index is a static file, so anything but 200 means it is missing or moved, not a negotiation to retry", rawURL, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading registry %q: %w", rawURL, err)
	}
	reg, err := ParseRegistryNamed(rawURL, body)
	if err != nil {
		return nil, err
	}
	if err := reg.Validate(); err != nil {
		return nil, err
	}
	return reg, nil
}

// rootLoc is the index's own top-level address, for a refusal about the index as
// a whole (a missing version) rather than about one key.
func (r *Registry) rootLoc() scene.Loc {
	return scene.Loc{File: r.name()}
}

// name returns the index's origin as it appears in errors.
func (r *Registry) name() string {
	if r == nil || r.file == "" {
		return unnamedRegistry
	}
	return r.file
}

// locAt resolves the address of a top-level index key, degrading to the file-only
// address when the key was not recorded — the same contract Manifest.locAt keeps.
func (r *Registry) locAt(key string) scene.Loc {
	if off, ok := r.offsets[key]; ok {
		line, col := position(r.src, off)
		return scene.Loc{File: r.file, Line: line, Col: col}
	}
	return scene.Loc{File: r.file}
}

// legalRegistryVersionList renders the closed version set for an error message,
// sorted so the message is stable across runs.
func legalRegistryVersionList() string {
	out := make([]string, 0, len(legalRegistryVersions))
	for v := range legalRegistryVersions {
		out = append(out, v)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// FilterEntries returns the index entries whose name or description contains the
// query as a case-insensitive substring; an empty query returns every entry (the
// browse view is open but unfiltered). It is the pure computational core of the
// J3 search-filter follow-up — the function the host loop will call on every
// keystroke to recompute the community browse list — kept here, over the parsed
// index, exactly as fold.FilterSlashMatches is kept over the command registry it
// filters. Signing the `community.*` view-state bind, the fold field that
// carries the result, and the keystroke loop that calls this stay the deferred
// live half (DESIGN-BLOCK-J.md J3 follow-up); this half is pure and testable
// without any of it, which is why it lands first.
//
// It is a faithful port of FilterSlashMatches (empty→all, case-insensitive
// substring, a nil slice when nothing matches so an empty result and "not yet
// filtered" never collapse) with one domain adaptation recorded here: the slash
// menu matches on the command Name alone because a command is chosen by name,
// but a registry browse is a search over the two human-readable fields the
// installer card actually renders — the plugin's name and its one-line
// description — so a user who recalls "streams prices" but not the id "tick"
// still finds the row. The id is intentionally not matched: it is an internal
// namespace token, not text the user reads off the card, so matching it would
// surface rows on a substring the browse view never shows them.
func (r *Registry) FilterEntries(query string) []RegistryEntry {
	if r == nil {
		return nil
	}
	if query == "" {
		return r.Entries
	}
	q := strings.ToLower(query)
	var out []RegistryEntry
	for _, e := range r.Entries {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Description), q) {
			out = append(out, e)
		}
	}
	return out
}
