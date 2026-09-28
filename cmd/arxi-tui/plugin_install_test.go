package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
)

// This file covers installBehavioralPlugin, the cmd-edge thread from a URL to a
// mounted plugin. The three units it calls are proven elsewhere — the fetch cap
// in plugin_archive_fetch_test.go, the extraction/validation/digest in
// internal/ext, the grant-path spawn in the supervisor mount and Root tests — so
// the concern here is the seam: that a fetch or install failure is surfaced
// unspawned, and that a rejection walks the whole thread (fetch, install to disk,
// gate decision, prompt) and still spawns nothing. The grant/spawn direction is
// deliberately NOT re-tested here: it needs a live ext/v1 subprocess, which the
// supervisor package already drives by re-execing its test binary, and duplicating
// that rig at the cmd layer would test the supervisor, not this thread.

// behavioralBundleJSON is a minimal valid behavioral manifest naming an executable
// the test bundle ships. It matches the fixtures ValidateBehavioral and
// InstallFromBundle are tested against so all three agree on a valid package.
const behavioralBundleJSON = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "capabilities": ["events.emit"]
}`

// buildInstallBundle writes a .tar.gz carrying the manifest and, unless omitExec
// is set, the executable it names. It is enough to drive installBehavioralPlugin
// up to the mount: the executable file need only exist for the install's
// existence check, because every test here stops at or before the spawn.
func buildInstallBundle(t *testing.T, manifest string, omitExec bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name string, mode int64, body []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(body))}); err != nil {
			t.Fatalf("writing tar header for %q: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("writing tar body for %q: %v", name, err)
		}
	}
	write("plugin.json", 0o644, []byte(manifest))
	if !omitExec {
		write("tick", 0o755, []byte("#!/bin/sh\n"))
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return buf.Bytes()
}

// stubFetcher is a patch.Fetcher that returns fixed bytes or a fixed error, so the
// thread is driven with no network. It is the same injected-seam pattern the
// manifest-add path uses in its own tests.
type stubFetcher struct {
	data []byte
	err  error
}

func (s stubFetcher) Fetch(rawURL string) (string, []byte, error) {
	if s.err != nil {
		return "", nil, s.err
	}
	return rawURL, s.data, nil
}

// TestInstallBehavioralPluginRejectionSpawnsNothing walks the whole thread with a
// prompt that rejects: the bundle is fetched, laid out on disk (installed is
// returned so a rejection can name the package), the gate is consulted, and the
// rejection comes back as ErrConsentRejected with no supervisor and nothing in the
// routing table. This is the security-load-bearing path — a plugin the user
// refused must never run — exercised end to end short of the spawn.
func TestInstallBehavioralPluginRejectionSpawnsNothing(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	store := ext.NewPluginStore()
	reg := supervisor.NewRegistry()

	reject := func(m *ext.Manifest, declared []string) (supervisor.ConsentAnswer, error) {
		return supervisor.ConsentAnswer{Rejected: true}, nil
	}
	fetch := stubFetcher{data: buildInstallBundle(t, behavioralBundleJSON, false)}

	s, installed, err := installBehavioralPlugin(context.Background(), "https://example/tick.tar.gz", fetch, root, gate, store, reg, reject)
	if !errors.Is(err, supervisor.ErrConsentRejected) {
		t.Fatalf("a rejected install returned err=%v, want ErrConsentRejected; a plugin the user refused must not spawn and the refusal must be reportable distinctly from a failure", err)
	}
	if s != nil {
		t.Errorf("a rejected install returned a live supervisor; nothing may spawn on a rejection")
	}
	if installed == nil {
		t.Fatal("a rejected install must still return the laid-out package so the caller can name what was refused")
	}
	if _, statErr := os.Stat(installed.Root); statErr != nil {
		t.Errorf("the package must be laid out on disk before consent is decided (download precedes the gate): %v", statErr)
	}
	if err := reg.SendAction("tick", "refresh", nil); err == nil {
		t.Errorf("a rejected plugin was registered for action routing; nothing may reach the routing table without a grant")
	}
}

// TestInstallBehavioralPluginPropagatesAFetchError proves a fetch failure stops
// the thread before anything is written: the error is returned, no supervisor and
// no installed package come back, and the plugins root stays empty.
func TestInstallBehavioralPluginPropagatesAFetchError(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	store := ext.NewPluginStore()
	reg := supervisor.NewRegistry()

	sentinel := errors.New("network down")
	grant := func(m *ext.Manifest, declared []string) (supervisor.ConsentAnswer, error) {
		t.Fatal("the prompt must not be reached when the fetch fails")
		return supervisor.ConsentAnswer{}, nil
	}

	s, installed, err := installBehavioralPlugin(context.Background(), "https://example/tick.tar.gz", stubFetcher{err: sentinel}, root, gate, store, reg, grant)
	if !errors.Is(err, sentinel) {
		t.Fatalf("a fetch error must propagate; got %v", err)
	}
	if s != nil || installed != nil {
		t.Errorf("a failed fetch must yield no supervisor and no installed package; got s=%v installed=%v", s, installed)
	}
	ents, _ := os.ReadDir(root)
	if len(ents) != 0 {
		t.Errorf("a failed fetch must write nothing under the plugins root; found %d entries", len(ents))
	}
}

// TestInstallBehavioralPluginRefusesABadBundle proves an install failure (here a
// bundle whose manifest is missing) is surfaced with nothing mounted: the error
// carries the install refusal, installed is nil (there is no laid-out package to
// report), and the prompt is never reached.
func TestInstallBehavioralPluginRefusesABadBundle(t *testing.T) {
	root := t.TempDir()
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	store := ext.NewPluginStore()
	reg := supervisor.NewRegistry()

	// A bundle with no plugin.json: it extracts, but there is no manifest, so
	// InstallFromBundle refuses before any digest or mount.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "tick", Typeflag: tar.TypeReg, Mode: 0o755, Size: 3})
	_, _ = tw.Write([]byte("bin"))
	_ = tw.Close()
	_ = gz.Close()

	grant := func(m *ext.Manifest, declared []string) (supervisor.ConsentAnswer, error) {
		t.Fatal("the prompt must not be reached when the bundle does not install")
		return supervisor.ConsentAnswer{}, nil
	}

	s, installed, err := installBehavioralPlugin(context.Background(), "https://example/bad.tar.gz", stubFetcher{data: buf.Bytes()}, root, gate, store, reg, grant)
	if err == nil {
		t.Fatal("a bundle with no manifest must be refused before mount")
	}
	if !strings.Contains(err.Error(), "plugin.json") {
		t.Errorf("the refusal must name the missing manifest; got %v", err)
	}
	if s != nil || installed != nil {
		t.Errorf("a bundle that does not install must yield no supervisor and no installed package; got s=%v installed=%v", s, installed)
	}
}

// TestPluginsRootPathHonorsOverride pins the two branches of the path resolver:
// ARXI_PLUGINS_DIR is returned verbatim (a test and a non-default config home both
// rely on it), and without it the default sits under the ~/.arxi tree the consent
// store and run log share.
func TestPluginsRootPathHonorsOverride(t *testing.T) {
	t.Setenv("ARXI_PLUGINS_DIR", filepath.Join("custom", "plugins"))
	got, err := pluginsRootPath()
	if err != nil {
		t.Fatalf("resolving with an override set: %v", err)
	}
	if got != filepath.Join("custom", "plugins") {
		t.Errorf("ARXI_PLUGINS_DIR must be returned verbatim; got %q", got)
	}

	t.Setenv("ARXI_PLUGINS_DIR", "")
	got, err = pluginsRootPath()
	if err != nil {
		t.Fatalf("resolving the default: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".arxi", "plugins")) {
		t.Errorf("the default must live under the ~/.arxi tree; got %q", got)
	}
}
