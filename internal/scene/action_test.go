package scene

import (
	"errors"
	"strings"
	"testing"
)

// The action grammar is the closed vocabulary H8 signed (BINDS.md §4.8). These
// tests pin what ParseAction accepts and, through validateOnPress, what a
// document is refused for — the two must agree because they are the same reader.

// The three live prefixes each parse to their kind with the argument kept
// verbatim (interpolation is the host's job at press time, so the arg is not
// expanded here). A regression that dropped the argument would make every
// cmd:/focus:/answer: dispatch a no-op with no error.
func TestParseActionAcceptsTheClosedPrefixes(t *testing.T) {
	cases := []struct {
		in       string
		wantKind ActionKind
		wantArg  string
	}{
		{"cmd:/agent 5", ActionCmd, "/agent 5"},
		{"cmd:/max chat", ActionCmd, "/max chat"},
		{"focus:reject", ActionFocus, "reject"},
		{"answer:approve", ActionAnswer, "approve"},
		{"answer:reject", ActionAnswer, "reject"},
		{"answer:reply", ActionAnswer, "reply"},
	}
	for _, tc := range cases {
		got, err := ParseAction(tc.in)
		if err != nil {
			t.Errorf("ParseAction(%q) refused a well-formed action: %v\n"+
				"consequence: a scene author who wrote a legal action gets a load-time refusal, so a\n"+
				"button that should work cannot be authored at all.\n"+
				"remedy: accept cmd:/focus:/answer: with a non-empty argument (BINDS.md §4.8).", tc.in, err)
			continue
		}
		if got.Kind != tc.wantKind {
			t.Errorf("ParseAction(%q).Kind = %d, want %d: the host would route the press to the wrong arm", tc.in, got.Kind, tc.wantKind)
		}
		if got.Arg != tc.wantArg {
			t.Errorf("ParseAction(%q).Arg = %q, want %q: the argument is the whole content of the action, and a dropped one makes the dispatch a no-op", tc.in, got.Arg, tc.wantArg)
		}
	}
}

// Every malformed action is refused, each for the reason a repair loop can act
// on. A prefix outside the set, an empty argument, an out-of-vocabulary answer
// kind, and the reserved ext: arm are the four ways an action can be wrong.
func TestParseActionRefusesMalformedActions(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		mustSay string
	}{
		{"no prefix at all", "justtext", "no action prefix"},
		{"unknown prefix", "run:/help", "unknown action prefix"},
		{"empty cmd argument", "cmd:", "no command"},
		{"empty focus argument", "focus:", "no node id"},
		{"empty answer argument", "answer:", "no kind"},
		{"answer kind outside the set", "answer:maybe", "closed answer vocabulary"},
		{"ext is deferred to Block I", "ext:tick:refresh", "Block I"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAction(tc.in)
			if err == nil {
				t.Fatalf("ParseAction(%q) accepted a malformed action.\n"+
					"consequence: the action vocabulary is not closed, so a mistyped or unsupported\n"+
					"action loads and the host silently does nothing with it — the silent drop.\n"+
					"remedy: refuse any prefix outside cmd:/focus:/answer:, an empty argument, an\n"+
					"answer kind outside the closed set, and the reserved ext: arm (BINDS.md §4.8).", tc.in)
			}
			if !strings.Contains(err.Error(), tc.mustSay) {
				t.Errorf("ParseAction(%q) refused, but the message %q does not say %q, so the author\n"+
					"cannot tell what to fix", tc.in, err.Error(), tc.mustSay)
			}
		})
	}
}

// validateOnPress refuses a malformed action with an address, the same net every
// other field gets. This is the load-time face of ParseAction: a document with a
// bad on_press must not validate clean.
func TestValidateOnPressRefusesWithAnAddress(t *testing.T) {
	body := `{"root":{"type":"box","children":[
	  {"id":"go","type":"text","text":"Go","on_press":"run:/nope"}
	]}}`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	verr := doc.Validate()
	if verr == nil {
		t.Fatal("an on_press naming an unknown prefix validated clean; a mistyped action would load and dispatch nothing")
	}
	var se *Error
	if !errors.As(verr, &se) {
		t.Fatalf("the refusal is not a *scene.Error, so it carries no address: %v", verr)
	}
	if se.Loc == (Loc{}) {
		t.Errorf("the on_press refusal carries no address, so the repair loop cannot locate it: %v", verr)
	}
	if !strings.Contains(verr.Error(), "on_press") {
		t.Errorf("the refusal does not name on_press: %q", verr.Error())
	}
}

// A well-formed on_press validates clean on an ordinary node — the positive half,
// so the refusal test above cannot pass by refusing everything.
func TestValidateOnPressAcceptsAWellFormedAction(t *testing.T) {
	body := `{"root":{"type":"text","text":"Help","on_press":"cmd:/help"}}`
	doc, err := ParseDocument([]byte(body))
	if err != nil {
		t.Fatalf("premise broken: %v", err)
	}
	if verr := doc.Validate(); verr != nil {
		t.Errorf("a well-formed on_press was refused: %v\n"+
			"consequence: H8 signed cmd:/help as a legal action, so a button using it must load.\n"+
			"remedy: validateOnPress must accept a cmd: action with a command line.", verr)
	}
}

// A {row.<field>} interpolation inside an on_press argument is validated against
// the enclosing template's row schema (Q20): a known field is accepted, an
// unknown one is refused, and one outside any template is refused — the same net
// a bare row.* bind gets, because interpolation resolves against the same rows.
func TestValidateOnPressChecksRowInterpolation(t *testing.T) {
	// agent.todos rows have a `task` field but no `state` field (BINDS.md §4.7).
	good := `{"root":{"type":"list","bind":"agent.todos",
	  "row_template":{"type":"text","bind":"row.task","on_press":"cmd:/agent {row.task}"}}}`
	bad := `{"root":{"type":"list","bind":"agent.todos",
	  "row_template":{"type":"text","bind":"row.task","on_press":"cmd:/agent {row.state}"}}}`
	outside := `{"root":{"type":"text","text":"x","on_press":"cmd:/agent {row.task}"}}`

	if doc, err := ParseDocument([]byte(good)); err != nil {
		t.Fatalf("premise broken: %v", err)
	} else if verr := doc.Validate(); verr != nil {
		t.Errorf("a {row.task} interpolation over agent.todos was refused: %v\n"+
			"remedy: an interpolation naming a field in the row schema must be accepted (Q20).", verr)
	}

	if doc, err := ParseDocument([]byte(bad)); err != nil {
		t.Fatalf("premise broken: %v", err)
	} else if verr := doc.Validate(); verr == nil {
		t.Error("a {row.state} interpolation over agent.todos validated clean; agent.todos rows have no state field, so the interpolation would silently expand to a placeholder")
	}

	if doc, err := ParseDocument([]byte(outside)); err != nil {
		t.Fatalf("premise broken: %v", err)
	} else if verr := doc.Validate(); verr == nil {
		t.Error("a {row.task} interpolation outside any row_template validated clean; there is no row to be relative to, so it can never resolve")
	}
}
