package ext

import "sort"

// This file is the pure half of Gate C's tool-gate composition (ADR-0009,
// DESIGN-BLOCK-K1-GATE-C.md, the stance and F5): given the core's own verdict for
// a tool call and the verdicts proposed by any mounted hooks, compute the one
// verdict the core acts on. It needs no process, no wire and no live agent — it
// is the fourth of the buildable-now quartet, and the one that is pure data.
//
// Two invariants are encoded here, both load-bearing:
//
//   - A hook may only NARROW, never widen (the agent.tool.policy-off-wire rule).
//     Composition is most-restrictive-wins (deny > ask > allow), so a plugin can
//     tighten a call to ask-first or deny but can never turn a core deny into an
//     allow. This makes "the user governs the agent" the only representable
//     direction: a plugin's allow against a core deny is simply the lower of the
//     two and loses.
//   - Stacking order is deterministic (F5). Under most-restrictive-wins the
//     composed verdict is already order-independent, but the surfaced REASON (which
//     hook's ask the user is shown) is not, so the hooks are ordered by their
//     consent-identity before the reason is chosen. Replay then surfaces the same
//     reason every time, rather than whatever order a map iteration happened to
//     produce.

// Verdict is a tool-call disposition. The three values are ordered by
// restrictiveness — the integer order IS the composition order, so "most
// restrictive wins" is a max and nothing else needs to know the ranking.
type Verdict int

const (
	// VerdictAllow lets the call proceed. Least restrictive.
	VerdictAllow Verdict = iota
	// VerdictAsk suspends the call to the durable inbox decision the user drives
	// (M4's inbox.approve/reject/reply). More restrictive than allow.
	VerdictAsk
	// VerdictDeny stops the call. Most restrictive.
	VerdictDeny
)

// String renders a verdict as the wire token a hook reply carries, so a decoded
// reply and a composed result name the disposition the same way.
func (v Verdict) String() string {
	switch v {
	case VerdictAllow:
		return "allow"
	case VerdictAsk:
		return "ask"
	case VerdictDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// ParseVerdict reads a wire verdict token into a Verdict, reporting whether it is
// one of the three legal tokens. An unknown token is NOT silently treated as
// allow (the permissive direction a narrow-only gate must never default to): the
// caller maps a bad reply to the fail-safe default (ask), the same place a timeout
// or an error reply lands (F3), rather than letting a malformed verdict loosen the
// call.
func ParseVerdict(s string) (Verdict, bool) {
	switch s {
	case "allow":
		return VerdictAllow, true
	case "ask":
		return VerdictAsk, true
	case "deny":
		return VerdictDeny, true
	default:
		return VerdictAllow, false
	}
}

// HookVerdict is one hook's proposed disposition for a tool call, carrying the
// identity it was consulted under (so the composer can order deterministically)
// and the reason it offers (so the surfaced reason is a real sentence the user
// sees, not just a level). Identity is the §I-H consent-identity tuple string the
// gate already keys everything by.
type HookVerdict struct {
	Identity string
	Verdict  Verdict
	Reason   string
}

// ComposedVerdict is the result of folding the core verdict and every hook
// verdict together: the one disposition the core acts on, plus the reason to
// surface. Reason is the reason of the identity-first hook AT the winning level;
// it is empty when the core verdict alone decided (no hook reached that level),
// because the core's own disposition is not a hook's reason to attribute.
type ComposedVerdict struct {
	Verdict Verdict
	Reason  string
}

// ComposeVerdict folds the core's verdict and the hooks' proposed verdicts into
// the single verdict the core acts on, most-restrictive-wins, with the surfaced
// reason chosen deterministically by consent identity (F5).
//
// The composed verdict is max(core, max over hooks): a hook can only raise the
// restriction, never lower it, which is the narrow-only invariant. The surfaced
// reason is the reason of the first hook — ordered by Identity — whose verdict
// equals the winning level; if no hook is at the winning level (the core verdict
// dominates, or there are no hooks), the reason is empty. Sorting a copy keeps the
// function pure: a caller's slice is never reordered under it.
func ComposeVerdict(core Verdict, hooks []HookVerdict) ComposedVerdict {
	winner := core
	for _, h := range hooks {
		if h.Verdict > winner {
			winner = h.Verdict
		}
	}
	ordered := append([]HookVerdict(nil), hooks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Identity < ordered[j].Identity
	})
	reason := ""
	for _, h := range ordered {
		if h.Verdict == winner {
			reason = h.Reason
			break
		}
	}
	return ComposedVerdict{Verdict: winner, Reason: reason}
}
