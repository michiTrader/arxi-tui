package ext

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// behavioralManifest is a minimal behavioral manifest for identity/consent tests:
// it carries every field the identity tuple reads. Each test below is a single
// deviation from this, so a failing identity comparison names exactly the field
// that did (or did not) move the hash.
func behavioralManifest() *Manifest {
	return &Manifest{
		ID:           "tick",
		Name:         "Community Ticker",
		Version:      "1.0.0",
		Protocol:     "ext/v1",
		Executable:   "./tick",
		Args:         []string{"--interval", "5s"},
		Capabilities: []string{"actions.register", "events.emit"},
	}
}

// TestIdentityBindsEveryTupleField is the load-bearing property of the identity
// tuple (DESIGN-BLOCK-I §I-H): a grant is remembered against name + version +
// protocol + executable + args + capability-set + digest, so changing ANY of
// those must change the identity — otherwise a grant for one program silently
// transfers to a different one. Capability *order* must NOT change it, since the
// set membership is the authority, not the declaration order.
func TestIdentityBindsEveryTupleField(t *testing.T) {
	base := Identity(behavioralManifest(), "digest-a")

	// The digest is the bytes the grant is bound to; a byte change re-asks.
	if base == Identity(behavioralManifest(), "digest-b") {
		t.Error("changing the package digest did not change the identity\n" +
			"consequence: a grant remembered for one package transfers to different bytes the user never saw — the exact substitution the digest exists to prevent (§I-H).\n" +
			"remedy: include the digest in the identity pre-image.")
	}

	mutate := []struct {
		field  string
		change func(*Manifest)
	}{
		{"name", func(m *Manifest) { m.Name = "Different Name" }},
		{"version", func(m *Manifest) { m.Version = "2.0.0" }},
		{"protocol", func(m *Manifest) { m.Protocol = "ext/v2" }},
		{"executable", func(m *Manifest) { m.Executable = "./other" }},
		{"args", func(m *Manifest) { m.Args = []string{"--interval", "10s"} }},
		{"capability membership", func(m *Manifest) { m.Capabilities = []string{"actions.register"} }},
		{"capability membership widened with tools.register", func(m *Manifest) {
			m.Capabilities = []string{"actions.register", "events.emit", "tools.register"}
		}},
	}
	for _, tc := range mutate {
		m := behavioralManifest()
		tc.change(m)
		if Identity(m, "digest-a") == base {
			t.Errorf("changing %s did not change the identity\n"+
				"consequence: a remembered grant would apply to a plugin that differs in %s, widening the grant past what the user consented to (§I-H).\n"+
				"remedy: include %s in the identity pre-image.", tc.field, tc.field, tc.field)
		}
	}

	// Capability order is not identity: a manifest listing the same powers in a
	// different order is the same plugin and must reuse the remembered grant.
	reordered := behavioralManifest()
	reordered.Capabilities = []string{"events.emit", "actions.register"}
	if Identity(reordered, "digest-a") != base {
		t.Error("reordering the capability list changed the identity\n" +
			"consequence: the same plugin re-asks for consent after a cosmetic reorder of its capabilities, because order was treated as authority when membership is.\n" +
			"remedy: sort the capability set before hashing it into the identity.")
	}
}

// TestIdentityDoesNotMutateTheManifest guards a subtle aliasing bug: Identity
// sorts the capability set, and if it sorted the manifest's own slice a live
// plugin's declared order would change underneath it. Identity is a pure read.
func TestIdentityDoesNotMutateTheManifest(t *testing.T) {
	m := behavioralManifest()
	m.Capabilities = []string{"events.emit", "actions.register"}
	_ = Identity(m, "digest-a")
	if m.Capabilities[0] != "events.emit" {
		t.Errorf("Identity sorted the manifest's own capability slice in place: now %v\n"+
			"consequence: computing an identity reorders a live manifest's declared capabilities, a spooky side effect on a pure read.\n"+
			"remedy: copy the slice before sorting.", m.Capabilities)
	}
}

// TestPackageDigestReflectsContentModeAndRefusesUnsafeEntries pins the digest
// computation (§I-H): the same bytes hash the same, a content change moves the
// digest, and a symlink is refused because it points at bytes the digest never
// read — a package that hid its real executable behind a link would otherwise be
// granted consent for a tree the user never saw. The symlink half is
// Windows-conditional because symlink creation needs a privilege there, and the
// escape-hatch policy (AGENTS.md) is that a guarantee proven only on one OS is a
// slogan — so the content/mode half runs everywhere and stands on its own.
func TestPackageDigestReflectsContentModeAndRefusesUnsafeEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "tick")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	one, err := PackageDigest(dir)
	if err != nil {
		t.Fatalf("PackageDigest refused a well-formed package tree: %v", err)
	}
	if again, err := PackageDigest(dir); err != nil || again != one {
		t.Fatalf("PackageDigest is not deterministic: first %q, second %q (err %v)\n"+
			"consequence: the same bytes produce a different identity on each mount, so a remembered grant never matches and the gate re-asks forever.", one, again, err)
	}

	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	two, err := PackageDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if one == two {
		t.Error("changing a file's content did not change the digest\n" +
			"consequence: a grant bound to this digest covers edited code, so a plugin can swap its behavior after consent without re-asking (§I-H).\n" +
			"remedy: hash the file bytes, not only the paths.")
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink(path, filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := PackageDigest(dir); err == nil {
			t.Error("PackageDigest accepted a package tree containing a symlink\n" +
				"consequence: a symlink points at bytes the digest did not read, so a grant bound to the tree covers content the consent screen never showed (§I-H).\n" +
				"remedy: refuse any non-regular entry rather than following it.")
		}
	}
}

// TestPackageDigestRefusesANonDirectoryRoot pins the root check: the digest is
// over a package tree, and a caller handing a single file has no canonical tree —
// refusing is more honest than hashing the one file as if it were a package.
func TestPackageDigestRefusesANonDirectoryRoot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "lonely")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PackageDigest(file); err == nil {
		t.Error("PackageDigest accepted a plain file as the package root\n" +
			"consequence: a caller pointing at one file gets a digest as if it were a package, hiding the sibling bytes a real package ships.\n" +
			"remedy: require the root to be a directory.")
	}
}
