package ext

import (
	"fmt"
	"sort"
)

// This file is the I5 consent gate: the decision, by identity, of which of a
// behavioral plugin's declared capabilities the user has granted, and whether
// that decision is remembered across sessions (DESIGN-BLOCK-I §I-H, Q15). The
// gate produces the `granted` subset the supervisor carries to the child in its
// ack (supervisor.Config.Granted), so "power granted at the gate, once"
// (invariant 7) has exactly one origin: a manifest declares what it *wants*, and
// only the gate decides what it *gets*.
//
// The whole behaviour is inherited from arxi-sim (LESSONS.md): rejection is
// session-local, grants persist as identity-bound allow-lists ("remember"), the
// capability set is closed, and installation trust is never capability trust —
// downloading a plugin does not grant it anything, the gate does.

// capToolsRegister is the capability a plugin needs to expose tools the agent
// may call on its own (Gate B, §I-J Decision 3). It is a distinct capability
// rather than a reuse of actions.register because the two are different powers
// the consent screen must let the user weigh apart: actions.register is "may
// receive button presses the user initiated," while this is "your agent may
// call this plugin on its own, with no user in the loop at call time." Named
// once here so the closed set, the manifest validator (validateTools) and the
// send gate cannot drift on the spelling.
const capToolsRegister = "tools.register"

// knownCapabilities is the closed vocabulary of powers a behavioral plugin may
// request. Closed because each capability is a door the host opens in its own
// code (DESIGN-BLOCK-H.md §capabilities): a manifest cannot mint a new power by
// naming it, so an unknown capability is a refusal at the gate, not a silent
// grant of something the host does not implement. The set matches the wire
// (DESIGN-BLOCK-I §I-A) and arxi-sim's capability.go; `actions.register` is the
// one the ext: press path already gates on (supervisor/send.go), and
// `tools.register` (§I-J) is the one the agent-call path gates on.
var knownCapabilities = map[string]bool{
	"events.subscribe": true,
	"events.emit":      true,
	"inbox.answer":     true,
	"actions.register": true,
	capToolsRegister:   true,
}

// KnownCapability reports whether a capability name belongs to the closed set.
// It is exported so the manifest validator and the gate ask the one question the
// same way, rather than each keeping its own list that could drift.
func KnownCapability(capability string) bool {
	return knownCapabilities[capability]
}

// CapabilityStatus is the three-way answer to "may this plugin use this
// capability right now." The three states are kept distinct because collapsing
// NotDeclared and NotGranted into one "denied" loses the diagnosis the user
// needs: NotDeclared means the manifest never asked (the plugin is broken or the
// scene names a power it does not carry), while NotGranted means the manifest
// asked and the user said no. A single error would make "it never asked"
// indistinguishable from "you refused it" (DESIGN-BLOCK-I §I-H, LESSONS.md).
type CapabilityStatus int

const (
	// CapabilityGranted: the capability is declared by the manifest and present
	// in the granted subset — the plugin may use it.
	CapabilityGranted CapabilityStatus = iota
	// CapabilityNotGranted: the manifest declared it, but consent was refused, so
	// it is absent from the granted subset. The user said no.
	CapabilityNotGranted
	// CapabilityNotDeclared: the manifest never listed it. The plugin is asking
	// for power it did not declare, which the manifest schema would never have
	// shown the user at the gate.
	CapabilityNotDeclared
)

// Classify answers, for one capability, which of the three states it is in given
// a manifest's declared set and the granted subset the gate produced. It is the
// single source the host uses to phrase the right refusal: the send path checks
// only membership of the granted set (it is on the hot wire path), but a
// user-facing report distinguishes the two denials so "it never asked" and "you
// said no" read differently.
func Classify(m *Manifest, granted []string, capability string) CapabilityStatus {
	declared := false
	for _, c := range m.Capabilities {
		if c == capability {
			declared = true
			break
		}
	}
	if !declared {
		return CapabilityNotDeclared
	}
	for _, g := range granted {
		if g == capability {
			return CapabilityGranted
		}
	}
	return CapabilityNotGranted
}

// ConsentStore is the persistence seam for remembered grants, keyed by the
// identity string (Identity). It is an interface so the gate does not care
// whether grants live in memory for one session or in a config file across
// sessions: the disk-backed implementation lands with the config layer, and
// until then the in-memory store gives the gate real "remember" semantics within
// a session that a test can exercise. Lookup returns the remembered granted set
// and whether one exists; Remember records an exact set against an identity.
type ConsentStore interface {
	Lookup(identity string) (granted []string, ok bool)
	Remember(identity string, granted []string)
}

// MemoryConsentStore is a session-scoped ConsentStore. It is the honest default
// before a config file exists: a grant remembered here survives a re-mount in
// the same session (the second Decide is Remembered) but not a restart of the
// host, which is exactly what "remember" means until the persisted store is
// wired.
type MemoryConsentStore struct {
	byIdentity map[string][]string
}

// NewMemoryConsentStore returns an empty in-memory store.
func NewMemoryConsentStore() *MemoryConsentStore {
	return &MemoryConsentStore{byIdentity: map[string][]string{}}
}

// Lookup returns a copy of the remembered granted set so a caller cannot mutate
// the store's slice in place — a granted set is authority, and handing out the
// live backing array would let one caller widen a remembered grant for the next.
func (s *MemoryConsentStore) Lookup(identity string) ([]string, bool) {
	g, ok := s.byIdentity[identity]
	if !ok {
		return nil, false
	}
	return append([]string(nil), g...), true
}

// Remember stores a copy of the granted set against the identity, replacing any
// prior grant for the same identity (a re-grant is not two entries). It copies
// for the same reason Lookup does: the store owns the set it hands back.
func (s *MemoryConsentStore) Remember(identity string, granted []string) {
	s.byIdentity[identity] = append([]string(nil), granted...)
}

// DecisionStatus is whether the gate could answer from memory or must ask.
type DecisionStatus int

const (
	// DecisionRemembered: a grant for this exact identity is on record, so the
	// gate answers with the remembered granted set and no prompt is shown.
	DecisionRemembered DecisionStatus = iota
	// DecisionNeedsConsent: no grant is on record for this identity, so the host
	// must prompt the user. The identity changing (a version bump, a new digest,
	// a different arg) lands here — a bump re-asks.
	DecisionNeedsConsent
)

// Decision is the gate's answer for one manifest+digest. When Remembered,
// Granted carries the remembered subset ready for supervisor.Config.Granted;
// when NeedsConsent, Granted is empty and Identity is the key the host will
// Grant or Reject against once the user answers.
type Decision struct {
	Status   DecisionStatus
	Identity string
	Granted  []string
}

// Gate is the consent gate over a ConsentStore. It is held by the loop and
// consulted before a behavioral plugin is spawned: no process starts before the
// gate returns a granted subset, because "download ≠ trust ≠ grant" (Q15) and a
// spawned process is already acting.
type Gate struct {
	store ConsentStore
}

// NewGate returns a gate backed by store. A nil store is a programming error
// rather than a silent no-remember mode: a gate that cannot remember would
// re-ask on every mount and quietly defeat the "remember" half of Q15, so it is
// caught here instead of surfacing as a mysteriously forgetful prompt.
func NewGate(store ConsentStore) *Gate {
	if store == nil {
		panic("ext.NewGate: nil ConsentStore; a gate with no store cannot remember a grant and would re-ask every mount")
	}
	return &Gate{store: store}
}

// Decide computes the manifest's identity against the digest and answers from
// the store. It never prompts and never spawns — it only reports whether a
// remembered grant covers this exact identity, so the caller can skip the prompt
// when it does and show it when it does not. The digest is the loader's
// PackageDigest over the fetched package, so a byte that changed since the grant
// was remembered produces a new identity and DecisionNeedsConsent (the grant does
// not transfer to bytes the user never saw).
func (g *Gate) Decide(m *Manifest, digest string) Decision {
	id := Identity(m, digest)
	if granted, ok := g.store.Lookup(id); ok {
		return Decision{Status: DecisionRemembered, Identity: id, Granted: granted}
	}
	return Decision{Status: DecisionNeedsConsent, Identity: id, Granted: nil}
}

// Grant records the user's answer to a NeedsConsent decision and returns the
// granted subset to carry to the child. It refuses to grant a capability the
// manifest did not declare or one outside the closed set: granting undeclared
// power is granting something the consent screen never showed, and granting an
// unknown capability is granting a door the host has no code for — both are the
// gate widening authority past what was asked, which is the one thing the gate
// exists to prevent.
//
// remember decides persistence and nothing else: with remember, the grant is
// stored against the identity so the next Decide for the same bytes is
// Remembered; without it, the grant holds for this mount only and the next mount
// re-asks. Rejection needs no method — the caller simply does not Grant, and
// because a rejection is never stored it is session-local by construction
// (DESIGN-BLOCK-I §I-H).
func (g *Gate) Grant(m *Manifest, digest string, granted []string, remember bool) ([]string, error) {
	declared := map[string]bool{}
	for _, c := range m.Capabilities {
		declared[c] = true
	}
	for _, want := range granted {
		if !KnownCapability(want) {
			return nil, fmt.Errorf("cannot grant unknown capability %q to plugin %q; the capability set is closed because each capability is a door the host opens in its own code (DESIGN-BLOCK-H.md) — a name the host does not implement cannot be granted", want, m.ID)
		}
		if !declared[want] {
			return nil, fmt.Errorf("cannot grant capability %q to plugin %q; the manifest never declared it, so the consent screen never showed it — granting it would widen authority past what the user was asked (DESIGN-BLOCK-I §I-H)", want, m.ID)
		}
	}
	out := append([]string(nil), granted...)
	sort.Strings(out)
	if remember {
		g.store.Remember(Identity(m, digest), out)
	}
	return out, nil
}
