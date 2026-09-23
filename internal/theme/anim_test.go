package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTheme writes JSON to a temp theme file and returns its path.
func writeTheme(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("failed to write test theme: %v", err)
	}
	return path
}

// TestLoadThemeWithAnimSectionParsesTimingTokensSeparately proves the two
// halves D4 signs: the `anim` section is read as timing tokens (not style
// tokens), and it shares no namespace with the style map — a name may appear in
// both without collision, and neither leaks into the other.
func TestLoadThemeWithAnimSectionParsesTimingTokensSeparately(t *testing.T) {
	path := writeTheme(t, `{
		"dim": { "attrs": ["dim"] },
		"anim": {
			"default":     { "duration_ms": 200, "curve": "ease_out", "fps": 30 },
			"marquee":     { "duration_ms": 0,   "curve": "linear",   "fps": 20 },
			"reveal.fast": { "duration_ms": 120, "curve": "ease_out" }
		}
	}`)

	th, err := Load(path)
	if err != nil {
		t.Fatalf("a theme with a valid anim section must load, and it did not: %v\nConsequence: D4's timing tokens cannot be defined, so no animation prop can name one.\nRemedy: pull `anim` out before parsing style tokens and validate each definition.", err)
	}

	def, ok := th.Anim("default")
	if !ok {
		t.Fatalf("the `default` timing token did not load.\nConsequence: every animated prop falls back to anim.default, so a missing default breaks all of them.\nRemedy: parse the anim section into the theme.")
	}
	if def.DurationMS != 200 || def.Curve != "ease_out" || def.FPS != 30 {
		t.Errorf("the `default` timing token parsed wrong.\n  got: %+v\n  want: {200 ease_out 30}\nConsequence: the render layer would drive the animation at the wrong duration/curve/rate.\nRemedy: map duration_ms/curve/fps straight through.", def)
	}

	// duration_ms 0 is continuous, not "unset" — the marquee case, signed.
	if m, _ := th.Anim("marquee"); m.DurationMS != 0 || m.Curve != "linear" {
		t.Errorf("the `marquee` timing token parsed wrong.\n  got: %+v\n  want duration_ms 0 (continuous) and curve linear\nConsequence: 0 must mean continuous, not defaulted, or the marquee stops.\nRemedy: keep DurationMS an int with 0 meaning continuous.", m)
	}

	// fps is optional: an omitted fps is 0, meaning the render layer substitutes
	// the host default. That must not be a load error.
	if rf, ok := th.Anim("reveal.fast"); !ok || rf.FPS != 0 {
		t.Errorf("`reveal.fast` omitted fps and must load with FPS 0 (host default).\n  got: %+v ok=%v\nConsequence: a valid theme is refused for leaving fps to the host.\nRemedy: fps is optional; 0 means unset.", rf, ok)
	}

	// A style token is not an anim token and vice versa: the namespaces are
	// separate, which is the whole reason anim is its own section.
	if !th.Has("dim") {
		t.Error("the style token `dim` was lost when the anim section was lifted out.\nConsequence: pulling `anim` deleted an unrelated token.\nRemedy: delete only the `anim` key before reading the rest as style tokens.")
	}
	if th.HasAnim("dim") {
		t.Error("the style token `dim` leaked into the anim namespace.\nConsequence: the two namespaces are not separate, which is the collision D4's separate section exists to prevent.\nRemedy: only the anim section populates the anim map.")
	}
	if th.Has("default") {
		t.Error("the timing token `default` leaked into the style namespace.\nConsequence: a timing name resolves as a style, so the emitter would paint with it.\nRemedy: the anim section never populates the style map.")
	}
}

// TestLoadThemeRefusesACurveOutsideTheClosedSet holds the closed-curve rule: a
// curve is an easing function in the binary, so a theme may name one but never
// define one. The refusal lists the legal set and says "choose", not "define".
func TestLoadThemeRefusesACurveOutsideTheClosedSet(t *testing.T) {
	path := writeTheme(t, `{
		"anim": { "bad": { "duration_ms": 100, "curve": "bouncy" } }
	}`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("a theme naming a curve outside the closed set must be refused; it loaded.\nConsequence: an unknown curve has no easing function behind it, so the prop would animate with nothing — the accepted-but-not-drawn class, in the theme this time.\nRemedy: reject a curve not in the closed set at load.")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bad") || !strings.Contains(msg, "bouncy") {
		t.Errorf("the refusal must name the offending token and its bad curve.\n  got: %v\nRemedy: include the token name and the rejected curve.", err)
	}
	for _, curve := range legalCurves {
		if !strings.Contains(msg, curve) {
			t.Errorf("the refusal must list the legal curve %q so the author can choose one.\n  got: %v\nRemedy: the remedy for a curve is 'choose a supported one', not 'define it'.", curve, err)
		}
	}
}

// TestLoadThemeRefusesNegativeDurationAndFPS pins the other theme-load rule: a
// run cannot end before it begins and a tick rate cannot be negative. Silently
// clamping either would hide the author's mistake, which is why it is a refusal.
func TestLoadThemeRefusesNegativeDurationAndFPS(t *testing.T) {
	cases := map[string]string{
		"negative duration_ms": `{ "anim": { "t": { "duration_ms": -1, "curve": "linear" } } }`,
		"negative fps":         `{ "anim": { "t": { "duration_ms": 100, "curve": "linear", "fps": -5 } } }`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeTheme(t, body))
			if err == nil {
				t.Fatalf("a theme with a %s must be refused; it loaded.\nConsequence: a negative timing value is a mistake, and clamping it silently draws something the author did not ask for.\nRemedy: refuse a negative duration_ms or fps at load, naming the token.", name)
			}
			if !strings.Contains(err.Error(), "t") {
				t.Errorf("the refusal must name the offending token.\n  got: %v", err)
			}
		})
	}
}

// TestLoadThemeRefusesAnAnimTokenWithNoCurve holds the missing-curve case: a
// timing token with a duration but no curve is incomplete, not defaulted — the
// curve is the behaviour and the theme must name one.
func TestLoadThemeRefusesAnAnimTokenWithNoCurve(t *testing.T) {
	_, err := Load(writeTheme(t, `{ "anim": { "t": { "duration_ms": 100 } } }`))
	if err == nil {
		t.Fatal("a timing token with no curve must be refused; it loaded.\nConsequence: a token with no easing has no behaviour, and picking one for the author invents the animation.\nRemedy: require a curve from the closed set.")
	}
}

// TestAThemeWithoutAnAnimSectionStillLoads guards the common case: the shipped
// factory themes carry no anim section today, and a theme that defines only
// style tokens must load with an empty anim map and no error.
func TestAThemeWithoutAnAnimSectionStillLoads(t *testing.T) {
	th, err := Load(writeTheme(t, `{ "dim": { "attrs": ["dim"] } }`))
	if err != nil {
		t.Fatalf("a theme with no anim section must load: %v\nConsequence: every existing theme file breaks the moment the anim parser is added.\nRemedy: treat a missing anim section as an empty timing map.", err)
	}
	if _, ok := th.Anim("default"); ok {
		t.Error("a theme with no anim section reported a `default` timing token.\nConsequence: the render layer would find a token the theme never wrote.\nRemedy: a missing anim section is the empty map, not synthesised defaults.")
	}
	if len(th.AnimNames()) != 0 {
		t.Errorf("a theme with no anim section must have no anim names; got %v.", th.AnimNames())
	}
}
