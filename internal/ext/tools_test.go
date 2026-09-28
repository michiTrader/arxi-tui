package ext

import (
	"strings"
	"testing"
)

// A well-formed behavioral manifest that declares one tool and the capability
// that tool requires. Every refusal test below is a single deviation from this,
// so a failure names exactly the field under test. It reuses the tick identity
// and in-package executable of validBehavioral so only the tools block is under
// examination.
const validBehavioralWithTool = `{
  "id": "tick",
  "name": "Community Ticker",
  "version": "1.0.0",
  "protocol": "ext/v1",
  "executable": "./tick",
  "capabilities": ["tools.register"],
  "tools": [
    { "name": "quote",
      "description": "Fetch the latest quote for a ticker symbol.",
      "parameters": { "type": "object",
        "properties": { "symbol": { "type": "string" } },
        "required": ["symbol"] } }
  ]
}`

// TestValidateBehavioralAcceptsAWellFormedToolsBlock is the positive anchor for
// the tools sweep: the fixture every refusal below deviates from must itself be
// accepted, or a later "it refused" result proves nothing — the fixture could be
// broken for an unrelated reason. It also proves a plugin declaring tools with
// the tools.register capability is a legal behavioral package.
func TestValidateBehavioralAcceptsAWellFormedToolsBlock(t *testing.T) {
	m, err := ParseNamed("plugin.json", []byte(validBehavioralWithTool))
	if err != nil {
		t.Fatalf("ParseNamed refused a well-formed tools manifest: %v", err)
	}
	if err := m.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused a well-formed tools manifest: %v\n"+
			"consequence: a plugin declaring a legal tool with its tools.register capability cannot be installed, so the tool door has no packages to open.\n"+
			"remedy: validateTools must accept a named tool with a parameters object when tools.register is declared.", err)
	}
}

// TestValidateBehavioralRefusesAToolWithNoName proves the name is required: it is
// the relative half of the agent-visible <plugin-id>.<name> (§I-J Decision 2), so
// a nameless tool is one the agent could never address. Counterfactual: the
// well-formed fixture above is accepted, so this refusal is provably about the
// missing name and not some unrelated field.
func TestValidateBehavioralRefusesAToolWithNoName(t *testing.T) {
	src := strings.Replace(validBehavioralWithTool, `{ "name": "quote",
      "description": "Fetch the latest quote for a ticker symbol.",`,
		`{ "description": "Fetch the latest quote for a ticker symbol.",`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a tool with no name; a nameless tool has no relative segment for the composed <plugin-id>.<name>, so the agent could never call it (§I-J Decision 2)")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("no-name refusal = %q; it must name the missing name so the author knows which field is absent", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateBehavioralRefusesAToolWithNoParameters proves the schema is
// required: the parameters are the JSON-Schema the host forwards to the agent
// verbatim (§I-J Decision 1), and a tool with no schema tells the agent nothing
// about how to call it. Counterfactual: the well-formed fixture is accepted.
func TestValidateBehavioralRefusesAToolWithNoParameters(t *testing.T) {
	src := strings.Replace(validBehavioralWithTool,
		`,
      "parameters": { "type": "object",
        "properties": { "symbol": { "type": "string" } },
        "required": ["symbol"] } }`,
		` }`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted a tool with no parameters; the schema is the tool's contract with the agent, and the host forwards it verbatim, so a missing schema is a tool the agent cannot call (§I-J Decision 1)")
	}
	if !strings.Contains(err.Error(), "parameters") {
		t.Fatalf("no-parameters refusal = %q; it must name the missing parameters so the author knows what to add", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateBehavioralRefusesDuplicateToolNames proves two tools sharing a
// relative name are refused: the composed <plugin-id>.<name> would collide and
// the second would shadow the first, so one could never be called. Counterfactual:
// renaming the second tool to a distinct name is accepted, so the refusal is about
// the collision and not about having two tools at all.
func TestValidateBehavioralRefusesDuplicateToolNames(t *testing.T) {
	twoSame := strings.Replace(validBehavioralWithTool,
		`        "required": ["symbol"] } }
  ]`,
		`        "required": ["symbol"] } },
    { "name": "quote",
      "parameters": { "type": "object" } }
  ]`, 1)
	m, err := ParseNamed("plugin.json", []byte(twoSame))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted two tools named \"quote\"; the composed <plugin-id>.<name> would collide and one would shadow the other, so a duplicate must be refused (§I-J)")
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Fatalf("duplicate refusal = %q; it must say the name is declared twice so the author knows the two collide", err.Error())
	}
	assertAddressed(t, err)

	// Counterfactual: a distinct second name is accepted, so the refusal is about
	// the shared name, not about declaring more than one tool.
	twoDistinct := strings.Replace(twoSame, `    { "name": "quote",
      "parameters": { "type": "object" } }`,
		`    { "name": "history",
      "parameters": { "type": "object" } }`, 1)
	m2, err := ParseNamed("plugin.json", []byte(twoDistinct))
	if err != nil {
		t.Fatalf("ParseNamed (distinct): %v", err)
	}
	if err := m2.ValidateBehavioral(); err != nil {
		t.Fatalf("ValidateBehavioral refused two distinctly-named tools: %v; the duplicate refusal must be about the shared name, not about a second tool existing", err)
	}
}

// TestValidateBehavioralRefusesToolsWithoutCapability proves the contradiction
// refusal: declaring tools without the tools.register capability is a manifest
// asking the agent to call powers the consent screen would never have shown,
// because that capability is exactly what puts "your agent may call this on its
// own" in front of the user. It mirrors checkBehavioral's own field
// contradictions. Counterfactual: adding the capability back (the well-formed
// fixture) is accepted, so the refusal is about the missing capability alone.
func TestValidateBehavioralRefusesToolsWithoutCapability(t *testing.T) {
	noCap := strings.Replace(validBehavioralWithTool,
		`"capabilities": ["tools.register"],`, "", 1)
	m, err := ParseNamed("plugin.json", []byte(noCap))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.ValidateBehavioral()
	if err == nil {
		t.Fatal("ValidateBehavioral accepted tools with no tools.register capability; a tool is power the agent invokes on its own, so declaring one without the capability that shows the user that power is a contradiction (§I-J Decision 3)")
	}
	if !strings.Contains(err.Error(), "tools.register") {
		t.Fatalf("missing-capability refusal = %q; it must name tools.register so the author knows which capability to add", err.Error())
	}
	assertAddressed(t, err)
}

// TestValidateRefusesToolsOnADeclarativeManifest proves `tools` is a behavioral
// field like binds/args/capabilities: a declarative manifest (no executable) that
// carries it is the declarative/behavioral contradiction checkBehavioral names,
// refused before it ever reaches the declarative loader's mount validation.
func TestValidateRefusesToolsOnADeclarativeManifest(t *testing.T) {
	src := strings.Replace(validDeclarative,
		`"protocol": "ext/v1",`,
		`"protocol": "ext/v1",
  "tools": [ { "name": "quote", "parameters": { "type": "object" } } ],`, 1)
	m, err := ParseNamed("plugin.json", []byte(src))
	if err != nil {
		t.Fatalf("ParseNamed: %v", err)
	}
	err = m.Validate()
	if err == nil {
		t.Fatal("Validate accepted a declarative manifest that declares tools; tools is a behavioral field (the agent calls a process), so a manifest with no executable declaring one is a contradiction (H-A discriminator)")
	}
	if !strings.Contains(err.Error(), "tools") || !strings.Contains(err.Error(), "executable") {
		t.Fatalf("contradiction refusal = %q; it must name both tools and the missing executable so the author knows which half to fix", err.Error())
	}
	assertAddressed(t, err)
}
