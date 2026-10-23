package scene

import "testing"

// pickScene is a one-node scene gated on bind.
func pickScene(bind string) string {
	return `{"root":{"type":"text","text":"x","when":"` + bind + `"}}`
}

// The ui.pick families are signed and a malformed address is refused with its shape.
// Consequence: a gate on a name that /pick can never write (an upper-case or dotted choice, a
// missing group) hides its node forever and says nothing.
// Remedy: the validator accepts ui.pick.is.<group>.<name> and ui.pick.none.<group> made of
// a-z, 0-9, '_' and '-', and refuses the rest naming the form.
func TestValidatePickBinds(t *testing.T) {
	good := []string{"ui.pick.is.tabs.one", "ui.pick.none.tabs", "ui.pick.is.side-bar.log_2"}
	for _, b := range good {
		doc, err := ParseDocument([]byte(pickScene(b)))
		if err != nil {
			t.Fatalf("%s: parse: %v", b, err)
		}
		if err := doc.Validate(); err != nil {
			t.Errorf("%s was refused: %v\nConsequence: a signed family is unusable.\nRemedy: accept the form.", b, err)
		}
	}
	bad := []string{
		"ui.pick.is.tabs",         // no choice
		"ui.pick.is.",             // nothing
		"ui.pick.is..one",         // empty group
		"ui.pick.is.tabs.",        // empty choice
		"ui.pick.is.Tabs.one",     // upper case
		"ui.pick.is.tabs.a.b",     // dotted choice
		"ui.pick.none.",           // no group
		"ui.pick.none.Tabs",       // upper case
		"ui.pick.none.tabs.extra", // dotted group
	}
	for _, b := range bad {
		doc, err := ParseDocument([]byte(pickScene(b)))
		if err != nil {
			t.Fatalf("%s: parse: %v", b, err)
		}
		err = doc.Validate()
		if err == nil {
			t.Errorf("%s was accepted.\nConsequence: a gate no /pick can satisfy loads silently and its node never shows.\nRemedy: refuse it with the form.", b)
			continue
		}
		if got := err.Error(); !containsAny(got, "ui.pick.is.<group>.<name>", "ui.pick.none.<group>") {
			t.Errorf("%s refused without naming the form: %v", b, err)
		}
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

func TestValidPickWord(t *testing.T) {
	for w, want := range map[string]bool{"a": true, "tab-1": true, "x_y": true, "": false, "A": false, "a.b": false, "a b": false, "é": false} {
		if got := ValidPickWord(w); got != want {
			t.Errorf("ValidPickWord(%q) = %v, want %v", w, got, want)
		}
	}
}
