package supervisor

import (
	"fmt"
	"strconv"
	"sync"
)

// Registry is the host's set of live behavioral plugins, keyed by plugin id. It
// is what the loop holds so an `on_press` ext:<plugin-id>:<action> press can find
// the right subprocess to route to (I4). Mounting a plugin (I5, behind consent)
// Adds its supervisor; /ui plugin remove Removes it; a press SendActions through
// it. The registry owns no process lifecycle itself — Close is the supervisor's —
// it only routes by id.
//
// It is the action-routing companion to the PluginStore: the store is where a
// plugin's bind values land (plugin→host), the registry is where a press is
// routed (host→plugin). Both are host-owned view-side state the loop holds beside
// ui.hidden, neither in the fold (invariant 2).
type Registry struct {
	mu     sync.Mutex
	byID   map[string]*Supervisor
	nextID uint64
}

// NewRegistry returns an empty registry. An empty registry routes every press to
// ErrPluginNotMounted, which the host reports as "no such plugin" — the honest
// state before any plugin is mounted, and the state the loop is in until I5 wires
// the consent-gated spawn.
func NewRegistry() *Registry {
	return &Registry{byID: map[string]*Supervisor{}}
}

// ErrPluginNotMounted is a press naming a plugin id no live plugin answers to. It
// is reported to the user, never a crash: a scene may name ext:tick:refresh
// before the tick plugin is mounted (the load-vs-runtime split the scene layer
// leaves to dispatch), and a stale button after an unmount lands here too.
var ErrPluginNotMounted = fmt.Errorf("no behavioral plugin with that id is mounted")

// Add registers a live supervisor under a plugin id, replacing any prior one so a
// re-mount is not two entries. The caller (I5) owns starting and closing the
// supervisor; the registry only holds the reference for routing.
func (r *Registry) Add(pluginID string, s *Supervisor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[pluginID] = s
}

// Remove drops a plugin from the routing table — the routing half of /ui plugin
// remove <id> (§I-F). It does not Close the supervisor: unmount closes the
// process and then removes it here, so a press arriving after removal is routed
// to ErrPluginNotMounted rather than to a supervisor being torn down.
func (r *Registry) Remove(pluginID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, pluginID)
}

// SendAction routes a parsed ext: press to the named plugin, generating the
// wire-correlation id the host owns (the plugin never chooses it). It resolves
// the supervisor by id, then delegates to Supervisor.SendAction, which applies
// the capability gate and writes the frame. A missing plugin is
// ErrPluginNotMounted; a live plugin missing the capability or currently down is
// the supervisor's ErrCapabilityNotGranted / ErrPluginNotLive — three distinct
// reports so the user can tell "never mounted" from "not permitted" from "down."
//
// args carries the {row.field} values the host has already resolved (§I-E);
// today a plain ext:<id>:<action> press carries none, and the per-element
// {row.field} substitution rides on the same template-row dispatch H8 parked, so
// this is where that resolved map arrives once that lands.
func (r *Registry) SendAction(pluginID, action string, args map[string]string) error {
	r.mu.Lock()
	s, ok := r.byID[pluginID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrPluginNotMounted, pluginID)
	}
	r.nextID++
	actionID := "a" + strconv.FormatUint(r.nextID, 10)
	r.mu.Unlock()
	return s.SendAction(actionID, action, args)
}
