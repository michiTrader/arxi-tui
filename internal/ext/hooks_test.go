package ext

import (
	"strings"
	"testing"
)

// A well-formed behavioral manifest declaring both hook kinds and the two
// capabilities they require. Every refusal test below is a single deviation from
// this, so a failure names exactly the field under test. It reuses the tick
// identity and in-package executable of validBehavioral so only the hooks block is
// under examination.
const validBehavioralWithHooks = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "capabilities": ["hooks.tool_gate", "hooks.compaction"],
  "hooks": [
    { "kind": "tool_gate", "tools": ["bash", "write"] },
    { "kind": "compaction" }
  ]
}`

// TestValidateBehavioralAcceptsAWellFormedHooksBlock is the positive anchor for the
// hooks sweep: the fixture every refusal below deviates from must itself be
// accepted, or a later "it refused" result proves nothing. It also proves a plugin
// declaring both hook kinds with their matching capabilities is a legal behavioral
// package (Decision 1 + Decision 2).
func TestValidateBehavioralAcceptsAWellFormedHooksBlock(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validBehavioralWithHooks))
	if err != nil {
		t.Fatalf("ParseNamed refused a well-formed hooks manifest: %v", err)
	}
	if err := m.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused a well-formed hooks manifest: %v\n"+
			"consequence: a plugin declaring legal hooks with their capabilities cannot be installed, so Gate C has no packages to open.\n"+
			"remedy: validateHooks must accept a known kind when its HookKindCapability is declared.", err)
	}
}

// TestValidateBehavioralRefusesAnUnknownHookKind proves the kind set is closed: a
// hook naming a kind outside {tool_gate, compaction} is refused, and the message
// names the deliberately-absent `prompt` so the author learns it is not a weaker
// hook but a rejected one (F1). Counterfactual: the well-formed fixture is
// accepted, so the refusal is about the kind alone.
func TestValidateBehavioralRefusesAnUnknownHookKind(t *testing.T) {
	src := strings.Replace(validBehavioralWithHooks,
		`{ "kind": "tool_gate", "tools": ["bash", "write"] }`,
		`{ "kind": "prompt" }`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted an unknown hook kind; the kind set is closed because a hook is a seam the host opens in its own code, so a kind the host has no seam for must be refused (ADR-0009 Decision 1)")
	}
	if !strings.Contains(err.Error(), "kind") || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("unknown-kind refusal = %q; it must name the kind and that prompt is deliberately absent so the author is not left guessing", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateBehavioralRefusesAnEmptyToolElement proves a tool_gate hook naming an
// empty string in its tools list is refused: an empty name matches no tool and
// would silently widen the hook to all tools or none — a declaration the author did
// not mean either way. Counterfactual: dropping the tools filter entirely (consult
// for every tool) is legal, so the refusal is about the empty element, not about
// filtering.
func TestValidateBehavioralRefusesAnEmptyToolElement(t *testing.T) {
	src := strings.Replace(validBehavioralWithHooks,
		`{ "kind": "tool_gate", "tools": ["bash", "write"] }`,
		`{ "kind": "tool_gate", "tools": ["bash", ""] }`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a tool_gate hook with an empty tool element; an empty name matches no tool and silently widens or voids the hook (ADR-0009 Decision 1)")
	}
	if !strings.Contains(err.Error(), "empty tool") {
		t.Fatalf("empty-tool refusal = %q; it must name the empty tool element so the author knows which entry is blank", err.Error())
	}
	assertAddressed(t, err)

	// Counterfactual: a tool_gate hook with no tools filter at all is accepted —
	// absent means "consult for every tool," which is legal.
	noFilter := strings.Replace(validBehavioralWithHooks,
		`{ "kind": "tool_gate", "tools": ["bash", "write"] }`,
		`{ "kind": "tool_gate" }`, 1)
	m2, err := ParseNamed("plugin.json", []byte(noFilter))
	if err != nil {
		t.Fatalf("ParseNamed (no filter): %v", err)
	}
	if err := m2.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused a tool_gate hook with no tools filter: %v; absent tools means consult-for-all, which is legal — the refusal must be about an empty element, not about omitting the filter", err)
	}
}

// TestValidateBehavioralRefusesToolsOnCompaction proves a compaction hook declaring
// a tools filter is refused: compaction rewrites the whole history and has no
// per-tool axis, so a tool filter there is a field with no meaning, refused rather
// than ignored (the §I-J nameless-tool precedent). Counterfactual: the well-formed
// compaction hook (no tools) is accepted.
func TestValidateBehavioralRefusesToolsOnCompaction(t *testing.T) {
	src := strings.Replace(validBehavioralWithHooks,
		`{ "kind": "compaction" }`,
		`{ "kind": "compaction", "tools": ["bash"] }`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a compaction hook with a tools filter; compaction is whole-history and has no per-tool axis, so a tool filter is meaningless and must be refused, not ignored (ADR-0009)")
	}
	if !strings.Contains(err.Error(), "compaction") || !strings.Contains(err.Error(), "tools") {
		t.Fatalf("tools-on-compaction refusal = %q; it must name compaction and tools so the author knows the field has no meaning there", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateBehavioralRefusesDuplicateCompaction proves a second compaction hook
// is refused: the core runs exactly one compaction.Generator (F2), and a stack of
// them has no deterministic composition rule the way tool-gate's
// most-restrictive-wins does. Counterfactual: two tool_gate hooks are NOT refused
// (they compose), so the refusal is about compaction's single-generator ceiling,
// not about declaring two hooks.
func TestValidateBehavioralRefusesDuplicateCompaction(t *testing.T) {
	src := strings.Replace(validBehavioralWithHooks,
		`{ "kind": "compaction" }`,
		`{ "kind": "compaction" },
    { "kind": "compaction" }`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted two compaction hooks; the core runs one generator and a stack has no deterministic composition rule, so a second compaction hook must be refused (ADR-0009 F2)")
	}
	if !strings.Contains(err.Error(), "compaction") {
		t.Fatalf("duplicate-compaction refusal = %q; it must name compaction so the author knows which hook is doubled", err.Error())
	}
	assertAddressed(t, err)

	// Counterfactual: two tool_gate hooks compose (most-restrictive-wins) and are
	// accepted — the single-hook ceiling is compaction's alone.
	twoGates := strings.Replace(validBehavioralWithHooks,
		`{ "kind": "tool_gate", "tools": ["bash", "write"] }`,
		`{ "kind": "tool_gate", "tools": ["bash"] },
    { "kind": "tool_gate", "tools": ["write"] }`, 1)
	m2, err := ParseNamed("plugin.json", []byte(twoGates))
	if err != nil {
		t.Fatalf("ParseNamed (two gates): %v", err)
	}
	if err := m2.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused two tool_gate hooks: %v; tool-gate hooks compose by most-restrictive-wins, so the single-hook ceiling is compaction's alone (F2)", err)
	}
}

// TestValidateBehavioralRefusesHooksWithoutCapability proves the contradiction
// refusal: declaring a hook of a given kind without the capability that kind
// requires is a manifest asking to subject the agent's turn to a power the consent
// screen never showed. It mirrors validateTools' tools-without-capability.
// Counterfactual: the well-formed fixture (both capabilities present) is accepted,
// so the refusal is about the missing capability alone.
func TestValidateBehavioralRefusesHooksWithoutCapability(t *testing.T) {
	// Drop hooks.tool_gate but keep the tool_gate hook: the kind now has no grant.
	noCap := strings.Replace(validBehavioralWithHooks,
		`"capabilities": ["hooks.tool_gate", "hooks.compaction"],`,
		`"capabilities": ["hooks.compaction"],`, 1)
	m, err := ParseNamed("plugin.json", []byte(noCap))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a tool_gate hook with no hooks.tool_gate capability; a hook subjects the agent's turn to the plugin, so declaring one without the capability that shows the user that power is a contradiction (ADR-0009 Decision 2)")
	}
	if !strings.Contains(err.Error(), "hooks.tool_gate") {
		t.Fatalf("missing-capability refusal = %q; it must name hooks.tool_gate so the author knows which capability to add", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateRefusesHooksOnADeclarativeManifest proves `hooks` is a behavioral
// field like tools/binds: a declarative manifest (no executable) that carries it is
// the declarative/behavioral contradiction checkBehavioral names, refused before it
// ever reaches the declarative loader's mount validation.
func TestValidateRefusesHooksOnADeclarativeManifest(t *testing.T) {
	src := strings.Replace(validDeclarative,
		`"protocol": "ext/v1",`,
		`"protocol": "ext/v1",
  "hooks": [ { "kind": "tool_gate" } ],`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a declarative manifest that declares hooks; hooks is a behavioral field (a process proposes a verdict), so a manifest with no executable declaring one is a contradiction (H-A discriminator)")
	}
	if !strings.Contains(err.Error(), "hooks") || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("contradiction refusal = %q; it must name both hooks and the missing executable so the author knows which half to fix", err.Error())
	}
	assertAddressed(t, err)
}

// TestHookCapabilitiesAreInTheClosedSet proves hooks.tool_gate and
// hooks.compaction (Decision 2) are known capabilities the gate can grant when a
// manifest declares them — the closed-set half of adding Gate C's powers.
// Counterfactual: removing either from knownCapabilities makes KnownCapability
// report false and the Grant below fail as an unknown capability, so a plugin the
// user consented to could never carry the power a behavior hook needs.
func TestHookCapabilitiesAreInTheClosedSet(t *testing.T) {
	for _, c := range []string{"hooks.tool_gate", "hooks.compaction"} {
		if !KnownCapability(c) {
			t.Fatalf("KnownCapability(%q) = false; a hook capability is not in the closed set, so the gate would refuse to grant it even when a manifest declares it (ADR-0009 Decision 2)", c)
		}
	}
	g := NewGate(NewMemoryConsentStore())
	m := behavioralManifest()
	m.Capabilities = append(m.Capabilities, "hooks.tool_gate", "hooks.compaction")
	granted, err := g.Grant(m, "digest-a", []string{"hooks.tool_gate", "hooks.compaction"}, false)
	if err != nil {
		t.Fatalf("Grant refused declared hook capabilities: %v\n"+
			"consequence: a user consents to a plugin's hook power and the gate rejects it, so no behavior hook is ever granted.\n"+
			"remedy: both hook capabilities must belong to the closed set.", err)
	}
	if len(granted) != 2 {
		t.Fatalf("granted set = %v; want both hook capabilities", granted)
	}
}

// TestGainingAHookCapabilityReAsks is piece 2's identity counterfactual: a plugin
// whose grant was remembered WITHOUT a hook capability, then re-declares the same
// identity WITH one, must return DecisionNeedsConsent — the hook capability joins
// the §I-H identity tuple's capability-set component, so gaining it is a new
// identity that re-asks. A grant that survived gaining hooks.tool_gate would be the
// grant-transfer the whole gate exists to prevent (approve a quiet plugin, ship one
// that vetoes the agent's calls).
func TestGainingAHookCapabilityReAsks(t *testing.T) {
	g := NewGate(NewMemoryConsentStore())
	before := behavioralManifest() // actions.register, events.emit
	if _, err := g.Grant(before, "digest-a", []string{"actions.register"}, true); err != nil {
		t.Fatal(err)
	}
	if d := g.Decide(before, "digest-a"); d.Status != DecisionRemembered {
		t.Fatalf("the original plugin decided %v before gaining a hook; want DecisionRemembered (the grant is on record)", d.Status)
	}

	after := behavioralManifest()
	after.Capabilities = append(after.Capabilities, "hooks.tool_gate")
	if d := g.Decide(after, "digest-a"); d.Status != DecisionNeedsConsent {
		t.Fatalf("a plugin that gained hooks.tool_gate decided %v against a grant remembered without it; want DecisionNeedsConsent\n"+
			"consequence: a plugin that gains the power to see and veto the agent's tool calls runs under a grant made when it had no such power — the grant-transfer the identity tuple exists to prevent (ADR-0009 Decision 2, §I-H).\n"+
			"remedy: the hook capabilities join the identity's capability-set component, so gaining one re-asks.", d.Status)
	}
}

// TestHookKindCapabilityMapsEachKindAndRefusesUnknown pins the single source the
// manifest validator and the supervisor gate both read: each known kind maps to its
// capability, and an unknown kind returns ("", false) rather than an empty-but-ok
// grant. If the mapping drifted, validateHooks and CallHook would gate a hook on
// the wrong (or no) capability.
func TestHookKindCapabilityMapsEachKindAndRefusesUnknown(t *testing.T) {
	for kind, want := range map[string]string{"tool_gate": "hooks.tool_gate", "compaction": "hooks.compaction"} {
		got, ok := HookKindCapability(kind)
		if !ok || got != want {
			t.Fatalf("HookKindCapability(%q) = (%q,%v); want (%q,true)", kind, got, ok, want)
		}
	}
	if got, ok := HookKindCapability("prompt"); ok || got != "" {
		t.Fatalf("HookKindCapability(\"prompt\") = (%q,%v); want (\"\",false)\n"+
			"consequence: an unknown kind maps to a capability (or an empty one treated as ok), so a hook the closed set does not know could be gated and spawned.\n"+
			"remedy: an unknown kind returns (\"\",false).", got, ok)
	}
}
