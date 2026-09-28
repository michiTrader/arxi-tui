package ext

import (
	"encoding/json"
	"strings"
	"testing"
)

// tickBinds is the declared bind map of a hypothetical "tick" plugin: a scalar
// price (kind text) and a numeric window (kind series). Every ingest test below
// is a frame measured against this declaration, so a refusal names exactly the
// field or shape under test.
func tickBinds() map[string]BindDecl {
	return map[string]BindDecl{
		"tick.price":   {Kind: "text"},
		"tick.history": {Kind: "series"},
	}
}

// TestIngestPublishesADeclaredScalarField pins the happy path: a single-field
// bind frame naming a declared text field lands in the store under its full,
// host-composed path, and the plugin never had to utter that prefix.
func TestIngestPublishesADeclaredScalarField(t *testing.T) {
	s := NewPluginStore()
	if err := s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"price","value":"$1.23"}`)); err != nil {
		t.Fatalf("Ingest refused a well-formed declared scalar frame: %v\n"+
			"consequence: a plugin's published value never reaches the store, so a mounted plugin renders forever as the empty placeholder.\n"+
			"remedy: accept a frame whose composed path is a key of the manifest binds map with a value shape matching its kind.", err)
	}
	snap := s.Snapshot()
	if snap["tick.price"] != "$1.23" {
		t.Errorf("after ingesting price=$1.23 the snapshot has tick.price=%q; want %q\n"+
			"consequence: the host composes the wrong path or fails to unquote the scalar, so the value the plugin sent is not the value the scene draws.", snap["tick.price"], "$1.23")
	}
}

// TestIngestComposesTheNamespacePrefix is the wire-security property in §I-C: the
// plugin publishes a RELATIVE field and the host owns the prefix, so a plugin can
// only ever write inside its own namespace. A frame naming "price" for plugin
// "tick" must land at "tick.price" and nowhere a plugin could choose.
func TestIngestComposesTheNamespacePrefix(t *testing.T) {
	s := NewPluginStore()
	if err := s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"price","value":"$1.23"}`)); err != nil {
		t.Fatalf("Ingest refused a well-formed frame: %v", err)
	}
	snap := s.Snapshot()
	if _, ok := snap["price"]; ok {
		t.Error("the store holds the bare relative field \"price\" as a top-level path\n" +
			"consequence: a plugin naming a field would land outside its namespace, letting it collide with or shadow a host bind — the exact spoof the host-owned prefix prevents (§I-C).")
	}
}

// TestIngestBatchesAtomically covers fork 2 (a batched multi-field frame) AND its
// atomicity guarantee together: a batched frame lands every field for one repaint,
// and a batched frame with ONE undeclared field lands NONE of them. A partial
// write is the torn frame — a new price beside an old history — the atomic
// snapshot exists to prevent.
func TestIngestBatchesAtomically(t *testing.T) {
	s := NewPluginStore()
	batch := json.RawMessage(`{"type":"bind","binds":{"price":"$2.00","history":[1,2,3]}}`)
	if err := s.Ingest("tick", tickBinds(), batch); err != nil {
		t.Fatalf("Ingest refused a well-formed batched frame: %v", err)
	}
	snap := s.Snapshot()
	if snap["tick.price"] != "$2.00" || snap["tick.history"] != "[1,2,3]" {
		t.Fatalf("a batched frame did not land both fields: got price=%q history=%q; want price=%q history=%q\n"+
			"consequence: the batched form fails its whole purpose — a coherent snapshot arriving in one repaint.", snap["tick.price"], snap["tick.history"], "$2.00", "[1,2,3]")
	}

	// One undeclared field poisons the whole frame: none of it may land. The bad
	// field is named to sort AFTER the good one ("price" < "zzz") so a
	// commit-as-you-go implementation would have already written the good field
	// before reaching the bad one — the exact partial write this asserts against.
	torn := json.RawMessage(`{"type":"bind","binds":{"price":"$9.99","zzz":"x"}}`)
	if err := s.Ingest("tick", tickBinds(), torn); err == nil {
		t.Fatal("Ingest accepted a batched frame containing an undeclared field\n" +
			"consequence: a plugin could smuggle an undeclared bind alongside a declared one, defeating the declared-vs-used rule at runtime (§4.4).")
	}
	if got := s.Snapshot()["tick.price"]; got != "$2.00" {
		t.Errorf("a rejected batched frame still overwrote tick.price to %q; want the pre-frame value %q\n"+
			"consequence: a partial write landed — the torn frame the atomic-commit contract (fork 2) exists to prevent.", got, "$2.00")
	}
}

// TestIngestRefusesAnUndeclaredField is the runtime face of the declared-vs-used
// rule (§4.4): a field whose composed path is not a manifest binds key is refused,
// and the refusal names the path so the plugin author can see what to declare.
func TestIngestRefusesAnUndeclaredField(t *testing.T) {
	s := NewPluginStore()
	err := s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"volume","value":10}`))
	if err == nil {
		t.Fatal("Ingest accepted a field the manifest does not declare\n" +
			"consequence: the open namespace would accept any field a plugin invents, so the manifest binds map would stop being the contract the scene validates against.")
	}
	if !strings.Contains(err.Error(), "tick.volume") {
		t.Errorf("the undeclared-field refusal is %q; it must name the composed path tick.volume so the author knows what to declare", err.Error())
	}
	if _, ok := s.Snapshot()["tick.volume"]; ok {
		t.Error("a refused field was written anyway; a refusal must leave the store untouched")
	}
}

// TestIngestRefusesAShapeThatContradictsTheKind is the wire-value axis of the
// closed kind set (§I-C): a declared text field must carry a scalar and a declared
// series field an array of numbers. A series value under a text field, and a
// scalar under a series field, are both refused before they can be stored as a
// value no node could draw.
func TestIngestRefusesAShapeThatContradictsTheKind(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		want  string
	}{
		{"array under a text kind", `{"type":"bind","field":"price","value":[1,2]}`, "an array"},
		{"scalar under a series kind", `{"type":"bind","field":"history","value":"nope"}`, "history"},
		{"object under a text kind", `{"type":"bind","field":"price","value":{"a":1}}`, "an object"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewPluginStore()
			err := s.Ingest("tick", tickBinds(), json.RawMessage(c.frame))
			if err == nil {
				t.Fatalf("Ingest accepted a value whose shape contradicts its declared kind (%s)\n"+
					"consequence: the store would hold a value the declared node type cannot render, so the kind declaration would stop meaning anything at runtime (§4.4).", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the shape refusal is %q; it must mention %q so the author can see what the wire sent", err.Error(), c.want)
			}
		})
	}
}

// TestIngestAcceptsScalarKinds proves the text-kind shape gate is a gate and not a
// wall: a string, a number, and a bool are all legal scalars a text node draws,
// and each renders to its plain string form.
func TestIngestAcceptsScalarKinds(t *testing.T) {
	cases := []struct {
		frame string
		want  string
	}{
		{`{"type":"bind","field":"price","value":"$1"}`, "$1"},
		{`{"type":"bind","field":"price","value":42}`, "42"},
		{`{"type":"bind","field":"price","value":true}`, "true"},
	}
	for _, c := range cases {
		s := NewPluginStore()
		if err := s.Ingest("tick", tickBinds(), json.RawMessage(c.frame)); err != nil {
			t.Fatalf("Ingest refused a legal scalar %q: %v", c.frame, err)
		}
		if got := s.Snapshot()["tick.price"]; got != c.want {
			t.Errorf("scalar %q rendered as %q; want %q\n"+
				"consequence: the store's JSON→string projection is wrong, so the engine (which resolves every bind to a string) would draw the JSON literal instead of the value.", c.frame, got, c.want)
		}
	}
}

// TestLivenessAppearsUnderUIPlugin pins the §4.3 liveness bind: a status set by
// the supervisor lifecycle is snapshotted under ui.plugin.<id>, which is where a
// footer or overlay reads `when: ui.plugin.tick` to report a degraded plugin.
func TestLivenessAppearsUnderUIPlugin(t *testing.T) {
	s := NewPluginStore()
	s.SetLiveness("tick", PluginLive)
	if got := s.Snapshot()["ui.plugin.tick"]; got != PluginLive {
		t.Errorf("liveness snapshot has ui.plugin.tick=%q; want %q\n"+
			"consequence: the diagnosis the placeholder cannot carry never reaches the scene, so a dead plugin looks identical to one that has simply not spoken yet (§I-G).", got, PluginLive)
	}
}

// TestDropPluginErasesTheNamespace is the store half of /ui plugin remove <id>
// (§I-F): after a drop, every <plugin-id>.* value and the liveness bind resolve to
// absent again, so the composed scene draws as it did before the mount. It must
// erase ONLY the named plugin's prefix, never a host field or a sibling plugin.
func TestDropPluginErasesTheNamespace(t *testing.T) {
	s := NewPluginStore()
	_ = s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"price","value":"$1"}`))
	s.SetLiveness("tick", PluginLive)
	// A sibling plugin whose prefix merely starts the same must survive: "tick"
	// is a prefix of "ticker" as a string, so a naive HasPrefix drop would take
	// the sibling too.
	sibling := map[string]BindDecl{"ticker.rate": {Kind: "text"}}
	_ = s.Ingest("ticker", sibling, json.RawMessage(`{"type":"bind","field":"rate","value":"5"}`))

	s.DropPlugin("tick")
	snap := s.Snapshot()
	if _, ok := snap["tick.price"]; ok {
		t.Error("DropPlugin left a value behind; an unmount must clear the plugin's whole namespace")
	}
	if _, ok := snap["ui.plugin.tick"]; ok {
		t.Error("DropPlugin left the liveness bind behind; an unmount erases it so the bind is falsy again")
	}
	if snap["ticker.rate"] != "5" {
		t.Errorf("DropPlugin(\"tick\") also erased sibling ticker.rate=%q; want it untouched\n"+
			"consequence: a dotted-prefix membership test that matched \"ticker\" would unmount one plugin and blank another sharing a name prefix.", snap["ticker.rate"])
	}
}

// drainChanged reports whether a repaint wake-up is currently buffered on the
// store's Changed() channel, consuming it if so. A non-blocking receive reads the
// store's coalescing seam from the test side: the mutators post at most one
// buffered signal, so this returns true exactly once per burst and false when the
// buffer is empty.
func drainChanged(s *PluginStore) bool {
	select {
	case <-s.Changed():
		return true
	default:
		return false
	}
}

// TestAFreshStoreSignalsNoRepaint pins the quiet start: a store nobody has written
// to has no wake-up buffered, so a session that mounts no plugin never fires the
// loop's Changed() case. Without this the loop could spin-repaint on a phantom
// signal the instant it starts.
func TestAFreshStoreSignalsNoRepaint(t *testing.T) {
	s := NewPluginStore()
	if drainChanged(s) {
		t.Error("a freshly constructed store already has a repaint wake-up buffered\n" +
			"consequence: the loop's Changed() case fires once with no plugin mounted and nothing changed, a repaint asked for by nobody.")
	}
}

// TestIngestSignalsARepaint is the core of the store→render wiring: a published
// value that lands in the store wakes the loop so the next repaint reads it.
// Without the wake-up a mounted plugin's frame sits in the store unseen until an
// unrelated event happens to repaint, which is indistinguishable from the plugin
// never having spoken.
func TestIngestSignalsARepaint(t *testing.T) {
	s := NewPluginStore()
	if err := s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"price","value":"$1.23"}`)); err != nil {
		t.Fatalf("Ingest refused a well-formed frame: %v", err)
	}
	if !drainChanged(s) {
		t.Error("an accepted Ingest posted no repaint wake-up on Changed()\n" +
			"consequence: a plugin's pushed value never triggers a redraw, so a mounted plugin renders as the empty placeholder until a keystroke or animation tick happens to repaint.\n" +
			"remedy: signalChanged after committing the write in Ingest.")
	}
}

// TestARefusedIngestSignalsNoRepaint is the direction that proves the signal names
// a real write, not merely an attempt: a frame refused for an undeclared field
// changed nothing, so it must not wake the loop. A signal posted before the
// validation gate would repaint on every malformed frame a broken plugin sends.
func TestARefusedIngestSignalsNoRepaint(t *testing.T) {
	s := NewPluginStore()
	err := s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"undeclared","value":"x"}`))
	if err == nil {
		t.Fatal("Ingest accepted an undeclared field; this test needs a refusal to measure")
	}
	if drainChanged(s) {
		t.Error("a refused Ingest still posted a repaint wake-up\n" +
			"consequence: a plugin spraying malformed frames would repaint the host on every one though the store never changed — the signal must sit behind the write, not before the validation gate.")
	}
}

// TestSetLivenessSignalsARepaint covers the liveness axis: a plugin going live or
// dead is a visible transition a scene may gate on with `when: ui.plugin.<id>`,
// so it wakes the loop like a value write. A dead plugin especially must repaint
// the report of its own death rather than freeze on its last live frame.
func TestSetLivenessSignalsARepaint(t *testing.T) {
	s := NewPluginStore()
	s.SetLiveness("tick", PluginDead)
	if !drainChanged(s) {
		t.Error("SetLiveness posted no repaint wake-up on Changed()\n" +
			"consequence: a plugin's death (or its promotion to live) never triggers a redraw, so ui.plugin.<id> gates and the death report update only on the next unrelated repaint.")
	}
}

// TestDropPluginSignalsARepaint is the /ui plugin remove half: an unmount removes
// the plugin's nodes from the composed scene, so it must wake the loop to redraw
// without them rather than leave them on screen until the next keystroke.
func TestDropPluginSignalsARepaint(t *testing.T) {
	s := NewPluginStore()
	_ = s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"price","value":"$1"}`))
	// Drain the Ingest's own signal so the receive below can only be the drop's.
	drainChanged(s)
	s.DropPlugin("tick")
	if !drainChanged(s) {
		t.Error("DropPlugin posted no repaint wake-up on Changed()\n" +
			"consequence: an unmounted plugin's nodes stay drawn until an unrelated event repaints, so `/ui plugin remove` appears to do nothing.")
	}
}

// TestChangeSignalsCoalesce proves the buffered-at-1, non-blocking contract: a
// burst of writes between two repaints collapses to a SINGLE wake-up. The writer
// never blocks on the loop draining (a hung loop cannot back-pressure the pump),
// and the loop wakes once and reads the latest of each value under last-value-wins
// — a per-write-guaranteed channel would instead hand the loop a backlog of N
// wake-ups to drain for one meaningful repaint.
func TestChangeSignalsCoalesce(t *testing.T) {
	s := NewPluginStore()
	for i := 0; i < 3; i++ {
		if err := s.Ingest("tick", tickBinds(), json.RawMessage(`{"type":"bind","field":"price","value":"$1"}`)); err != nil {
			t.Fatalf("Ingest refused a well-formed frame on write %d: %v", i, err)
		}
	}
	if !drainChanged(s) {
		t.Fatal("three writes posted no wake-up at all; the burst must leave exactly one")
	}
	if drainChanged(s) {
		t.Error("three writes left more than one wake-up buffered\n" +
			"consequence: the loop drains a backlog of signals for one meaningful repaint, and under a fast plugin the buffer would grow without bound — the send must coalesce, not queue.")
	}
}
