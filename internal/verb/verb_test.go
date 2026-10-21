package verb

import (
	"fmt"
	"strings"
	"testing"
)

// The catalogue is the list of what the interface can be told to do. These tests walk it:
// every verb, every parameter, and the values at the edges of each one.

func TestEveryVerbIsDescribedAndItsExampleParses(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range All() {
		if v.Name == "" || !strings.Contains(v.Name, ".") {
			t.Errorf("verb %q: a name is <area>.<action>\nRemedy: rename it.", v.Name)
		}
		if seen[v.Name] {
			t.Errorf("verb %q is listed twice\nConsequence: the second can never be reached.\nRemedy: remove one.", v.Name)
		}
		seen[v.Name] = true
		if strings.TrimSpace(v.Doc) == "" || v.Example == "" {
			t.Errorf("verb %s has no Doc or no Example\nConsequence: the model is told nothing about it, and the docs test cannot run it.\nRemedy: write both.", v.Name)
		}
		for i, p := range v.Params {
			if p.Name == "" || p.Doc == "" {
				t.Errorf("verb %s parameter %d has no name or doc", v.Name, i)
			}
			if p.Kind == Text && i != len(v.Params)-1 {
				t.Errorf("verb %s: a text parameter takes the rest of the line, so it must be the last", v.Name)
			}
			if p.Kind == Int && p.Max != 0 && p.Max < p.Min {
				t.Errorf("verb %s parameter %s: max %d is below min %d", v.Name, p.Name, p.Max, p.Min)
			}
		}
		if _, err := Parse(v.Example); err != nil {
			t.Errorf("the example of %s does not parse: %v\nConsequence: the documentation shows a call that is refused.\nRemedy: fix the example or the parameters.", v.Name, err)
		}
		if err := Check(v.Example); err != nil {
			t.Errorf("the example of %s does not pass Check: %v", v.Name, err)
		}
	}
	if len(seen) == 0 {
		t.Fatal("the catalogue is empty")
	}
}

// For each Int parameter: the value just under the minimum, the minimum, the maximum, just
// over it, a word that is not a number, and every keyword. Built from the catalogue, so a
// verb added later is covered without writing a case for it.
func TestEveryIntegerParameterIsHeldToItsLimits(t *testing.T) {
	for _, v := range All() {
		for pi, p := range v.Params {
			if p.Kind != Int {
				continue
			}
			call := func(val string) string {
				parts := []string{v.Name}
				for i, q := range v.Params {
					if i == pi {
						parts = append(parts, val)
					} else if q.Required || i < pi {
						parts = append(parts, fmt.Sprint(max(q.Min, 1)))
					}
				}
				return strings.Join(parts, " ")
			}
			good := []string{fmt.Sprint(p.Min)}
			bad := []string{fmt.Sprint(p.Min - 1), "abc", "1.5", "-0x1"}
			if p.Max > 0 {
				good = append(good, fmt.Sprint(p.Max))
				bad = append(bad, fmt.Sprint(p.Max+1))
			}
			good = append(good, p.Words...)
			for _, val := range good {
				if _, err := Parse(call(val)); err != nil {
					t.Errorf("%s: %q is refused: %v\nConsequence: a legal value cannot be used.\nRemedy: fix the limits of <%s>.", v.Name, call(val), err, p.Name)
				}
			}
			for _, val := range bad {
				if _, err := Parse(call(val)); err == nil {
					t.Errorf("%s: %q is accepted\nConsequence: a number out of range reaches the program.\nRemedy: check the limits of <%s> in checkValue.", v.Name, call(val), p.Name)
				}
			}
		}
	}
}

func TestEveryRequiredParameterMustBeWritten(t *testing.T) {
	for _, v := range All() {
		for pi, p := range v.Params {
			if !p.Required {
				continue
			}
			line := v.Name // everything before this parameter filled with a plain 1
			for i := 0; i < pi; i++ {
				line += " 1"
			}
			if _, err := Parse(line); err == nil {
				t.Errorf("%q is accepted without <%s>\nConsequence: the verb runs with a missing argument.\nRemedy: keep Required on the parameter.", line, p.Name)
			}
		}
	}
}

func TestAnUnknownVerbExtraTextControlsAndLongLinesAreRefused(t *testing.T) {
	first := All()[0]
	cases := map[string]string{
		"empty":         "",
		"unknown":       "shell.destroy 1",
		"no area":       "stop 1",
		"control":       "shell.stop 1\x1b[31m",
		"newline":       "shell.stop 1\nshell.stop 2",
		"too long":      first.Name + " " + strings.Repeat("x", MaxLine),
		"extra on stop": "shell.stop 1 2",
	}
	for name, line := range cases {
		if _, err := Parse(line); err == nil {
			t.Errorf("%s: %q is accepted\nConsequence: a call the catalogue does not describe reaches the program.", name, line)
		}
	}
	if err := Check("shell.destroy 1"); err == nil || !strings.Contains(err.Error(), "shell.stop") {
		t.Errorf("the refusal of an unknown verb must list the verbs that exist, got: %v", err)
	}
}

// A button in a list writes shell.stop {row.id}: the token is checked once the row is known,
// not when the scene is loaded; but the verb and the number of arguments are.
func TestARowTokenIsCheckedWhenTheRowIsKnownNotAtLoad(t *testing.T) {
	if err := Check("shell.stop {row.id}"); err != nil {
		t.Errorf("a row token in a number is refused at load: %v\nConsequence: no list can have a stop button.", err)
	}
	if _, err := Parse("shell.stop {row.id}"); err == nil {
		t.Error("a token that was never replaced is accepted at press time\nConsequence: the host would act on the text '{row.id}'.\nRemedy: Parse checks every value.")
	}
	if err := Check("shell.stop {row.id} {row.id}"); err == nil {
		t.Error("extra arguments after a token pass Check")
	}
	if err := Check("shell.nothing {row.id}"); err == nil {
		t.Error("an unknown verb with a token passes Check")
	}
}

func TestACallReturnsItsArgumentsInOrder(t *testing.T) {
	c, err := Parse("shell.read 3 50")
	if err != nil || c.Verb.Name != "shell.read" || len(c.Args) != 2 || c.Args[0] != "3" || c.Args[1] != "50" {
		t.Errorf("call = %+v, %v", c, err)
	}
	c, err = Parse("shell.start npm run dev -- --port 80")
	if err != nil || len(c.Args) != 1 || c.Args[0] != "npm run dev -- --port 80" {
		t.Errorf("a text parameter must take the rest of the line: %+v, %v", c, err)
	}
}

func TestACallerCannotChangeTheCatalogue(t *testing.T) {
	a := All()
	a[0].Name = "hacked"
	a[0].Params[0].Name = "hacked"
	if All()[0].Name == "hacked" || All()[0].Params[0].Name == "hacked" {
		t.Error("All() hands out the catalogue itself\nConsequence: one caller can rename a verb for all the others.\nRemedy: copy.")
	}
}
