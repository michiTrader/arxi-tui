package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/ext/supervisor"
	"github.com/michiTrader/arxi_tui/internal/patch"
)

// This file is the cmd-edge orchestration for `/ui plugin install <url>`: it
// threads the three pieces that were each landed and proven on their own — the
// bounded archive fetch (httpArchiveFetcher), the offline InstallFromBundle
// (extraction + behavioral validation + digest lay-out), and the consent-gated
// supervisor.Mount — into the single call the loop makes. It holds no loop state
// and no key handling on purpose: the consent Prompt is injected, so the modal
// loop supplies the on-screen consent scene and a test supplies a fixed answer,
// the same factoring supervisor.Mount already uses for its own Prompt. That is
// what keeps this thread testable without a live terminal.

// pluginsRootPath resolves the directory the digest-keyed package trees live
// under. ARXI_PLUGINS_DIR overrides it outright — a test points it at a temp dir,
// and a user with a non-default config home can too — otherwise it is
// ~/.arxi/plugins, the same ~/.arxi tree the consent store and the run log sit
// in. The staging tree InstallFromBundle builds is created *inside* this root so
// the atomic rename that finalises an install stays on one filesystem (§I-I
// Decision 4), which is why the whole cache has one root rather than a temp dir
// chosen elsewhere.
func pluginsRootPath() (string, error) {
	if p := os.Getenv("ARXI_PLUGINS_DIR"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home dir for the plugin cache path: %w", err)
	}
	return filepath.Join(home, ".arxi", "plugins"), nil
}

// installBehavioralPlugin fetches, installs and mounts one behavioral plugin from
// a URL, returning the live supervisor so the caller can Close it on `/ui plugin
// remove`. The order is the design's and non-negotiable: fetch the bundle under
// its cap, lay it out by digest offline (nothing runs), then let supervisor.Mount
// decide consent by that digest and spawn ONLY on a grant. installed is returned
// even when Mount fails so a rejection can be reported against the plugin that was
// on disk; on a fetch or install failure there is nothing to report and it is nil.
//
// The digest is the real PackageDigest over the laid-out tree, not a placeholder:
// it is half the consent identity, so passing an empty or fabricated one would
// make every mount look like a different plugin and break the remembered-grant
// contract the gate rests on — the exact reason the loop could not wire this until
// InstallFromBundle computed a real digest.
func installBehavioralPlugin(
	ctx context.Context,
	rawURL string,
	fetch patch.Fetcher,
	pluginsRoot string,
	gate *ext.Gate,
	store *ext.PluginStore,
	reg *supervisor.Registry,
	prompt supervisor.Prompt,
) (*supervisor.Supervisor, *ext.Installed, error) {
	_, data, err := fetch.Fetch(rawURL)
	if err != nil {
		return nil, nil, err
	}

	installed, err := ext.NewInstaller().InstallFromBundle(bytes.NewReader(data), pluginsRoot)
	if err != nil {
		return nil, nil, err
	}

	// Root carries the laid-out tree so the manifest's relative executable resolves
	// at spawn; the manifest keeps that relative path because it is half the
	// identity (supervisor.Config.Root). Granted is left unset — Mount overwrites it
	// from the gate, and pre-filling it here could not smuggle power past the gate
	// (invariant 7), but leaving it empty says so plainly.
	cfg := supervisor.Config{Manifest: *installed.Manifest, Root: installed.Root}
	s, err := supervisor.Mount(ctx, gate, cfg, installed.Digest, store, reg, prompt)
	if err != nil {
		return nil, installed, err
	}
	return s, installed, nil
}
