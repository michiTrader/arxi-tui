package keymap

import (
	"errors"
	"strings"
	"testing"

	"github.com/michiTrader/arxi_tui/internal/expr"
	"github.com/michiTrader/arxi_tui/internal/term"
)

var schema = expr.Schema{"a": expr.Bool, "b": expr.Bool}

func facts(a, b bool) expr.Lookup {
	return func(n string) (expr.Value, bool) {
		switch n {
		case "a":
			return expr.B(a), true
		case "b":
			return expr.B(b), true
		}
		return expr.Value{}, false
	}
}

func mustParse(t *testing.T, src string, check CheckDo) *Map {
	t.Helper()
	m, err := Parse([]byte(src), schema, check)
	if err != nil {
		t.Fatalf("Parse(%s): %v", src, err)
	}
	return m
}

func key(s string) term.Key {
	k, ok := term.ParseKey(s)
	if !ok {
		panic("bad key " + s)
	}
	return k
}

// Every refusal names the binding and carries the remedy, because the person who reads it
// is asking an agent to fix the file.
func TestParseRefusesWithThePlaceAndTheRemedy(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"not json", `nope`, "keymap:"},
		{"unknown field", `{"version":1,"bindings":[{"key":"f5","do":"do:x","then":1}]}`, "unknown field"},
		{"wrong version", `{"version":2,"bindings":[{"key":"f5","do":"do:x"}]}`, "version"},
		{"empty", `{"version":1,"bindings":[]}`, "no bindings"},
		{"ctrl+c", `{"version":1,"bindings":[{"key":"ctrl+c","do":"do:x"}]}`, "ctrl+c is the way out"},
		{"plain letter", `{"version":1,"bindings":[{"key":"q","do":"do:x"}]}`, "a letter you type with"},
		{"unknown key", `{"version":1,"bindings":[{"key":"hyper+zz","do":"do:x"}]}`, "not a key name"},
		{"no do", `{"version":1,"bindings":[{"key":"f5"}]}`, "no `do`"},
		{"long do", `{"version":1,"bindings":[{"key":"f5","do":"` + strings.Repeat("x", MaxDoRunes+1) + `"}]}`, "longer than"},
		{"unknown name", `{"version":1,"bindings":[{"key":"f5","when":"zzz","do":"do:x"}]}`, "binding 1 (f5)"},
		{"when is not a bool", `{"version":1,"bindings":[{"key":"f5","when":"1 + 1","do":"do:x"}]}`, "binding 1 (f5)"},
		{"hidden by an unconditional one", `{"version":1,"bindings":[{"key":"f5","do":"do:x"},{"key":"f5","when":"a","do":"do:y"}]}`, "can never run"},
		{"hidden by the same condition", `{"version":1,"bindings":[{"key":"f5","when":"a","do":"do:x"},{"key":"f5","when":" a ","do":"do:y"}]}`, "same `when`"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src), schema, nil)
		if err == nil {
			t.Errorf("%s: accepted. Consequence: a broken keymap loads and a key does the wrong thing. Remedy: refuse it in Parse.", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: the refusal is %q and should mention %q", c.name, err, c.want)
		}
	}
}

func TestParseRefusesTooManyBindings(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"version":1,"bindings":[`)
	for i := 0; i <= MaxBindings; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"key":"f5","when":"a && ` + strings.Repeat("a && ", i%3) + `b","do":"do:x"}`)
	}
	b.WriteString(`]}`)
	_, err := Parse([]byte(b.String()), schema, nil)
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("got %v; want a refusal about the number of bindings", err)
	}
}

func TestParseRunsTheHostCheckOnEveryAction(t *testing.T) {
	check := func(do string) error {
		if do == "do:bad" {
			return errors.New("the host says no")
		}
		return nil
	}
	_, err := Parse([]byte(`{"version":1,"bindings":[{"key":"f5","do":"do:ok"},{"key":"f6","do":"do:bad"}]}`), schema, check)
	if err == nil || !strings.Contains(err.Error(), "binding 2 (f6)") || !strings.Contains(err.Error(), "the host says no") {
		t.Fatalf("got %v; want binding 2's refusal carrying the host's reason", err)
	}
}

func TestResolveTakesTheFirstBindingWhoseConditionHolds(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[
	  {"key":"ctrl+g","when":"a","do":"do:first"},
	  {"key":"ctrl+g","when":"b","do":"do:second"},
	  {"key":"ctrl+g","do":"do:last"}]}`, nil)
	for _, c := range []struct {
		a, b bool
		want string
	}{{true, true, "do:first"}, {true, false, "do:first"}, {false, true, "do:second"}, {false, false, "do:last"}} {
		got, ok, err := m.Resolve(key("ctrl+g"), facts(c.a, c.b))
		if err != nil || !ok || got.Do != c.want {
			t.Errorf("a=%v b=%v: got (%q, %v, %v), want %q. Consequence: the wrong action runs. Remedy: keep file order and stop at the first held condition.", c.a, c.b, got.Do, ok, err, c.want)
		}
	}
}

func TestResolveSaysNoWhenNothingHoldsOrTheKeyIsNotNamed(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[{"key":"ctrl+g","when":"a","do":"do:x"}]}`, nil)
	if _, ok, err := m.Resolve(key("ctrl+g"), facts(false, false)); ok || err != nil {
		t.Errorf("a false condition took the key: ok=%v err=%v", ok, err)
	}
	if _, ok, err := m.Resolve(key("ctrl+h"), facts(true, true)); ok || err != nil {
		t.Errorf("an unnamed key was taken: ok=%v err=%v", ok, err)
	}
}

func TestResolveReportsAConditionItCannotDecideAndDoesNotTakeTheKey(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[{"key":"ctrl+g","when":"a","do":"do:x"}]}`, nil)
	none := func(string) (expr.Value, bool) { return expr.Value{}, false }
	_, ok, err := m.Resolve(key("ctrl+g"), none)
	if ok || err == nil || !strings.Contains(err.Error(), "binding 1 (ctrl+g)") {
		t.Fatalf("got ok=%v err=%v. Consequence: a name the host forgot to answer swallows the key in silence. Remedy: return the error so the host can show it.", ok, err)
	}
}

func TestANilMapTakesNothing(t *testing.T) {
	var m *Map
	if _, ok, err := m.Resolve(key("ctrl+o"), facts(true, true)); ok || err != nil {
		t.Error("a nil map took a key")
	}
	if m.Takes(key("ctrl+o")) || m.Bindings() != nil || len(m.Keys()) != 0 || len(m.Names()) != 0 {
		t.Error("a nil map must be empty")
	}
}

func TestKeysAreCanonicalSoAnySpellingFindsTheBinding(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[{"key":"Ctrl+O","do":"do:x"}]}`, nil)
	if !m.Takes(key("ctrl+o")) {
		t.Error("Ctrl+O was not found as ctrl+o")
	}
	if got := m.Keys(); len(got) != 1 || got[0] != "ctrl+o" {
		t.Errorf("Keys() = %v, want [ctrl+o]", got)
	}
}

// "alt+ctrl+g" and "ctrl+alt+g" are one key: the first spelling must find the binding the
// second one asks for, and two bindings of them are one key for the shadowing check too.
func TestModifierOrderDoesNotMakeAnotherKey(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[{"key":"alt+ctrl+g","do":"do:x"}]}`, nil)
	if _, ok, _ := m.Resolve(key("ctrl+alt+g"), facts(true, true)); !ok {
		t.Error("alt+ctrl+g was stored as typed, so the key the terminal reports (ctrl+alt+g) never finds it. Remedy: store the canonical spelling.")
	}
	if got := m.Keys(); len(got) != 1 || got[0] != "ctrl+alt+g" {
		t.Errorf("Keys() = %v, want [ctrl+alt+g]", got)
	}
	_, err := Parse([]byte(`{"version":1,"bindings":[{"key":"alt+ctrl+g","do":"do:x"},{"key":"ctrl+alt+g","when":"a","do":"do:y"}]}`), schema, nil)
	if err == nil || !strings.Contains(err.Error(), "can never run") {
		t.Errorf("two spellings of one key were not seen as one: %v", err)
	}
}

func TestTakesIgnoresTheConditionAndBindingsIsACopy(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[{"key":"ctrl+g","when":"a","do":"do:x"},{"key":"f5","do":"do:y"}]}`, nil)
	if !m.Takes(key("ctrl+g")) || !m.Takes(key("f5")) || m.Takes(key("f6")) {
		t.Error("Takes must say whether a key is named, whatever its condition")
	}
	bs := m.Bindings()
	bs[0].Do = "do:hacked"
	if m.Bindings()[0].Do != "do:x" {
		t.Error("Bindings() leaked the map's own storage; a caller could rewrite the keymap after it was checked")
	}
	if got := m.Keys(); len(got) != 2 || got[0] != "ctrl+g" || got[1] != "f5" {
		t.Errorf("Keys() = %v", got)
	}
}

func TestNamesListsEachNameOnce(t *testing.T) {
	m := mustParse(t, `{"version":1,"bindings":[
	  {"key":"ctrl+g","when":"a && b","do":"do:x"},
	  {"key":"ctrl+h","when":"a","do":"do:y"},
	  {"key":"f5","do":"do:z"}]}`, nil)
	got := m.Names()
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("Names() = %v, want a and b once each", got)
	}
}

func TestTheFactoryKeymapParsesAgainstItsOwnNames(t *testing.T) {
	if len(FactoryJSON) == 0 {
		t.Fatal("factory.json is not embedded. Remedy: keep //go:embed factory.json in factory.go.")
	}
	// The host owns the schema and the action check; here the grammar alone must hold.
	sch := expr.Schema{}
	for _, n := range []string{"menu.open", "key.user_bound", "screen.open", "approval.pending", "find.open", "agent.working", "ui.focused", "busy.steers"} {
		sch[n] = expr.Bool
	}
	m, err := Parse(FactoryJSON, sch, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ctrl+o", "ctrl+f"} {
		if !m.Takes(key(k)) {
			t.Errorf("factory keymap lost %s", k)
		}
	}
}
