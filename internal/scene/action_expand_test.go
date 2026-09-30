package scene

import (
	"strings"
	"testing"
)

// ExpandRowInterpolation is the press-time substitution the validator's
// interpolationTokens/validateOnPress check at load (Q20). These tests pin the
// substitution the way action_test.go pins the parse: the two halves must agree
// on what a {row.<field>} token is, because a field the validator accepts and
// the expander cannot resolve is the scope/schema drift this function refuses.

// Every {row.<field>} token in the argument is replaced by its scope value, and
// more than one token in one argument is fully expanded. A regression that
// stopped after the first token would send cmd:/agent m-1 {row.role} to the
// core -- a half-substituted line naming a run correctly but carrying an
// unexpanded brace the core never asked for.
func TestExpandRowInterpolationSubstitutesEveryRowField(t *testing.T) {
	row := map[string]string{"row.id": "m-1", "row.role": "reviewer"}
	got, err := ExpandRowInterpolation("/agent {row.id} {row.role}", row)
	if err != nil {
		t.Fatalf("ExpandRowInterpolation refused a fully-declared argument: %v\n"+
			"consequence: a template row whose fields are all in scope cannot be pressed at all.\n"+
			"remedy: substitute each {row.<field>} present in the scope.", err)
	}
	if want := "/agent m-1 reviewer"; got != want {
		t.Fatalf("ExpandRowInterpolation = %q, want %q: every {row.<field>} must be replaced "+
			"by its scope value, or the dispatched line addresses the wrong element", got, want)
	}
}

// Text on either side of a token is preserved: the substitution edits only the
// braces, so the command the row dispatches is the one the author wrote with the
// field spliced in. A regression that rebuilt the string from tokens alone would
// drop the "/agent " prefix and dispatch a bare id as a command.
func TestExpandRowInterpolationPreservesTextAroundTokens(t *testing.T) {
	row := map[string]string{"row.id": "m-1"}
	got, err := ExpandRowInterpolation("/agent {row.id} --now", row)
	if err != nil {
		t.Fatalf("ExpandRowInterpolation refused a declared argument: %v", err)
	}
	if want := "/agent m-1 --now"; got != want {
		t.Fatalf("ExpandRowInterpolation = %q, want %q: text around a token must survive verbatim", got, want)
	}
}

// A {row.<field>} the scope does not carry is an error, and the returned string
// is empty rather than a half-substituted argument. This is the load-bearing
// guard: the validator has already refused any field the element schema does not
// declare, so a miss here is a scope/schema drift inside the host, and expanding
// it to "" would address the wrong run (or none) silently -- the wrong frame
// this project holds to be worse than a loud refusal.
func TestExpandRowInterpolationMissingFieldIsAnError(t *testing.T) {
	row := map[string]string{"row.id": "m-1"} // row.role deliberately absent
	got, err := ExpandRowInterpolation("/agent {row.id} {row.role}", row)
	if err == nil {
		t.Fatalf("ExpandRowInterpolation expanded {row.role} against a scope that lacks it and returned %q with no error\n"+
			"consequence: a field the scope builder forgot is expanded to the empty string, so cmd:/agent addresses a run\n"+
			"the operator never chose, and the drift between the row schema and the scope is invisible.\n"+
			"remedy: return an error naming the missing field, the way a validated {row.<field>} must resolve.", got)
	}
	if got != "" {
		t.Fatalf("ExpandRowInterpolation returned %q alongside its error; a refused expansion must yield no "+
			"argument, so a caller that ignores the error cannot dispatch a half-substituted line", got)
	}
	if !strings.Contains(err.Error(), "row.role") {
		t.Fatalf("ExpandRowInterpolation error %q does not name the missing field; the message must say which "+
			"{row.<field>} drifted so the scope builder can be fixed", err)
	}
}

// A {...} token that is not a row.* reference is left verbatim: interpolationTokens
// ignores it at load (it belongs to a later host resolver), so this pass must not
// consume it either. A regression that stripped or blanked non-row braces would
// edit a string the author wrote for a downstream resolver, and the two passes
// would disagree on which braces are theirs.
func TestExpandRowInterpolationLeavesNonRowTokensVerbatim(t *testing.T) {
	row := map[string]string{"row.id": "m-1"}
	got, err := ExpandRowInterpolation("/agent {row.id} {plugin.foo}", row)
	if err != nil {
		t.Fatalf("ExpandRowInterpolation refused an argument over a non-row token: %v; a {...} that is not "+
			"row.* is not this pass's to resolve, so it must not error on one", err)
	}
	if want := "/agent m-1 {plugin.foo}"; got != want {
		t.Fatalf("ExpandRowInterpolation = %q, want %q: a non-row {...} token must survive untouched for the "+
			"resolver it belongs to", got, want)
	}
}

// An argument with no interpolation is returned unchanged, and a nil scope is
// legal for one: a plain cmd:/max chat button carries no row and must not need a
// scope to dispatch. This is the common case (most on_press values are literal),
// so a regression that required a non-nil map would break every non-template
// button.
func TestExpandRowInterpolationNoTokensIsIdentity(t *testing.T) {
	got, err := ExpandRowInterpolation("/max chat", nil)
	if err != nil {
		t.Fatalf("ExpandRowInterpolation refused a literal argument with a nil scope: %v; a button with no "+
			"{row.<field>} needs no row to be pressed", err)
	}
	if want := "/max chat"; got != want {
		t.Fatalf("ExpandRowInterpolation = %q, want %q: an argument with no token must pass through unchanged", got, want)
	}
}

// An unterminated '{' is not a token: interpolationTokens stops at it and treats
// the tail as literal, so the expander writes the brace and the remainder back
// verbatim rather than erroring or inventing a field name. The two must agree on
// where a token ends, or a malformed author string is a load-time pass and a
// press-time failure.
func TestExpandRowInterpolationUnterminatedBraceIsVerbatim(t *testing.T) {
	got, err := ExpandRowInterpolation("/agent {row.id", map[string]string{"row.id": "m-1"})
	if err != nil {
		t.Fatalf("ExpandRowInterpolation errored on an unterminated brace: %v; interpolationTokens treats it "+
			"as literal, so the expander must too", err)
	}
	if want := "/agent {row.id"; got != want {
		t.Fatalf("ExpandRowInterpolation = %q, want %q: an unterminated '{' is not an interpolation and must "+
			"survive verbatim, matching interpolationTokens", got, want)
	}
}
