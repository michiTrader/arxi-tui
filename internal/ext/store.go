package ext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// PluginStore is the host-owned latest-value store that bridges a behavioral
// plugin's asynchronous NDJSON pushes to the host's pull-by-frame bind
// resolution (I3, ADR-0007 §I-D). A plugin's reader goroutine calls Ingest on
// each `bind` frame; the render loop reads a Snapshot once per repaint. The two
// are decoupled by this store precisely so the fold never waits on a plugin
// (invariant 2): a value arriving is view state, not a fold event, so geometry
// and scroll survive it and the escape hatch is never behind a plugin's I/O.
//
// It is deliberately NOT fold.State. A value rebuilt from the run log each frame
// would forget a pushed value the moment the next frame folded — and, worse, a
// plugin's `tick.price` entering the fold would make a stranger's process an
// authority over the run log, the exact authority split ADR-0003 and §4.4 keep
// apart. The store lives beside ui.hidden and the animation clock as host view
// state the loop re-attaches to the renderer each repaint.
//
// The store is the single source for two bind families the resolver consults
// before fold.State: a plugin's own `<plugin-id>.*` values, and the host-owned
// `ui.plugin.<id>` liveness bind (§4.3) the supervisor's lifecycle writes. The
// liveness bind lives here rather than in the plugin's namespace so a dying
// plugin cannot suppress the report of its own death.
type PluginStore struct {
	mu sync.Mutex
	// values maps a full bind path ("tick.price") to the latest rendered value.
	// The path is composed by the host from plugin_id + "." + field, never by the
	// plugin, so a plugin can only ever write inside its own namespace (§I-C).
	values map[string]string
	// liveness maps a plugin id to its ui.plugin.<id> status string. Absent means
	// no behavioral plugin with that id is mounted, so the bind is falsy for when.
	liveness map[string]string
}

// The four ui.plugin.<id> liveness statuses (BINDS.md §4.3). They are named
// constants rather than string literals so the supervisor lifecycle (I5) and the
// resolver name the same set, and so the wire-facing vocabulary stays English in
// one place. The empty string (no constant) is the falsy default: no plugin with
// that id is mounted.
const (
	// PluginStarting is a plugin spawned but pre-handshake: the process is up but
	// has not yet acked, so it has published nothing.
	PluginStarting = "starting"
	// PluginLive is a plugin whose handshake acked and which may now publish.
	PluginLive = "live"
	// PluginError is a plugin in restart backoff or emitting repeated malformed
	// frames: degraded but not yet given up on.
	PluginError = "error"
	// PluginDead is a plugin whose process has exited and whose restart budget is
	// exhausted; its last-published values remain in the store, frozen and stale.
	PluginDead = "dead"
)

// NewPluginStore returns an empty store. An empty store resolves every plugin
// bind to absent, which the resolver renders as the placeholder — the §I-G "a
// live mount shows the placeholder, not mock, before the first frame" rule.
func NewPluginStore() *PluginStore {
	return &PluginStore{
		values:   map[string]string{},
		liveness: map[string]string{},
	}
}

// bindFrame is the plugin→host `bind` line (§I-C). A frame carries EITHER a
// single field/value OR a batched binds map, never a mix the host would have to
// reconcile; the batched form exists so a coherent multi-field snapshot lands
// atomically for one repaint (fork 2), which under last-value-wins pull-by-frame
// is the only way to avoid a torn frame showing a new price beside an old
// series. `kind` is deliberately absent from the wire: it is declared once in the
// manifest binds map, and repeating it would let the two disagree, the same
// single-discriminator reasoning that made `executable` the sole declarative
// /behavioral switch (ADR-0006).
type bindFrame struct {
	Type  string                     `json:"type"`
	Field string                     `json:"field,omitempty"`
	Value json.RawMessage            `json:"value,omitempty"`
	Binds map[string]json.RawMessage `json:"binds,omitempty"`
}

// Ingest maps one plugin→host `bind` frame into the store under the plugin's
// namespace, validating every field against the manifest's `binds` map before it
// writes any of them. binds is the manifest's declared bind map (full paths →
// declaration); pluginID is the plugin's id, prepended to each relative field to
// form the full path the plugin never utters itself.
//
// The frame is validated whole and committed whole (§I-C fork 2): if any field
// is undeclared or its value's shape contradicts the declared kind, NOTHING is
// written and the error names the first offending field. A partial write is the
// torn frame the atomic-snapshot contract exists to prevent, so a batched frame
// with one bad field leaves the store exactly as it was rather than landing the
// good half. The error is the runtime companion to H5's load-time refusal: the
// manifest binds map is the single source, consulted directly, never copied.
func (s *PluginStore) Ingest(pluginID string, binds map[string]BindDecl, raw json.RawMessage) error {
	var f bindFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("plugin %q sent a bind frame that is not JSON: %v", pluginID, err)
	}
	pending, err := s.validateFrame(pluginID, binds, f)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for path, rendered := range pending {
		s.values[path] = rendered
	}
	return nil
}

// validateFrame resolves a frame into the full-path→rendered-value writes it
// would make, refusing the whole frame if any single field is undeclared or
// mis-shaped. It reads the frame's two mutually exclusive forms (single field,
// batched binds) into one pending set so the commit in Ingest is form-agnostic.
func (s *PluginStore) validateFrame(pluginID string, binds map[string]BindDecl, f bindFrame) (map[string]string, error) {
	raw := map[string]json.RawMessage{}
	switch {
	case f.Binds != nil:
		if f.Field != "" {
			return nil, fmt.Errorf("plugin %q sent a bind frame with both a single %q field and a batched binds map; a frame carries one form or the other so the host never has to reconcile them (ADR-0007 §I-C)", pluginID, f.Field)
		}
		for field, v := range f.Binds {
			raw[field] = v
		}
	case f.Field != "":
		raw[f.Field] = f.Value
	default:
		return nil, fmt.Errorf("plugin %q sent an empty bind frame (no field and no binds map); a bind frame publishes at least one value (ADR-0007 §I-C)", pluginID)
	}
	pending := make(map[string]string, len(raw))
	for _, field := range sortedKeys(raw) {
		path := pluginID + "." + field
		decl, ok := binds[path]
		if !ok {
			return nil, fmt.Errorf("plugin %q published field %q (path %q), which its manifest does not declare in `binds`; a plugin may only write the fields it declares, the runtime face of the declared-vs-used rule (BINDS.md §4.4)", pluginID, field, path)
		}
		if err := validateShape(decl.Kind, raw[field]); err != nil {
			return nil, fmt.Errorf("plugin %q published field %q: %v", pluginID, field, err)
		}
		pending[path] = renderValue(raw[field])
	}
	return pending, nil
}

// SetLiveness records a plugin's ui.plugin.<id> status (§4.3). The supervisor
// lifecycle drives it; the resolver reads it. An empty status is stored as a
// present-but-falsy value rather than a deletion so a scene gating on
// `when: ui.plugin.<id>` reads "not live" rather than "never mounted" while the
// process is between states — DropPlugin, not this, is how an unmount erases it.
func (s *PluginStore) SetLiveness(pluginID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveness[pluginID] = status
}

// DropPlugin removes a plugin's values and its liveness bind — the store half of
// /ui plugin remove <id> (§I-F). After it returns, every `<plugin-id>.*` bind and
// the `ui.plugin.<id>` bind resolve to absent again, so the composed scene draws
// as it did before the mount. It names no host field: a plugin only ever owns its
// own prefix.
func (s *PluginStore) DropPlugin(pluginID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := pluginID + "."
	for path := range s.values {
		if len(path) > len(prefix) && path[:len(prefix)] == prefix {
			delete(s.values, path)
		}
	}
	delete(s.liveness, pluginID)
}

// Snapshot returns a fresh copy of every resolvable plugin bind for one repaint:
// each plugin's published `<plugin-id>.*` values, plus the `ui.plugin.<id>`
// liveness bind for every mounted plugin. It is a copy so the render walk reads a
// stable frame while the reader goroutine keeps writing — the last-value-wins
// store overwrites under the lock, and a frame sees the values as of the instant
// it snapshotted, never a half-updated map. The resolver checks this map before
// fold.State, and an absent path falls through to the placeholder.
func (s *PluginStore) Snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.values)+len(s.liveness))
	for path, v := range s.values {
		out[path] = v
	}
	for id, status := range s.liveness {
		out["ui.plugin."+id] = status
	}
	return out
}

// validateShape refuses a value whose JSON shape contradicts its declared kind
// (§I-C). It is the wire-value analogue of scene's bindKindNodeTypes: that map
// says which node TYPE a kind may be drawn under, this says which value SHAPE a
// kind may carry on the wire. They are two axes of the same closed kind set
// (text, series; BINDS.md §4.4), kept in the two packages that own their
// respective axis rather than merged into one, because a plugin whose value shape
// is wrong is a runtime refusal and a scene whose node type is wrong is a
// load-time one.
func validateShape(kind string, value json.RawMessage) error {
	switch kind {
	case "text":
		// A text value is a JSON scalar — a string, number, or bool — the shape a
		// text or marquee node draws. An array or object has no scalar rendering,
		// so accepting it would store a value no text node could show.
		var scalar interface{}
		if err := json.Unmarshal(value, &scalar); err != nil {
			return fmt.Errorf("its declared kind is %q but its value is not valid JSON: %v", kind, err)
		}
		switch scalar.(type) {
		case string, float64, bool:
			return nil
		default:
			return fmt.Errorf("its declared kind is %q, which carries a scalar (string, number, or bool), but its value is %s", kind, shapeName(value))
		}
	case "series":
		// A series is a JSON array of numbers — the sparkline's numeric axis. The
		// whole window arrives each frame (never a delta) because last-value-wins
		// pull-by-frame requires each frame to be self-contained (§I-D).
		var nums []float64
		if err := json.Unmarshal(value, &nums); err != nil {
			return fmt.Errorf("its declared kind is %q, which carries an array of numbers, but its value is %s", kind, shapeName(value))
		}
		return nil
	default:
		// A kind the manifest validator accepted but this switch does not know is
		// a widening of the closed set that reached the manifest and not the wire.
		// Failing loudly here is the fifth-instance guard: a new kind must teach
		// both axes, not silently store an unshaped value.
		return fmt.Errorf("its declared kind is %q, which the wire value validator does not know; the kind set is closed at text and series and widens per node as a signed change (BINDS.md §4.4)", kind)
	}
}

// shapeName names a JSON value's shape for a refusal message, so a mis-shaped
// value reports "an array" rather than echoing bytes the user must decode.
func shapeName(value json.RawMessage) string {
	var v interface{}
	if err := json.Unmarshal(value, &v); err != nil {
		return "not valid JSON"
	}
	switch v.(type) {
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a bool"
	case nil:
		return "null"
	case []interface{}:
		return "an array"
	case map[string]interface{}:
		return "an object"
	default:
		return "an unknown shape"
	}
}

// renderValue projects a validated wire value to the string the resolver hands
// the engine. The engine resolves every bind to a string (render.go
// resolveBind), so the store owns the JSON→string projection and the engine never
// decodes a plugin's JSON: a string is unquoted to its contents, and any other
// shape (number, bool, or a series array) is its compact JSON form. A series
// therefore stores as "[1,3,2]" — present and truthy for `when`, awaiting the
// sparkline node that will parse it, rather than absent.
func renderValue(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err == nil {
		return buf.String()
	}
	return string(raw)
}

// sortedKeys returns a map's keys sorted, so a batched frame's validation refuses
// its fields in a deterministic order and a test naming "the first offending
// field" is stable across runs.
func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
