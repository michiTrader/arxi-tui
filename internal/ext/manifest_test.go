package ext

import (
	"errors"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
)

// A minimal, well-formed declarative manifest: identity, one contributed token,
// one mounted fragment binding only host state. Every refusal test below is a
// single deviation from this, so a failure names exactly the field under test.
const validDeclarative = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "tokens": { "profit": { "fg": "green" } },
  "mounts": [
    { "where": "top-right", "fragment": { "type": "text", "text": "hi" } }
  ]
}`

// TestLoadsAValidDeclarativeManifest is the positive control: the whole point of
// H2 is that a declarative plugin — data, no executable — loads. If this fails,
// every refusal test below is measuring a validator that rejects everything, so
// this runs first.
func TestLoadsAValidDeclarativeManifest(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validDeclarative))
	if err != nil {
		t.Fatalf("ParseNamed refused a well-formed manifest: %v; a declarative manifest is data and must parse", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate refused a well-formed declarative manifest: %v; H2's promise is that a zero-code plugin loads", err)
	}
	if m.ID != "tick" || m.Executable != "" {
		t.Fatalf("parsed manifest = {id:%q executable:%q}, want {id:\"tick\" executable:\"\"}; the identity and the declarative discriminator must survive the parse", m.ID, m.Executable)
	}
}

// TestRefusesABehavioralManifest is H2's load-bearing counterfactual: a manifest
// with an executable is behavioral, and mounting a process is Block I. H2 must
// refuse it rather than silently load it — the counterfactual, run by hand, is
// that deleting the executable check in checkBehavioral lets this manifest load,
// which is exactly the ungated-code path the declarative/behavioral split exists
// to prevent.
func TestRefusesABehavioralManifest(t *testing.T) {
	src := strings.Replace(validDeclarative,
		`"protocol": "ext/v1",`,
		`"protocol": "ext/v1",
  "executable": "./tick",`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a manifest with an executable; a behavioral plugin runs foreign code and must be refused by H2 (behavioral mounting is Block I), not silently loaded")
	}
	if !strings.Contains(err.Error(), "Block I") {
		t.Fatalf("behavioral refusal = %q; it must send the author to Block I (the behavioral path), so the message names it", err.Error())
	}
	assertAddressed(t, err)
}

// TestRefusesADeclarativeManifestDeclaringBehavioralFields covers the
// discriminator failing the other way: no executable, but a behavioral field
// present. A declarative plugin streams nothing, so a capability/args/binds/
// consent_required declaration contradicts it — refused so the author fixes the
// contradiction rather than getting a plugin that claims a capability it can
// never use.
func TestRefusesADeclarativeManifestDeclaringBehavioralFields(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		inject string
	}{
		{"capabilities", "capabilities", `"capabilities": ["events.emit"],`},
		{"args", "args", `"args": ["--x"],`},
		{"consent_required", "consent_required", `"consent_required": true,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(validDeclarative,
				`"protocol": "ext/v1",`,
				`"protocol": "ext/v1",
  `+tc.inject, 1)
			m, err := ParseNamed("plugin.json", []byte(src))
			if err != nil {
				t.Fatalf("ParseNamed: %v", err)
			}
			err = m.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a declarative manifest declaring %q; without an executable that field configures a process that never runs, which is a contradiction the discriminator must refuse", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("contradiction refusal = %q; it must name the offending field %q so the author knows which half to fix", err.Error(), tc.field)
			}
			assertAddressed(t, err)
		})
	}
}

// TestRefusesAnEmptyPlugin covers a manifest with valid identity but no mounts
// and no tokens. Such a plugin does nothing, and an accepted no-op is a grant
// that bought nothing — indistinguishable to the user from a broken load.
func TestRefusesAnEmptyPlugin(t *testing.T) {
	src := `{
  "id": "tick",
  "name": "T",
  "version": "1.0.0",
  "protocol": "ext/v1"
}`
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a plugin with no mounts and no tokens; a plugin that contributes nothing is a no-op grant and must be refused")
	}
	assertAddressed(t, err)
}

// TestRefusesAMalformedIdentityBlock covers the identity refusals: a bad id
// grammar, missing required fields, and an unknown protocol. Each is a single
// deviation from the valid manifest so the failure names the field.
func TestRefusesAMalformedIdentityBlock(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
		want string // substring the refusal must contain
	}{
		{"uppercase id", `"id": "tick",`, `"id": "Tick",`, "identifier"},
		{"empty id", `"id": "tick",`, `"id": "",`, "id"},
		{"missing name", `"name": "Community Ticker",`, ``, "name"},
		{"missing version", `"version": "1.0.0",`, ``, "version"},
		{"unknown protocol", `"protocol": "ext/v1",`, `"protocol": "ext/v9",`, "protocol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(validDeclarative, tc.old, tc.new, 1)
			m, err := ParseNamed("plugin.json", []byte(src))
			if err != nil {
				t.Fatalf("ParseNamed: %v", err)
			}
			err = m.Validate()
			if err == nil {
				t.Fatalf("Validate accepted a manifest with a %s; a malformed identity block breaks the namespace/consent contract and must be refused", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("identity refusal for %s = %q; it must mention %q so the author can find the field", tc.name, err.Error(), tc.want)
			}
			assertAddressed(t, err)
		})
	}
}

// TestRefusesAMalformedTokenBlock covers a plugin whose contributed token is
// itself invalid (an unparseable colour). It must be refused by the same token
// validator a theme file gets — the single-validator contract theme.LoadBytes
// exists to keep.
func TestRefusesAMalformedTokenBlock(t *testing.T) {
	src := strings.Replace(validDeclarative,
		`"tokens": { "profit": { "fg": "green" } },`,
		`"tokens": { "profit": { "fg": "notacolor" } },`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a plugin defining a token with an unparseable colour; a plugin token must pass the same parse a theme token does")
	}
	assertAddressed(t, err)
}

// TestRefusesAFragmentThatFailsTheSceneValidator covers the reuse H1 signs: a
// mounted fragment is an ordinary scene subtree and gets no weaker check than a
// hand-written document. A fragment binding an unsigned name is refused by the
// scene validator, and — because H2 rebases the fragment onto the manifest — the
// refusal points at the manifest line the author wrote, not a line in a detached
// copy.
func TestRefusesAFragmentThatFailsTheSceneValidator(t *testing.T) {
	// Laid out so the fragment object's opening brace is on line 9; the scene
	// validator addresses an unsigned bind at its node's object, so the refusal
	// must resolve to that manifest line.
	src := `{
  "id": "tick",
  "name": "T",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "mounts": [
    {
      "where": "top-right",
      "fragment": {
        "type": "text",
        "bind": "totally.invented"
      }
    }
  ]
}`
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a fragment binding an unsigned name; a mounted fragment must pass the same scene validator a hand-written document does")
	}
	loc := locOf(t, err)
	if loc.File != "plugin.json" {
		t.Fatalf("fragment refusal file = %q, want %q; the rebased fragment must carry the manifest's name, not a detached copy's", loc.File, "plugin.json")
	}
	if loc.Line != 9 {
		t.Fatalf("fragment refusal line = %d, want 9; the rebase must report the manifest line the fragment occupies (the fragment object opens on line 9), so an author can jump to it", loc.Line)
	}
}

// TestAcceptsAFragmentBindingHostState confirms the refusal above is about the
// unsigned bind, not about fragments in general: a fragment binding a signed
// host bind loads. A declarative plugin streams nothing, so binding host state
// is exactly what its fragments are allowed to do.
func TestAcceptsAFragmentBindingHostState(t *testing.T) {
	src := strings.Replace(validDeclarative,
		`"fragment": { "type": "text", "text": "hi" }`,
		`"fragment": { "type": "text", "bind": "agent.working" }`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate refused a fragment binding the signed host bind agent.working: %v; a declarative fragment may bind any host field", err)
	}
}

// TestRefusesAMountWithNoWhereOrFragment covers the two structural mount
// refusals: a mount must say where it lands and must carry a fragment. Neither
// is a no-op — a mount with no where is unplaceable, and a mount with no fragment
// mounts nothing.
func TestRefusesAMountWithNoWhereOrFragment(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{"no where", `{ "where": "top-right", "fragment": { "type": "text", "text": "hi" } }`, `{ "fragment": { "type": "text", "text": "hi" } }`},
		{"no fragment", `{ "where": "top-right", "fragment": { "type": "text", "text": "hi" } }`, `{ "where": "top-right" }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := strings.Replace(validDeclarative, tc.old, tc.new, 1)
			m, err := ParseNamed("plugin.json", []byte(src))
			if err != nil {
				t.Fatalf("ParseNamed: %v", err)
			}
			if err := m.Validate(); err == nil {
				t.Fatalf("Validate accepted a mount with %s; the mount is then unusable and must be refused", tc.name)
			}
		})
	}
}

// assertAddressed fails unless the error carries a file — the project's rule
// that a refusal without a location is a bug. It reaches through both error
// types H2 returns: an ext.Error for a manifest-level refusal and a scene.Error
// for a fragment refusal.
func assertAddressed(t *testing.T, err error) {
	t.Helper()
	loc := locOf(t, err)
	if loc.File == "" {
		t.Fatalf("refusal %q carries no file; a refusal without a location is a bug (AGENTS.md)", err.Error())
	}
}

// locOf extracts the address from either error type, failing if the error is
// neither — an unaddressed refusal is the defect this package refuses to ship.
func locOf(t *testing.T, err error) scene.Loc {
	t.Helper()
	var extErr *Error
	if errors.As(err, &extErr) {
		return extErr.Loc
	}
	var sceneErr *scene.Error
	if errors.As(err, &sceneErr) {
		return sceneErr.Loc
	}
	t.Fatalf("refusal %q is neither an *ext.Error nor a *scene.Error; every refusal must carry an address", err.Error())
	return scene.Loc{}
}
