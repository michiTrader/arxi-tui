package supervisor

import (
	"context"
	"fmt"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// This file is the I5 orchestration: the one verb that turns a loaded behavioral
// manifest into a running, registered, pumping plugin — but only after the
// consent gate says so. It is the seam I3 (DrainInto) and I4 (Registry) each
// pointed at with "the live mount is I5": here the goroutine that pumps frames
// into the store is launched, and here the supervisor is Added to the routing
// table an ext: press resolves against.
//
// The order is load-time and non-negotiable (§I-F): the gate decides BEFORE any
// process is spawned, because "download ≠ trust ≠ grant" (Q15) and a spawned
// process is already acting. Mount never spawns on a rejection and never spawns
// before Grant returns the consented subset.

// ConsentAnswer is the user's reply to a consent prompt for a plugin the gate
// has not seen before. Granted is the subset of the manifest's declared
// capabilities the user approved; Remember asks the gate to persist that grant
// against the plugin's identity; Rejected is an explicit no, kept distinct from
// an empty Granted so "run it with no capabilities" and "do not run it" are
// different answers — the first spawns a powerless plugin, the second spawns
// nothing.
type ConsentAnswer struct {
	Granted  []string
	Remember bool
	Rejected bool
}

// Prompt is the host's user-facing consent screen, called ONLY when the gate
// returns DecisionNeedsConsent — a remembered plugin never prompts. It receives
// the manifest and the capabilities it declared (what the screen must show) and
// returns the user's answer. It is a callback so the mount orchestration carries
// no UI: a test supplies a fixed answer, and the real loop supplies the consent
// scene. An error from the prompt (the user could not be asked) aborts the mount
// without spawning, which is the safe default — no answer is not a yes.
type Prompt func(m *ext.Manifest, declared []string) (ConsentAnswer, error)

// ErrConsentRejected is a mount the user explicitly refused at the gate. It is
// returned rather than swallowed so the caller can report "you rejected this
// plugin" distinctly from a load or spawn failure — the rejection is
// session-local (nothing was remembered), so a later mount re-prompts.
var ErrConsentRejected = fmt.Errorf("plugin consent rejected")

// Mount runs the full consent-gated lifecycle for one behavioral plugin: decide
// by identity, prompt if unseen, then — only on a grant — spawn the supervisor
// with the consented subset, register it for action routing, and launch the pump
// that streams its frames into the store. It returns the live supervisor so the
// caller can Close it on /ui plugin remove.
//
// digest is the loader's PackageDigest over the fetched package; it is half the
// identity, so passing a stale or empty digest silently makes every mount look
// like a different plugin. cfg carries the Manifest and the process-shaping
// fields (timeouts, backoff); cfg.Granted is IGNORED and overwritten — the gate
// is the only authority over granted power (invariant 7), so a caller cannot
// pre-fill it to smuggle a capability past the gate.
func Mount(ctx context.Context, gate *ext.Gate, cfg Config, digest string, store *ext.PluginStore, reg *Registry, prompt Prompt) (*Supervisor, error) {
	m := &cfg.Manifest

	decision := gate.Decide(m, digest)
	var granted []string
	switch decision.Status {
	case ext.DecisionRemembered:
		// A grant for these exact bytes is on record: use it verbatim, no prompt.
		// This is the silent-remount property — a trusted plugin comes up without
		// re-asking (Q15).
		granted = decision.Granted
	case ext.DecisionNeedsConsent:
		answer, err := prompt(m, m.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("consent prompt for plugin %q failed: %w", m.ID, err)
		}
		if answer.Rejected {
			return nil, fmt.Errorf("%w: %q", ErrConsentRejected, m.ID)
		}
		// Grant is the only path that reaches the granted set, so it re-checks the
		// answer against the declared and closed sets — a prompt that returned an
		// undeclared capability is refused here rather than carried to the child.
		granted, err = gate.Grant(m, digest, answer.Granted, answer.Remember)
		if err != nil {
			return nil, fmt.Errorf("granting consent for plugin %q: %w", m.ID, err)
		}
	default:
		return nil, fmt.Errorf("plugin %q: unhandled consent decision %v; a new DecisionStatus was added without a mount branch, so a plugin would spawn (or not) by accident", m.ID, decision.Status)
	}

	// The gate has spoken: the granted subset is now the only power the child
	// will ever be told it has. Overwrite cfg.Granted so a caller-supplied value
	// can never reach the ack (invariant 7).
	cfg.Granted = granted

	s := Start(ctx, cfg)
	reg.Add(m.ID, s)
	// The pump blocks until the process dies or is Closed, so it runs on its own
	// goroutine — the loop stays free (I3). On a clean Close it DropPlugins the
	// namespace; the caller's Close is what ends this goroutine.
	go func() {
		s.DrainInto(store)
		// A dead or removed plugin must not linger in the routing table, or a
		// later ext: press resolves to a supervisor that will only ever answer
		// ErrPluginNotLive. Removing here (rather than only on /ui plugin remove)
		// closes the window between a process death and the user unmounting it.
		reg.Remove(m.ID)
	}()
	return s, nil
}
