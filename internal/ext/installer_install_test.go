package ext

import (
	"archive/tar"
	"bytes"
	"os"
	"strings"
	"testing"
)

// This file covers InstallFromBundle, the offline orchestration that threads the
// units tested elsewhere — Extract, ValidateBehavioral, LayoutByDigest — into the
// one call the installer edge makes. The steps those units own are proven in
// their own tests; here the concern is the *seam*: that the manifest is read from
// inside the extracted tree, that the executable is confirmed present (the check
// no earlier unit makes), that the staging tree never leaks, and that the digest
// a grant will bind to travels all the way through a real bundle unchanged.

// installableBundle is the well-formed behavioral package these tests deviate
// from one field at a time: a behavioral manifest at the tree root and the
// executable it names. Keeping the manifest and the file it points at together in
// one builder means a test that removes the file, or edits the manifest, names
// exactly the deviation under test.
func installableBundle(t *testing.T, manifest string, execName string, execBody []byte) []byte {
	t.Helper()
	entries := []bundleEntry{
		{name: pluginManifestName, typeflag: tar.TypeReg, body: []byte(manifest)},
	}
	if execName != "" {
		entries = append(entries, bundleEntry{name: execName, typeflag: tar.TypeReg, mode: 0o755, body: execBody})
	}
	return buildBundle(t, entries)
}

// behavioralManifestJSON is the minimal manifest InstallFromBundle must accept: an
// identity, the ext/v1 protocol, and an executable naming a file the bundle ships.
// It matches the fixture ValidateBehavioral is tested against so the two agree on
// what a valid behavioral package looks like.
const behavioralManifestJSON = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "capabilities": ["events.emit"]
}`

// noStagingLeaks fails if any staging directory survived under pluginsRoot. The
// staging tree carries the still-unvalidated bytes of a fetched bundle; leaving
// one behind on any exit path would accumulate half-installs under the live cache
// the digest lay-out is meant to keep clean, so every InstallFromBundle test
// asserts the deferred cleanup actually ran.
func noStagingLeaks(t *testing.T, pluginsRoot string) {
	t.Helper()
	ents, err := os.ReadDir(pluginsRoot)
	if err != nil {
		t.Fatalf("reading plugins root %q: %v", pluginsRoot, err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "install-") {
			t.Errorf("staging directory %q leaked into the plugins root; a refusal or an idempotent skip must remove the temp tree, or half-installs accumulate under the live cache", e.Name())
		}
	}
}

// TestInstallFromBundleAcceptsAWellFormedPackage is the positive control: a bundle
// carrying a behavioral manifest and the executable it names installs, the result
// names the manifest and a non-empty digest, and the laid-out tree contains both
// files. If this fails every refusal test below is measuring an installer that
// rejects everything, so it runs first.
func TestInstallFromBundleAcceptsAWellFormedPackage(t *testing.T) {
	root := t.TempDir()
	data := installableBundle(t, behavioralManifestJSON, "tick", []byte("#!/bin/sh\n"))

	got, err := NewInstaller().InstallFromBundle(bytes.NewReader(data), root)
	if err != nil {
		t.Fatalf("a well-formed behavioral bundle must install: %v", err)
	}
	if got.Manifest == nil || got.Manifest.ID != "tick" {
		t.Fatalf("the install must return the parsed manifest with its id; got %+v", got.Manifest)
	}
	if got.Digest == "" {
		t.Fatal("the install must return a package digest; it is half the consent identity, and an empty digest makes every plugin look like a different one")
	}
	if _, err := os.Stat(got.Root); err != nil {
		t.Fatalf("the laid-out package root must exist: %v", err)
	}
	if _, err := os.Stat(got.Root + string(os.PathSeparator) + pluginManifestName); err != nil {
		t.Fatalf("the laid-out tree must carry the manifest: %v", err)
	}
	if _, err := os.Stat(got.Root + string(os.PathSeparator) + "tick"); err != nil {
		t.Fatalf("the laid-out tree must carry the executable: %v", err)
	}
	noStagingLeaks(t, root)
}

// TestInstallFromBundleRefusesAMissingManifest covers a bundle that extracts but
// carries no manifest at its root: the install is refused naming plugin.json, so
// the author learns the package is missing the file the whole contract hangs on
// rather than getting an opaque parse failure. The staging tree must still be
// cleaned.
func TestInstallFromBundleRefusesAMissingManifest(t *testing.T) {
	root := t.TempDir()
	data := buildBundle(t, []bundleEntry{
		{name: "tick", typeflag: tar.TypeReg, mode: 0o755, body: []byte("#!/bin/sh\n")},
	})

	_, err := NewInstaller().InstallFromBundle(bytes.NewReader(data), root)
	if err == nil {
		t.Fatal("a bundle with no plugin.json must be refused; a behavioral package keeps its manifest inside the digested tree, and without it there are no terms to consent to")
	}
	if !strings.Contains(err.Error(), pluginManifestName) {
		t.Errorf("the refusal must name the missing manifest file; got: %v", err)
	}
	noStagingLeaks(t, root)
}

// TestInstallFromBundleRefusesADeclarativeManifest proves InstallFromBundle runs
// the behavioral validation path, not Validate: a manifest with no executable is
// declarative and belongs on the /ui plugin add path, so the installer refuses it.
// The counterfactual is the positive test above, whose only difference is the
// executable — so this measures the executable requirement, not a validator that
// rejects everything.
func TestInstallFromBundleRefusesADeclarativeManifest(t *testing.T) {
	root := t.TempDir()
	declarative := strings.Replace(behavioralManifestJSON, `"executable": "./tick",`, "", 1)
	data := installableBundle(t, declarative, "", nil)

	_, err := NewInstaller().InstallFromBundle(bytes.NewReader(data), root)
	if err == nil {
		t.Fatal("a manifest with no executable is declarative and must be refused by the behavioral installer; it has nothing to run behind the consent gate")
	}
	if !strings.Contains(err.Error(), "executable") {
		t.Errorf("the refusal must name the missing executable; got: %v", err)
	}
	noStagingLeaks(t, root)
}

// TestInstallFromBundleRefusesAMissingExecutableFile is the load-bearing new
// guard, and it is proven in both directions: a manifest naming ./tick with no
// tick file in the bundle validates (the path is in-package) yet cannot run, so
// the install is refused here; adding the file — the only change — makes the same
// manifest install. Without the negative case the check would be untested; without
// the positive case it could be refusing every package for an unrelated reason.
func TestInstallFromBundleRefusesAMissingExecutableFile(t *testing.T) {
	root := t.TempDir()

	missing := installableBundle(t, behavioralManifestJSON, "", nil)
	_, err := NewInstaller().InstallFromBundle(bytes.NewReader(missing), root)
	if err == nil {
		t.Fatal("a manifest naming an executable the bundle does not ship must be refused; it validates and then never spawns, and a consent grant would bind to a tree that cannot run")
	}
	if !strings.Contains(err.Error(), "no such file is in the package") {
		t.Errorf("the refusal must say the named executable is absent from the package; got: %v", err)
	}
	noStagingLeaks(t, root)

	present := installableBundle(t, behavioralManifestJSON, "tick", []byte("#!/bin/sh\n"))
	if _, err := NewInstaller().InstallFromBundle(bytes.NewReader(present), t.TempDir()); err != nil {
		t.Fatalf("the same manifest with the executable file present must install; the check rejects a missing file, not a present one: %v", err)
	}
}

// TestInstallFromBundleRefusesAHostileBundleBeforeReadingTheManifest proves the
// order §I-I fixes: extraction refusals fire before the manifest is read, so a
// traversal entry is rejected at write time and no bytes land outside the root —
// even though this bundle also carries a valid manifest that would otherwise
// install. If the manifest were read first, a hostile entry would already be on
// disk before anything refused it.
func TestInstallFromBundleRefusesAHostileBundleBeforeReadingTheManifest(t *testing.T) {
	root := t.TempDir()
	data := buildBundle(t, []bundleEntry{
		{name: pluginManifestName, typeflag: tar.TypeReg, body: []byte(behavioralManifestJSON)},
		{name: "tick", typeflag: tar.TypeReg, mode: 0o755, body: []byte("#!/bin/sh\n")},
		{name: "../escape", typeflag: tar.TypeReg, body: []byte("owned")},
	})

	_, err := NewInstaller().InstallFromBundle(bytes.NewReader(data), root)
	if err == nil {
		t.Fatal("a bundle with a traversal entry must be refused even when it carries a valid manifest; extraction is the security boundary and runs before the manifest is read")
	}
	if !strings.Contains(err.Error(), "escapes the package root") {
		t.Errorf("the refusal must name the traversal; got: %v", err)
	}
	noStagingLeaks(t, root)
}

// TestInstallFromBundleDigestIsStableAndKeyed is the whole-install counterfactual:
// installing identical bytes twice yields the same digest and root (idempotent, so
// a remembered grant re-mounts the same plugin), and changing one byte of the
// executable yields a different digest and root — so a grant made for one package
// can never silently transfer to different code. This is LayoutByDigest's property
// re-checked through the full install, because it is the identity the consent gate
// depends on and a break here is invisible until a grant transfers.
func TestInstallFromBundleDigestIsStableAndKeyed(t *testing.T) {
	root := t.TempDir()
	in := NewInstaller()

	a := installableBundle(t, behavioralManifestJSON, "tick", []byte("v1"))
	first, err := in.InstallFromBundle(bytes.NewReader(a), root)
	if err != nil {
		t.Fatalf("first install must succeed: %v", err)
	}

	again, err := in.InstallFromBundle(bytes.NewReader(installableBundle(t, behavioralManifestJSON, "tick", []byte("v1"))), root)
	if err != nil {
		t.Fatalf("re-installing identical bytes must succeed: %v", err)
	}
	if again.Digest != first.Digest || again.Root != first.Root {
		t.Fatalf("identical bytes must install idempotently (same digest, same root); first (%q,%q) second (%q,%q)", first.Digest, first.Root, again.Digest, again.Root)
	}

	changed, err := in.InstallFromBundle(bytes.NewReader(installableBundle(t, behavioralManifestJSON, "tick", []byte("v2"))), root)
	if err != nil {
		t.Fatalf("installing a one-byte-changed executable must succeed: %v", err)
	}
	if changed.Digest == first.Digest || changed.Root == first.Root {
		t.Fatalf("a one-byte change in the executable must move the digest and root, or a grant remembered for the first package silently authorises the second; got digest %q root %q", changed.Digest, changed.Root)
	}
	noStagingLeaks(t, root)
}
