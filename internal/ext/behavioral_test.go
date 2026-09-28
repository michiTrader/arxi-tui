package ext

import (
	"strings"
	"testing"
)

// A minimal, well-formed behavioral manifest: identity, an in-package
// executable, and one requested capability. It mounts no fragment and defines no
// token on purpose — a behavioral plugin whose whole contribution is the process
// it runs is the common case the installer exists for, and checkEmpty must not
// reject it. Every refusal test below is a single deviation from this, so a
// failure names exactly the field under test.
const validBehavioral = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "capabilities": ["events.emit"]
}`

// TestValidateBehavioralAcceptsWhatValidateRefuses is the load-bearing
// counterfactual for the whole door: the SAME manifest that Validate refuses
// because it carries an executable (H2) is accepted by ValidateBehavioral. Both
// halves run over one manifest so the test cannot pass by the two paths quietly
// agreeing — Validate must reject and send the author to Block I, and the
// installer's door must then accept the very thing Block I is. If ValidateBehavioral
// grew a spurious refusal, the accept half fails; if the executable refusal were
// deleted from checkBehavioral, the reject half fails.
func TestValidateBehavioralAcceptsWhatValidateRefuses(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validBehavioral))
	if err != nil {
		t.Fatalf("ParseNamed refused a well-formed behavioral manifest: %v; a behavioral manifest is still parseable data", err)
	}
	if err := m.Validate(); err == nil {
		t.Fatal("Validate accepted a behavioral manifest; the declarative loader must still refuse an executable (H2), or the installer's door lifts a refusal that was never there")
	} else if !strings.Contains(err.Error(), "Block I") {
		t.Fatalf("Validate refusal = %q; it must still send the author to Block I so the behavioral path is named", err.Error())
	}
	if err := m.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused a well-formed behavioral manifest: %v; the installer is the one door that accepts an executable, and refusing it here means no behavioral plugin can ever be installed", err)
	}
}

// TestValidateBehavioralRefusesADeclarativeManifest proves the door swings only
// one way: a manifest with no executable has no behavioral install to perform and
// is sent to the manifest-only path. The counterfactual is the same manifest with
// an executable added — it must then be accepted, so the refusal is provably
// about the missing executable and not some unrelated field the declarative
// fixture happens to trip.
func TestValidateBehavioralRefusesADeclarativeManifest(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validDeclarative))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a manifest with no executable; a declarative package has nothing for the behavioral installer to mount and belongs on the /ui plugin add path, so it must be refused")
	}
	if !strings.Contains(err.Error(), "executable") {
		t.Fatalf("declarative refusal = %q; it must name the missing executable so the author knows which half to add", err.Error())
	}
	assertAddressed(t, err)

	// Counterfactual: adding an executable to the same fixture must flip it to
	// accepted, or the refusal above was not about the executable at all.
	withExec := strings.Replace(validDeclarative,
		`"protocol": "ext/v1",`,
		`"protocol": "ext/v1",
  "executable": "./tick",`, 1)
	m2, err := ParseNamed("plugin.json", []byte(withExec))
	if err != nil {
		t.Fatalf("ParseNamed (with executable): %v", err)
	}
	if err := m2.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused the declarative fixture with an executable added: %v; the only change was the executable, so the earlier refusal must have been exactly its absence", err)
	}
}

// TestValidateBehavioralRefusesAnOutOfPackageExecutable proves the in-package
// rule fires on the path and only the path. A climbing `..` and an absolute path
// both name bytes PackageDigest never read, so a grant bound to the tree's digest
// would authorise an unseen program — each is refused. The second half is the one
// that makes it a traversal check and not a blanket ban: a nested in-package path
// (`bin/run`) extracts, so the guard rejects escape, not depth.
func TestValidateBehavioralRefusesAnOutOfPackageExecutable(t *testing.T) {
	for _, exec := range []string{"../escape", "/usr/bin/tick"} {
		src := strings.Replace(validBehavioral, `"executable": "./tick",`, `"executable": "`+exec+`",`, 1)
		m, err := ParseNamed("plugin.json", []byte(src))
		if err != nil {
			t.Fatalf("ParseNamed (%q): %v", exec, err)
		}
		err = m.ValidateBehavioral()
		if err == nil {
			t.Fatalf("ValidateBehavioral accepted executable %q; a path escaping the package names code the digest never covered, and a grant on the tree's digest would authorise a program the user never saw", exec)
		}
		if !strings.Contains(err.Error(), "outside the package") {
			t.Fatalf("out-of-package refusal for %q = %q; it must name that the executable escapes the package so the author sees the boundary crossed", exec, err.Error())
		}
		assertAddressed(t, err)
	}

	// Counterfactual: a nested but in-package executable is accepted, so the
	// refusals above are about escaping the root, not about the path having a
	// directory in it.
	nested := strings.Replace(validBehavioral, `"executable": "./tick",`, `"executable": "bin/run",`, 1)
	m, err := ParseNamed("plugin.json", []byte(nested))
	if err != nil {
		t.Fatalf("ParseNamed (nested): %v", err)
	}
	if err := m.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused an in-package nested executable %q: %v; the in-package rule rejects escape, not a subdirectory", "bin/run", err)
	}
}

// TestValidateBehavioralStillEnforcesIdentity proves the behavioral door is a
// superset of the declarative refusals, not a bypass of them: dropping the id —
// the bind namespace, mount scope and consent key — is refused here exactly as it
// is on the declarative path, because ValidateBehavioral runs validateIdentity
// before it ever reaches the executable.
func TestValidateBehavioralStillEnforcesIdentity(t *testing.T) {
	noID := strings.Replace(validBehavioral, `"id": "tick",
  `, "", 1)
	m, err := ParseNamed("plugin.json", []byte(noID))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a behavioral manifest with no id; the id is the consent key and bind namespace, so lifting the executable refusal must not lift the identity refusals with it")
	}
	if !strings.Contains(err.Error(), "id") {
		t.Fatalf("missing-id refusal = %q; it must name the id so the author knows which required field is absent", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateBehavioralStillValidatesMounts proves the same net a hand-written
// scene and a declarative plugin get is applied to a behavioral plugin's mounts:
// a mount with no `where` is refused here as it is under Validate, so the
// behavioral door reuses the fragment/mount validators rather than skipping them.
func TestValidateBehavioralStillValidatesMounts(t *testing.T) {
	src := strings.Replace(validBehavioral,
		`"capabilities": ["events.emit"]`,
		`"capabilities": ["events.emit"],
  "mounts": [ { "fragment": { "type": "text", "text": "hi" } } ]`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a mount with no where; a behavioral plugin's mounts must pass the same placement net as a declarative one, or the behavioral door is a hole in the mount validator")
	}
	if !strings.Contains(err.Error(), "where") {
		t.Fatalf("no-where refusal = %q; it must name the missing where so the author knows the mount is unplaceable", err.Error())
	}
	assertAddressed(t, err)
}
