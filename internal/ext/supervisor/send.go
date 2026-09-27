package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
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
)

// capActionsRegister is the capability an ext: press requires (DESIGN-BLOCK-H,
// the closed capability set; §I-E). It is named once here so the gate and its
// refusal message cannot drift from the manifest vocabulary.
const capActionsRegister = "actions.register"

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
