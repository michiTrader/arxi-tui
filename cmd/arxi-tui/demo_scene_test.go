package main

import (
	"os"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/scene"
	"github.com/michiTrader/arxi_tui/internal/theme"
)

// The animated demo is the document the user launches to watch the motion, so
// it must be a genuinely shippable scene: it has to parse, validate, raise no
// warning, and — the part that broke silently before — name only anim and style
// tokens the shipped theme defines. In particular it opts into the "slow" anim
// token added so the reveal and transition play at reading speed; if that token
// were dropped from the theme this fails here rather than the demo quietly
// snapping back to the 200ms flash the user could not see.
func TestAnimatedDemoIsShippable(t *testing.T) {
	data, err := os.ReadFile("../../demos/animated.json")
	if err != nil {
		t.Fatalf("read demos/animated.json: %v", err)
	}
	doc, err := scene.ParseDocument(data)
	if err != nil {
		t.Fatalf("the animated demo does not parse: %v", err)
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("the animated demo does not validate: %v", err)
	}
	if w := doc.Warnings(); len(w) > 0 {
		t.Fatalf("the animated demo raises warnings, so it is not clean to ship: %v", w)
	}
	if errs := scene.ValidateTokens(doc, theme.SOBRIA()); len(errs) > 0 {
		t.Fatalf("the animated demo names a token the sobria theme does not define; the reveal/transition would not resolve their timing: %v", errs)
	}
}

// TestSlowAnimTokenIsDigestible pins the decision that the demo's reveal is slow
// enough to watch. The default reveal (200ms) is the "0 animations" the user
// reported; "slow" must be a multi-second run, or the demo is fast again.
func TestSlowAnimTokenIsDigestible(t *testing.T) {
	def, ok := theme.SOBRIA().Anim("slow")
	if !ok {
		t.Fatalf("the sobria theme defines no \"slow\" anim token; the animated demo has nothing digestible to name")
	}
	if def.DurationMS < 3000 {
		t.Errorf("the \"slow\" token runs for %dms; under ~3s the reveal is over before it can be read, which is the speed the user could not follow", def.DurationMS)
	}
}
