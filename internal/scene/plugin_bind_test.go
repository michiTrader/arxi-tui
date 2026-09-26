package scene

import (
	"strings"
	"testing"
)

// docWithBind builds a one-node document whose single node has the given type
// and bind, so a plugin-namespace test asserts on exactly the bind under test
// and nothing else. It uses the real parser (not a hand-built Node) so the node
// carries an address and a refusal has a file:line to report, the same as a
// mounted fragment does.
func docWithBind(t *testing.T, nodeType, bind string) *Document {
	t.Helper()
	src := []byte(`{"root":{"type":"` + nodeType + `","bind":"` + bind + `"}}`)
	doc, err := ParseDocument(src)
	if err != nil {
		t.Fatalf("ParseDocument(%q,%q): %v", nodeType, bind, err)
	}
	return doc
}

// TestPluginBindDeclaredAndCorrectResolves is H5's positive control and its
// load-bearing counterfactual in one: a bind in the plugin's own namespace,
// declared with a kind the node can draw, must resolve under a plugin scope — and
// the same document with no scope (plain Validate) must be refused as unsigned.
// The second half is the counterfactual DESIGN-BLOCK-H.md H5 names ("disabling
// the branch refuses even the declared-and-correct use"): if this bind resolved
// without the scope, the whole namespace branch would be doing nothing.
func TestPluginBindDeclaredAndCorrectResolves(t *testing.T) {
	doc := docWithBind(t, "text", "tick.price")
	scope := &PluginScope{ID: "tick", Binds: map[string]string{"tick.price": "text"}}

	if err := doc.ValidateWithPlugin(scope); err != nil {
		t.Fatalf("ValidateWithPlugin refused a declared, correctly-typed plugin bind: %v; a plugin's fragment must be able to use the binds it declares, or the namespace is unusable", err)
	}
	if err := doc.Validate(); err == nil {
		t.Fatal("Validate (no plugin scope) accepted tick.price; without a scope a plugin bind is an unsigned foreign name and must be refused — if it passes here the ValidateWithPlugin branch is not what accepts it, so the whole H5 check is inert")
	}
}

// TestPluginSeriesBindOnSparklineResolves covers the second signed kind: a
// `series` bind on a sparkline, the sparkline being the node type that draws the
// numeric axis (BINDS.md §4.4). Without both kinds tested, the kind map could
// carry a single entry and the "closed set" would be one value pretending to be
// a set.
func TestPluginSeriesBindOnSparklineResolves(t *testing.T) {
	doc := docWithBind(t, "sparkline", "tick.history")
	scope := &PluginScope{ID: "tick", Binds: map[string]string{"tick.history": "series"}}

	if err := doc.ValidateWithPlugin(scope); err != nil {
		t.Fatalf("ValidateWithPlugin refused a declared series bind on a sparkline: %v; series is the sparkline's axis and must resolve there", err)
	}
}

// TestPluginBindUnderWrongKindIsRefused is the kind guard: a `text` bind used on
// a sparkline is a scene defect, because the sparkline draws the numeric axis and
// a scalar string is not that axis (BINDS.md §4.4). The refusal must name the
// field, its declared kind and the node, so the author can see which of the two
// to move.
func TestPluginBindUnderWrongKindIsRefused(t *testing.T) {
	doc := docWithBind(t, "sparkline", "tick.price")
	scope := &PluginScope{ID: "tick", Binds: map[string]string{"tick.price": "text"}}

	err := doc.ValidateWithPlugin(scope)
	if err == nil {
		t.Fatal("ValidateWithPlugin accepted a kind:text bind on a sparkline; the kind is the axis the value rides, and a text value on the numeric axis draws nothing — the wrong pairing must be refused, not rendered as a silent mismatch")
	}
	for _, want := range []string{"tick.price", "text", "sparkline"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("wrong-kind refusal = %q, missing %q; the message must name the field, its declared kind and the offending node so the author knows which to move", err.Error(), want)
		}
	}
}

// TestPluginBindFieldNotDeclaredIsRefused covers a bind inside the plugin's
// namespace that the plugin does not declare. It is the declared-vs-used rule's
// negative case for a plugin that does declare *some* binds: the field name is
// wrong or the declaration missing, and the manifest's binds map is the single
// authority (BINDS.md §4.4).
func TestPluginBindFieldNotDeclaredIsRefused(t *testing.T) {
	doc := docWithBind(t, "text", "tick.volume")
	scope := &PluginScope{ID: "tick", Binds: map[string]string{"tick.price": "text"}}

	err := doc.ValidateWithPlugin(scope)
	if err == nil {
		t.Fatal("ValidateWithPlugin accepted tick.volume when the plugin declares only tick.price; an undeclared field in the plugin's own namespace must be refused, or the binds map is not the authority it is documented to be")
	}
	if !strings.Contains(err.Error(), "tick.volume") || !strings.Contains(err.Error(), "no such field") {
		t.Errorf("undeclared-field refusal = %q, want it to name tick.volume and say the plugin declares no such field", err.Error())
	}
}

// TestDeclarativePluginNamespaceIsEmpty is the load-time face of the
// declarative/behavioral split (ADR-0006): a declarative plugin declares no binds,
// so its namespace is empty and any use of it is undeclared. This is a distinct
// message from the field-not-declared case above on purpose — a declarative
// author's fix is "bind a host field / go behavioral", not "spell the field
// right" — so the two must not collapse into one diagnosis.
func TestDeclarativePluginNamespaceIsEmpty(t *testing.T) {
	doc := docWithBind(t, "text", "tick.price")
	scope := &PluginScope{ID: "tick", Binds: map[string]string{}}

	err := doc.ValidateWithPlugin(scope)
	if err == nil {
		t.Fatal("ValidateWithPlugin accepted tick.price for a plugin declaring no binds; a declarative plugin streams nothing, so its namespace is empty and the use must be refused")
	}
	if !strings.Contains(err.Error(), "declares no binds") {
		t.Errorf("declarative-namespace refusal = %q, want it to say the plugin declares no binds (the declarative/behavioral split), not that a field name is wrong", err.Error())
	}
}

// TestPluginBindOutsideNamespaceIsUnsigned confirms the scope does not widen the
// inventory for names outside the plugin's own prefix: a bind under a different
// prefix falls through to the ordinary unsigned-bind refusal even with a scope in
// effect. Without this, a plugin scope would be a hole through which any foreign
// name resolved.
func TestPluginBindOutsideNamespaceIsUnsigned(t *testing.T) {
	doc := docWithBind(t, "text", "other.price")
	scope := &PluginScope{ID: "tick", Binds: map[string]string{"tick.price": "text"}}

	err := doc.ValidateWithPlugin(scope)
	if err == nil {
		t.Fatal("ValidateWithPlugin accepted other.price under a tick scope; a plugin scope legalises only that plugin's own namespace, so a foreign prefix must still be refused as unsigned")
	}
	if !strings.Contains(err.Error(), "unsigned bind") {
		t.Errorf("out-of-namespace refusal = %q, want the ordinary unsigned-bind message (BINDS.md §4.5), not a plugin-namespace one", err.Error())
	}
}

// TestHostBindResolvesEvenWhenPluginIdShadows pins the precedence that keeps a
// plugin from capturing host state: a plugin whose id spells a host namespace
// ("agent") must not shadow a signed host bind ("agent.working"). The host
// inventory is consulted before the plugin scope, so the host field resolves as
// itself regardless of the scope's id.
func TestHostBindResolvesEvenWhenPluginIdShadows(t *testing.T) {
	doc := docWithBind(t, "text", "agent.working")
	scope := &PluginScope{ID: "agent", Binds: map[string]string{}}

	if err := doc.ValidateWithPlugin(scope); err != nil {
		t.Fatalf("ValidateWithPlugin refused the signed host bind agent.working under a plugin scope whose id is \"agent\": %v; a plugin id must never shadow host state, so the host inventory must win", err)
	}
}
