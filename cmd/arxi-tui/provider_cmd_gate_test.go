package main

import (
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/driver"
)

// requireProviderVerbs decides, from the hello the core sent, whether this
// connection can manage providers from the TUI. These tests pin the three
// refused states apart from the one accepting state, the same way the
// requireRunStart tests do, because dispatchProviderCmd commits to a worker
// round-trip only after this gate passes -- a gate that accepted a core without
// the executors would send a command the core answers with not_implemented and
// report that as the user's failure.

// allProviderVerbs is a hello whose declared and implemented lists carry every
// verb the gate needs, the accepting fixture the refusal tests mutate from. It
// is built from providerVerbs so a verb added to the production set is one this
// fixture is missing, failing the accepting test rather than silently passing a
// gate that no longer checks it.
func allProviderVerbs() *driver.Hello {
	verbs := append([]string{"schema"}, providerVerbs...)
	return &driver.Hello{Type: "hello", Types: verbs, Implemented: verbs}
}

// TestRequireProviderVerbsAcceptsAFullyWiredCore is the accepting state: a core
// that declares and implements all four provider verbs passes, so the loop may
// dispatch a /provider or /model command against it.
func TestRequireProviderVerbsAcceptsAFullyWiredCore(t *testing.T) {
	if err := requireProviderVerbs(allProviderVerbs()); err != nil {
		t.Fatalf("requireProviderVerbs refused a core that implements every provider "+
			"verb: %v; a fully wired core is exactly what the TUI needs to manage providers", err)
	}
}

// TestRequireProviderVerbsRefusesAHalfWiredCore is the decision that makes the
// gate all-or-nothing: a core implementing three of the four verbs is refused,
// because the provider surface is one capability from the user's side and a
// /model disable that only discovers its missing executor after being typed is
// the not_implemented trap one verb deep. The counterfactual is model.disable
// specifically, the last verb and the one a provider.add-only build would lack.
func TestRequireProviderVerbsRefusesAHalfWiredCore(t *testing.T) {
	hello := allProviderVerbs()
	implemented := []string{}
	for _, v := range hello.Implemented {
		if v == "model.disable" {
			continue
		}
		implemented = append(implemented, v)
	}
	hello.Implemented = implemented

	err := requireProviderVerbs(hello)
	if err == nil {
		t.Fatal("requireProviderVerbs accepted a core missing model.disable's executor; " +
			"the feature is advertised whole, so /model disable would be typed and then " +
			"answered not_implemented -- the trap the gate exists to catch up front")
	}
	if !strings.Contains(err.Error(), "model.disable") {
		t.Fatalf("the refusal did not name the missing verb, so the user cannot tell which "+
			"capability the connected core lacks: %v", err)
	}
	if !strings.Contains(err.Error(), "not_implemented") {
		t.Fatalf("the half-wired refusal did not name not_implemented, so it reads as a "+
			"transient failure rather than a permanent gap in this binary: %v", err)
	}
}

// TestRequireProviderVerbsRefusesUndeclared is the wrong-surface state: a verb
// absent from `types` entirely means the connected core is not the vocabulary
// this host manages providers over. The refusal must be distinct from the
// unimplemented one -- a different kernel, not a newer build of the same one --
// so it must NOT name not_implemented.
func TestRequireProviderVerbsRefusesUndeclared(t *testing.T) {
	hello := &driver.Hello{
		Type:        "hello",
		Types:       []string{"run.start", "schema"},
		Implemented: []string{"run.start", "schema"},
	}
	err := requireProviderVerbs(hello)
	if err == nil {
		t.Fatal("requireProviderVerbs accepted a core that declares no provider verb at all; " +
			"that surface cannot manage providers and the host must say so, not send into the void")
	}
	if strings.Contains(err.Error(), "not_implemented") {
		t.Fatalf("an undeclared provider verb was reported as not_implemented; the two states "+
			"have different remedies (wrong surface vs. unwired executor) and must not collapse: %v", err)
	}
}

// TestRequireProviderVerbsRefusesNilHello guards the ordering contract: the gate
// is meaningless before the handshake fills the hello, so a nil hello is a caller
// bug (gating before connecting), refused rather than silently treated as
// "providers unavailable".
func TestRequireProviderVerbsRefusesNilHello(t *testing.T) {
	err := requireProviderVerbs(nil)
	if err == nil {
		t.Fatal("requireProviderVerbs accepted a nil hello; with no hello there is no " +
			"implemented list to gate on, so this must refuse rather than pass or panic")
	}
	if !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("the nil-hello refusal did not point at the missing handshake, so it does "+
			"not tell the caller the gate ran too early: %v", err)
	}
}

// TestOldCoreRefusalSaysHowToUpdateIt pins the message a user actually met: an arxi.exe
// built before the provider verbs existed. It used to say "wrong kernel" and print the
// whole declared list, which sent the reader looking for a different program. The
// refusal must say the core is too old, name every missing verb, and give the rebuild
// command -- and must not call it a different kernel.
func TestOldCoreRefusalSaysHowToUpdateIt(t *testing.T) {
	old := &driver.Hello{Type: "hello", Types: []string{"schema", "model.list", "run.start"},
		Implemented: []string{"schema", "model.list", "run.start"}}
	err := requireProviderVerbs(old)
	if err == nil {
		t.Fatal("requireProviderVerbs accepted a core with no provider.add")
	}
	for _, want := range []string{"too old", "provider.add", "model.enable", "model.disable", "git pull", "go build"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("old-core refusal %q is missing %q; the user cannot tell what is wrong or what to run", err, want)
		}
	}
	if strings.Contains(err.Error(), "wrong kernel") {
		t.Errorf("old-core refusal still blames a different kernel: %v", err)
	}
	if strings.Contains(err.Error(), "model.list") {
		t.Errorf("old-core refusal names model.list as missing, but this core declares it: %v", err)
	}
}
