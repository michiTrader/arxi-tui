package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// tickManifestBinds is the declared bind map matching the frame TestHelperProcess
// publishes ({"field":"price"}), so DrainInto's Ingest accepts it. Without a
// declaration the store would (correctly) refuse the field, so a pump test that
// forgot this would be testing the refusal path, not the stream path.
func tickManifestBinds() map[string]ext.BindDecl {
	return map[string]ext.BindDecl{"tick.price": {Kind: "text"}}
}

// TestDrainIntoStreamsBindsAndGoesLive is the end-to-end I3 path over a real
// subprocess: a live plugin publishes a bind frame, DrainInto maps it into the
// store under the composed <plugin-id>.* path, and the liveness bind reaches
// "live". It closes the loop the store and render tests each pin one end of —
// the supervisor's forwarded frame actually becomes a resolvable value.
func TestDrainIntoStreamsBindsAndGoesLive(t *testing.T) {
	cfg := helperConfig("", "tick", nil)
	cfg.Manifest.Binds = tickManifestBinds()
	store := ext.NewPluginStore()
	s := Start(context.Background(), cfg)
	defer s.Close()

	done := make(chan struct{})
	go func() { s.DrainInto(store); close(done) }()

	// The helper stays alive after publishing, so DrainInto blocks; poll the store
	// for the streamed value rather than waiting on the goroutine.
	deadline := time.After(6 * time.Second)
	for {
		snap := store.Snapshot()
		if snap["tick.price"] == "$1.23" {
			if snap["ui.plugin.tick"] != ext.PluginLive {
				t.Errorf("the plugin published a frame but ui.plugin.tick=%q, want %q\n"+
					"consequence: a streaming plugin is not reported live, so a scene gating on its liveness cannot tell it apart from one that never started.", snap["ui.plugin.tick"], ext.PluginLive)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("tick.price never reached the store within 6s (snapshot=%v)\n"+
				"consequence: the supervisor forwards frames but nothing maps them into binds, so a behavioral plugin's stream is invisible — the whole of I3.", store.Snapshot())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// A clean Close is /ui plugin remove <id>: DrainInto must drop the namespace.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-done
	snap := store.Snapshot()
	if _, ok := snap["tick.price"]; ok {
		t.Errorf("after a clean Close the plugin's value survived (snapshot=%v)\n"+
			"consequence: /ui plugin remove leaves stale values behind, so a removed plugin keeps drawing (§I-F).", snap)
	}
	if _, ok := snap["ui.plugin.tick"]; ok {
		t.Error("after a clean Close the liveness bind survived; an unmount erases it so the bind is falsy again")
	}
}

// TestDrainIntoFreezesAtLastValuesOnDeath is the fork-4 terminal state: when the
// process dies and the restart budget is exhausted, the last-published values
// STAY in the store (frozen, stale, non-crashing — the fold is untouched) and the
// liveness bind goes "dead". This is the pair to the clean-Close drop above: the
// two terminal states §I-G separates must not behave alike, or a dead plugin's
// last frame would vanish exactly when the user needs to see it went stale.
func TestDrainIntoFreezesAtLastValuesOnDeath(t *testing.T) {
	cfg := helperConfig("die", "tick", nil)
	cfg.MaxRestarts = 0 // one launch, then the exit is a terminal death
	cfg.Manifest.Binds = tickManifestBinds()
	store := ext.NewPluginStore()
	s := Start(context.Background(), cfg)
	defer s.Close()

	done := make(chan struct{})
	go func() { s.DrainInto(store); close(done) }()

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("DrainInto did not return after the plugin died within 6s")
	}
	snap := store.Snapshot()
	if snap["tick.price"] != "$1.23" {
		t.Errorf("after the plugin died tick.price=%q, want the last-published %q\n"+
			"consequence: death erased the last values instead of freezing them, so a scene shows an empty placeholder rather than stale-but-informative data (§I-G / invariant 2).", snap["tick.price"], "$1.23")
	}
	if snap["ui.plugin.tick"] != ext.PluginDead {
		t.Errorf("after the plugin died ui.plugin.tick=%q, want %q\n"+
			"consequence: the death carries no diagnosis, so the frozen values look live and the user cannot tell the plugin stopped.", snap["ui.plugin.tick"], ext.PluginDead)
	}
}
