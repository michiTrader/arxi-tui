package ext

import (
	"testing"
)

// TestDecideRemembersByIdentity is the "remember" half of Q15: after a grant is
// recorded with remember, a Decide for the same manifest+digest answers from
// memory (no second prompt) and hands back the exact granted set. This is the
// property that makes a trusted plugin mount silently on the next session.
func TestDecideRemembersByIdentity(t *testing.T) {
	g := NewGate(NewMemoryConsentStore())
	m := behavioralManifest()

	if d := g.Decide(m, "digest-a"); d.Status != DecisionNeedsConsent {
		t.Fatalf("a first, un-remembered mount decided %v; want DecisionNeedsConsent\n"+
			"consequence: the gate would skip the consent prompt for a plugin it has never seen — download silently becoming grant, the exact collapse Q15 forbids.", d.Status)
	}

	granted, err := g.Grant(m, "digest-a", []string{"actions.register"}, true)
	if err != nil {
		t.Fatalf("Grant refused a declared capability: %v", err)
	}

	d := g.Decide(m, "digest-a")
	if d.Status != DecisionRemembered {
		t.Fatalf("after a remembered grant, Decide returned %v; want DecisionRemembered\n"+
			"consequence: a plugin the user chose to remember re-asks on every mount, so \"remember\" bought nothing (Q15).", d.Status)
	}
	if len(d.Granted) != 1 || d.Granted[0] != "actions.register" {
		t.Fatalf("remembered decision carried granted=%v; want [actions.register]\n"+
			"consequence: the remembered subset does not reach supervisor.Config.Granted, so the ack tells the plugin the wrong powers.", d.Granted)
	}
	_ = granted
}

// TestAVersionBumpReAsks is the counterfactual to the remember test on the
// identity axis: the SAME grant is on record, but a bumped version is a
// different identity, so Decide must return to NeedsConsent. A grant that
// survived a version change would be consent laundering — approve v1, ship v2.
func TestAVersionBumpReAsks(t *testing.T) {
	g := NewGate(NewMemoryConsentStore())
	m := behavioralManifest()
	if _, err := g.Grant(m, "digest-a", []string{"actions.register"}, true); err != nil {
		t.Fatal(err)
	}

	bumped := behavioralManifest()
	bumped.Version = "2.0.0"
	if d := g.Decide(bumped, "digest-a"); d.Status != DecisionNeedsConsent {
		t.Fatalf("a bumped version decided %v against a grant remembered for the old version; want DecisionNeedsConsent\n"+
			"consequence: consent approved for v1 silently covers v2, so a plugin ships new behavior under an old grant (§I-H, Q15).", d.Status)
	}

	// The digest axis of the same property: same manifest, changed bytes.
	if d := g.Decide(m, "digest-changed"); d.Status != DecisionNeedsConsent {
		t.Fatalf("changed package bytes decided %v against a grant remembered for the old digest; want DecisionNeedsConsent\n"+
			"consequence: edited code runs under a grant made for different bytes (§I-H).", d.Status)
	}
}

// TestGrantWithoutRememberIsSessionLocal, together with the reject case below,
// pins that remember is the ONLY thing that persists a grant. Grant(remember:
// false) hands back the subset for this mount but records nothing, so the next
// Decide re-asks; a rejection is simply never granting, and it too leaves no
// trace. Both are the "rejection is session-local" contract (§I-H).
func TestGrantWithoutRememberIsSessionLocal(t *testing.T) {
	g := NewGate(NewMemoryConsentStore())
	m := behavioralManifest()

	granted, err := g.Grant(m, "digest-a", []string{"actions.register"}, false)
	if err != nil {
		t.Fatalf("Grant refused a declared capability: %v", err)
	}
	if len(granted) != 1 || granted[0] != "actions.register" {
		t.Fatalf("a one-shot grant returned %v; want [actions.register]\n"+
			"consequence: a plugin granted for this mount does not receive the power in its ack.", granted)
	}
	if d := g.Decide(m, "digest-a"); d.Status != DecisionNeedsConsent {
		t.Fatalf("after Grant(remember=false), Decide returned %v; want DecisionNeedsConsent\n"+
			"consequence: a grant the user did NOT ask to remember was persisted anyway, so a one-time approval becomes permanent (§I-H).", d.Status)
	}

	// A rejection is the absence of a Grant. Nothing was recorded, so the next
	// Decide re-asks — the same session-local outcome, reached by not granting.
	if d := g.Decide(m, "digest-a"); d.Status != DecisionNeedsConsent {
		t.Fatalf("a rejected (never-granted) plugin decided %v; want DecisionNeedsConsent", d.Status)
	}
}

// TestGrantRefusesUndeclaredAndUnknownCapabilities pins the one thing the gate
// exists to prevent: widening authority past what was asked. A capability the
// manifest never declared was never on the consent screen; a capability outside
// the closed set is a door the host has no code for. Granting either is the gate
// minting power, so both are refused.
func TestGrantRefusesUndeclaredAndUnknownCapabilities(t *testing.T) {
	g := NewGate(NewMemoryConsentStore())
	m := behavioralManifest() // declares actions.register, events.emit

	// Undeclared: a known capability the manifest did not list.
	if _, err := g.Grant(m, "digest-a", []string{"inbox.answer"}, true); err == nil {
		t.Error("Grant accepted inbox.answer, which the manifest never declared\n" +
			"consequence: the gate grants a power the consent screen never showed the user (§I-H) — approval of a plugin silently carries more than it asked for.")
	}

	// Unknown: a capability outside the closed set entirely.
	if _, err := g.Grant(m, "digest-a", []string{"filesystem.write"}, true); err == nil {
		t.Error("Grant accepted filesystem.write, which is outside the closed capability set\n" +
			"consequence: a manifest could name any string and have it granted, defeating the closed set that makes each capability a door the host opens in its own code (DESIGN-BLOCK-H.md).")
	}
}

// TestToolsRegisterIsInTheClosedSet proves tools.register (§I-J Decision 3) is a
// known capability the gate can grant when a manifest declares it — the closed-set
// half of adding the tool door's power. KnownCapability must recognise it, and a
// manifest declaring it must be able to grant it. Counterfactual: removing
// tools.register from knownCapabilities makes KnownCapability report false and the
// Grant below fail as an unknown capability, so a plugin the user consented to
// could never carry the power the tool door needs.
func TestToolsRegisterIsInTheClosedSet(t *testing.T) {
	if !KnownCapability("tools.register") {
		t.Fatal("KnownCapability(\"tools.register\") = false; the tool door's capability is not in the closed set, so the gate would refuse to grant it even when a manifest declares it (§I-J Decision 3)")
	}
	g := NewGate(NewMemoryConsentStore())
	m := behavioralManifest()
	m.Capabilities = append(m.Capabilities, "tools.register")
	granted, err := g.Grant(m, "digest-a", []string{"tools.register"}, false)
	if err != nil {
		t.Fatalf("Grant refused a declared tools.register: %v\n"+
			"consequence: a user consents to a plugin's tool power and the gate rejects it, so no tool the agent can call is ever granted.\n"+
			"remedy: tools.register must belong to the closed capability set.", err)
	}
	if len(granted) != 1 || granted[0] != "tools.register" {
		t.Fatalf("granted set = %v; want [tools.register]", granted)
	}
}

// TestClassifyDistinguishesNotDeclaredFromNotGranted is §I-H's "the user can
// tell 'it never asked' from 'you said no'." The three states must be distinct:
// a capability in the granted set is Granted, one the manifest declared but the
// gate did not grant is NotGranted, and one the manifest never listed is
// NotDeclared. Collapsing the two denials loses the diagnosis.
func TestClassifyDistinguishesNotDeclaredFromNotGranted(t *testing.T) {
	m := behavioralManifest() // declares actions.register, events.emit
	granted := []string{"actions.register"}

	cases := []struct {
		capability string
		want       CapabilityStatus
		why        string
	}{
		{"actions.register", CapabilityGranted, "declared and in the granted set"},
		{"events.emit", CapabilityNotGranted, "declared by the manifest but not granted — the user said no"},
		{"inbox.answer", CapabilityNotDeclared, "never listed in the manifest — it never asked"},
	}
	for _, tc := range cases {
		if got := Classify(m, granted, tc.capability); got != tc.want {
			t.Errorf("Classify(%q) = %v, want %v (%s)\n"+
				"consequence: the host cannot phrase the right refusal — \"it never asked\" and \"you said no\" become the same message, and a broken plugin looks like a rejected one (§I-H).",
				tc.capability, got, tc.want, tc.why)
		}
	}
}

// TestNewGatePanicsOnNilStore pins that a store is mandatory: a gate that cannot
// remember would silently re-ask on every mount, defeating the remember half of
// Q15 in a way that looks like a forgetful prompt rather than a bug.
func TestNewGatePanicsOnNilStore(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewGate(nil) did not panic\n" +
				"consequence: a gate with no store silently loses the \"remember\" half of Q15 — every mount re-asks and the user cannot tell why.")
		}
	}()
	NewGate(nil)
}
