package theme

import "fmt"

// EvalCurve maps a fraction of a run t (in [0,1]) to the eased phase the render
// layer draws at, for the named curve. It is the "easing function that is code
// in the mother binary" the AnimDef comment names: the theme picks a curve name
// from the closed set, and this is the behaviour behind each name. The host
// clock (ADR-0005) owns wall time and calls this to turn elapsed/duration into
// the phase the renderer consumes, so the curve is applied once, in one place,
// rather than at each render site.
//
// t is clamped to [0,1]: a negative elapsed cannot happen and a run past its
// duration is settled at 1, so a caller need not clamp before calling. An
// unknown curve falls back to linear rather than panicking — validateAnimDef
// already refuses an unknown curve at theme load with the legal set listed, so
// reaching here with one is a caller that skipped that gate, and a linear ramp
// is the least surprising thing to draw while the real fix is the load-time
// refusal. This is deliberately not a second refusal: a curve name is validated
// where a theme is read, not on the hot path a clock ticks.
func EvalCurve(name string, t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	switch name {
	case "ease_in":
		// Quadratic ease-in: slow to start, accelerating. Standard t².
		return t * t
	case "ease_out":
		// Quadratic ease-out: fast to start, decelerating. The reflection of
		// ease_in, 1-(1-t)².
		return t * (2 - t)
	case "ease_in_out":
		// Quadratic in the first half, its reflection in the second, meeting
		// at (0.5, 0.5) so the two halves are continuous.
		if t < 0.5 {
			return 2 * t * t
		}
		u := 1 - t
		return 1 - 2*u*u
	case "step":
		// No intermediate frames: the run holds at its start until it
		// completes, then snaps to the end. The endpoints are already handled
		// above (t<=0 → 0, t>=1 → 1), so a t strictly inside the run is still
		// at the start — the snap happens at completion, not partway.
		return 0
	case "linear":
		return t
	default:
		return t
	}
}

// AnimDef is one timing token: a named duration-and-curve the render/emit layer
// consumes to drive an animation prop (transition/scroll/reveal/enter). It is
// the `[anim]` vocabulary docs/TOKENS.md signs (D4). It lives in the theme, in a
// section separate from style tokens, so a timing name can never collide with a
// style-token name.
//
// A timing token is pure data here — a duration, a named curve, a tick rate.
// The curve *name* selects an easing function that is code in the mother binary
// (see curveSet); the theme names one, it does not supply one. That asymmetry
// is the whole reason the curve set is closed while the style-token namespace is
// open: a style token is data the emitter interprets, a curve is behaviour, and
// "bring your own curve" is "bring your own render code into the mother
// renderer", which the plugin boundary (Scene 6) and the wasm ADR (Q14) forbid.
type AnimDef struct {
	// DurationMS is how long one pass runs. 0 means continuous — no end, driven
	// at FPS — which is the marquee case. It is never negative; a negative
	// duration is a theme-load error, because a run cannot end before it starts
	// and silently clamping it would hide the author's mistake.
	DurationMS int `json:"duration_ms"`
	// Curve is the easing applied over the run, a name from the closed set. An
	// unknown curve is refused with the legal set listed, and the remedy is
	// "choose a supported curve" rather than "define it": the theme cannot
	// supply an easing function, only name one the binary already has.
	Curve string `json:"curve"`
	// FPS is the tick rate. Optional: 0 means "unset", and the render layer
	// substitutes a host constant. A negative FPS is a theme-load error for the
	// same reason a negative duration is.
	FPS int `json:"fps,omitempty"`
}

// curveSet is the closed set of easing curves the binary implements. It is a
// set rather than a slice because membership is the only question asked of it
// at load time; the ordered spelling for error messages is legalCurves below,
// kept beside it so the two cannot drift.
//
// Adding a curve is a mother-binary change with its own freeze, like adding a
// node type: the name here must be matched by an easing function wherever the
// render layer maps a curve name to behaviour, and a name accepted at load with
// no implementation behind it would be the accepted-but-not-drawn class one
// more time.
var curveSet = map[string]bool{
	"linear":      true,
	"ease_in":     true,
	"ease_out":    true,
	"ease_in_out": true,
	"step":        true,
}

// legalCurves is curveSet in the signed order, for the refusal message. TOKENS.md
// lists them in this order and the error quotes it verbatim so the message and
// the document agree.
var legalCurves = []string{"linear", "ease_in", "ease_out", "ease_in_out", "step"}

// validateAnimDef checks one timing definition against the signed rules: the
// curve must be in the closed set, and neither duration_ms nor fps may be
// negative. The error names the offending key and, for a bad curve, lists the
// legal set — the remedy differs from a style token's on purpose (choose one,
// do not define one), because a curve is code the theme cannot provide.
//
// name is the token's name in the anim section, threaded in so a theme with
// several timing tokens says which one is wrong rather than just that one is.
func validateAnimDef(name string, def AnimDef) error {
	if def.Curve == "" {
		return fmt.Errorf("anim token %q declares no curve; a timing token needs one of the closed curve set %v", name, legalCurves)
	}
	if !curveSet[def.Curve] {
		return fmt.Errorf("anim token %q names curve %q, which is not in the closed curve set; choose a supported curve (one of %v) — a curve is an easing function in the binary, not data the theme can define", name, def.Curve, legalCurves)
	}
	if def.DurationMS < 0 {
		return fmt.Errorf("anim token %q has negative duration_ms %d; a run cannot end before it begins (0 means continuous)", name, def.DurationMS)
	}
	if def.FPS < 0 {
		return fmt.Errorf("anim token %q has negative fps %d; the tick rate cannot be negative (omit it to take the host default)", name, def.FPS)
	}
	return nil
}
