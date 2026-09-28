package ext

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// bundleEntry is one member of a test .tar.gz. It is deliberately expressive
// enough to build the *hostile* bundles these tests exist to reject — a symlink,
// a traversal path — not only the well-formed one, because a guard proven only
// on clean input has demonstrated nothing (the counterfactual rule).
type bundleEntry struct {
	name     string
	typeflag byte
	mode     int64
	linkname string
	body     []byte
}

// buildBundle writes entries into an in-memory gzipped tar, so every test drives
// Extract with real archive bytes rather than a stub — the extraction path,
// gzip and tar readers included, is what is under test.
func buildBundle(t *testing.T, entries []bundleEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     mode,
			Linkname: e.linkname,
			Size:     int64(len(e.body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header for %q: %v", e.name, err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("writing tar body for %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return buf.Bytes()
}

// TestExtractAcceptsARegularBundle is the positive direction: a bundle of a
// directory and two regular files extracts, the bytes land verbatim, and the
// executable bit survives — so a later refusal test that fails proves the
// hostile shape was rejected, not that Extract rejects everything.
func TestExtractAcceptsARegularBundle(t *testing.T) {
	data := buildBundle(t, []bundleEntry{
		{name: "bin/", typeflag: tar.TypeDir},
		{name: "bin/run", typeflag: tar.TypeReg, mode: 0o755, body: []byte("#!/bin/sh\n")},
		{name: "plugin.json", typeflag: tar.TypeReg, body: []byte(`{"id":"x"}`)},
	})
	dest := t.TempDir()
	if err := NewInstaller().Extract(bytes.NewReader(data), dest); err != nil {
		t.Fatalf("a well-formed bundle must extract, got: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "plugin.json"))
	if err != nil {
		t.Fatalf("plugin.json must be written: %v", err)
	}
	if string(got) != `{"id":"x"}` {
		t.Fatalf("plugin.json bytes must land verbatim; got %q", string(got))
	}
	info, err := os.Stat(filepath.Join(dest, "bin", "run"))
	if err != nil {
		t.Fatalf("bin/run must be written: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 && runtime.GOOS != "windows" {
		// The executable bit is a Unix concept: Windows has no such file bit, so Go
		// reports every regular file there as non-executable and PackageDigest's
		// exec-bit marker is uniformly 0 on that platform. Asserting it would fail on
		// the Windows leg of CI for a property the OS cannot represent, so the
		// assertion is scoped to where the bit is real.
		t.Fatalf("the executable bit must survive extraction, or a bundled binary is not runnable and PackageDigest's exec-bit record has nothing to bind to; got mode %v", info.Mode())
	}
}

// TestExtractRefusesPathTraversal proves the traversal refusal fires on the path
// and only the path: `../escape` is rejected, while the byte-identical entry at
// a local name extracts. Without the second half a test that merely rejected the
// name `escape` anywhere would pass, and the guard would not be about traversal
// at all.
func TestExtractRefusesPathTraversal(t *testing.T) {
	hostile := buildBundle(t, []bundleEntry{
		{name: "../escape", typeflag: tar.TypeReg, body: []byte("owned")},
	})
	if err := NewInstaller().Extract(bytes.NewReader(hostile), t.TempDir()); err == nil {
		t.Fatalf("a `..` entry escaping the root must be refused, or a bundle writes outside the package tree (tar-slip)")
	} else if !strings.Contains(err.Error(), "escapes the package root") {
		t.Fatalf("the refusal must name the traversal; got: %v", err)
	}

	benign := buildBundle(t, []bundleEntry{
		{name: "escape", typeflag: tar.TypeReg, body: []byte("owned")},
	})
	if err := NewInstaller().Extract(bytes.NewReader(benign), t.TempDir()); err != nil {
		t.Fatalf("the same name without `..` is a legal in-package file and must extract; got: %v", err)
	}
}

// TestExtractRefusesSymlink proves the entry-kind refusal, mirroring
// PackageDigest's own symlink refusal so a bundle that would fail the digest walk
// is rejected earlier. The link name is local, so the refusal is about the entry
// *kind*, not its path — the counterfactual is the accepted-regular-file case in
// the positive test above, which shares the code path up to the type switch.
func TestExtractRefusesSymlink(t *testing.T) {
	data := buildBundle(t, []bundleEntry{
		{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	})
	err := NewInstaller().Extract(bytes.NewReader(data), t.TempDir())
	if err == nil {
		t.Fatalf("a symlink entry must be refused; it points at bytes the digest never read, so a grant would cover content the consent screen never showed (§I-H)")
	}
	if !strings.Contains(err.Error(), "not a regular file or directory") {
		t.Fatalf("the refusal must name the entry kind; got: %v", err)
	}
}

// TestExtractRefusesDecompressionBomb proves the shared byte budget cuts a bundle
// off, and that the boundary is exact: a total at the cap extracts, one byte over
// is refused. A small cap keeps the test archive tiny while exercising the same
// arithmetic the 128 MiB default uses.
func TestExtractRefusesDecompressionBomb(t *testing.T) {
	in := &Installer{MaxBytes: 16, MaxEntries: 16}

	atCap := buildBundle(t, []bundleEntry{
		{name: "a", typeflag: tar.TypeReg, body: bytes.Repeat([]byte("x"), 16)},
	})
	if err := in.Extract(bytes.NewReader(atCap), t.TempDir()); err != nil {
		t.Fatalf("a bundle exactly at the byte cap must extract, or the cap is off by one and rejects legal maxima; got: %v", err)
	}

	overCap := buildBundle(t, []bundleEntry{
		{name: "a", typeflag: tar.TypeReg, body: bytes.Repeat([]byte("x"), 17)},
	})
	if err := in.Extract(bytes.NewReader(overCap), t.TempDir()); err == nil {
		t.Fatalf("a bundle one byte over the cap must be refused, or a gzip bomb fills the disk before the digest runs")
	} else if !strings.Contains(err.Error(), "extraction cap") {
		t.Fatalf("the refusal must name the cap; got: %v", err)
	}
}

// TestExtractRefusesTooManyEntries proves the entry-count cap catches the
// many-empty-files bomb the byte cap misses: at the cap the bundle extracts, one
// entry over is refused.
func TestExtractRefusesTooManyEntries(t *testing.T) {
	in := &Installer{MaxBytes: installMaxBytes, MaxEntries: 2}

	atCap := buildBundle(t, []bundleEntry{
		{name: "a", typeflag: tar.TypeReg},
		{name: "b", typeflag: tar.TypeReg},
	})
	if err := in.Extract(bytes.NewReader(atCap), t.TempDir()); err != nil {
		t.Fatalf("a bundle at the entry cap must extract; got: %v", err)
	}

	overCap := buildBundle(t, []bundleEntry{
		{name: "a", typeflag: tar.TypeReg},
		{name: "b", typeflag: tar.TypeReg},
		{name: "c", typeflag: tar.TypeReg},
	})
	if err := in.Extract(bytes.NewReader(overCap), t.TempDir()); err == nil {
		t.Fatalf("a bundle over the entry cap must be refused, or a many-tiny-files bomb passes the byte cap unchecked")
	} else if !strings.Contains(err.Error(), "entries") {
		t.Fatalf("the refusal must name the entry cap; got: %v", err)
	}
}

// TestLayoutByDigestIsKeyedAndIdempotent proves the three Decision 4 properties
// that the grant-transfer safety rests on: the tree lands at root/<id>/<digest>;
// a re-lay-out of identical bytes is idempotent (same path, bytes kept); and the
// load-bearing counterfactual — a one-byte change moves the digest, so it lands
// at a different path and a remembered grant cannot silently transfer to it.
func TestLayoutByDigestIsKeyedAndIdempotent(t *testing.T) {
	in := NewInstaller()

	extract := func(body []byte) string {
		t.Helper()
		data := buildBundle(t, []bundleEntry{
			{name: "plugin.json", typeflag: tar.TypeReg, body: body},
		})
		tmp := t.TempDir()
		if err := in.Extract(bytes.NewReader(data), tmp); err != nil {
			t.Fatalf("extracting probe bundle: %v", err)
		}
		return tmp
	}

	root := t.TempDir()

	path1, digest1, err := in.LayoutByDigest(extract([]byte(`{"id":"p"}`)), root, "p")
	if err != nil {
		t.Fatalf("laying out a fresh package must succeed: %v", err)
	}
	if filepath.Base(path1) != digest1 || filepath.Base(filepath.Dir(path1)) != "p" {
		t.Fatalf("the tree must land at root/<id>/<digest>; got %q for id p digest %q", path1, digest1)
	}
	if _, err := os.Stat(filepath.Join(path1, "plugin.json")); err != nil {
		t.Fatalf("the laid-out tree must contain the package bytes: %v", err)
	}

	path2, digest2, err := in.LayoutByDigest(extract([]byte(`{"id":"p"}`)), root, "p")
	if err != nil {
		t.Fatalf("re-laying identical bytes must succeed: %v", err)
	}
	if path2 != path1 || digest2 != digest1 {
		t.Fatalf("identical bytes must be idempotent (same digest, same path); first (%q,%q) second (%q,%q)", path1, digest1, path2, digest2)
	}
	if _, err := os.Stat(filepath.Join(path1, "plugin.json")); err != nil {
		t.Fatalf("an idempotent re-lay-out must keep the bytes already on record, not clobber them: %v", err)
	}

	path3, digest3, err := in.LayoutByDigest(extract([]byte(`{"id":"q"}`)), root, "p")
	if err != nil {
		t.Fatalf("laying out one-byte-changed bytes must succeed: %v", err)
	}
	if digest3 == digest1 || path3 == path1 {
		t.Fatalf("a one-byte change must move the digest and the path, or a remembered grant silently transfers to different bytes — the exact failure the digest exists to prevent; changed digest %q path %q equalled the original", digest3, path3)
	}
}
