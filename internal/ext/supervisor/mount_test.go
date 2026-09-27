package supervisor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// These tests pin I5's orchestration: Mount decides consent by identity, prompts
// only when unseen, and spawns+registers+pumps ONLY on a grant. The observable
// proof is the helper's published `price` frame reaching the store — the helper
// exits non-zero if the ack's granted set is wrong (ARXI_EXPECT_GRANTED), so a
// value in the store proves the gate's granted subset, and nothing else, reached
// the child.

// mountHelperConfig builds a Config whose manifest DECLARES capabilities (so the
// gate can grant them) and whose helper expects an exact granted set in its ack.
// declared is what the manifest asks for; expect is what the helper demands the
// ack carry — the two differ in the caller-supplied-granted test, which is the
// whole point of that counterfactual. Binds declares tick.price so the pump's
// Ingest accepts the helper's published frame.
func mountHelperConfig(id string, declared, expect []string) Config {
	env := []string{"ARXI_EXT_HELPER=1", "ARXI_MODE=", "ARXI_EXPECT_ID=" + id}
	if len(expect) > 0 {
		env = append(env, "ARXI_EXPECT_GRANTED="+strings.Join(expect, ","))
	}
	return Config{
		Manifest: ext.Manifest{
			ID:           id,
			Name:         "helper",
			Version:      "1",
			Protocol:     "ext/v1",
			Executable:   os.Args[0],
			Args:         []string{"-test.run=^TestHelperProcess$"},
			Capabilities: declared,
			Binds:        map[string]ext.BindDecl{id + ".price": {Kind: "text"}},
		},
		Environment:      env,
		HandshakeTimeout: 2 * time.Second,
		ShutdownTimeout:  time.Second,
		InitialBackoff:   time.Millisecond,
		FrameBuffer:      8,
	}
}

// waitForBind polls the store until the plugin's price bind lands or the deadline
// passes. The pump owns Frames(), so a Mount test observes through the store
// rather than the channel — reading Frames() directly would compete with the
// pump goroutine for the same frames.
func waitForBind(t *testing.T, store *ext.PluginStore, key string) bool {
	t.Helper()
	deadline := time.After(6 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if _, ok := store.Snapshot()[key]; ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// grantAll is a Prompt that grants exactly the declared capabilities and asks to
// remember them — the "user approved everything" answer, used where the test is
// about the mechanism rather than the subset.
func grantAll(remember bool) Prompt {
	return func(m *ext.Manifest, declared []string) (ConsentAnswer, error) {
		return ConsentAnswer{Granted: append([]string(nil), declared...), Remember: remember}, nil
	}
}

// TestMountSpawnsRegistersAndRemembers is the happy path across two mounts. The
// first mount is unseen, so the prompt fires, consent is granted-and-remembered,
// the plugin spawns, its bind lands in the store, and the registry routes to it.
// The second mount of the same identity must NOT prompt (the grant is
// remembered) and must still spawn. This is the silent-remount property of Q15.
func TestMountSpawnsRegistersAndRemembers(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	store := ext.NewPluginStore()
	reg := NewRegistry()

	prompted := 0
	prompt := func(m *ext.Manifest, declared []string) (ConsentAnswer, error) {
		prompted++
		return ConsentAnswer{Granted: []string{capActionsRegister}, Remember: true}, nil
	}

	cfg := mountHelperConfig("tick", []string{capActionsRegister}, []string{capActionsRegister})
	s, err := Mount(context.Background(), gate, cfg, "digest-a", store, reg, prompt)
	if err != nil {
		t.Fatalf("Mount refused a granted plugin: %v\n"+
			"consequence: a consented behavioral plugin never starts, so the whole behavioral path is dead on arrival.", err)
	}
	defer s.Close()

	if !waitForBind(t, store, "tick.price") {
		t.Fatal("the mounted plugin's bind never reached the store\n" +
			"consequence: either the plugin was spawned with the wrong granted set (the helper exited on a bad ack) or the pump was never launched — a mounted plugin renders forever empty.")
	}
	if prompted != 1 {
		t.Fatalf("the first, unseen mount prompted %d times; want exactly 1\n"+
			"consequence: an unseen plugin either spawns with no consent (0) or nags repeatedly — the gate must ask once.", prompted)
	}
	// The registry routes by id: a live, granted plugin answers SendAction rather
	// than ErrPluginNotMounted.
	if err := reg.SendAction("tick", "refresh", nil); err != nil {
		t.Fatalf("registry did not route to the mounted plugin: %v\n"+
			"consequence: an ext:tick:refresh press cannot find the process Mount just started — the routing half of the mount never happened.", err)
	}

	// Second mount of the SAME identity: remembered, so no prompt.
	s2, err := Mount(context.Background(), gate, cfg, "digest-a", store, reg, prompt)
	if err != nil {
		t.Fatalf("re-mount of a remembered plugin failed: %v", err)
	}
	defer s2.Close()
	if prompted != 1 {
		t.Errorf("re-mounting a remembered plugin prompted again (total %d); want still 1\n"+
			"consequence: \"remember\" bought nothing — a trusted plugin re-asks on every mount (Q15).", prompted)
	}
}

// TestMountDoesNotSpawnOnReject pins that a rejection spawns NOTHING: no process,
// no registry entry, no liveness. The rejection is session-local (the gate
// remembered nothing), which the returned ErrConsentRejected lets the caller
// report distinctly from a failure.
func TestMountDoesNotSpawnOnReject(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	store := ext.NewPluginStore()
	reg := NewRegistry()

	reject := func(m *ext.Manifest, declared []string) (ConsentAnswer, error) {
		return ConsentAnswer{Rejected: true}, nil
	}
	cfg := mountHelperConfig("tick", []string{capActionsRegister}, nil)
	s, err := Mount(context.Background(), gate, cfg, "digest-a", store, reg, reject)
	if !errors.Is(err, ErrConsentRejected) {
		t.Fatalf("Mount on a rejected plugin returned err=%v (supervisor=%v); want ErrConsentRejected\n"+
			"consequence: a plugin the user refused still ran — the gate's rejection did not stop the spawn (Q15).", err, s)
	}
	if s != nil {
		t.Error("Mount returned a live supervisor for a rejected plugin; a rejection must spawn no process")
	}
	if _, ok := store.Snapshot()["ui.plugin.tick"]; ok {
		t.Error("a rejected plugin has a liveness bind; nothing about it should reach the store")
	}
	if err := reg.SendAction("tick", "refresh", nil); !errors.Is(err, ErrPluginNotMounted) {
		t.Errorf("a rejected plugin is routable (err=%v); it must be absent from the registry", err)
	}
}

// TestMountIgnoresCallerSuppliedGranted is the load-bearing counterfactual for
// "the gate is the only authority over Granted" (invariant 7). The caller
// pre-fills cfg.Granted with a SUPERSET, but the prompt grants only
// actions.register and the helper's ack expectation is only actions.register. If
// Mount honored cfg.Granted, the child would see two capabilities, exit on the
// mismatch, and no bind would land. The bind landing proves cfg.Granted was
// overwritten by the gate's decision.
func TestMountIgnoresCallerSuppliedGranted(t *testing.T) {
	gate := ext.NewGate(ext.NewMemoryConsentStore())
	store := ext.NewPluginStore()
	reg := NewRegistry()

	cfg := mountHelperConfig("tick", []string{capActionsRegister, "events.emit"}, []string{capActionsRegister})
	// The smuggling attempt: a caller fills Granted with more than will be granted.
	cfg.Granted = []string{capActionsRegister, "events.emit"}

	// The prompt grants only the one capability, so the ack must carry only it.
	prompt := func(m *ext.Manifest, declared []string) (ConsentAnswer, error) {
		return ConsentAnswer{Granted: []string{capActionsRegister}, Remember: false}, nil
	}
	s, err := Mount(context.Background(), gate, cfg, "digest-a", store, reg, prompt)
	if err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	defer s.Close()

	if !waitForBind(t, store, "tick.price") {
		t.Fatal("no bind landed, so the child rejected its ack\n" +
			"consequence: cfg.Granted (a superset) reached the child instead of the gate's decision — a caller smuggled a capability past the gate (invariant 7).")
	}
}
