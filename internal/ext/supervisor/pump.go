package supervisor

import (
	"github.com/michiTrader/arxi_tui/internal/ext"
)

// DrainInto streams this supervisor's forwarded frames into the host-owned
// plugin store, mapping each `bind` frame into the plugin's `<plugin-id>.*`
// namespace and writing the `ui.plugin.<id>` liveness bind from the supervisor's
// lifecycle (I3 / ADR-0007 §I-D, §I-G). It is the concrete "stream NDJSON frames
// into binds" verb: I2 forwards raw frames on Frames() and keeps the wire alive;
// this is the consumer that turns them into resolvable values.
//
// It BLOCKS until Frames() closes, so the caller runs it on its own goroutine —
// the loop's, once a plugin is mounted (I5). It cannot capture the escape hatch
// (invariant 6): it only reads a channel and writes a store, exactly as the
// animation tick case only ever adds a reason to repaint. The store's own mutex
// makes the writes safe against a render goroutine snapshotting concurrently.
//
// Ingest errors are DROPPED here, not fatal (§I-G): a single malformed or
// undeclared-field frame is the offending frame dropped, not the process killed
// — the repeated-malformed-stream kill is a supervisor decision, not a store one.
// A store that refused the whole plugin on one bad frame would let a plugin's
// typo blank its good binds.
func (s *Supervisor) DrainInto(store *ext.PluginStore) {
	id := s.cfg.Manifest.ID
	binds := s.cfg.Manifest.Binds

	// starting: the process is up (Start spawned it) but has published nothing
	// yet. The first forwarded frame is I2's observable proof the handshake acked
	// (a frame is only ever forwarded after the ack), so it is the signal this
	// bridge has for "live" without reaching into the supervisor's internals. A
	// live-but-silent plugin therefore reads "starting" until its first publish;
	// promoting it to "live" at handshake time rather than first-frame needs a
	// supervisor lifecycle signal I2 does not expose, and is the noted refinement
	// rather than a value invented here.
	store.SetLiveness(id, ext.PluginStarting)
	live := false
	for f := range s.Frames() {
		if !live {
			store.SetLiveness(id, ext.PluginLive)
			live = true
		}
		if f.Type == "bind" {
			_ = store.Ingest(id, binds, f.Raw)
		}
		// Other frame types (an action ok/error correlation) are I4's; a type this
		// bridge does not consume is ignored rather than dropped from the wire,
		// which is why I2 forwards every type verbatim.
	}

	// Frames() closed: supervision ended. Err() distinguishes the two terminal
	// states §I-G separates. A non-nil error is a death (fatal protocol or the
	// restart budget exhausted): the last-published values STAY in the store,
	// frozen and stale, and the liveness bind goes "dead" so the scene can say so
	// — the diagnosis the frozen placeholder cannot carry. A nil error is a clean
	// Close, i.e. /ui plugin remove <id>: the plugin's whole namespace is dropped
	// so the composed scene draws as it did before the mount (§I-F).
	if s.Err() != nil {
		store.SetLiveness(id, ext.PluginDead)
		return
	}
	store.DropPlugin(id)
}
