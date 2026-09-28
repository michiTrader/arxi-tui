package supervisor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests pin I4's host→plugin half: SendAction writes an `action` frame to
// the live child gated on the granted capability, and the Registry routes a
// press by plugin id. The "echoaction" helper mode echoes each received action
// back as an "actionecho" frame, so the round-trip is observable on Frames() —
// the assertion a subprocess test can actually make.

// waitFirstFrame blocks until the plugin's first published frame arrives, which
// I2 forwards only after the handshake acks — so it is the point at which the
// child is live and SendAction's sender has been registered. A test that sent an
// action before this would race the handshake and see ErrPluginNotLive for a
// reason that is not what it is testing.
func waitFirstFrame(t *testing.T, s *Supervisor) {
	t.Helper()
	select {
	case f, ok := <-s.Frames():
		if !ok {
			t.Fatalf("Frames closed before the plugin was live; err=%v", s.Err())
		}
		if f.Type != "bind" {
			t.Fatalf("first frame type = %q, want the handshake-proof bind frame", f.Type)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("plugin never became live (no first frame within 6s)")
	}
}

// TestSupervisorSendActionRoundTrips proves an ext: press reaches the plugin with
// its action name and args intact. The helper echoes the received action back, so
// finding "refresh" and the resolved arg on Frames() proves the frame crossed the
// wire whole. Counterfactual: clearing the sender in runOnce (never registering
// c.enc) makes SendAction return ErrPluginNotLive and no echo ever arrives — the
// registration is what makes the wire two-way.
func TestSupervisorSendActionRoundTrips(t *testing.T) {
	s := Start(context.Background(), helperConfig("echoaction", "tick", []string{capActionsRegister}))
	defer s.Close()
	waitFirstFrame(t, s)

	if err := s.SendAction("a1", "refresh", map[string]string{"symbol": "AAPL"}); err != nil {
		t.Fatalf("SendAction on a live, granted plugin failed: %v\n"+
			"consequence: an ext: press cannot reach the plugin, so a plugin button does nothing.\n"+
			"remedy: SendAction must write the action frame to the live child's stdin.", err)
	}

	deadline := time.After(6 * time.Second)
	for {
		select {
		case f, ok := <-s.Frames():
			if !ok {
				t.Fatal("Frames closed before the action was echoed back")
			}
			if f.Type != "actionecho" {
				continue // the earlier bind frames; keep reading for the echo
			}
			raw := string(f.Raw)
			if !strings.Contains(raw, `"action":"refresh"`) {
				t.Errorf("echoed frame %s does not carry action \"refresh\"; the action name was lost or altered on the wire", raw)
			}
			if !strings.Contains(raw, `"symbol":"AAPL"`) {
				t.Errorf("echoed frame %s does not carry the resolved arg symbol=AAPL; args must cross the wire as concrete values (§I-E)", raw)
			}
			if !strings.Contains(raw, `"id":"a1"`) {
				t.Errorf("echoed frame %s does not carry the correlation id \"a1\"; the host owns the id and it must reach the plugin", raw)
			}
			return
		case <-deadline:
			t.Fatal("no actionecho within 6s; the action frame did not reach the plugin")
		}
	}
}

// TestSupervisorSendActionRefusesUngranted proves the capability gate is
// load-bearing: a plugin the user did not grant actions.register cannot be sent
// an action even while it is live. The gate is checked before the child is
// touched, so the refusal is the same whether or not the process is up.
// Counterfactual: removing the isGranted check makes this SendAction succeed and
// route an action to a plugin that was never consented that power (invariant 7).
func TestSupervisorSendActionRefusesUngranted(t *testing.T) {
	// Granted the empty set: the plugin is live but was consented no capabilities.
	s := Start(context.Background(), helperConfig("echoaction", "tick", nil))
	defer s.Close()
	waitFirstFrame(t, s)

	err := s.SendAction("a1", "refresh", nil)
	if !errors.Is(err, ErrCapabilityNotGranted) {
		t.Fatalf("SendAction to an ungranted plugin err = %v, want ErrCapabilityNotGranted\n"+
			"consequence: a scene could route actions to a plugin the user never granted the\n"+
			"actions.register capability — power taken, not granted at the gate (invariant 7).\n"+
			"remedy: SendAction refuses when actions.register is absent from the granted set.", err)
	}
}

// TestSupervisorSendActionNotLive proves a press arriving while the plugin is
// down is reported, not crashed: after Close the sender is cleared, so SendAction
// (with the capability granted, so it passes the gate and reaches the live check)
// returns ErrPluginNotLive. This is the §I-G "the scene draws, never crashes"
// rule on the host→plugin side.
func TestSupervisorSendActionNotLive(t *testing.T) {
	s := Start(context.Background(), helperConfig("echoaction", "tick", []string{capActionsRegister}))
	waitFirstFrame(t, s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err := s.SendAction("a1", "refresh", nil)
	if !errors.Is(err, ErrPluginNotLive) {
		t.Fatalf("SendAction to a closed plugin err = %v, want ErrPluginNotLive\n"+
			"consequence: a press against a dead plugin panics or writes to a dead pipe instead\n"+
			"of reporting; the escape hatch and the scene must survive a plugin's death.\n"+
			"remedy: the sender is cleared when the child dies, and SendAction reports it.", err)
	}
}

// TestRegistryRoutesByID proves the registry finds the right plugin by id and
// reports the two failure modes distinctly: an unmounted id is
// ErrPluginNotMounted, a routed press reaches the named plugin, and a removed
// plugin is unmounted again. Counterfactual: a Remove that also Closed the
// supervisor, or an Add that did not replace by id, would each be observable as a
// press reaching the wrong process or none.
func TestRegistryRoutesByID(t *testing.T) {
	reg := NewRegistry()

	// A press to a plugin no one mounted is reported, never a crash.
	if err := reg.SendAction("tick", "refresh", nil); !errors.Is(err, ErrPluginNotMounted) {
		t.Fatalf("SendAction to an empty registry err = %v, want ErrPluginNotMounted; a scene may\n"+
			"name ext:tick:refresh before the plugin is mounted, and that must report, not crash", err)
	}

	s := Start(context.Background(), helperConfig("echoaction", "tick", []string{capActionsRegister}))
	defer s.Close()
	waitFirstFrame(t, s)
	reg.Add("tick", s)

	if err := reg.SendAction("tick", "refresh", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("SendAction through the registry to a mounted plugin failed: %v", err)
	}
	// Confirm it actually reached the plugin (not just returned nil): the echo
	// carries the host-generated correlation id, which the registry owns.
	deadline := time.After(6 * time.Second)
	for {
		select {
		case f, ok := <-s.Frames():
			if !ok {
				t.Fatal("Frames closed before the routed action was echoed")
			}
			if f.Type == "actionecho" && strings.Contains(string(f.Raw), `"action":"refresh"`) {
				goto routed
			}
		case <-deadline:
			t.Fatal("the registry-routed action never reached the plugin")
		}
	}
routed:
	reg.Remove("tick")
	if err := reg.SendAction("tick", "refresh", nil); !errors.Is(err, ErrPluginNotMounted) {
		t.Fatalf("SendAction after Remove err = %v, want ErrPluginNotMounted; a stale button after an\n"+
			"unmount must route nowhere, not to a torn-down supervisor", err)
	}
}

// TestSupervisorCallToolRoundTrips proves the agent-call path (§I-J Decision 4):
// CallTool writes the id-correlated action frame, the "replytool" helper answers
// `{type:ok, id, result}`, and CallTool returns that result — the value the agent
// reasons over. The helper echoes the tool name and args into the result, so
// finding them proves the request crossed the wire whole AND the reply was routed
// back to this exact caller by its correlation id. Counterfactual: dropping the
// reader's `s.calls.deliver` diversion (never routing the ok frame to the waiter)
// makes this time out — the round-trip is what the pending-call correlation buys.
func TestSupervisorCallToolRoundTrips(t *testing.T) {
	s := Start(context.Background(), helperConfig("replytool", "tick", []string{capToolsRegister}))
	defer s.Close()
	waitFirstFrame(t, s)

	result, err := s.CallTool("quote", map[string]string{"symbol": "AAPL"})
	if err != nil {
		t.Fatalf("CallTool on a live, granted plugin failed: %v\n"+
			"consequence: the agent cannot call a plugin tool, so a mounted plugin teaches the agent nothing it can use.\n"+
			"remedy: CallTool must write the action frame and return the plugin's id-correlated reply.", err)
	}
	raw := string(result)
	if !strings.Contains(raw, `"tool":"quote"`) {
		t.Errorf("tool result %s does not carry the tool name; the action name was lost or the wrong reply was routed back", raw)
	}
	if !strings.Contains(raw, `"symbol":"AAPL"`) {
		t.Errorf("tool result %s does not carry the resolved arg symbol=AAPL; args must cross the wire as concrete values (§I-J)", raw)
	}
}

// TestSupervisorCallToolRefusesUngranted proves the capability gate is
// load-bearing on the agent-call path exactly as it is on the press path: a
// plugin the user did not grant tools.register cannot have its tools called even
// while it is live, and the refusal is the same whether or not the process is up
// (the grant is the boundary, not the process state). Counterfactual: removing the
// isGranted check makes this CallTool reach a plugin the user never consented to
// call autonomously (invariant 7).
func TestSupervisorCallToolRefusesUngranted(t *testing.T) {
	// Granted the empty set: the plugin is live but was consented no capabilities.
	s := Start(context.Background(), helperConfig("replytool", "tick", nil))
	defer s.Close()
	waitFirstFrame(t, s)

	_, err := s.CallTool("quote", map[string]string{"symbol": "AAPL"})
	if !errors.Is(err, ErrCapabilityNotGranted) {
		t.Fatalf("CallTool on an ungranted plugin err = %v, want ErrCapabilityNotGranted\n"+
			"consequence: the agent could call tools on a plugin the user never granted tools.register —\n"+
			"power taken, not granted at the gate (invariant 7).\n"+
			"remedy: CallTool refuses when tools.register is absent from the granted set.", err)
	}
}

// TestSupervisorCallToolTimesOut proves the wait is bounded: the "silenttool"
// helper reads the action frame and never replies, and CallTool must return
// ErrToolTimeout rather than block forever. The deadline is asserted in the test
// harness (a select on the result against a generous ceiling), so a CallTool that
// truly hung would fail here as a test timeout, not pass. Counterfactual: removing
// the timer arm of CallTool's select makes this hang past the ceiling.
func TestSupervisorCallToolTimesOut(t *testing.T) {
	cfg := helperConfig("silenttool", "tick", []string{capToolsRegister})
	cfg.CallTimeout = 300 * time.Millisecond
	s := Start(context.Background(), cfg)
	defer s.Close()
	waitFirstFrame(t, s)

	type outcome struct {
		result []byte
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := s.CallTool("quote", map[string]string{"symbol": "AAPL"})
		done <- outcome{r, err}
	}()
	select {
	case o := <-done:
		if !errors.Is(o.err, ErrToolTimeout) {
			t.Fatalf("CallTool against a silent plugin err = %v, want ErrToolTimeout\n"+
				"consequence: the agent-facing layer cannot tell a quiet plugin from a working one, and a hung\n"+
				"call would block whatever awaits it.\n"+
				"remedy: CallTool bounds the wait with CallTimeout and returns ErrToolTimeout.", o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CallTool did not return within 5s against a silent plugin; the wait is not bounded and a hung " +
			"plugin freezes the caller — the exact thing the timeout exists to prevent (invariant 6)")
	}
}

// TestSupervisorCallToolMapsErrorReply proves an `error` reply is a DISTINCT
// outcome from a timeout: the "errortool" helper answers an error frame, and
// CallTool must return ErrToolFailed carrying the plugin's own message — not
// ErrToolTimeout, and not a nil error with an empty result. Distinguishing the two
// is what lets the agent tell "the plugin went quiet" from "the plugin answered
// that it could not." Counterfactual: mapping an error frame to ErrToolTimeout (or
// treating any reply as ok) makes this fail on the errors.Is check.
func TestSupervisorCallToolMapsErrorReply(t *testing.T) {
	s := Start(context.Background(), helperConfig("errortool", "tick", []string{capToolsRegister}))
	defer s.Close()
	waitFirstFrame(t, s)

	_, err := s.CallTool("quote", map[string]string{"symbol": "AAPL"})
	if !errors.Is(err, ErrToolFailed) {
		t.Fatalf("CallTool against an error-replying plugin err = %v, want ErrToolFailed\n"+
			"consequence: a plugin's explicit failure is reported as a timeout or as success, so the agent\n"+
			"acts on a result the plugin never produced.\n"+
			"remedy: an `error` reply maps to ErrToolFailed, distinct from ErrToolTimeout.", err)
	}
	if errors.Is(err, ErrToolTimeout) {
		t.Fatalf("CallTool error %v is also ErrToolTimeout; a reported failure and a timeout must be distinct so the agent can tell them apart (§I-J Decision 4)", err)
	}
	if !strings.Contains(err.Error(), "no such symbol") {
		t.Errorf("CallTool error %v does not carry the plugin's own message; the agent needs the plugin's diagnosis, not a generic failure", err)
	}
}
