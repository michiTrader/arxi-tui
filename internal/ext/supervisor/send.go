package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// This file is the host→plugin half of the wire: routing an `on_press`
// ext:<plugin-id>:<action> into the running subprocess as an `action` frame
// (I4, DESIGN-BLOCK-I §I-E, ADR-0007). I2 owns spawn/handshake/read; I3 owns the
// plugin→host `bind` frames; this is the one place the host speaks to the plugin
// after the ack.

var (
	// ErrPluginNotLive is a SendAction to a plugin whose child is not currently
	// accepting frames — it has died, is between restarts in backoff, or has not
	// yet completed the handshake. It is NOT a crash and NOT fatal to the
	// supervisor: a press that arrives while the plugin is down is reported to
	// the user (the placeholder-not-crash rule, §I-G), and the plugin may come
	// back on a later restart.
	ErrPluginNotLive = errors.New("plugin is not live")
	// ErrCapabilityNotGranted is a SendAction the plugin's granted capability set
	// does not permit. Routing an action requires actions.register, the wire face
	// of "power granted at the gate, once" (invariant 7): a plugin the user did
	// not grant that capability cannot be sent actions even if a scene names one.
	ErrCapabilityNotGranted = errors.New("capability not granted")
	// ErrToolTimeout is a CallTool whose plugin read the request but did not
	// reply within CallTimeout (§I-J Decision 4). It is kept DISTINCT from
	// ErrToolFailed because the agent-facing layer reports them apart: a timeout
	// is "the plugin went quiet, retrying may help," while a failure is "the
	// plugin answered that it could not," and collapsing the two would tell the
	// agent a quiet plugin had refused.
	ErrToolTimeout = errors.New("tool call timed out")
	// ErrToolFailed is a CallTool the plugin answered with an `error` frame
	// rather than a result. The plugin ran and reported it could not satisfy the
	// call; the wrapped message carries the plugin's own code/message so the
	// agent sees why, and it is distinct from a timeout for the reason above.
	ErrToolFailed = errors.New("tool call failed")
)

// capActionsRegister is the capability an ext: press requires (DESIGN-BLOCK-H,
// the closed capability set; §I-E). It is named once here so the gate and its
// refusal message cannot drift from the manifest vocabulary.
const capActionsRegister = "actions.register"

// capToolsRegister is the capability a CallTool requires (§I-J Decision 3): the
// wire face of "your agent may call this plugin on its own." It is a distinct
// power from actions.register — a tool call has no user in the loop at call
// time — so it gates the agent-call path separately, and is spelled the same as
// the ext package's closed-set entry (consent.go capToolsRegister).
const capToolsRegister = "tools.register"

// wireAction is the host→plugin `action` line (§I-E). `id` correlates the frame
// with a later plugin response; `action` is the <action> segment of
// ext:<plugin-id>:<action>; `args` carries the {row.field} values ALREADY
// resolved by the host (the plugin receives concrete values, never scene syntax).
// args is always a non-nil object on the wire — an empty map, not null — so a
// plugin reading it need not distinguish "no args" from "field missing".
type wireAction struct {
	Type   string            `json:"type"`
	ID     string            `json:"id"`
	Action string            `json:"action"`
	Args   map[string]string `json:"args"`
}

// sender holds the currently-live child's stdin encoder. It is set after the
// handshake acks (a plugin cannot receive an action before it knows its grants)
// and cleared when the child dies, both under senderMu, so a SendAction racing a
// restart sees a nil sender and reports ErrPluginNotLive rather than writing to a
// dead pipe.
type sender struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (s *sender) set(enc *json.Encoder) {
	s.mu.Lock()
	s.enc = enc
	s.mu.Unlock()
}

func (s *sender) clear() {
	s.mu.Lock()
	s.enc = nil
	s.mu.Unlock()
}

// send encodes one frame to the live child, holding the lock across the write so
// two SendAction calls cannot interleave their bytes on the pipe. A nil encoder
// (no live child) is ErrPluginNotLive; a write error (the pipe closed under us as
// the child died) is surfaced so the caller reports it rather than assuming the
// action landed.
func (s *sender) send(frame wireAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enc == nil {
		return ErrPluginNotLive
	}
	if err := s.enc.Encode(frame); err != nil {
		return fmt.Errorf("%w: writing action frame: %v", ErrPluginNotLive, err)
	}
	return nil
}

// SendAction routes a press to the plugin as an `action` frame (§I-E). It is the
// concrete "route an on_press ext: action into the subprocess" verb, the I4
// analogue of I3's DrainInto: the host resolves the action name and args and this
// puts them on the wire, gated on the granted capability.
//
// The capability gate is checked first, before touching the child, so a plugin
// that was never granted actions.register is refused identically whether or not
// it happens to be live — the grant is the boundary, not the process state. args
// is normalised to a non-nil map so the wire frame always carries an args object.
// The plugin still only proposes: it responds by publishing new bind values
// (I-C), so SendAction returns once the frame is written and does not wait on a
// reply (the id-correlated ok/error ack is a §I-E refinement, noted where the
// reader forwards non-bind frames).
func (s *Supervisor) SendAction(actionID, action string, args map[string]string) error {
	if !s.isGranted(capActionsRegister) {
		return fmt.Errorf("%w: plugin %q was not granted %q, so an ext: press cannot be routed to it (DESIGN-BLOCK-I §I-E, invariant 7); the user did not consent to this plugin registering actions", ErrCapabilityNotGranted, s.cfg.Manifest.ID, capActionsRegister)
	}
	if args == nil {
		args = map[string]string{}
	}
	return s.send.send(wireAction{Type: "action", ID: actionID, Action: action, Args: args})
}

// isGranted reports whether the plugin was granted a capability at the I5 gate.
// It reads cfg.Granted (the consented subset carried to the child in the ack), so
// the host's send gate and the plugin's own knowledge of its powers come from the
// one source the handshake communicated.
func (s *Supervisor) isGranted(capability string) bool {
	for _, g := range s.cfg.Granted {
		if g == capability {
			return true
		}
	}
	return false
}

// wireReply is a plugin→host tool reply (§I-J Decision 4): the id-correlated
// `{type:ok, id, result}` or `{type:error, id, error{code,message}}` the plugin
// answers a tool call with. This is where the §I-E `ok`/error ack graduates from
// "so a button can show it was accepted" to load-bearing: a button ignored the
// reply; a tool call is the reply. Result stays raw because it is the agent's
// value to reason over, not the host's to interpret.
type wireReply struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *wireError      `json:"error,omitempty"`
}

// wireError is the closed-set refusal shape a plugin returns for a failed tool
// call (§I-A frame vocabulary). Its fields are surfaced in ErrToolFailed so the
// agent sees the plugin's own diagnosis, not a generic "it failed."
type wireError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// pendingCalls correlates each in-flight CallTool with the reply the reader will
// route to it, keyed by the host-owned correlation id. A tool call registers a
// buffered channel before the frame is sent and unregisters it when the call
// returns; the reader delivers exactly one reply to that channel and never
// blocks on it. The host owns every id, so no plugin can invent one that lands
// on a call it was not answering.
type pendingCalls struct {
	mu     sync.Mutex
	byID   map[string]chan json.RawMessage
	nextID uint64
}

// register reserves a correlation id and its one-slot reply channel. The channel
// is buffered so deliver never blocks even if the CallTool has already timed out
// and stopped selecting on it; the id carries a `t` prefix so a tool correlation
// id is visually distinct from SendAction's `a` ids in a frame dump, though they
// never share the pending map (SendAction registers no waiter).
func (p *pendingCalls) register() (string, chan json.RawMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byID == nil {
		p.byID = map[string]chan json.RawMessage{}
	}
	p.nextID++
	id := "t" + strconv.FormatUint(p.nextID, 10)
	ch := make(chan json.RawMessage, 1)
	p.byID[id] = ch
	return id, ch
}

// unregister drops a call's waiter. It is deferred by CallTool so a reply that
// arrives after a timeout finds no waiter and is forwarded verbatim rather than
// delivered to a caller that has gone.
func (p *pendingCalls) unregister(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.byID, id)
}

// deliver routes a reply line to the call awaiting id, reporting whether one was
// waiting. A non-blocking send is deliberate: the channel is buffered at one and
// only one reply is ever delivered per id, so a second `ok` for the same id (a
// misbehaving plugin) is dropped rather than blocking the reader goroutine that
// keeps the whole wire alive.
func (p *pendingCalls) deliver(id, line string) bool {
	if id == "" {
		return false
	}
	p.mu.Lock()
	ch, ok := p.byID[id]
	p.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- json.RawMessage(append([]byte(nil), line...)):
	default:
	}
	return true
}

// CallTool is the agent-call verb (§I-J Decision 4): the await-a-reply sibling of
// the fire-and-forget SendAction. Where SendAction routes a user press and
// returns once the frame is written, CallTool writes the id-correlated `action`
// frame and then WAITS for the plugin's `ok`/`error` reply, because the agent
// called the tool to get a value it will reason over in the same turn — a value
// dribbling into a bind three frames later (the I4 model) is useless to it.
//
// It reuses the `action` channel rather than inventing a plugin-facing frame:
// the host already owns this frame and this correlation id, so the tool door is
// "the action channel plus the wait." The only new plugin obligation is the
// reply, which §I-E already drafted.
//
// The capability gate is checked first, before the child is touched, so a plugin
// never granted tools.register is refused identically whether or not it is live —
// the grant is the boundary, not the process state (invariant 7). The wait is
// bounded three ways: the plugin's reply, CallTimeout (ErrToolTimeout), and the
// supervisor's own ctx being cancelled by Close (ErrPluginNotLive). It never
// waits on the UI loop, so a hung plugin cannot freeze the interface or the panic
// gesture (invariant 6) — the same rule startInstall follows.
//
// A timeout and an `error` reply map to DISTINCT Go errors (ErrToolTimeout vs
// ErrToolFailed) so the agent-facing layer, when Block M lands, can tell "the
// plugin went quiet" from "the plugin answered that it could not."
func (s *Supervisor) CallTool(tool string, args map[string]string) (json.RawMessage, error) {
	if !s.isGranted(capToolsRegister) {
		return nil, fmt.Errorf("%w: plugin %q was not granted %q, so the agent cannot call its tools (DESIGN-BLOCK-I §I-J, invariant 7); the user did not consent to this plugin exposing agent-callable tools", ErrCapabilityNotGranted, s.cfg.Manifest.ID, capToolsRegister)
	}
	if args == nil {
		args = map[string]string{}
	}
	id, reply := s.calls.register()
	defer s.calls.unregister(id)

	// Register the waiter BEFORE sending, so a reply cannot race in ahead of the
	// waiter and be dropped as uncorrelated. If the write fails (no live child),
	// the deferred unregister cleans up and the caller sees the send error.
	if err := s.send.send(wireAction{Type: "action", ID: id, Action: tool, Args: args}); err != nil {
		return nil, err
	}

	timer := time.NewTimer(s.cfg.CallTimeout)
	defer timer.Stop()
	select {
	case line := <-reply:
		return decodeReply(line)
	case <-timer.C:
		return nil, fmt.Errorf("%w: plugin %q did not reply to tool %q within %s", ErrToolTimeout, s.cfg.Manifest.ID, tool, s.cfg.CallTimeout)
	case <-s.ctx.Done():
		// Close cancelled supervision while the call was in flight: the plugin is
		// gone, reported the same way a press against a dead plugin is (§I-G).
		return nil, fmt.Errorf("%w: supervision ended while awaiting tool %q", ErrPluginNotLive, tool)
	}
}

// decodeReply turns a plugin's reply line into a result or a typed error. An
// `ok` frame yields its raw result (an absent result is a valid empty answer,
// not an error — the plugin ran and had nothing to return). An `error` frame
// yields ErrToolFailed carrying the plugin's own code and message. Any other
// type on a correlated reply is itself a protocol error: the reader only routes
// `ok`/`error` here, so a third type would be a bug in the router, reported
// rather than silently treated as success.
func decodeReply(line json.RawMessage) (json.RawMessage, error) {
	var r wireReply
	if err := json.Unmarshal(line, &r); err != nil {
		return nil, fmt.Errorf("%w: plugin reply is not JSON: %v", ErrToolFailed, err)
	}
	switch r.Type {
	case "ok":
		return r.Result, nil
	case "error":
		if r.Error != nil && (r.Error.Code != "" || r.Error.Message != "") {
			return nil, fmt.Errorf("%w: plugin reported [%s] %s", ErrToolFailed, r.Error.Code, r.Error.Message)
		}
		return nil, fmt.Errorf("%w: plugin returned an error frame with no diagnosis", ErrToolFailed)
	default:
		return nil, fmt.Errorf("%w: correlated reply had unexpected type %q", ErrToolFailed, r.Type)
	}
}
