package expr

import (
	"strings"
	"testing"
)

var schema = Schema{
	"focus":        String,
	"input.at_end": Bool,
	"input.empty":  Bool,
	"shells.count": Int,
	"busy":         Bool,
	"width":        Int,
}

func vals(m map[string]Value) Lookup {
	return func(n string) (Value, bool) { v, ok := m[n]; return v, ok }
}

var world = map[string]Value{
	"focus":        S("prompt"),
	"input.at_end": B(true),
	"input.empty":  B(false),
	"shells.count": N(2),
	"busy":         B(false),
	"width":        N(80),
}

func evalBool(t *testing.T, src string) bool {
	t.Helper()
	p, err := Compile(src, schema, Bool)
	if err != nil {
		t.Fatalf("%q should compile and did not: %v", src, err)
	}
	v, err := p.EvalBool(vals(world))
	if err != nil {
		t.Fatalf("%q should evaluate and did not: %v", src, err)
	}
	return v
}

func evalKind(t *testing.T, src string, want Kind) Value {
	t.Helper()
	p, err := Compile(src, schema, want)
	if err != nil {
		t.Fatalf("%q should compile and did not: %v", src, err)
	}
	v, err := p.Eval(vals(world))
	if err != nil {
		t.Fatalf("%q should evaluate and did not: %v", src, err)
	}
	return v
}

// refused checks that src is refused at compile time and that the message carries each hint:
// a refusal that does not say what to write instead leaves the model guessing.
func refused(t *testing.T, src string, want Kind, hints ...string) {
	t.Helper()
	_, err := Compile(src, schema, want)
	if err == nil {
		t.Fatalf("%q should be refused and was accepted; a wrong condition would then fail at the first key press", src)
	}
	for _, h := range hints {
		if !strings.Contains(err.Error(), h) {
			t.Errorf("%q: the refusal %q should mention %q so the author knows the remedy", src, err, h)
		}
	}
}

func TestConditionsDecide(t *testing.T) {
	cases := map[string]bool{
		"focus == 'prompt' && input.at_end && shells.count > 0": true,
		"focus == \"chat\" || busy":                             false,
		"!busy":                                                 true,
		"!(input.empty || busy)":                                true,
		"true":                                                  true,
		"false || true":                                         true,
		"width >= 80 && width <= 80":                            true,
		"width != 80":                                           false,
		"width < 80":                                            false,
		"width < 81 && 79 < width":                              true,
		"width > 80":                                            false,
		"shells.count * 2 + 1 == 5":                             true,
		"shells.count + 1 * 2 == 4":                             true, // * binds tighter than +
		"(shells.count + 1) * 2 == 6":                           true,
		"10 - 3 - 2 == 5":                                       true, // left to right
		"100 / 10 / 5 == 2":                                     true,
		"17 % 5 == 2":                                           true,
		"-width < 0":                                            true,
		"true || false && false":                                true, // && binds tighter than ||
		"'a' + 'b' == 'ab'":                                     true,
		"focus == 'prom' + 'pt'":                                true,
	}
	for src, want := range cases {
		if got := evalBool(t, src); got != want {
			t.Errorf("%q gave %v, want %v", src, got, want)
		}
	}
}

func TestValuesOfOtherKinds(t *testing.T) {
	if v := evalKind(t, "shells.count * 3", Int); v.I != 6 {
		t.Errorf("number result = %v, want 6", v)
	}
	if v := evalKind(t, "focus + '!'", String); v.S != "prompt!" {
		t.Errorf("text result = %v, want prompt!", v)
	}
}

func TestStringEscapes(t *testing.T) {
	if !evalBool(t, `'it\'s' == "it's"`) {
		t.Error(`\' inside a quoted text should be the quote itself`)
	}
	if !evalBool(t, `'a\\b' == 'a' + '\\' + 'b'`) {
		t.Error(`\\ should be one backslash, so that a text can end with one`)
	}
}

func TestShortCircuitDoesNotReadTheRight(t *testing.T) {
	reads := 0
	get := func(n string) (Value, bool) {
		if n == "busy" {
			reads++
		}
		return world[n], true
	}
	for _, c := range []struct {
		src  string
		want bool
	}{{"input.at_end || busy", true}, {"input.empty && busy", false}} {
		p, err := Compile(c.src, schema, Bool)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.EvalBool(get)
		if err != nil || got != c.want {
			t.Fatalf("%q = %v, %v; want %v", c.src, got, err, c.want)
		}
	}
	if reads != 0 {
		t.Errorf("busy was read %d times; the right side of && and || must not be read when the left decides, or a missing value would break a condition that is already settled", reads)
	}
	p, _ := Compile("input.at_end && busy", schema, Bool)
	if _, err := p.EvalBool(get); err != nil || reads != 1 {
		t.Errorf("when the left does not decide, the right must be read once; reads=%d err=%v", reads, err)
	}
}

func TestMissingValueIsAnErrorNotFalse(t *testing.T) {
	p, _ := Compile("busy", schema, Bool)
	_, err := p.EvalBool(vals(nil))
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("a name without a value must fail and name itself, got %v; treating it as false would hide a broken host", err)
	}
}

func TestHostDisagreeingWithItsSchemaIsReported(t *testing.T) {
	p, _ := Compile("busy && input.at_end", schema, Bool)
	_, err := p.EvalBool(vals(map[string]Value{"busy": N(1), "input.at_end": B(true)}))
	if err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Errorf("a number given for a yes/no must be reported as a host/schema disagreement, got %v", err)
	}
	p, _ = Compile("busy || input.at_end", schema, Bool)
	_, err = p.EvalBool(vals(map[string]Value{"busy": B(false), "input.at_end": S("x")}))
	if err == nil {
		t.Error("a wrongly kinded right side must be reported too")
	}
	p, _ = Compile("shells.count + width == 3", schema, Bool)
	_, err = p.EvalBool(vals(map[string]Value{"shells.count": N(1), "width": S("w")}))
	if err == nil || !strings.Contains(err.Error(), "disagree") {
		t.Errorf("mixed kinds at an operator must be reported, got %v", err)
	}
	p, _ = Compile("!busy", schema, Bool)
	if _, err = p.EvalBool(vals(map[string]Value{"busy": N(1)})); err == nil {
		t.Error("! on a number given by the host must be reported")
	}
	p, _ = Compile("-width < 0", schema, Bool)
	if _, err = p.EvalBool(vals(map[string]Value{"width": S("1")})); err == nil {
		t.Error("- on a text given by the host must be reported")
	}
}

func TestNoTruthiness(t *testing.T) {
	refused(t, "shells.count", Bool, "compare it", "shells.count > 0")
	refused(t, "focus", Bool, "compare it", "'name'")
	refused(t, "!shells.count", Bool, "compare it first")
	refused(t, "busy && shells.count", Bool, "compare the value first")
	refused(t, "shells.count || busy", Bool, "yes/no")
}

func TestKindRules(t *testing.T) {
	refused(t, "focus == 3", Bool, "same kind", "a number is written 3")
	refused(t, "width < 'a'", Bool, "compares numbers")
	refused(t, "busy < true", Bool, "compares numbers")
	refused(t, "focus + 1 == 2", Bool, "+ adds two numbers or joins two texts")
	refused(t, "busy + busy == 2", Bool, "+ adds")
	refused(t, "'a' - 'b' == 'c'", Bool, "works on numbers")
	refused(t, "-focus == 'a'", Bool, "negates a number")
	refused(t, "width < 3 < 4", Bool, "compares numbers")
	refused(t, "width + 1", Bool, "is needed here", "(width + 1) > 0")
	refused(t, "busy", Int, "number is needed")
}

func TestMisspeltNamesAreRefusedWithSuggestions(t *testing.T) {
	refused(t, "shell.count > 0", Bool, `"shell.count"`, "did you mean shells.count")
	refused(t, "inputat_end", Bool, "input.at_end")
	refused(t, "zzzzzzzzzzzzzz", Bool, "listed by the host")
	// substring neighbours are suggested even when far in edit distance
	refused(t, "count", Bool, "shells.count")
}

func TestCloserNamesAreSuggestedFirst(t *testing.T) {
	s := Schema{"abcdxy": Bool, "abce": Bool}
	_, err := Compile("abcd", s, Bool)
	if err == nil || !strings.Contains(err.Error(), "abce or abcdxy") {
		t.Errorf("the nearest spelling must come first, even when it sorts later: %v", err)
	}
}

func TestNamesAreSortedWhateverTheMapOrder(t *testing.T) {
	for i := 0; i < 30; i++ {
		p, err := Compile("width > 0 && busy && focus == 'a' && shells.count > 0 && input.at_end", schema, Bool)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(p.Names(), ","); got != "busy,focus,input.at_end,shells.count,width" {
			t.Fatalf("Names = %q; callers compare and print them, so the order must be stable", got)
		}
	}
}

func TestSuggestionsAreCappedAndOrdered(t *testing.T) {
	s := Schema{"aa": Bool, "ab": Bool, "ac": Bool, "ad": Bool, "ae": Bool}
	_, err := Compile("a", s, Bool)
	if err == nil {
		t.Fatal("a is not a name")
	}
	msg := err.Error()
	if !strings.Contains(msg, "aa or ab or ac?") {
		t.Errorf("at most three suggestions, closest and then alphabetical first: got %q", msg)
	}
	if strings.Contains(msg, "ad") {
		t.Errorf("a fourth suggestion is noise: %q", msg)
	}
}

func TestLexerRefusals(t *testing.T) {
	refused(t, "", Bool, "empty")
	refused(t, "   ", Bool, "empty")
	refused(t, "width = 80", Bool, "assignment", "==")
	refused(t, "busy & busy", Bool, "a single &", "write &&")
	refused(t, "busy | busy", Bool, "a single |", "write ||")
	refused(t, "width > 1.5", Bool, "whole number")
	refused(t, "width > 1e3", Bool, "whole number")
	refused(t, "Focus == 'x'", Bool, "lowercase")
	refused(t, "focus == 'abc", Bool, "never closes")
	refused(t, "width > 1 ? 2 : 3", Bool, "not part of the expression language")
	refused(t, "input. == 1", Bool, "stray")
	refused(t, "input..at_end", Bool, "stray")
	refused(t, "width > 99999999999999999999", Bool, "larger than")
	refused(t, "width > 1099511627777", Bool, "larger than")
}

func TestParserRefusals(t *testing.T) {
	refused(t, "width >", Bool, "ends where a value was expected")
	refused(t, "(width > 1", Bool, "not closed")
	refused(t, "width > 1 busy", Bool, "&& or ||")
	refused(t, "width > 1)", Bool, "after a complete expression")
	refused(t, "&& busy", Bool, "not expected here")
	refused(t, "()", Bool, "not expected here")
	refused(t, "(busy ')'", Bool, "not closed") // a text that looks like ) does not close
}

func TestPositionsPointAtTheProblem(t *testing.T) {
	_, err := Compile("busy && shel.count > 0", schema, Bool)
	if err == nil || !strings.Contains(err.Error(), "at 9:") {
		t.Errorf("the message should say the column of the bad name (9), got %v", err)
	}
}

func TestLimits(t *testing.T) {
	long := strings.Repeat("busy || ", 60) + "busy" // 484 runes
	refused(t, long, Bool, "characters", "split")
	many := strings.Repeat("a||", 100) + "a" // under length, over node limit
	s := Schema{"a": Bool}
	if _, err := Compile(many, s, Bool); err == nil || !strings.Contains(err.Error(), "parts") {
		t.Errorf("an expression over the node limit must be refused and say 'parts', got %v", err)
	}
	deep := strings.Repeat("(", 30) + "busy" + strings.Repeat(")", 30)
	refused(t, deep, Bool, "nested")
	ok := strings.Repeat("(", 10) + "busy" + strings.Repeat(")", 10)
	if _, err := Compile(ok, schema, Bool); err != nil {
		t.Errorf("modest nesting is fine: %v", err)
	}
	if _, err := Compile(strings.Repeat("busy || ", 40)+"busy", schema, Bool); err != nil {
		t.Errorf("a long but reasonable chain must compile: %v", err)
	}
}

func TestArithmeticGuards(t *testing.T) {
	run := func(src string) error {
		p, err := Compile(src, schema, Bool)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		_, err = p.EvalBool(vals(world))
		return err
	}
	if err := run("1 / 0 == 1"); err == nil || !strings.Contains(err.Error(), "dividing by zero") || !strings.Contains(err.Error(), "divisor") {
		t.Errorf("division by zero must be refused with a remedy, got %v", err)
	}
	if err := run("1 % 0 == 1"); err == nil || !strings.Contains(err.Error(), "dividing by zero") {
		t.Errorf("modulo by zero must be refused, got %v", err)
	}
	if err := run("1099511627776 * 2 > 0"); err == nil || !strings.Contains(err.Error(), "largest number") {
		t.Errorf("a product past the limit must be refused, got %v", err)
	}
	if err := run("2 * 1099511627776 > 0"); err == nil {
		t.Error("the product limit must not depend on operand order")
	}
	// 2^40 * 2^40 is 2^80, which wraps to exactly 0 in 64 bits: the later range check
	// cannot see it, so the product itself must be guarded.
	if err := run("1099511627776 * 1099511627776 == 0"); err == nil || !strings.Contains(err.Error(), "largest number") {
		t.Errorf("a product that wraps around must be refused, not read as 0, got %v", err)
	}
	if err := run("1099511627776 + 1 > 0"); err == nil || !strings.Contains(err.Error(), "largest number") {
		t.Errorf("a sum past the limit must be refused, got %v", err)
	}
	if err := run("-1099511627776 - 1 < 0"); err == nil {
		t.Error("a negative result past the limit must be refused too")
	}
	if err := run("1099511627776 * 1 == 1099511627776"); err != nil {
		t.Errorf("a product exactly at the limit is allowed: %v", err)
	}
	if err := run("0 * 1099511627776 == 0"); err != nil {
		t.Errorf("zero times anything is zero: %v", err)
	}
	if err := run("-7 / 2 == -3"); err != nil {
		t.Errorf("division truncates toward zero: %v", err)
	}
	p, _ := Compile("width > 0", schema, Bool)
	_, err := p.EvalBool(vals(map[string]Value{"width": N(MaxInt * 4)}))
	if err == nil || !strings.Contains(err.Error(), "width") {
		t.Errorf("a host number past the limit must be refused and name itself, got %v", err)
	}
}

func TestStringGrowthIsBounded(t *testing.T) {
	big := strings.Repeat("x", 600)
	sch := Schema{"t": String}
	p, err := Compile("t + t == t", sch, Bool)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.EvalBool(vals(map[string]Value{"t": S(big)}))
	if err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("a joined text past the limit must be refused, got %v", err)
	}
	if _, err = p.EvalBool(vals(map[string]Value{"t": S("é")})); err != nil {
		t.Errorf("a short join is fine: %v", err)
	}
	// the limit counts characters, not bytes
	multi := strings.Repeat("é", 500)
	if _, err = p.EvalBool(vals(map[string]Value{"t": S(multi)})); err != nil {
		t.Errorf("1000 characters of 2 bytes each are within the limit: %v", err)
	}
}

func TestNamesAndMetadata(t *testing.T) {
	p, err := Compile("focus == 'x' && (shells.count > 0 || focus != 'y') && true", schema, Bool)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Names(), ","); got != "focus,shells.count" {
		t.Errorf("Names = %q, want the distinct names sorted: focus,shells.count", got)
	}
	p.Names()[0] = "zzz"
	if p.Names()[0] != "focus" {
		t.Error("Names must return a copy so a caller cannot change what the program reads")
	}
	if p.Source() != "focus == 'x' && (shells.count > 0 || focus != 'y') && true" {
		t.Errorf("Source = %q", p.Source())
	}
	if p.Kind() != Bool {
		t.Errorf("Kind = %v", p.Kind())
	}
}

func TestEvalBoolOnANonBoolProgram(t *testing.T) {
	p, _ := Compile("width + 1", schema, Int)
	if _, err := p.EvalBool(vals(world)); err == nil {
		t.Error("EvalBool on a number program must fail instead of returning false")
	}
}

func TestKindAndValueStrings(t *testing.T) {
	if Bool.String() != "yes/no" || Int.String() != "number" || String.String() != "text" || Kind(0).String() != "unknown" {
		t.Error("kind names are used in messages and are part of the contract")
	}
	if B(true).String() != "true" || N(5).String() != "5" || S("a").String() != "a" || (Value{}).String() != "" {
		t.Error("value strings")
	}
}

func TestProgramsAreSharable(t *testing.T) {
	p, _ := Compile("width > 1 && focus == 'prompt'", schema, Bool)
	done := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		go func() {
			ok := true
			for j := 0; j < 200; j++ {
				v, err := p.EvalBool(vals(world))
				ok = ok && err == nil && v
			}
			done <- ok
		}()
	}
	for i := 0; i < 8; i++ {
		if !<-done {
			t.Fatal("a compiled program must give the same answer from several goroutines")
		}
	}
}
