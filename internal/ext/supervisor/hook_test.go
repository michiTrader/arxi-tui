package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// These tests pin Gate C's awaited round-trip (hook.go, ADR-0009): CallHook writes
// the id-correlated `action` frame naming the hook, awaits the reply with the same
// CallTimeout CallTool uses (F4), and maps a verdict reply, a timeout, an error
// frame and a malformed reply to distinct outcomes — so the core-facing layer can
// apply the fail-safe default (F3) and report them apart. The helper modes
// "replyhook"/"badhook" (plus the reused "errortool"/"silenttool") make each path
// observable against a real subprocess, no live agent.

// TestSupervisorCallHookRoundTrips proves a tool_gate hook's proposed verdict
// crosses the wire and comes back parsed. The "replyhook" helper answers
// {verdict:"ask", reason:"…", action:"hook.tool_gate"}, so finding the verdict and
// the "hook."-prefixed action proves the request named the hook (not a bare tool
// call) AND the reply was routed back to this exact caller by its correlation id.
// Counterfactual: the capability gate removed, the round-trip would still work but
// the ungranted test below would fail — the two together pin that CallHook both
// works and refuses.
func TestSupervisorCallHookRoundTrips(t *testing.T) {
	s := Start(context.Background(), helperConfig("replyhook", "tick", []string{"hooks.tool_gate"}))
	defer s.Close()
	waitFirstFrame(t, s)

	reply, err := s.CallHook("tool_gate", map[string]string{"tool": "bash"})
	if err != nil {
		t.Fatalf("CallHook on a live, granted plugin failed: %v\n"+
			"consequence: a mounted tool_gate hook can never be consulted, so Gate C's plugin-facing half does nothing.\n"+
			"remedy: CallHook must write the hook action frame and return the plugin's id-correlated verdict reply.", err)
	}
	if reply.Verdict != ext.VerdictAsk {
		t.Errorf("CallHook returned verdict %v, want ask; the proposed verdict was lost or mis-parsed on the way back", reply.Verdict)
	}
	if reply.Reason != "needs a human" {
		t.Errorf("CallHook returned reason %q, want \"needs a human\"; the surfaced reason must survive the round-trip so the user sees why", reply.Reason)
	}
}

// TestSupervisorCallHookRefusesUngranted proves the capability gate is load-bearing
// on the hook path exactly as it is on the tool and press paths: a plugin the user
// did not grant hooks.tool_gate cannot have its tool_gate hook consulted even while
// it is live, and the refusal is the same whether or not the process is up (the
// grant is the boundary, not the process state). Counterfactual: removing the
// isGranted check in CallHook routes a hook call to a plugin the user never
// consented to let see and veto the agent's calls (invariant 7).
func TestSupervisorCallHookRefusesUngranted(t *testing.T) {
	// Granted the empty set: the plugin is live but was consented no capabilities.
	s := Start(context.Background(), helperConfig("replyhook", "tick", nil))
	defer s.Close()
	waitFirstFrame(t, s)

	_, err := s.CallHook("tool_gate", map[string]string{"tool": "bash"})
	if !errors.Is(err, ErrCapabilityNotGranted) {
		t.Fatalf("CallHook on an ungranted plugin err = %v, want ErrCapabilityNotGranted\n"+
			"consequence: a plugin the user never granted hooks.tool_gate could still gate the agent's tool calls — power taken, not granted at the gate (invariant 7).\n"+
			"remedy: CallHook refuses when the hook's capability is absent from the granted set.", err)
	}
}

// TestSupervisorCallHookRejectsUnknownKind proves an unknown kind is a reported
// programming error, not a silent gate on an empty capability. validateHooks
// refuses an unknown kind at install, so reaching CallHook with one is a host bug;
// CallHook must say so rather than consult the plugin under no capability.
// Counterfactual: mapping an unknown kind to an empty capability and proceeding
// would spawn a hook the closed set does not know.
func TestSupervisorCallHookRejectsUnknownKind(t *testing.T) {
	s := Start(context.Background(), helperConfig("replyhook", "tick", []string{"hooks.tool_gate"}))
	defer s.Close()
	waitFirstFrame(t, s)

	_, err := s.CallHook("prompt", nil)
	if err == nil {
		t.Fatal("CallHook accepted an unknown hook kind \"prompt\"; the kind set is closed (prompt is deliberately out, F1), so an unknown kind must be reported, not gated on an empty capability")
	}
	if errors.Is(err, ErrCapabilityNotGranted) {
		t.Fatalf("CallHook on an unknown kind returned ErrCapabilityNotGranted; an unknown kind is a host bug, distinct from a plugin the user refused — collapsing them hides the bug: %v", err)
	}
}

// TestSupervisorCallHookTimesOut proves the wait is bounded: the "silenttool"
// helper reads the action frame and never replies, and CallHook must return
// ErrHookTimeout rather than block forever. A hung hook that froze the caller is
// the exact thing invariant 6 forbids — a hook runs on a worker with a timeout,
// never on the loop. Counterfactual: removing the timer arm of CallHook's select
// makes this hang past the ceiling.
func TestSupervisorCallHookTimesOut(t *testing.T) {
	cfg := helperConfig("silenttool", "tick", []string{"hooks.tool_gate"})
	cfg.CallTimeout = 300 * time.Millisecond
	s := Start(context.Background(), cfg)
	defer s.Close()
	waitFirstFrame(t, s)

	type outcome struct {
		reply HookReply
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := s.CallHook("tool_gate", map[string]string{"tool": "bash"})
		done <- outcome{r, err}
	}()
	select {
	case o := <-done:
		if !errors.Is(o.err, ErrHookTimeout) {
			t.Fatalf("CallHook against a silent plugin err = %v, want ErrHookTimeout\n"+
				"consequence: the core-facing layer cannot tell a quiet hook from a working one to apply the fail-safe, and a hung call would freeze whatever awaits it.\n"+
				"remedy: CallHook bounds the wait with CallTimeout and returns ErrHookTimeout.", o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CallHook did not return within 5s against a silent plugin; the wait is not bounded and a hung hook freezes the caller — the exact thing the timeout exists to prevent (invariant 6)")
	}
}

// TestSupervisorCallHookMapsErrorReply proves an `error` reply is a DISTINCT
// outcome from a timeout: the "errortool" helper answers an error frame, and
// CallHook must return ErrHookFailed carrying the plugin's own message — not
// ErrHookTimeout, and not a verdict. Distinguishing the two lets the core-facing
// layer tell "the hook went quiet" from "the hook answered it could not," though
// both resolve to the same fail-safe ceiling (F3). Counterfactual: mapping an error
// frame to ErrHookTimeout (or to a verdict) makes this fail on the errors.Is check.
func TestSupervisorCallHookMapsErrorReply(t *testing.T) {
	s := Start(context.Background(), helperConfig("errortool", "tick", []string{"hooks.tool_gate"}))
	defer s.Close()
	waitFirstFrame(t, s)

	_, err := s.CallHook("tool_gate", map[string]string{"tool": "bash"})
	if !errors.Is(err, ErrHookFailed) {
		t.Fatalf("CallHook against an error-replying plugin err = %v, want ErrHookFailed\n"+
			"consequence: a hook's explicit failure is reported as a timeout or as a verdict, so the core acts on a disposition the plugin never proposed.\n"+
			"remedy: an `error` reply maps to ErrHookFailed, distinct from ErrHookTimeout.", err)
	}
	if errors.Is(err, ErrHookTimeout) {
		t.Fatalf("CallHook error %v is also ErrHookTimeout; a reported failure and a timeout must be distinct so the fail-safe layer can report them apart (F3)", err)
	}
}

// TestSupervisorCallHookRejectsAMalformedVerdict proves a reply that decodes as
// `ok` but carries no legal verdict token is ErrHookBadReply, NEVER a silent allow.
// The "badhook" helper answers {verdict:"permit"}; a narrow-only gate must treat a
// garbled verdict as the caller's cue to fail safe, not as permission. Counterfactual:
// defaulting an unparseable token to VerdictAllow would let a malformed reply loosen
// a call — the one direction Gate C must never default to (verdict.go ParseVerdict).
func TestSupervisorCallHookRejectsAMalformedVerdict(t *testing.T) {
	s := Start(context.Background(), helperConfig("badhook", "tick", []string{"hooks.tool_gate"}))
	defer s.Close()
	waitFirstFrame(t, s)

	reply, err := s.CallHook("tool_gate", map[string]string{"tool": "bash"})
	if !errors.Is(err, ErrHookBadReply) {
		t.Fatalf("CallHook against a plugin returning verdict \"permit\" err = %v, want ErrHookBadReply\n"+
			"consequence: a malformed verdict is read as a legal one (or silently as allow), so a garbled reply could loosen a call — the direction a narrow-only gate must never default to (F3).\n"+
			"remedy: an unparseable verdict token is ErrHookBadReply, and the caller fails safe.", err)
	}
	// On the error path the returned HookReply is the zero value and must NOT be
	// consumed as a verdict — the error is the signal. (A zero HookReply has
	// Verdict == VerdictAllow, which is exactly why a bad reply returns an error
	// rather than that zero value as if it were a real allow.)
	_ = reply
}
