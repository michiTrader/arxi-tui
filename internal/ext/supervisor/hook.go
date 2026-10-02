package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/michiTrader/arxi_tui/internal/ext"
)

// This file is the host→plugin half of Gate C's awaited round-trip (ADR-0009,
// DESIGN-BLOCK-K1-GATE-C.md "The invocation shape"): CallHook, the request/response
// sibling of §I-J's CallTool. Where CallTool asks a plugin for a tool result the
// agent reasons over, CallHook asks a plugin for a PROPOSED VERDICT the core folds
// most-restrictive-wins before acting on it. The two share the wire — the same
// id-correlated `action` frame and `ok`/`error` reply — because the design reuses
// the §I-E round-trip rather than inventing a hook frame; CallHook is "the action
// channel plus the wait," gated on a hook capability instead of tools.register.
//
// It is the third of the buildable-now quartet. The core-facing half that would
// feed this verdict into TurnToolPolicyResolver waits on Block M and the surface
// bump (named in the design, never faked here), so CallHook stands alone against a
// helper process exactly as CallTool did: observable headless, no live agent.

var (
	// ErrHookTimeout is a CallHook whose plugin read the request but did not reply
	// within CallTimeout (F4 reuses the §I-J CallTool deadline). It is DISTINCT
	// from ErrHookFailed so the core-facing layer can report "the hook went quiet"
	// apart from "the hook answered it could not" — but both resolve to the same
	// fail-safe ceiling (F3: a tool_gate that fell over is treated as `ask`, never a
	// silent `allow`), applied by the caller, not swallowed here.
	ErrHookTimeout = errors.New("hook call timed out")
	// ErrHookFailed is a CallHook the plugin answered with an `error` frame. The
	// hook ran and reported it could not propose a verdict; the wrapped message
	// carries the plugin's own code/message, and it is distinct from a timeout for
	// the reason above.
	ErrHookFailed = errors.New("hook call failed")
	// ErrHookBadReply is a CallHook whose reply decoded as `ok` but did not carry a
	// legal verdict token (allow/ask/deny). It is kept distinct from ErrHookFailed —
	// an `error` frame is the plugin saying "I could not," a bad reply is the plugin
	// answering nonsense — so a plugin that returns a malformed verdict is not read
	// as a refusal. Either way the caller fails safe (F3), but the log tells them
	// apart. A bad verdict is NEVER coerced to allow: the narrow-only gate must not
	// let a garbled reply loosen a call (verdict.go ParseVerdict, same stance).
	ErrHookBadReply = errors.New("hook reply carried no legal verdict")
)

// hookActionPrefix namespaces a hook invocation's `action` segment so it cannot
// collide with a tool call's action name. A plugin that declares both a tool named
// "tool_gate" and a tool_gate hook would otherwise receive two different requests
// under the same action string; prefixing the hook makes "call your tool X" and
// "run your hook of kind X" distinguishable on the one shared channel. It mirrors
// the design's illustrative `gate.tool` naming (DESIGN-BLOCK-K1-GATE-C.md, the
// invocation shape) without freezing a per-kind string the signature did not sign.
const hookActionPrefix = "hook."

// HookReply is one plugin's proposed disposition for a hook invocation: the verdict
// it proposes and the reason it offers. It is the per-plugin half the core-facing
// layer lifts into an ext.HookVerdict (adding this plugin's §I-H identity) before
// folding it with the core verdict and any sibling hooks (ext.ComposeVerdict). The
// verdict is already parsed to ext.Verdict here so a malformed token is an error at
// the wire, not a bad value carried inward.
type HookReply struct {
	Verdict ext.Verdict
	Reason  string
}

// wireHookResult is the `result` object a tool_gate hook's `ok` reply carries
// (DESIGN-BLOCK-K1-GATE-C.md, the invocation shape:
// `{"type":"ok","id":"h9","result":{"verdict":"ask","reason":"…"}}`). Verdict is
// the wire token (allow/ask/deny); reason is the sentence surfaced to the user when
// this hook's verdict wins the fold. Both stay strings on the wire and are parsed
// into the typed HookReply here.
type wireHookResult struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

// CallHook invokes a mounted hook and returns the verdict it proposes (ADR-0009).
// It is the awaited round-trip of Gate C's plugin-facing half: it writes the
// id-correlated `action` frame naming the hook kind, waits for the plugin's
// `ok`/`error` reply with the same CallTimeout CallTool uses (F4), and returns the
// parsed verdict and reason — leaving the fold (most-restrictive-wins) and the
// fail-safe default (F3) to the caller, which alone knows the core verdict and the
// hook's ceiling.
//
// The capability gate is checked first, before the child is touched, so a plugin
// never granted the hook's capability is refused identically whether or not it is
// live — the grant is the boundary, not the process state (invariant 7). An unknown
// kind is a programming error (validateHooks refuses it at install, long before a
// mount), reported rather than silently gating on an empty capability. The wait is
// bounded three ways exactly as CallTool's is — the reply, CallTimeout, and the
// supervisor ctx — so a hung hook cannot freeze the loop or capture the panic
// gesture (invariant 6): a hook runs on a worker with a timeout, never on the loop.
func (s *Supervisor) CallHook(kind string, args map[string]string) (HookReply, error) {
	cap, ok := ext.HookKindCapability(kind)
	if !ok {
		return HookReply{}, fmt.Errorf("ext/supervisor: CallHook on unknown hook kind %q; the kind set is closed and validateHooks refuses an unknown kind at install (ADR-0009), so reaching the gate with one is a host bug, not a plugin fault", kind)
	}
	if !s.isGranted(cap) {
		return HookReply{}, fmt.Errorf("%w: plugin %q was not granted %q, so its %q hook cannot be consulted (DESIGN-BLOCK-K1-GATE-C.md Decision 2, invariant 7); the user did not consent to this plugin seeing and vetoing the agent's turn", ErrCapabilityNotGranted, s.cfg.Manifest.ID, cap, kind)
	}
	if args == nil {
		args = map[string]string{}
	}
	id, reply := s.calls.register()
	defer s.calls.unregister(id)

	// Register the waiter BEFORE sending, so a reply cannot race ahead of the
	// waiter and be dropped as uncorrelated — the CallTool ordering rule.
	if err := s.send.send(wireAction{Type: "action", ID: id, Action: hookActionPrefix + kind, Args: args}); err != nil {
		return HookReply{}, err
	}

	timer := time.NewTimer(s.cfg.CallTimeout)
	defer timer.Stop()
	select {
	case line := <-reply:
		return decodeHookReply(line)
	case <-timer.C:
		return HookReply{}, fmt.Errorf("%w: plugin %q did not answer its %q hook within %s", ErrHookTimeout, s.cfg.Manifest.ID, kind, s.cfg.CallTimeout)
	case <-s.ctx.Done():
		return HookReply{}, fmt.Errorf("%w: supervision ended while awaiting the %q hook", ErrPluginNotLive, kind)
	}
}

// decodeHookReply turns a hook's reply line into a parsed verdict or a typed error.
// It reuses decodeReply for the `ok`/`error` split — an `error` frame becomes
// ErrToolFailed, which it rewraps as ErrHookFailed so the hook path reports under
// its own vocabulary — then parses the `ok` result's verdict token. An `ok` reply
// whose result is absent, non-JSON, or carries no legal verdict token is
// ErrHookBadReply, NOT a silent allow: a narrow-only gate treats a garbled verdict
// as the caller's cue to fail safe, never as permission (F3, ParseVerdict).
func decodeHookReply(line json.RawMessage) (HookReply, error) {
	result, err := decodeReply(line)
	if err != nil {
		// An `error` frame surfaced as ErrToolFailed; re-attribute it to the hook
		// path so the caller matches ErrHookFailed, preserving the plugin's message.
		if errors.Is(err, ErrToolFailed) {
			return HookReply{}, fmt.Errorf("%w: %s", ErrHookFailed, err.Error())
		}
		return HookReply{}, err
	}
	if len(result) == 0 {
		return HookReply{}, fmt.Errorf("%w: the `ok` reply carried no result object", ErrHookBadReply)
	}
	var r wireHookResult
	if jerr := json.Unmarshal(result, &r); jerr != nil {
		return HookReply{}, fmt.Errorf("%w: the result was not a verdict object: %v", ErrHookBadReply, jerr)
	}
	v, okv := ext.ParseVerdict(r.Verdict)
	if !okv {
		return HookReply{}, fmt.Errorf("%w: %q is not one of allow/ask/deny", ErrHookBadReply, r.Verdict)
	}
	return HookReply{Verdict: v, Reason: r.Reason}, nil
}
