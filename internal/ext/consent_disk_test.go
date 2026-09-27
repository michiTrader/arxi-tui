package ext

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiskStorePersistsAcrossReopen is the whole reason the disk store exists:
// a remembered grant must survive the process, not just a re-mount in one
// session (MemoryConsentStore's limit). Remember on one store, then open a fresh
// store at the same path — the "restart" — and the grant is still there.
// Counterfactual (run by hand): making Remember skip flushLocked leaves the file
// absent, the reopened store empty, and this test fails on the missing grant.
func TestDiskStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	s, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatalf("opening a fresh store: %v", err)
	}
	s.Remember("id-a", []string{"actions.register", "events.emit"})
	if err := s.Err(); err != nil {
		t.Fatalf("Remember reported a write error: %v\nconsequence: the grant never reached disk, so \"remember\" bought nothing across a restart (Q15).", err)
	}

	reopened, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatalf("reopening the store: %v", err)
	}
	got, ok := reopened.Lookup("id-a")
	if !ok {
		t.Fatalf("a remembered grant was gone after reopen\nconsequence: the plugin the user chose to remember re-asks on every host restart — the disk store is doing nothing the memory store did not (Q15).")
	}
	if len(got) != 2 || got[0] != "actions.register" || got[1] != "events.emit" {
		t.Fatalf("reopened grant = %v; want [actions.register events.emit]\nconsequence: the persisted granted subset does not round-trip, so the ack after a restart tells the plugin the wrong powers.", got)
	}
}

// TestDiskStoreReGrantReplacesNotAppends pins that a second Remember for the same
// identity replaces the set rather than accumulating — a user who narrows a grant
// must not have the old wider set survive on disk beside the new one. This is the
// disk face of the memory store's "a re-grant is not two entries."
func TestDiskStoreReGrantReplacesNotAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	s, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Remember("id-a", []string{"actions.register", "events.emit"})
	s.Remember("id-a", []string{"actions.register"})

	reopened, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reopened.Lookup("id-a")
	if len(got) != 1 || got[0] != "actions.register" {
		t.Fatalf("after narrowing the grant, reopened set = %v; want [actions.register]\nconsequence: a capability the user revoked survives on disk, so narrowing a grant does not actually take away the power (invariant 7).", got)
	}
}

// TestDiskStoreMalformedFileIsRefusedNotSilentlyEmptied is the counterfactual to
// "a missing file is first run": a file that EXISTS but does not parse is a
// data-loss event, not a fresh install, so Open must refuse rather than start
// empty. Counterfactual (run by hand): making Unmarshal errors fall through to an
// empty store returns (store, nil) here and fails this test — and in production
// would silently re-ask for every remembered plugin, hiding the corruption.
func TestDiskStoreMalformedFileIsRefusedNotSilentlyEmptied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenDiskConsentStore(path)
	if err == nil {
		t.Fatalf("a malformed consent file opened without error (store=%v)\nconsequence: a corrupt or truncated consent file starts the host empty, indistinguishable from first run — every remembered grant is silently forgotten and re-asked. Remedy: OpenDiskConsentStore must return an error on unparseable content.", s)
	}
}

// TestDiskStoreMissingFileIsFirstRun pins the other half: a path that does not
// exist yet is the ordinary first run, so Open succeeds with an empty store. A
// store that errored on a missing file would make the very first mount on a clean
// machine fail.
func TestDiskStoreMissingFileIsFirstRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist-yet", "consent.json")
	s, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatalf("opening a store at a nonexistent path errored: %v\nconsequence: the first ever mount on a clean machine fails before the consent gate is even consulted.", err)
	}
	if _, ok := s.Lookup("anything"); ok {
		t.Fatalf("a fresh store reported a remembered grant\nconsequence: the gate would skip the consent prompt for a plugin it has never seen — download silently becoming grant.")
	}
}

// TestDiskStoreLookupReturnsACopy pins the same ownership rule the memory store
// has: a caller mutating the returned slice must not alter the store's authority.
func TestDiskStoreLookupReturnsACopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	s, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Remember("id-a", []string{"actions.register"})
	got, _ := s.Lookup("id-a")
	got[0] = "events.emit"
	again, _ := s.Lookup("id-a")
	if again[0] != "actions.register" {
		t.Fatalf("mutating a Lookup result changed the stored grant to %v\nconsequence: a caller could widen a remembered grant in place for the next Lookup — the store must own the authority it hands back.", again)
	}
}

// TestDiskStoreBackedGateRemembersAcrossReopen proves the disk store satisfies the
// gate's "remember across restart" purpose end to end: a Gate over one disk store
// Grants with remember, a Gate over a freshly reopened store at the same path
// answers the same identity from memory with no prompt. This is the property the
// loop will rely on when the config layer points a gate at a persisted file.
func TestDiskStoreBackedGateRemembersAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consent.json")
	store, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGate(store)
	m := behavioralManifest()
	if _, err := g.Grant(m, "digest-a", []string{"actions.register"}, true); err != nil {
		t.Fatalf("Grant refused a declared capability: %v", err)
	}

	reopened, err := OpenDiskConsentStore(path)
	if err != nil {
		t.Fatal(err)
	}
	g2 := NewGate(reopened)
	d := g2.Decide(m, "digest-a")
	if d.Status != DecisionRemembered {
		t.Fatalf("a gate over the reopened disk store decided %v; want DecisionRemembered\nconsequence: a grant the user chose to remember does not survive a host restart, so the disk store fails the one job it exists for (Q15).", d.Status)
	}
	if len(d.Granted) != 1 || d.Granted[0] != "actions.register" {
		t.Fatalf("reopened decision carried granted=%v; want [actions.register]\nconsequence: the remembered subset is not the one carried to supervisor.Config.Granted after a restart.", d.Granted)
	}
}
