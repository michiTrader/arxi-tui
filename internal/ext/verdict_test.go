package ext

import "testing"

// These tests pin the pure half of Gate C (verdict.go, ADR-0009): the
// most-restrictive-wins fold and its deterministic reason selection. No process,
// no wire — just the two load-bearing properties, each with the counterfactual the
// design named: a plugin `allow` must never override a core `deny` (narrow-only),
// and reordering the inputs must change neither the composed verdict nor the
// surfaced reason (identity-ordered determinism, F5).

// TestVerdictOrderingIsRestrictiveness pins the one fact the whole fold rests on:
// the integer order of the three verdicts IS their restrictiveness order, so
// "most restrictive wins" can be a plain max. If someone reordered the iota, deny
// would stop dominating and a plugin could loosen a call by returning the newly
// highest value — the narrow-only invariant turned inside out with no other code
// change.
func TestVerdictOrderingIsRestrictiveness(t *testing.T) {
	if !(VerdictAllow < VerdictAsk && VerdictAsk < VerdictDeny) {
		t.Fatalf("verdict order allow=%d ask=%d deny=%d is not allow<ask<deny\n"+
			"consequence: ComposeVerdict is a max over this order, so a wrong ordering makes some verdict other than deny win the fold — a hook could then widen a call, defeating the narrow-only invariant (ADR-0009).\n"+
			"remedy: keep the const block ordered least- to most-restrictive.",
			VerdictAllow, VerdictAsk, VerdictDeny)
	}
}

// TestParseVerdictRefusesUnknownTokensWithoutLooseningToAllow pins that an
// unparseable token is reported as not-ok and NEVER silently becomes allow. The
// returned Verdict on the false path is incidental; the ok bool is the contract.
// A gate that mapped a bad token to allow would let a garbled reply loosen a call,
// the one direction a narrow-only gate must never default to.
func TestParseVerdictRefusesUnknownTokensWithoutLooseningToAllow(t *testing.T) {
	for tok, want := range map[string]Verdict{"allow": VerdictAllow, "ask": VerdictAsk, "deny": VerdictDeny} {
		if v, ok := ParseVerdict(tok); !ok || v != want {
			t.Fatalf("ParseVerdict(%q) = (%v,%v); want (%v,true)", tok, v, ok, want)
		}
	}
	if _, ok := ParseVerdict("permit"); ok {
		t.Fatal("ParseVerdict(\"permit\") reported ok\n" +
			"consequence: a malformed verdict token is treated as a legal one, and the caller cannot tell it apart to fail safe (F3).\n" +
			"remedy: only allow/ask/deny are legal; everything else returns ok=false.")
	}
}

// TestComposeVerdictNarrowsNeverWidens is the narrow-only counterfactual: against
// a core `deny`, a hook proposing `allow` must lose — the composed verdict stays
// `deny`. The reverse (a core `allow` a hook tightens to `deny`) must take the
// hook's `deny`. Together they prove the fold is max over restrictiveness, so a
// plugin can only raise the bar, which is the whole "the user governs the agent"
// direction (ADR-0009, the stance).
func TestComposeVerdictNarrowsNeverWidens(t *testing.T) {
	// A hook's allow cannot loosen a core deny.
	got := ComposeVerdict(VerdictDeny, []HookVerdict{{Identity: "p1", Verdict: VerdictAllow, Reason: "looks fine"}})
	if got.Verdict != VerdictDeny {
		t.Fatalf("ComposeVerdict(deny, [allow]).Verdict = %v, want deny\n"+
			"consequence: a plugin's allow overrode the core's deny — the agent widened its own tool policy through a hook, the exact thing agent.tool.policy-off-wire forbids (ADR-0009).\n"+
			"remedy: the fold is most-restrictive-wins; a hook can never lower the core verdict.", got.Verdict)
	}
	// A hook's deny tightens a core allow.
	got = ComposeVerdict(VerdictAllow, []HookVerdict{{Identity: "p1", Verdict: VerdictDeny, Reason: "blocked"}})
	if got.Verdict != VerdictDeny || got.Reason != "blocked" {
		t.Fatalf("ComposeVerdict(allow, [deny]) = {%v,%q}, want {deny,\"blocked\"}\n"+
			"consequence: a hook that vetoes a call the core would allow is ignored, so a tool_gate hook cannot do the one thing it exists for.\n"+
			"remedy: the hook verdict wins when it is more restrictive, and its reason is surfaced.", got.Verdict, got.Reason)
	}
	// An ask sits between: a core allow becomes ask when a hook asks.
	got = ComposeVerdict(VerdictAllow, []HookVerdict{{Identity: "p1", Verdict: VerdictAsk, Reason: "please confirm"}})
	if got.Verdict != VerdictAsk {
		t.Fatalf("ComposeVerdict(allow, [ask]).Verdict = %v, want ask; a hook must be able to escalate an allow to the durable inbox decision (F3 ceiling)", got.Verdict)
	}
}

// TestComposeVerdictReasonIsEmptyWhenCoreAloneDecides pins that the surfaced reason
// is a HOOK's reason or nothing: when no hook reaches the winning level (here the
// core deny dominates every hook), the reason is empty, because the core's own
// disposition is not a hook's reason to attribute. A non-empty reason here would
// put words in a plugin's mouth it never said.
func TestComposeVerdictReasonIsEmptyWhenCoreAloneDecides(t *testing.T) {
	got := ComposeVerdict(VerdictDeny, []HookVerdict{{Identity: "p1", Verdict: VerdictAllow, Reason: "fine"}})
	if got.Verdict != VerdictDeny || got.Reason != "" {
		t.Fatalf("ComposeVerdict(deny, [allow]) = {%v,%q}, want {deny,\"\"}\n"+
			"consequence: the user is shown a reason attributed to a hook that never proposed the winning verdict — a fabricated justification.\n"+
			"remedy: the reason is a hook's only when a hook is at the winning level; otherwise empty.", got.Verdict, got.Reason)
	}
	// No hooks at all: deny from the core, empty reason.
	if got := ComposeVerdict(VerdictDeny, nil); got.Verdict != VerdictDeny || got.Reason != "" {
		t.Fatalf("ComposeVerdict(deny, nil) = {%v,%q}, want {deny,\"\"}", got.Verdict, got.Reason)
	}
}

// TestComposeVerdictIsOrderIndependentButReasonIsIdentityOrdered is the F5
// counterfactual: reordering the hook inputs changes NEITHER the composed verdict
// (max is commutative) NOR the surfaced reason (the hooks are sorted by consent
// identity before the reason is chosen). Two hooks both propose deny; the surfaced
// reason must be the identity-first hook's reason regardless of input order, so
// replay surfaces the same sentence every time instead of whatever a map iteration
// produced.
func TestComposeVerdictIsOrderIndependentButReasonIsIdentityOrdered(t *testing.T) {
	a := HookVerdict{Identity: "aaa", Verdict: VerdictDeny, Reason: "from a"}
	b := HookVerdict{Identity: "bbb", Verdict: VerdictDeny, Reason: "from b"}

	forward := ComposeVerdict(VerdictAllow, []HookVerdict{a, b})
	reverse := ComposeVerdict(VerdictAllow, []HookVerdict{b, a})

	if forward.Verdict != VerdictDeny || reverse.Verdict != VerdictDeny {
		t.Fatalf("composed verdict changed with input order: forward=%v reverse=%v; most-restrictive-wins is a commutative max and must not depend on order", forward.Verdict, reverse.Verdict)
	}
	if forward.Reason != "from a" || reverse.Reason != "from a" {
		t.Fatalf("surfaced reason = forward %q, reverse %q; want \"from a\" both ways\n"+
			"consequence: the reason the user sees (and replay records) depends on map/slice order, so the same mount can surface a different plugin's justification on replay (F5).\n"+
			"remedy: order the hooks by their §I-H consent identity before choosing the reason at the winning level.", forward.Reason, reverse.Reason)
	}
}
