package main

import (
	"bytes"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/ext"
	"github.com/michiTrader/arxi_tui/internal/patch"
)

// This file is the cmd-edge orchestration for a bundle install (J4): the fetch +
// per-plugin decision half that turns a bundle URL into the inputs the one consent
// screen and the fan-out consume. It is the bundle analogue of plugin_install.go's
// installBehavioralPlugin — the impure loop half DESIGN-BLOCK-J.md defers after the
// two pure artifacts (BundleConsentScene, GrantBundle) — and it lands the same way:
// the fetch→lay-out→Decide sequence in one function with injected fetchers, so the
// H6/I5 pipeline the design says a bundle plugin flows through "unchanged" is
// literally the same calls a standalone install makes, run once per plugin, and the
// whole thread is testable without a live terminal or a network.
//
// It stops at the decision, on purpose. The design splits a bundle install at the
// consent screen: everything before it (fetch each manifest_url, lay it out by
// digest, Decide) is a pure-of-the-loop computation over injected fetchers, and
// everything after it (show the one screen, fan one answer out to N grants via
// GrantBundle, then grant-then-compose theme.Merge + patch.Mount + supervisor.Start)
// is the modal loop's, which owns the keyboard and the live scene/theme/supervisor
// state. Resolving is the half that can be proven by a counterfactual here; the
// compose half is proven where the loop state lives.

// bundleResolution is everything resolveBundle computed before the user is asked:
// the parsed-and-validated bundle (its name/description lead the consent screen and
// its embedded scene/theme are composed after a grant), one BundlePluginDecision
// per referenced plugin (the gate's per-plugin Decide answer the screen and the
// fan-out both read), and the Installed package each decision was computed over
// (its Root is where the executable resolves at spawn, so the compose half needs it
// paired with the grant GrantBundle returns).
//
// decisions and installed are index-aligned and in bundle order: the fan-out grants
// in that order, and the compose half spawns in it, so keeping the two slices
// parallel rather than a map is what lets a BundleGrant (keyed by plugin ID) be
// paired back to the tree it was laid out into without re-deriving the digest.
type bundleResolution struct {
	bundle    *ext.Bundle
	decisions []ext.BundlePluginDecision
	installed []*ext.Installed
}

// resolveBundle fetches a bundle from bundleURL, validates it, and runs the
// per-plugin H6/I5 pipeline (fetch the archive, lay it out by digest offline, ask
// the gate to Decide against that digest) once per referenced plugin, returning the
// inputs the consent screen and the fan-out consume. It grants nothing and spawns
// nothing: "download ≠ trust ≠ grant" (Q15) holds across a bundle exactly as it
// holds for a single plugin, so the gate is only *consulted* here and the grant is
// the modal loop's to make on the user's answer.
//
// The two fetchers are separate because they carry different bodies under different
// caps: bundleFetch reads a small JSON document (the bundle manifest, the registry
// fetcher's egress class) and archiveFetch reads each plugin's `.tar.gz` (the
// behavioral-install egress class, a far larger cap). Injecting both is what lets a
// test drive the whole thread from fixtures with no server, the same seam
// installBehavioralPlugin takes for its one fetcher.
//
// A failure at any plugin aborts the whole resolution with nothing decided for the
// rest: a bundle is all-or-nothing (DESIGN-BLOCK-J.md — a scene wired to a plugin
// that could not be fetched is a scene with dead binds), so a plugin that fails to
// fetch, extract, validate or lay out fails the bundle rather than silently
// shrinking it to the plugins that happened to load. The refusal names the plugin's
// URL so the user learns which reference in the share they typed is the broken one.
func resolveBundle(
	bundleURL string,
	bundleFetch patch.Fetcher,
	archiveFetch patch.Fetcher,
	pluginsRoot string,
	gate *ext.Gate,
) (*bundleResolution, error) {
	name, data, err := bundleFetch.Fetch(bundleURL)
	if err != nil {
		return nil, err
	}

	b, err := ext.ParseBundleNamed(name, data)
	if err != nil {
		return nil, err
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}

	decisions := make([]ext.BundlePluginDecision, 0, len(b.Plugins))
	installed := make([]*ext.Installed, 0, len(b.Plugins))
	for _, p := range b.Plugins {
		_, archive, err := archiveFetch.Fetch(p.ManifestURL)
		if err != nil {
			return nil, fmt.Errorf("bundle %q: fetching plugin %q: %w", b.Name, p.ManifestURL, err)
		}
		inst, err := ext.NewInstaller().InstallFromBundle(bytes.NewReader(archive), pluginsRoot)
		if err != nil {
			return nil, fmt.Errorf("bundle %q: installing plugin %q: %w", b.Name, p.ManifestURL, err)
		}
		// The digest is the real PackageDigest over the laid-out tree, so Decide
		// answers against the plugin's own per-manifest identity — the same call the
		// standalone install makes, and the property the rejected aggregate-identity
		// reading (DESIGN-BLOCK-J.md) would have destroyed: a grant remembered for
		// these exact bytes, by a bundle or a standalone add, is found here.
		decision := gate.Decide(inst.Manifest, inst.Digest)
		decisions = append(decisions, ext.BundlePluginDecision{
			Manifest: inst.Manifest,
			Digest:   inst.Digest,
			Decision: decision,
		})
		installed = append(installed, inst)
	}

	return &bundleResolution{bundle: b, decisions: decisions, installed: installed}, nil
}
