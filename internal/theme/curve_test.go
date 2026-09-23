package theme

import (
	"math"
	"testing"
)

// EvalCurve is the behaviour behind a curve name (D4 / ADR-0005): the host
// clock turns elapsed/duration into a fraction and this eases it into the phase
// the renderer draws. These pin the endpoints every curve shares and the shape
// each one owns, because a curve that eased the wrong way — or clipped its
// endpoints — would make a one-shot reveal start part-drawn or overshoot, and
// the phase is a pure function the golden path depends on being deterministic.

// Every curve pins both endpoints: a run that has not started shows nothing and
// a run that has ended is fully settled, whatever the easing does between. A
// curve that failed this would make a node appear part-revealed at t=0 or never
// finish at t=1.
func TestEvalCurvePinsBothEndpointsForEveryCurve(t *testing.T) {
	for _, curve := range legalCurves {
		if got := EvalCurve(curve, 0); got != 0 {
			t.Errorf("curve %q at t=0 is %v, want 0; a one-shot animation must start from its\n"+
				"beginning frame, not part-way through", curve, got)
		}
		if got := EvalCurve(curve, 1); got != 1 {
			t.Errorf("curve %q at t=1 is %v, want 1; a one-shot animation must settle at its final\n"+
				"frame, not short of it", curve, got)
		}
	}
}

// Out-of-range fractions are clamped, so a caller that hands over a negative
// elapsed or a run past its duration gets the endpoint rather than an
// extrapolation. This is the contract that lets the clock skip clamping before
// it calls.
func TestEvalCurveClampsOutOfRange(t *testing.T) {
	for _, curve := range legalCurves {
		if got := EvalCurve(curve, -0.5); got != 0 {
			t.Errorf("curve %q below 0 is %v, want 0 (clamped); the phase may not run backward", curve, got)
		}
		if got := EvalCurve(curve, 1.5); got != 1 {
			t.Errorf("curve %q above 1 is %v, want 1 (clamped); a settled run may not overshoot", curve, got)
		}
	}
}

// linear is the identity between the endpoints: the fraction is the phase. This
// is the curve the marquee names and the baseline the eased curves are measured
// against.
func TestEvalCurveLinearIsTheIdentity(t *testing.T) {
	for _, tc := range []float64{0.25, 0.5, 0.75} {
		if got := EvalCurve("linear", tc); math.Abs(got-tc) > 1e-9 {
			t.Errorf("linear at %v is %v, want %v; linear must not ease", tc, got, tc)
		}
	}
}

// ease_in starts slower than linear and ease_out starts faster, which is the
// whole reason to name one over the other. Pinning the direction at the
// midpoint's neighbourhood catches an easing wired backwards — the mistake that
// looks like an animation "playing in reverse".
func TestEvalCurveEaseInAndOutBendOppositeWays(t *testing.T) {
	const at = 0.25
	in := EvalCurve("ease_in", at)
	out := EvalCurve("ease_out", at)
	if !(in < at) {
		t.Errorf("ease_in at %v is %v, which is not below the linear %v; ease_in must start slow", at, in, at)
	}
	if !(out > at) {
		t.Errorf("ease_out at %v is %v, which is not above the linear %v; ease_out must start fast", at, out, at)
	}
	// ease_in_out is symmetric about the midpoint: its value at t and at 1-t
	// sum to 1, so neither half dominates.
	lo := EvalCurve("ease_in_out", 0.3)
	hi := EvalCurve("ease_in_out", 0.7)
	if math.Abs((lo+hi)-1) > 1e-9 {
		t.Errorf("ease_in_out is not symmetric about 0.5: f(0.3)=%v + f(0.7)=%v = %v, want 1", lo, hi, lo+hi)
	}
}

// step shows no intermediate frame: it holds at the start for the whole run and
// only completes at the end. A mid-run value other than 0 would mean the "no
// easing, just a delay" curve was drawing a partial frame it promised not to.
func TestEvalCurveStepHoldsUntilCompletion(t *testing.T) {
	for _, tc := range []float64{0.01, 0.5, 0.99} {
		if got := EvalCurve("step", tc); got != 0 {
			t.Errorf("step at %v is %v, want 0; step draws no intermediate frame — it holds at the\n"+
				"start until the run completes", tc, got)
		}
	}
}
