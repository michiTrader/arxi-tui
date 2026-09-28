package ext

// A bundle is J4 of Block J (DESIGN-BLOCK-J.md): scene + theme + plugins under
// one manifest, shared as a single document and installed behind one consent
// screen. This file is the pure, network-free core — the parser and validator —
// landed first the way J2's ParseRegistry and J3's InstallerScene were, with the
// live wiring (fetch each referenced manifest, aggregate the Q15 identity, show
// the one consent screen, then theme.Merge + patch.Mount on a single grant) the
// follow-up increment.
//
// It mirrors registry.go and manifest.go deliberately: the same Parse/ParseNamed/
// Validate shape, the same addressed *Error, the same closed-set-because-code
// version discipline (legalBundleVersions is to a bundle what legalRegistryVersions
// is to an index and legalProtocols is to a manifest). Reusing those pieces rather
// than re-spelling them is why a bundle refusal carries file:line for free and
// cannot drift from a manifest refusal on what an address looks like.
//
// Like registry.go, Validate NEVER fetches. It checks the bundle's own shape and
// that its embedded scene parses and its embedded theme block is well-formed; the
// referenced plugins' manifests — and the bundle scene's binds into those plugins'
// namespaces, which cannot be resolved without them — are fetched and Validated at
// mount, exactly as the registry validates an entry's shape and defers the fetched
// manifest to install time.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// legalBundleVersions is the closed set of bundle-schema versions the host
// recognises. Closed for the same reason legalRegistryVersions and legalProtocols
// are: the bundle shape is code on both sides, so an unknown version is refused at
// load, not negotiated. A future v2 is a signed addition here, never a silent
// accept.
var legalBundleVersions = map[string]bool{"bundle/v1": true}

// unnamedBundle names a bundle parsed from bytes with no origin, mirroring
// unnamedManifest and unnamedRegistry. Angle-bracketed so it can never be mistaken
// for a real file.
const unnamedBundle = "<bundle>"

// Bundle is a parsed share: a schema version, the human-facing identity the one
// consent screen shows, and the three optional contributions (an embedded scene,
// an embedded theme token block, and plugin references). The address book
// (src/file/offsets) is kept for the same reason Manifest and Registry keep it — a
// refusal names the key the author wrote rather than the file's first byte.
type Bundle struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description string `json:"description"`

	// Scene is the interface the bundle ships: a full scene document (a "root"
	// node tree), kept raw so it is handed to the scene parser verbatim rather
	// than re-modelled here. Theme is an embedded token block, exactly the shape a
	// manifest's `tokens` block and a theme file have, kept raw for the same reason
	// and validated through the one token validator (theme.LoadBytes).
	Scene json.RawMessage `json:"scene,omitempty"`
	Theme json.RawMessage `json:"theme,omitempty"`

	// Plugins are discovery, not embedded code: each names where a plugin's
	// manifest is fetched, and the install flows through the existing H6/I5
	// pipeline (fetch, Validate, consent) unchanged. The bundle grants nothing the
	// install pipeline does not already gate.
	Plugins []BundlePlugin `json:"plugins,omitempty"`

	src     []byte
	file    string
	offsets map[string]int
}

// BundlePlugin is one plugin the bundle needs, named by the HTTPS URL of its
// manifest — the same manifest_url shape a registry entry carries, so the fetch
// path and its HTTPS-only refusal are the ones J2 already signed.
type BundlePlugin struct {
	ManifestURL string `json:"manifest_url"`
}

// ParseBundle parses a bundle from bytes with no origin name.
func ParseBundle(data []byte) (*Bundle, error) {
	return ParseBundleNamed(unnamedBundle, data)
}

// ParseBundleNamed is ParseBundle for a bundle whose origin has a name — the name
// a refusal prints. A caller that fetched the bytes passes the source URL so a
// malformed public bundle points the user at the file it came from. It parses
// only; call Validate to run the refusals.
func ParseBundleNamed(name string, data []byte) (*Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, jsonError(name, data, err)
	}
	b.src = data
	b.file = name
	b.offsets = topLevelKeyOffsets(data)
	return &b, nil
}

// Validate runs the J4 refusals in order: the schema version first (closed
// because code), then the identity the one consent screen shows, then the empty
// check that makes a share honest, then each contribution — the embedded scene,
// the embedded theme block, and each plugin reference. It returns the first
// refusal, each carrying file:line.
//
// It never fetches. The embedded scene is checked for well-formedness (it parses
// and declares a root); its full bind validation is deferred to mount, because a
// bundle scene may wire into the namespaces of the plugins it bundles, and those
// namespaces are unknown until each plugin's manifest is fetched. This is the same
// split the registry makes — validate the shape here, defer the fetched dependency
// to install — and the reason Validate stays a pure function over the bundle bytes
// and is testable without a network.
func (b *Bundle) Validate() error {
	if b == nil {
		return nil
	}
	if err := b.validateIdentity(); err != nil {
		return err
	}
	if err := b.checkEmpty(); err != nil {
		return err
	}
	if err := b.validateScene(); err != nil {
		return err
	}
	if err := b.validateThemeBlock(); err != nil {
		return err
	}
	if err := b.validatePlugins(); err != nil {
		return err
	}
	return nil
}

// validateIdentity refuses a bundle whose version or human-facing identity is
// missing or unrecognised. The version is closed-because-code; the name and
// description are what the one consent screen shows, and consent to an unnamed,
// undescribed share is consent the user could not read — the same argument the
// manifest's required name and the registry entry's required description make.
func (b *Bundle) validateIdentity() error {
	if b.Version == "" {
		return &Error{Loc: b.rootLoc(), Msg: "bundle has no version; the version is a closed-set schema tag the host must recognise (DESIGN-BLOCK-J.md J4) — add \"version\": \"bundle/v1\""}
	}
	if !legalBundleVersions[b.Version] {
		return &Error{Loc: b.locAt("version"), Msg: fmt.Sprintf("unknown bundle version %q; the bundle schema is code on both sides so it is a closed set, not negotiated (DESIGN-BLOCK-J.md J4) — the legal versions are %s", b.Version, legalBundleVersionList())}
	}
	if b.Name == "" {
		return &Error{Loc: b.rootLoc(), Msg: "bundle has no name; the name is the human-facing label the one consent screen shows, and consent to an unnamed share is consent the user could not read (DESIGN-BLOCK-J.md J4) — add a \"name\""}
	}
	if b.Description == "" {
		return &Error{Loc: b.rootLoc(), Msg: "bundle has no description; the description is the one-line summary the consent screen shows, and a share with no summary is one the user accepts blind (DESIGN-BLOCK-J.md J4) — add a \"description\""}
	}
	return nil
}

// checkEmpty refuses a bundle that contributes nothing, ported from the manifest's
// check of the same name. A share that carries no scene, no theme and no plugins is
// a grant that bought nothing: the user shared something and the workspace did not
// change, indistinguishable from a broken load. A bundle must contribute at least
// one of the three.
func (b *Bundle) checkEmpty() error {
	if !hasTokens(b.Scene) && !hasTokens(b.Theme) && len(b.Plugins) == 0 {
		return &Error{Loc: b.rootLoc(), Msg: fmt.Sprintf("bundle %q contributes no scene, no theme and no plugins; a share that changes nothing is a grant that bought nothing, indistinguishable from a broken load (DESIGN-BLOCK-J.md J4) — add a scene, a theme or a plugin", b.Name)}
	}
	return nil
}

// validateScene checks the embedded scene is a well-formed document that declares
// a root. It rebases the scene bytes onto the bundle so a parse refusal points at
// the bundle line the author wrote (the same "address for free" technique
// manifest.go uses for a mounted fragment), then runs scene.ParseNamed and
// RefuseEmpty.
//
// It deliberately does NOT run the scene validator's bind/token/node-type refusals
// here: a bundle scene may reference the `<plugin-id>.*` namespaces of the plugins
// it bundles, and those namespaces are unknown until each plugin's manifest is
// fetched, so full validation with the bundled plugins' scopes is a mount-time
// concern. Running it here would falsely refuse the very bundles J4 exists to
// share — a curated scene wired to its plugins. RefuseEmpty is the honest pure-core
// guarantee: the scene parses and has something to draw.
func (b *Bundle) validateScene() error {
	if !hasTokens(b.Scene) {
		return nil
	}
	name, sceneBytes := b.rebaseScene()
	doc, err := scene.ParseNamed(name, sceneBytes)
	if err != nil {
		return err
	}
	if err := doc.RefuseEmpty(); err != nil {
		return &Error{Loc: b.locAt("scene"), Msg: fmt.Sprintf("bundle scene is invalid: %v; a bundle's scene is the interface it ships, so it must declare a \"root\" node to draw (DESIGN-BLOCK-J.md J4)", err), Err: err}
	}
	return nil
}

// rebaseScene returns the name and bytes to hand scene.ParseNamed such that the
// scene's own addressing reports bundle-absolute file:line. Unlike a mounted
// fragment (a node the manifest wraps in {"root":…}), a bundle scene is already a
// complete document, so no wrapper is needed: the scene bytes are padded with the
// bundle's leading newlines and spaces up to their true position, which is valid
// leading JSON whitespace and leaves every line inside the scene matching the
// bundle. When the scene bytes are not found in the source (a hand-built Bundle
// with no src), it falls back to the raw bytes under a named-but-unpositioned
// origin, so a refusal still has a file to name.
func (b *Bundle) rebaseScene() (string, []byte) {
	if len(b.src) == 0 {
		return b.name() + " (scene)", b.Scene
	}
	off := bytes.Index(b.src, b.Scene)
	if off < 0 {
		return b.name() + " (scene)", b.Scene
	}
	line, col := position(b.src, off)
	var sb strings.Builder
	if line > 1 {
		sb.WriteString(strings.Repeat("\n", line-1))
	}
	if col > 1 {
		sb.WriteString(strings.Repeat(" ", col-1))
	}
	sb.Write(b.Scene)
	return b.name(), []byte(sb.String())
}

// validateThemeBlock checks the embedded theme token block through the same token
// validator a theme file and a manifest's `tokens` block get (theme.LoadBytes), so
// a bundle's malformed token is refused by the same parse a malformed theme is. An
// absent or empty block is a no-op. It reuses the manifest's Theme-parsing helper
// shape: one token validator, no second drift-prone reader, and the refusal carries
// the bundle-addressed file:line rather than theme.LoadBytes's bare name.
func (b *Bundle) validateThemeBlock() error {
	if !hasTokens(b.Theme) {
		return nil
	}
	if _, err := theme.LoadBytes(b.name(), b.Theme); err != nil {
		return &Error{Loc: b.locAt("theme"), Msg: fmt.Sprintf("bundle theme block is invalid: %v", err), Err: err}
	}
	return nil
}

// validatePlugins refuses a plugin reference whose manifest_url is absent or not
// HTTPS, the same refusal validateManifestURL makes for a registry entry and for
// the same security reason: a bundle is attacker-controlled data, and a plaintext
// http:// URL invites a man-in-the-middle to swap the manifest between the share
// the user consented to and the code the install gate spawns, while a file:// or
// other scheme would let a shared bundle name a path on the user's own disk.
func (b *Bundle) validatePlugins() error {
	for i, p := range b.Plugins {
		if p.ManifestURL == "" {
			return &Error{Loc: b.locAt("plugins"), Msg: fmt.Sprintf("bundle plugin %d has no manifest_url; a plugin reference is the URL its manifest is fetched from, Validated and consent-gated through the same pipeline as /ui plugin add (DESIGN-BLOCK-J.md J4) — add a \"manifest_url\"", i)}
		}
		u, err := url.Parse(p.ManifestURL)
		if err != nil {
			return &Error{Loc: b.locAt("plugins"), Msg: fmt.Sprintf("bundle plugin %d has a malformed manifest_url %q: %v", i, p.ManifestURL, err)}
		}
		if u.Scheme != "https" {
			return &Error{Loc: b.locAt("plugins"), Msg: fmt.Sprintf("bundle plugin %d has manifest_url %q with scheme %q; a manifest is fetched over HTTPS only, because a shared bundle is attacker-controlled data and a plaintext or file:// URL would let it redirect the install to swapped code or a local path (DESIGN-BLOCK-J.md J4 security note) — use an https:// URL", i, p.ManifestURL, u.Scheme)}
		}
	}
	return nil
}

// rootLoc is the bundle's own top-level address, for a refusal about the bundle as
// a whole (a missing version, an empty share) rather than about one key.
func (b *Bundle) rootLoc() scene.Loc {
	return scene.Loc{File: b.name()}
}

// name returns the bundle's origin as it appears in errors.
func (b *Bundle) name() string {
	if b == nil || b.file == "" {
		return unnamedBundle
	}
	return b.file
}

// locAt resolves the address of a top-level bundle key, degrading to the file-only
// address when the key was not recorded — the same contract Manifest.locAt and
// Registry.locAt keep.
func (b *Bundle) locAt(key string) scene.Loc {
	if off, ok := b.offsets[key]; ok {
		line, col := position(b.src, off)
		return scene.Loc{File: b.file, Line: line, Col: col}
	}
	return scene.Loc{File: b.file}
}

// legalBundleVersionList renders the closed version set for an error message,
// sorted so the message is stable across runs.
func legalBundleVersionList() string {
	out := make([]string, 0, len(legalBundleVersions))
	for v := range legalBundleVersions {
		out = append(out, v)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
