package ext

import (
	"fmt"
	"path"
	"path/filepath"
)

// ValidateBehavioral is the installer's gated door (DESIGN-BLOCK-I §I-I step 3):
// the one validation path that accepts a behavioral manifest — the exact
// manifest Validate refuses on sight.
//
// # Why a second entry point rather than a flag on Validate
//
// The H2 executable refusal (checkBehavioral) is not a lint; it is the whole
// declarative/behavioral guarantee — "load a declarative plugin, zero code" is
// enforceable precisely because a manifest with an executable never reaches a
// mount through the declarative loader. Lifting that refusal is a capability,
// and a capability belongs to a named caller, not a boolean anyone can pass. The
// installer is that caller: it is the one door that also owns the consent gate
// and the process supervisor, so it is the one place where "there is an
// executable" is a precondition rather than a rejection. Every other loader
// (LoadFile, the H6 /ui path) keeps calling Validate and keeps refusing code.
//
// # What it still refuses
//
// A behavioral manifest is a superset of a declarative one, not a different
// schema, so every non-behavioral refusal still runs: the identity block is
// required and checked the same way, the contributed token block and every
// mounted fragment go through the same validators a theme file and a
// hand-written scene get. The two differences are deliberate and each is a
// decision §I-I signed:
//
//   - the executable is *required* here (a package with nothing to run belongs
//     on the manifest-only path, not the installer), and
//   - checkEmpty is not run: a behavioral plugin whose whole contribution is the
//     process it runs mounts no fragment and defines no token, and refusing that
//     as a no-op would reject the common case the installer exists for.
func (m *Manifest) ValidateBehavioral() error {
	if m == nil {
		return nil
	}
	if err := m.validateIdentity(); err != nil {
		return err
	}
	if err := m.requireExecutable(); err != nil {
		return err
	}
	if err := m.validateExecutablePath(); err != nil {
		return err
	}
	if err := m.validateTools(); err != nil {
		return err
	}
	if err := m.validateTokensBlock(); err != nil {
		return err
	}
	if err := m.validateMounts(); err != nil {
		return err
	}
	return nil
}

// requireExecutable refuses a declarative manifest (no executable) reaching the
// installer. It is the mirror of checkBehavioral's refusal, one door over: H2
// refuses a manifest that *has* an executable, and the installer refuses one
// that *lacks* it. A package with nothing to run has no behavioral install to
// perform — extracting it, digesting it and gating consent on a process that
// never spawns is work with no product — and it already has a home on the H6
// manifest-only path (`/ui plugin add`), so the author is sent there rather than
// left with a plugin the installer accepted but the supervisor has nothing to
// mount.
func (m *Manifest) requireExecutable() error {
	if m.Executable == "" {
		return &Error{Loc: m.rootLoc(), Msg: "manifest has no executable, so it is declarative; the behavioral installer mounts a process behind the consent gate, and a package with nothing to run has no behavioral install to do — add an \"executable\", or install it as a declarative plugin with /ui plugin add (DESIGN-BLOCK-I §I-I)"}
	}
	return nil
}

// validateExecutablePath enforces the in-package rule identity.go states as a
// promise and no code enforced until here: "an executable outside the package is
// code the digest never covered" (DESIGN-BLOCK-H.md, §I-H). The consent identity
// binds a grant to PackageDigest over the laid-out tree; an executable at `../x`
// or `/usr/bin/x` names bytes that walk never read, so a grant made against the
// tree's digest would authorise spawning a program the user never saw and the
// digest cannot vouch for.
//
// It reuses the exact predicate the extraction boundary uses on every tar entry
// (Extract.resolveEntry): path.Clean in slash form — manifest paths are
// slash-separated for portability, like tar names — then filepath.IsLocal on the
// OS form, which rejects a climbing `..`, an absolute path, and on Windows a
// drive-relative path or a reserved device name. Sharing the predicate is the
// point: the file that is refused as an entry and the executable that is refused
// as a target are refused by the same rule, so the two cannot drift into
// disagreeing on what "inside the package" means.
func (m *Manifest) validateExecutablePath() error {
	cleaned := path.Clean(m.Executable)
	local := filepath.FromSlash(cleaned)
	if !filepath.IsLocal(local) {
		return &Error{Loc: m.locAt("executable"), Msg: fmt.Sprintf("executable %q resolves outside the package; a `..`, absolute, or Windows drive-relative or reserved path names code the package digest never covered, so a consent grant bound to the tree's digest would authorise a program the user never saw (DESIGN-BLOCK-I §I-H) — the executable must be a path inside the package tree", m.Executable)}
	}
	return nil
}

// validateTools refuses a malformed `tools` block (Gate B, DESIGN-BLOCK-I §I-J
// "What is buildable now", Decisions 1 and 3). A tool is a declared, digested
// field like `binds`, so the well-formedness net runs here in the behavioral
// validator, beside the executable checks, rather than at the gate — a package
// whose tool declarations are broken should be refused at install, not spawned
// and then discovered wrong when the agent first calls one.
//
// Three refusals, each with its counterfactual (reverting it accepts a malformed
// block):
//
//   - a tool with no name, because the name is the relative half of the
//     host-composed `<plugin-id>.<name>` the agent sees (§I-J Decision 2); a
//     nameless tool is one the agent could never address.
//   - a tool with no parameters, because the schema is the tool's contract with
//     the agent and the host forwards it verbatim (§I-J Decision 1); a tool with
//     no schema tells the agent nothing about how to call it.
//   - two tools sharing a relative name, because the composed name would collide
//     and the second would shadow the first — the same reason a bind namespace
//     is one-value-per-field.
//
// And one contradiction, mirroring checkBehavioral's own field contradictions
// (the `consent_required:false`-with-`executable` precedent): declaring `tools`
// without declaring the `tools.register` capability is a manifest asking the
// agent to call powers the consent screen would never have shown, because the
// capability is what puts "your agent may call this plugin on its own" in front
// of the user. The two must agree, so the author is told to add the capability
// or drop the tools rather than shipping tools no grant can ever cover.
func (m *Manifest) validateTools() error {
	if len(m.Tools) == 0 {
		return nil
	}
	declared := false
	for _, c := range m.Capabilities {
		if c == capToolsRegister {
			declared = true
			break
		}
	}
	if !declared {
		return &Error{Loc: m.locAt("tools"), Msg: fmt.Sprintf("manifest %q declares tools but not the %q capability; a tool is power the agent invokes on its own, and %q is the grant the consent screen shows the user for exactly that (DESIGN-BLOCK-I §I-J) — add %q to \"capabilities\", or drop \"tools\"", m.ID, capToolsRegister, capToolsRegister, capToolsRegister)}
	}
	seen := map[string]bool{}
	for i, tool := range m.Tools {
		if tool.Name == "" {
			return &Error{Loc: m.locAt("tools"), Msg: fmt.Sprintf("tool %d has no name; the name is the relative half of the agent-visible <plugin-id>.<name> (DESIGN-BLOCK-I §I-J Decision 2), so a nameless tool is one the agent can never address — add a \"name\"", i)}
		}
		if !hasTokens(tool.Parameters) {
			return &Error{Loc: m.locAt("tools"), Msg: fmt.Sprintf("tool %q has no parameters; the parameters are the JSON-Schema the host forwards to the agent verbatim (DESIGN-BLOCK-I §I-J Decision 1), and a tool with no schema tells the agent nothing about how to call it — add a \"parameters\" object", tool.Name)}
		}
		if seen[tool.Name] {
			return &Error{Loc: m.locAt("tools"), Msg: fmt.Sprintf("tool %q is declared twice; the relative name composes into the agent-visible <plugin-id>.<name>, so a duplicate would shadow the first and one of the two could never be called (DESIGN-BLOCK-I §I-J) — give each tool a distinct name", tool.Name)}
		}
		seen[tool.Name] = true
	}
	return nil
}
