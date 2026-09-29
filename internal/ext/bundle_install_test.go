package ext

import (
	"errors"
	"testing"
)

// This file pins GrantBundle, the J4 fan-out (bundle_install.go): one bundle answer
// out to N per-plugin grants against each plugin's own identity, grant-then-compose,
// all-or-nothing. Each test is a property the design names, and each carries the
// counterfactual it was written against — a fan-out that granted on a rejection, or
// only granted the first plugin, or re-granted a remembered one, or composed after a
// mid-way refusal, would each pass a weaker test and fail exactly one of these.

// unknownCapManifest declares a capability outside the closed KnownCapability set.
// A manifest like this would be refused at load, but GrantBundle re-checks through
// Gate.Grant, so it is the probe for the grant-then-compose abort: the fan-out must
// stop with an error before the caller composes, not carry an ungranted power.
func unknownCapManifest() *Manifest {
	return &Manifest{
		ID:           "rogue",
		Name:         "Rogue",
		Version:      "1.0.0",
		Protocol:     "ext/v1",
		Executable:   "./rogue",
		Capabilities: []string{"filesystem.write"}, // not in knownCapabilities
	}
}

// needsConsent builds a DecisionNeedsConsent decision for a manifest+digest, the
// state a plugin the gate has never seen is in — the input the fan-out must Grant.
func needsConsent(gate *Gate, m *Manifest, digest string) BundlePluginDecision {
	return BundlePluginDecision{Manifest: m, Digest: digest, Decision: gate.Decide(m, digest)}
}

// TestGrantBundleRejectionGrantsNothing is the all-or-nothing property: a rejected
// bundle grants no plugin, remembers nothing, and returns ErrBundleRejected so the
// loop composes nothing. The counterfactual is a fan-out that granted before
// checking Rejected — it would spawn the plugins of a bundle the user refused.
func TestGrantBundleRejectionGrantsNothing(t *testing.T) {
	store := NewMemoryConsentStore()
	gate := NewGate(store)
	m1, m2 := behavioralManifest(), secondBehavioralManifest()
	decisions := []BundlePluginDecision{
		needsConsent(gate, m1, consentTestDigest),
		needsConsent(gate, m2, consentTestDigest),
	}

	grants, err := GrantBundle(gate, decisions, BundleAnswer{Rejected: true})
	if !errors.Is(err, ErrBundleRejected) {
		t.Fatalf("a rejected bundle returned err=%v, want ErrBundleRejected; the loop must be able to name the user's own 'no' and compose nothing", err)
	}
	if grants != nil {
		t.Errorf("a rejected bundle produced %d grants; nothing may be granted when the user said no", len(grants))
	}
	if _, ok := store.Lookup(Identity(m1, consentTestDigest)); ok {
		t.Errorf("a rejected bundle remembered a grant; a rejection is session-local and must persist nothing")
	}
}

// TestGrantBundleFansOutToEveryPlugin is the N-grant property: a bundle of two
// unseen plugins produces a grant for BOTH, each carrying that plugin's own declared
// capabilities. The counterfactual is a fan-out that granted only decisions[0] (or
// only the last) — it would leave one plugin of the bundle ungranted, mounting a
// scene with dead binds into the plugin the loop then failed to spawn.
func TestGrantBundleFansOutToEveryPlugin(t *testing.T) {
	gate := NewGate(NewMemoryConsentStore())
	m1, m2 := behavioralManifest(), secondBehavioralManifest()
	decisions := []BundlePluginDecision{
		needsConsent(gate, m1, consentTestDigest),
		needsConsent(gate, m2, consentTestDigest),
	}

	grants, err := GrantBundle(gate, decisions, BundleAnswer{})
	if err != nil {
		t.Fatalf("granting a two-plugin bundle: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("a two-plugin bundle produced %d grants, want 2; every plugin the bundle ships must be granted, not only the first", len(grants))
	}
	byID := map[string][]string{}
	for _, g := range grants {
		byID[g.ID] = g.Granted
	}
	if len(byID["tick"]) != 2 {
		t.Errorf("plugin %q granted %v, want its two declared capabilities; a bundle grant is the plugin's declared set", "tick", byID["tick"])
	}
	if len(byID["clock"]) != 1 || byID["clock"][0] != "actions.register" {
		t.Errorf("plugin %q granted %v, want [actions.register]; each plugin's own declared set is granted, not a shared one", "clock", byID["clock"])
	}
}

// TestGrantBundleWithRememberPersistsEachIdentity proves "remember" writes N
// per-plugin rows, each keyed by that plugin's OWN identity — the property the
// rejected aggregate-identity reading would have destroyed. After a remembered
// grant, each plugin's standalone Decide must find it already granted, so the same
// plugin installed alone or carried by a second bundle is not re-prompted. The
// counterfactual is a fan-out that remembered one aggregate row: neither plugin's
// per-manifest identity would be found on record.
func TestGrantBundleWithRememberPersistsEachIdentity(t *testing.T) {
	store := NewMemoryConsentStore()
	gate := NewGate(store)
	m1, m2 := behavioralManifest(), secondBehavioralManifest()
	decisions := []BundlePluginDecision{
		needsConsent(gate, m1, consentTestDigest),
		needsConsent(gate, m2, consentTestDigest),
	}

	if _, err := GrantBundle(gate, decisions, BundleAnswer{Remember: true}); err != nil {
		t.Fatalf("granting-and-remembering a two-plugin bundle: %v", err)
	}
	for _, m := range []*Manifest{m1, m2} {
		if d := gate.Decide(m, consentTestDigest); d.Status != DecisionRemembered {
			t.Errorf("plugin %q was granted-and-remembered in the bundle but its own Decide is %v, want DecisionRemembered; a bundle grant must transfer to the standalone install of the same bytes", m.ID, d.Status)
		}
	}
}

// TestGrantBundleCarriesRememberedPluginsWithoutRegranting is the trusted-no-new-
// power property on the grant side: a plugin the gate already remembers is carried
// through with its remembered set and is NOT re-granted (no second Grant call, no
// re-persist). The counterfactual is a fan-out that Granted every decision
// unconditionally — it would re-ask the gate to grant a plugin the user is not being
// prompted about on this screen. Here the remembered plugin's stored set is narrower
// than its declared set, so a spurious re-grant (which grants m.Capabilities) would
// widen it and show up as a changed granted set.
func TestGrantBundleCarriesRememberedPluginsWithoutRegranting(t *testing.T) {
	store := NewMemoryConsentStore()
	gate := NewGate(store)
	m := behavioralManifest() // declares actions.register AND events.emit

	// Pre-remember a NARROWER grant than the manifest declares: only events.emit.
	if _, err := gate.Grant(m, consentTestDigest, []string{"events.emit"}, true); err != nil {
		t.Fatalf("seeding a remembered narrow grant: %v", err)
	}

	decisions := []BundlePluginDecision{{Manifest: m, Digest: consentTestDigest, Decision: gate.Decide(m, consentTestDigest)}}
	if decisions[0].Decision.Status != DecisionRemembered {
		t.Fatalf("precondition: the seeded plugin must Decide as Remembered, got %v", decisions[0].Decision.Status)
	}

	grants, err := GrantBundle(gate, decisions, BundleAnswer{})
	if err != nil {
		t.Fatalf("granting a bundle with a remembered plugin: %v", err)
	}
	if len(grants) != 1 || len(grants[0].Granted) != 1 || grants[0].Granted[0] != "events.emit" {
		t.Errorf("remembered plugin carried grant %v, want the narrow remembered [events.emit]; a re-grant would have widened it to the full declared set", grants[0].Granted)
	}
}

// TestGrantBundleAbortsOnAGrantRefusalBeforeCompose is the grant-then-compose
// atomicity property: if any plugin's Grant refuses (here a manifest declaring a
// capability outside the closed set), the fan-out returns an error and the caller
// composes nothing. The counterfactual is a fan-out that skipped or swallowed the
// Grant error — it would return a partial grant list and let the loop compose a
// bundle whose rogue plugin was never legally granted.
func TestGrantBundleAbortsOnAGrantRefusalBeforeCompose(t *testing.T) {
	gate := NewGate(NewMemoryConsentStore())
	good, rogue := behavioralManifest(), unknownCapManifest()
	decisions := []BundlePluginDecision{
		needsConsent(gate, good, consentTestDigest),
		needsConsent(gate, rogue, consentTestDigest),
	}

	grants, err := GrantBundle(gate, decisions, BundleAnswer{})
	if err == nil {
		t.Fatal("a bundle with a plugin declaring an unknown capability was granted; the fan-out must abort so the loop composes nothing")
	}
	if grants != nil {
		t.Errorf("an aborted fan-out returned %d grants; on a refusal nothing may be handed to the loop to compose", len(grants))
	}
}

// TestConsentsForMirrorsTheGateDecision proves the projection the screen renders
// agrees with the gate: a remembered plugin is marked Remembered, an unseen one is
// not. The counterfactual is a projection that flipped the flag — the screen would
// re-detail a trusted plugin (asking again) or list a fresh grant as trusted
// (hiding the power the user is about to give).
func TestConsentsForMirrorsTheGateDecision(t *testing.T) {
	store := NewMemoryConsentStore()
	gate := NewGate(store)
	seen, unseen := behavioralManifest(), secondBehavioralManifest()
	if _, err := gate.Grant(seen, consentTestDigest, seen.Capabilities, true); err != nil {
		t.Fatalf("seeding a remembered grant: %v", err)
	}

	consents := ConsentsFor([]BundlePluginDecision{
		{Manifest: seen, Digest: consentTestDigest, Decision: gate.Decide(seen, consentTestDigest)},
		{Manifest: unseen, Digest: consentTestDigest, Decision: gate.Decide(unseen, consentTestDigest)},
	})
	if len(consents) != 2 {
		t.Fatalf("ConsentsFor projected %d consents from 2 decisions", len(consents))
	}
	if !consents[0].Remembered {
		t.Errorf("the already-granted plugin %q was not marked Remembered; the screen would re-detail and re-ask a trusted plugin", seen.ID)
	}
	if consents[1].Remembered {
		t.Errorf("the unseen plugin %q was marked Remembered; the screen would hide the power the user is about to grant", unseen.ID)
	}
}
