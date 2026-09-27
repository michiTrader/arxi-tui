package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// consentTestManifest is a minimal behavioral manifest for the loop's consent
// wiring: it carries every field the identity tuple reads, so a grant remembered
// against it is keyed by a real identity, not a placeholder. It mirrors the
// behavioralManifest helper the ext package tests use — kept local because that
// one is an unexported test helper in another package.
func consentTestManifest() *ext.Manifest {
	return &ext.Manifest{
		ID:           "tick",
		Name:         "Community Ticker",
		Version:      "1.0.0",
		Protocol:     "ext/v1",
		Executable:   "./tick",
		Args:         []string{"--interval", "5s"},
		Capabilities: []string{"actions.register", "events.emit"},
	}
}

const consentLoopTestDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// TestConsentStorePathHonorsOverride checks the ARXI_CONSENT_FILE override wins
// outright. The override is what a test and a user with a non-default config home
// both rely on; if the resolver ignored it and always returned the home default,
// every test would fight over one real file in the developer's home and the
// wiring below could not be exercised in isolation.
func TestConsentStorePathHonorsOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom-consent.json")
	t.Setenv("ARXI_CONSENT_FILE", want)
	got, err := consentStorePath()
	if err != nil {
		t.Fatalf("consentStorePath with override set: %v", err)
	}
	if got != want {
		t.Fatalf("consentStorePath() = %q; want the ARXI_CONSENT_FILE value %q.\n"+
			"consequence: the override is ignored, so a user with a non-default config home writes grants to the wrong file and tests share one real path.\n"+
			"remedy: return the ARXI_CONSENT_FILE value verbatim when it is set.", got, want)
	}
}

// TestConsentStorePathDefaultsUnderHome checks the default lands in the same
// ~/.arxi tree the run log uses. The exact leaf matters because it is the file
// name every future run will look for a remembered grant under; a drift here
// silently orphans every grant a prior version wrote.
func TestConsentStorePathDefaultsUnderHome(t *testing.T) {
	t.Setenv("ARXI_CONSENT_FILE", "")
	got, err := consentStorePath()
	if err != nil {
		t.Fatalf("consentStorePath with no override: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir in this environment: %v", err)
	}
	want := filepath.Join(home, ".arxi", "consent.json")
	if got != want {
		t.Fatalf("consentStorePath() = %q; want %q.\n"+
			"consequence: the default consent file moved, so every grant a prior run remembered is orphaned and the user is re-asked for all of them.\n"+
			"remedy: keep the default at ~/.arxi/consent.json unless the move is intended and every reader is updated together.", got, want)
	}
}

// TestOpenConsentGateRemembersAcrossReopen is the whole point of the config
// layer: a grant the user chose to remember must survive a restart, and the only
// way to prove the loop's gate is actually disk-backed (not the session store the
// fallback returns) is to grant through one gate, build a second from the same
// path, and see the second answer Remembered without a prompt. If openConsentGate
// silently handed back a MemoryConsentStore, the second Decide would be
// NeedsConsent and this fails.
func TestOpenConsentGateRemembersAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	t.Setenv("ARXI_CONSENT_FILE", path)
	m := consentTestManifest()

	gate, warn := openConsentGate()
	if warn != "" {
		t.Fatalf("openConsentGate on a fresh (missing) file warned %q; a first run has no file yet and that is not an error.", warn)
	}
	if _, err := gate.Grant(m, consentLoopTestDigest, m.Capabilities, true); err != nil {
		t.Fatalf("Grant with remember: %v", err)
	}

	reopened, warn := openConsentGate()
	if warn != "" {
		t.Fatalf("reopening the gate after a remembered grant warned %q; the file we just wrote must load clean.", warn)
	}
	dec := reopened.Decide(m, consentLoopTestDigest)
	if dec.Status != ext.DecisionRemembered {
		t.Fatalf("after a remembered grant, a reopened gate decided %v; want DecisionRemembered.\n"+
			"consequence: \"remember\" does not survive a restart — the gate the loop builds is not disk-backed, so every remembered plugin re-prompts on the next launch.\n"+
			"remedy: openConsentGate must build the gate over the DiskConsentStore at the resolved path, not fall back to a session store when the file is fine.", dec.Status)
	}
}

// TestOpenConsentGateFallsBackWithoutErasingMalformedFile pins the fallback
// decision: a corrupt consent file must not brick the TUI, and it must not be
// silently reset. The gate still comes up (over a session store), a warning names
// the file, and — the counterfactual that matters — the malformed bytes are still
// on disk afterwards, so a user can recover the grants a reset would have
// destroyed.
func TestOpenConsentGateFallsBackWithoutErasingMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	t.Setenv("ARXI_CONSENT_FILE", path)
	garbage := []byte("{ this is not valid json")
	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatalf("seeding a malformed consent file: %v", err)
	}

	gate, warn := openConsentGate()
	if gate == nil {
		t.Fatal("openConsentGate returned a nil gate on a malformed file; the TUI must still come up so the user can read the warning and keep using every non-plugin surface.")
	}
	if warn == "" {
		t.Fatal("openConsentGate on a malformed file returned no warning; a silently-swallowed corrupt allow-list is indistinguishable from a fresh install, which is the data-loss the store refuses to hide.")
	}
	if !strings.Contains(warn, path) {
		t.Errorf("the malformed-file warning %q does not name the file %q; the user cannot inspect a file the warning does not point at.", warn, path)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the consent file is gone after fallback: %v; a corrupt file must be left for the user to recover, not deleted.", err)
	}
	if string(after) != string(garbage) {
		t.Fatalf("the malformed consent file was rewritten during fallback (now %q); resetting it destroys the grants the user could otherwise recover, which is exactly the silent loss the fallback exists to avoid.", string(after))
	}
}
