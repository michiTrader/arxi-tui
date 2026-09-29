package ext

import "fmt"

// This file is the J4 fan-out: the sequencing half DESIGN-BLOCK-J.md names as one
// of the two only-new artifacts a bundle install adds (the other, the presentation
// half, is BundleConsentScene). It is the pure core the live loop calls once the
// user has answered the one bundle consent screen — no network, no spawn, no UI —
// so the security-load-bearing decision (one answer fans out to N per-plugin
// grants, each against the plugin's OWN unchanged Q15 identity, in grant-then-
// compose order) is pinned by a counterfactual here rather than only inside the
// modal loop. It lands the way J2's ParseRegistry and J4's ParseBundle did: the
// pure unit first, dead until the loop wiring that fetches and composes calls it.
//
// # Why the fan-out lives here and not in the loop
//
// The rejected reading of "one consent screen" was one aggregate identity over the
// whole bundle (DESIGN-BLOCK-J.md "How 'one consent screen' aggregates N grants"):
// it would have made Gate.Decide/Grant bundle-shaped and broken grant transfer in
// both directions. The chosen reading keeps Identity/Decide/Grant strictly
// per-manifest and aggregates only the decision and its presentation. That makes
// the fan-out a pure function over the same per-plugin gate the standalone install
// uses N times — so it belongs beside consent.go, testable without a loop, not
// buried in the select where its all-or-nothing and grant-then-compose properties
// could only be exercised through a live terminal.

// BundlePluginDecision pairs one fetched-and-laid-out plugin with the gate's
// per-plugin Decide result. It is what the loop hands the fan-out after it has run
// the unchanged H6/I5 pipeline (fetch each manifest_url, InstallFromBundle,
// LayoutByDigest, Gate.Decide) once per plugin: the Manifest and Digest are the
// plugin's own identity inputs, and Decision.Status says whether the gate already
// remembers a grant for exactly these bytes. Keeping the Decision here rather than
// re-deriving it means the fan-out and the consent screen read the same answer the
// loop computed, so a plugin shown as trusted cannot be re-granted by a fan-out
// that recomputed the decision differently.
type BundlePluginDecision struct {
	Manifest *Manifest
	Digest   string
	Decision Decision
}

// ConsentsFor projects the loop's per-plugin decisions into the
// []BundlePluginConsent BundleConsentScene renders, so the screen and the fan-out
// read one list. Remembered is taken straight from the gate's decision, which is
// the whole point of the trusted-no-new-power distinction: the screen lists a
// remembered plugin by name only and the fan-out skips its Grant, and both must
// agree on which plugins those are or the user could grant a power the screen said
// was already trusted (or the screen could hide a fresh grant the fan-out makes).
func ConsentsFor(decisions []BundlePluginDecision) []BundlePluginConsent {
	out := make([]BundlePluginConsent, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, BundlePluginConsent{
			Manifest:   d.Manifest,
			Digest:     d.Digest,
			Remembered: d.Decision.Status == DecisionRemembered,
		})
	}
	return out
}

// BundleAnswer is the user's single reply to the one bundle consent screen. Unlike
// supervisor.ConsentAnswer it carries no per-capability selection: a bundle is
// all-or-nothing (DESIGN-BLOCK-J.md — "a scene wired to a plugin the user rejected
// is a scene with dead binds"), so `y`/`r` grant every declared capability of
// every not-yet-remembered plugin and `n` grants nothing. Remember maps to the
// gate's per-plugin persistence, and Rejected is kept distinct from a zero value
// so an unanswered screen (the loop's default) is never mistaken for a grant —
// "no answer is not a yes" (Q15) survives the crossing from the loop to here.
type BundleAnswer struct {
	Remember bool
	Rejected bool
}

// BundleGrant is the granted subset one plugin carries to its supervisor
// (supervisor.Config.Granted) after the fan-out. ID names the plugin so the loop
// can pair the grant with the Installed package it laid out; Granted is the exact
// subset the gate produced, which for a freshly-consented plugin is its declared
// set and for an already-remembered plugin is the set on record. Both kinds appear
// in the fan-out's output because the loop must spawn ALL of the bundle's plugins,
// not only the ones it just prompted for.
type BundleGrant struct {
	ID      string
	Granted []string
}

// ErrBundleRejected is a bundle the user refused at the one consent screen. It is
// returned rather than swallowed so the loop can report "you rejected this bundle"
// distinctly from a grant refusal or a fetch failure, and — because the fan-out
// grants and composes nothing on a rejection — a later install of the same bundle
// re-prompts (the rejection is session-local by construction, the same contract
// ErrConsentRejected keeps for a single plugin).
var ErrBundleRejected = fmt.Errorf("bundle consent rejected")

// GrantBundle fans the single bundle answer out to N per-plugin grants and returns
// the granted subset each plugin will carry to its supervisor. It is the sequencing
// the design pins: grant-then-compose, all-or-nothing, one Grant per not-yet-
// remembered plugin against that plugin's own identity.
//
// On a rejection it grants nothing, remembers nothing and composes nothing (the
// caller must not compose when this returns an error): a bundle is the interface it
// ships, and a partially granted bundle is a scene with dead binds. On a grant it
// walks the decisions in order and, for each plugin the gate did NOT already
// remember, calls Gate.Grant(m, digest, m.Capabilities, answer.Remember) — the same
// call the standalone install makes, N times — so the grant binds to the plugin's
// own per-manifest identity and "remember" writes N per-plugin rows a later
// standalone or cross-bundle sight of the same bytes finds already granted. An
// already-remembered plugin is carried through with its remembered set and is NOT
// re-granted: re-granting it would be a second identical row and would re-persist a
// grant the user is not being asked about on this screen.
//
// # Grant-then-compose and what "atomic" scopes to
//
// The N Grant calls precede any patch.Mount/theme.Merge in the loop, so a Grant
// that refuses (a manifest declaring a capability KnownCapability does not know)
// aborts the whole install with nothing composed — the workspace is unchanged, the
// atomicity checkEmpty protects at parse time carried to install time
// (DESIGN-BLOCK-J.md). The atomicity is over the WORKSPACE, deliberately not over
// the remember store: a plugin granted-and-remembered before a later plugin's Grant
// fails keeps its remembered row, which is harmless and true — the user did consent
// to that plugin's terms on this screen, so a later standalone sight of it finding
// the grant on record is correct, not a leak. Making the remember store transactional
// too would need a gate primitive that does not exist and would buy nothing the
// compose-scoped atomicity does not already give.
func GrantBundle(gate *Gate, decisions []BundlePluginDecision, answer BundleAnswer) ([]BundleGrant, error) {
	if answer.Rejected {
		return nil, ErrBundleRejected
	}
	grants := make([]BundleGrant, 0, len(decisions))
	for _, d := range decisions {
		m := d.Manifest
		if d.Decision.Status == DecisionRemembered {
			// Trusted-no-new-power: the screen listed it by name, the gate already
			// holds its grant, so carry that set through without a second Grant.
			grants = append(grants, BundleGrant{ID: m.ID, Granted: d.Decision.Granted})
			continue
		}
		granted, err := gate.Grant(m, d.Digest, m.Capabilities, answer.Remember)
		if err != nil {
			// Grant-then-compose: abort before the caller composes so a bundle that
			// cannot be fully granted changes nothing in the workspace.
			return nil, fmt.Errorf("granting consent for bundle plugin %q: %w", m.ID, err)
		}
		grants = append(grants, BundleGrant{ID: m.ID, Granted: granted})
	}
	return grants, nil
}
