// Package ext loads plugin manifests. A manifest is data — JSON that names a
// plugin's identity, the scene fragments it mounts, the tokens it contributes,
// and (for a behavioral plugin) the executable it runs and the binds it streams.
//
// This package is the declarative half of Block H (DESIGN-BLOCK-H.md, signed
// into SCENES.md Scene 6 / BINDS.md §4.4 / PLAN.md ADR-0006). The load-bearing
// split it enforces is structural, not a footnote: the presence of `executable`
// is the single discriminator between a declarative plugin (data, zero code,
// zero risk) and a behavioral one (an external process gated by Block I). H2
// implements only the declarative path and refuses a behavioral manifest until
// Block I builds the process supervisor and the consent gate — so "load a
// declarative plugin, zero code" is enforceable precisely because "zero code" is
// the absence of one field, checkable at load with no gate involved.
//
// The manifest never renders itself and imports no UI package: like
// internal/scene it stays on the pure-data side of the arch seam, depending only
// on scene (to validate a mounted fragment through the same validator a
// hand-written document gets) and theme (to validate a contributed token block
// through the same token validator a theme file gets). One validator per
// concern, two callers — never a second, drift-prone copy.
package ext

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// legalProtocols is the closed set of wire-protocol versions a manifest may
// name. Closed because the protocol is code on both sides (DESIGN-BLOCK-H.md
// H1): an unknown protocol is refused at load, not negotiated. A declarative
// manifest still names it, so the field's absence is never ambiguous with an
// old manifest.
var legalProtocols = map[string]bool{"ext/v1": true}

// idPattern is the plugin id grammar, carried over from arxi-sim's name pattern.
// The id has three roles the schema depends on — it is the `<plugin-id>.*` bind
// namespace prefix, the `<plugin-id>/` mount-id scope, and the key a consent
// grant is remembered against — so it must be a clean identifier, which is why
// it is required even for a declarative manifest that streams nothing.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Manifest is a parsed plugin manifest. The declarative block (`tokens`,
// `mounts`) is the zero-code contribution both plugin kinds may carry; the
// behavioral block (`executable` and everything gated on it) is specified here
// so the schema is frozen once, but H2 refuses any manifest that fills it —
// mounting a process is Block I's work.
type Manifest struct {
	// Identity block — always required.
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Protocol string `json:"protocol"`

	// Declarative block — the zero-code contribution.
	//
	// Tokens stays raw so it can be handed to the theme token validator
	// verbatim: its shape is exactly a theme's token block, so LoadBytes checks
	// it with no second parser (H4 adds only the merge). Mounts carries each
	// fragment as raw JSON for the same reason a scene fragment survives a /ui
	// patch — the scene validator, not this package, is the net for a fragment's
	// undeclared keys.
	Tokens json.RawMessage `json:"tokens,omitempty"`
	Mounts []Mount         `json:"mounts,omitempty"`

	// Behavioral block — present iff Executable is. Specified for Block I;
	// refused by H2. ConsentRequired is a pointer so an absent declaration is
	// distinguishable from an explicit false, which the contradiction check
	// below depends on.
	Executable      string              `json:"executable,omitempty"`
	Args            []string            `json:"args,omitempty"`
	Capabilities    []string            `json:"capabilities,omitempty"`
	ConsentRequired *bool               `json:"consent_required,omitempty"`
	Binds           map[string]BindDecl `json:"binds,omitempty"`

	// The address book, kept so a refusal names a position instead of only a
	// reason (the same contract scene.Document keeps). Set by the parser; a
	// Manifest built by hand degrades to the file-only address rather than
	// inventing a line.
	src     []byte
	file    string
	offsets map[string]int
}

// Mount places one scene fragment somewhere in the host tree. Where reuses D2's
// `where` grammar plus overlay anchors (H-B); resolving it is H3's job, so H2
// only requires it to be present. Fragment is an ordinary scene subtree.
type Mount struct {
	Where    string          `json:"where"`
	Fragment json.RawMessage `json:"fragment"`
}

// BindDecl declares one `<plugin-id>.*` field a behavioral plugin will stream:
// its kind (which node type the value is legal under, read by H5) and a mock
// (the placeholder the preview path renders before a frame arrives, read by
// Block J). It is specified here and unused by H2, which streams nothing.
type BindDecl struct {
	Kind string          `json:"kind"`
	Mock json.RawMessage `json:"mock,omitempty"`
}

// Parse parses a manifest from bytes with no origin name. Syntax and type errors
// carry an address taken from the offset encoding/json already computed.
func Parse(data []byte) (*Manifest, error) {
	return ParseNamed(unnamedManifest, data)
}

// ParseNamed is Parse for a manifest whose origin has a name — the name a
// refusal prints, so a caller that read the bytes from disk passes the path and
// the user gets an address they can open in an editor. It parses only; call
// Validate to run the refusals, or LoadFile to do both.
func ParseNamed(name string, data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, jsonError(name, data, err)
	}
	m.src = data
	m.file = name
	m.offsets = topLevelKeyOffsets(data)
	return &m, nil
}

// LoadFile reads, parses and validates a manifest from disk. It exists so the
// path reaches every refusal automatically: the alternative is every caller
// remembering to pass the name it just read from, and the measured history of
// that rule (scene.ParseFile) is that an address a caller must remember to
// supply is an address that goes missing.
func LoadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := ParseNamed(path, data)
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// jsonError converts an encoding/json failure into an addressed manifest error,
// mirroring scene.jsonError: both error types the standard library returns here
// carry a byte offset, and throwing it away was the measured bug that rule
// exists to prevent.
func jsonError(name string, data []byte, err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		line, col := position(data, int(syntaxErr.Offset))
		return &Error{Loc: scene.Loc{File: name, Line: line, Col: col}, Msg: "invalid JSON: " + syntaxErr.Error(), Err: err}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		line, col := position(data, int(typeErr.Offset))
		return &Error{Loc: scene.Loc{File: name, Line: line, Col: col}, Msg: "invalid JSON: " + typeErr.Error(), Err: err}
	}
	return &Error{Loc: scene.Loc{File: name}, Msg: "invalid JSON: " + err.Error(), Err: err}
}

// locAt resolves the address of a top-level manifest key, degrading to the
// file-only address when the key was not recorded (a hand-built Manifest, or a
// complaint about a key the author did not write).
func (m *Manifest) locAt(key string) scene.Loc {
	if off, ok := m.offsets[key]; ok {
		line, col := position(m.src, off)
		return scene.Loc{File: m.file, Line: line, Col: col}
	}
	return scene.Loc{File: m.file}
}

// name returns the manifest's origin as it appears in errors.
func (m *Manifest) name() string {
	if m == nil || m.file == "" {
		return unnamedManifest
	}
	return m.file
}

// Validate runs the H2 refusals in order: identity first (the most fundamental),
// then the declarative/behavioral discriminator, then the contradiction and
// empty checks that make the discriminator honest, then the contributed data —
// the token block and each mounted fragment — through the same validators a
// theme file and a hand-written scene get. It returns the first refusal, each
// carrying file:line.
//
// A behavioral manifest (one with an `executable`) is refused here, before any
// data validation: mounting code is Block I's work, and H2's promise is exactly
// that it never loads one. That refusal is this method's load-bearing
// counterfactual — a manifest with an executable must be refused, not silently
// loaded — so it is checked early and tested in both directions.
func (m *Manifest) Validate() error {
	if m == nil {
		return nil
	}
	if err := m.validateIdentity(); err != nil {
		return err
	}
	if err := m.checkBehavioral(); err != nil {
		return err
	}
	if err := m.checkEmpty(); err != nil {
		return err
	}
	if err := m.validateTokensBlock(); err != nil {
		return err
	}
	if err := m.validateMounts(); err != nil {
		return err
	}
	return nil
}

// validateIdentity refuses a manifest whose identity block is missing or
// malformed. Every field here is required for a declarative manifest too: the id
// is a namespace prefix, the name and version are consent identity, and the
// protocol is closed-because-code — an absent protocol is refused rather than
// defaulted so it is never ambiguous with an old manifest that predates the
// field.
func (m *Manifest) validateIdentity() error {
	if m.ID == "" {
		return &Error{Loc: m.rootLoc(), Msg: "manifest has no id; the id is the plugin's bind namespace, mount-id scope and consent key, so it is required even for a declarative plugin (DESIGN-BLOCK-H.md H1) — add an \"id\" matching [a-z][a-z0-9-]{0,62}"}
	}
	if !idPattern.MatchString(m.ID) {
		return &Error{Loc: m.locAt("id"), Msg: fmt.Sprintf("plugin id %q is not a legal identifier; it must match [a-z][a-z0-9-]{0,62} (lowercase, digits and hyphens, starting with a letter) because it prefixes every bind and mounted node id", m.ID)}
	}
	if m.Name == "" {
		return &Error{Loc: m.rootLoc(), Msg: "manifest has no name; the name is the human-facing label the consent screen and installer show (DESIGN-BLOCK-H.md H1) — add a \"name\""}
	}
	if m.Version == "" {
		return &Error{Loc: m.rootLoc(), Msg: "manifest has no version; the version is part of the consent identity (Q15), so a version bump re-asks consent — add a \"version\""}
	}
	if m.Protocol == "" {
		return &Error{Loc: m.rootLoc(), Msg: "manifest has no protocol; the protocol is a closed-set wire version the host must recognise (DESIGN-BLOCK-H.md H1) — add \"protocol\": \"ext/v1\""}
	}
	if !legalProtocols[m.Protocol] {
		return &Error{Loc: m.locAt("protocol"), Msg: fmt.Sprintf("unknown protocol %q; the protocol is code on both sides so it is a closed set, not negotiated (DESIGN-BLOCK-H.md H1) — the legal versions are %s", m.Protocol, legalProtocolList())}
	}
	return nil
}

// checkBehavioral applies the H-A discriminator. An `executable` makes a
// manifest behavioral, and H2 refuses to load one: the process supervisor and
// the consent gate are Block I, and loading code with neither would be exactly
// the ungated execution the split exists to prevent. A declarative manifest that
// nonetheless declares a behavioral field is the discriminator failing the other
// way — a contradiction refused rather than trusted, so the author is told which
// half to fix instead of getting a plugin that runs nothing but claims it will.
func (m *Manifest) checkBehavioral() error {
	if m.Executable != "" {
		return &Error{Loc: m.locAt("executable"), Msg: fmt.Sprintf("manifest %q declares an executable, so it is a behavioral plugin; behavioral plugins run an external process behind the consent gate, which is Block I — H2 loads only declarative plugins (no executable). Remove the executable to mount it as data, or wait for the behavioral path", m.ID)}
	}
	// No executable: the behavioral fields must be absent too, or the manifest
	// is declarative-but-behavioral, which is a contradiction the H-A
	// discriminator names. Checked in a fixed order so the reported field is
	// deterministic.
	if m.Capabilities != nil {
		return m.contradiction("capabilities")
	}
	if m.Args != nil {
		return m.contradiction("args")
	}
	if m.Binds != nil {
		return m.contradiction("binds")
	}
	if m.ConsentRequired != nil {
		return m.contradiction("consent_required")
	}
	return nil
}

// contradiction is the refusal for a declarative manifest (no executable) that
// carries a behavioral field. It names both the fix directions so the author
// resolves the contradiction rather than guessing which field is the mistake.
func (m *Manifest) contradiction(field string) error {
	return &Error{Loc: m.locAt(field), Msg: fmt.Sprintf("manifest declares %q but has no executable; %q is a behavioral field (it configures the external process), and a declarative manifest streams nothing, so the two contradict (DESIGN-BLOCK-H.md H1) — add an \"executable\" to make it behavioral, or drop %q to keep it declarative", field, field, field)}
}

// checkEmpty refuses a manifest that contributes neither UI nor tokens. An
// accepted no-op plugin is a grant that bought nothing: the user mounted
// something and the tree did not change, which is indistinguishable from a
// broken load. A plugin must mount at least one fragment or contribute at least
// one token.
func (m *Manifest) checkEmpty() error {
	if len(m.Mounts) == 0 && !hasTokens(m.Tokens) {
		return &Error{Loc: m.rootLoc(), Msg: fmt.Sprintf("plugin %q contributes no mounts and no tokens; a plugin that mounts no UI and defines no tokens does nothing, and an accepted no-op is a grant that bought nothing (DESIGN-BLOCK-H.md H1) — add a mount or a token", m.ID)}
	}
	return nil
}

// validateTokensBlock checks the contributed token block through the same token
// validator a theme file gets (theme.LoadBytes), so a plugin's malformed token
// is refused by the same parse a malformed theme is. H4 adds the merge; this is
// only the well-formedness net, which the merge would need anyway.
//
// A token *reference* inside a mounted fragment is a different check — it needs
// the merged theme and is H4's — so it is not run here. What this refuses is a
// token *definition* the plugin itself got wrong (a bad colour, an unknown
// attribute), before that definition is ever merged.
//
// It delegates to Theme() so the token block has exactly one parser: validation
// and the H4 merge cannot drift on which bytes are legal, because the check that
// gates Validate is the same call the merge reads its theme from.
func (m *Manifest) validateTokensBlock() error {
	_, err := m.Theme()
	return err
}

// Theme parses the manifest's contributed token block into a theme so the host
// can merge it into the active theme (H4). It returns an empty theme when the
// manifest contributes no tokens, so a mount that adds only fragments merges a
// no-op rather than forcing every caller to nil-check. It reuses
// theme.LoadBytes — the one token validator, the same one a theme file gets — so
// a token block Validate accepted parses here without a second, drift-prone
// reader, and a malformed block is refused with the manifest-addressed file:line
// rather than theme.LoadBytes's bare name.
func (m *Manifest) Theme() (*theme.Theme, error) {
	if !hasTokens(m.Tokens) {
		return theme.FromMap(nil), nil
	}
	t, err := theme.LoadBytes(m.name(), m.Tokens)
	if err != nil {
		return nil, &Error{Loc: m.locAt("tokens"), Msg: fmt.Sprintf("plugin token block is invalid: %v", err), Err: err}
	}
	return t, nil
}

// validateMounts checks each mounted fragment. A mount must say where it goes
// and carry a fragment, and the fragment is validated through the scene
// validator — the same net a hand-written document gets, which is why a fragment
// is an ordinary scene subtree rather than a bespoke shape.
//
// Resolving the `where` grammar (D2 addressing plus overlay anchors) and
// applying the `<plugin-id>/` id prefix are H3's job; H2 only requires `where`
// to be present so an unplaceable mount is refused at load rather than at mount.
// A fragment using a `<plugin-id>.*` bind is refused here by the current scene
// validator (the namespace is not yet signed) — which is correct for H2, whose
// declarative plugins stream nothing and so bind only host fields; H5 lifts that
// refusal for a behavioral plugin's declared binds.
func (m *Manifest) validateMounts() error {
	for i, mnt := range m.Mounts {
		if mnt.Where == "" {
			return &Error{Loc: m.locAt("mounts"), Msg: fmt.Sprintf("mount %d has no where; a mount must name where its fragment lands in the host tree (DESIGN-BLOCK-H.md H-B) — add a \"where\" such as \"top-right\" or \"below <id>\"", i)}
		}
		if !hasTokens(mnt.Fragment) {
			return &Error{Loc: m.locAt("mounts"), Msg: fmt.Sprintf("mount %d has no fragment; a declarative mount is a scene fragment plus its where (DESIGN-BLOCK-H.md H1) — add a \"fragment\"", i)}
		}
		if err := m.validateFragment(i, mnt.Fragment); err != nil {
			return err
		}
	}
	return nil
}

// validateFragment runs one mounted fragment through the scene validator with
// its address rebased onto the manifest, so a refusal inside a fragment points
// at the manifest line the author actually wrote rather than at a line inside a
// detached copy. See rebaseFragment for how the manifest-absolute address is
// reconstructed and the one case (a single-line manifest) where the column is
// approximate.
//
// It validates with the plugin scope (H5): a bind in the manifest's own
// `<id>.` namespace resolves iff the manifest declares it, rather than being
// refused as unsigned. For a declarative manifest the scope's binds are empty —
// the checkBehavioral refusal above guarantees a manifest reaching here has no
// `binds` — so any use of the plugin's own namespace is refused as the empty-
// namespace case, which is exactly the declarative/behavioral split at load time.
func (m *Manifest) validateFragment(i int, fragment json.RawMessage) error {
	scope := m.pluginScope()
	wrapped, ok := m.rebaseFragment(fragment)
	if !ok {
		// The fragment bytes were not found in the source (a hand-built
		// Manifest with no src, or an unlocatable duplicate); fall back to the
		// fragment's own bytes named by the mount, so the refusal still has a
		// file and a locator even without manifest-absolute lines.
		wrapped = append(append([]byte(`{"root":`), fragment...), '}')
		doc, err := scene.ParseNamed(fmt.Sprintf("%s (mount %d fragment)", m.name(), i), wrapped)
		if err != nil {
			return err
		}
		return doc.ValidateWithPlugin(scope)
	}
	doc, err := scene.ParseNamed(m.name(), wrapped)
	if err != nil {
		return err
	}
	return doc.ValidateWithPlugin(scope)
}

// pluginScope projects the manifest's identity and declared binds into the
// scene-owned scope the fragment validator consults (H5 / BINDS.md §4.4). The
// scene package cannot import ext (the arch seam), so the projection lives here
// and reads the manifest's `binds` map directly — it is never copied into a
// second inventory, so the manifest stays the one source the way SignedBinds
// documents. A declarative manifest has no binds, so the scope carries the id
// with an empty bind set: its namespace exists but is empty, which is what makes
// a declarative fragment's use of it the refused empty-namespace case.
func (m *Manifest) pluginScope() *scene.PluginScope {
	binds := make(map[string]string, len(m.Binds))
	for name, decl := range m.Binds {
		binds[name] = decl.Kind
	}
	return &scene.PluginScope{ID: m.ID, Binds: binds}
}

// rebaseFragment builds a scene document wrapping one fragment such that the
// fragment sits at its true manifest byte position, so scene's own addressing
// reports manifest-absolute file:line for free (the larger half of H1's "address
// for free" claim).
//
// The wrapper is `{"root":` + padding + fragment + `}`. The padding is the
// manifest newlines and leading spaces up to the fragment's `{`, so the
// fragment's first byte lands on the same line and column it occupies in the
// manifest, and every line inside the fragment matches too. The wrapper's
// `{"root":` sits at the very start; it only shares a line with the fragment
// when the fragment begins on manifest line 1 (a minified single-line manifest),
// the one case where the reported column on that first line is shifted by the
// wrapper — noted rather than fixed because a hand-written manifest is
// multi-line and exact.
//
// It reports false when the fragment bytes are not found in the source, so the
// caller can fall back rather than rebase against a wrong offset.
func (m *Manifest) rebaseFragment(fragment json.RawMessage) ([]byte, bool) {
	if len(m.src) == 0 {
		return nil, false
	}
	off := bytes.Index(m.src, fragment)
	if off < 0 {
		return nil, false
	}
	line, col := position(m.src, off)
	var b strings.Builder
	b.WriteString(`{"root":`)
	if line > 1 {
		b.WriteString(strings.Repeat("\n", line-1))
		b.WriteString(strings.Repeat(" ", col-1))
	} else {
		// Same line as the wrapper: one space keeps the JSON well-formed.
		b.WriteByte(' ')
	}
	b.Write(fragment)
	b.WriteByte('}')
	return []byte(b.String()), true
}

// rootLoc is the manifest's own top-level address, used for a refusal about the
// manifest as a whole (a missing required field, an empty plugin) rather than
// about one key.
func (m *Manifest) rootLoc() scene.Loc {
	return scene.Loc{File: m.name()}
}

// hasTokens reports whether a raw JSON value carries content — a non-empty
// object. It is how "no tokens" and "no fragment" are told apart from a key
// written as null or {}: encoding/json keeps the RawMessage bytes, so the
// emptiness question is answered from what the author actually wrote.
func hasTokens(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	if bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return false
	}
	return true
}

// legalProtocolList renders the closed protocol set for an error message, sorted
// so the message is stable across runs.
func legalProtocolList() string {
	out := make([]string, 0, len(legalProtocols))
	for p := range legalProtocols {
		out = append(out, p)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
