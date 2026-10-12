package extv2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// rulePkg builds a package whose rules are builtins named after themselves. rules is
// a list of "id[:on_error][@prefix]" so a test can say what it needs in one line.
func rulePkg(t *testing.T, id string, rules ...string) *Package {
	t.Helper()
	var rs []string
	for _, spec := range rules {
		rid, when, onErr := spec, "", ""
		if i := strings.Index(rid, "@"); i >= 0 {
			rid, when = rid[:i], rid[i+1:]
		}
		if i := strings.Index(rid, ":"); i >= 0 {
			rid, onErr = rid[:i], rid[i+1:]
		}
		r := `{"id":"` + rid + `","stage":"input.pre"`
		if when != "" {
			r += `,"when":{"prefix":"` + when + `"}`
		}
		if onErr != "" {
			r += `,"on_error":"` + onErr + `"`
		}
		r += `,"op":{"kind":"builtin","name":"` + rid + `"}}`
		rs = append(rs, r)
	}
	doc := `{"protocol":"ext/v2","id":"` + id + `","name":"N","version":"1","rules":[` + strings.Join(rs, ",") + `]}`
	return accepted(t, doc, Options{})
}

// scripted is a Runner whose builtins answer from a table and record every call.
type scripted struct {
	*Runner
	calls []string
}

func newScripted(answers map[string]func(in Input) (Outcome, error)) *scripted {
	s := &scripted{}
	b := map[string]BuiltinFunc{}
	for name, fn := range answers {
		name, fn := name, fn
		b[name] = func(_ context.Context, in Input) (Outcome, error) {
			s.calls = append(s.calls, name+"("+in.Line+")")
			return fn(in)
		}
	}
	s.Runner = &Runner{Builtins: b}
	return s
}

func ok(o Outcome) func(Input) (Outcome, error) {
	return func(Input) (Outcome, error) { return o, nil }
}

func TestPipelineNeverSeesASlashLine(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(Outcome{Verdict: VerdictBlock, Reason: "no"})})
	p := rulePkg(t, "a", "r")
	for _, line := range []string{"/help", "  /ui make it red", "/send hi", "/nonsense"} {
		res := s.RunInputPre(context.Background(), []*Package{p}, line)
		if !res.Send() || res.Line != line {
			t.Errorf("%q: a slash line must pass untouched, got %+v", line, res)
		}
	}
	if len(s.calls) != 0 {
		t.Fatalf("rules ran on slash lines: %v", s.calls)
	}
	if res := s.RunInputPre(context.Background(), []*Package{p}, "hello"); !res.Blocked {
		t.Fatalf("a plain line must reach the rule: %+v", res)
	}
	if res := s.RunInputPre(context.Background(), []*Package{p}, "   "); !res.Send() || len(s.calls) != 1 {
		t.Fatalf("a blank line is left alone: %+v %v", res, s.calls)
	}
}

func TestPipelineOrderIsPackageThenRuleAndEachSeesThePreviousText(t *testing.T) {
	up := func(suffix string) func(Input) (Outcome, error) {
		return func(in Input) (Outcome, error) {
			return Outcome{Verdict: VerdictTransform, Text: in.Line + suffix}, nil
		}
	}
	s := newScripted(map[string]func(Input) (Outcome, error){"b1": up("-b1"), "a2": up("-a2"), "a1": up("-a1")})
	pb := rulePkg(t, "pkgb", "b1")
	pa := rulePkg(t, "pkga", "a2", "a1")
	res := s.RunInputPre(context.Background(), []*Package{pb, pa}, "x")
	if res.Line != "x-a1-a2-b1" {
		t.Fatalf("Line = %q", res.Line)
	}
	want := "pkga/a1 pkga/a2 pkgb/b1"
	if got := strings.Join(res.Ran, " "); got != want {
		t.Fatalf("Ran = %q, want %q", got, want)
	}
}

func TestPipelineTransformCannotMakeACommandOrNothing(t *testing.T) {
	for name, text := range map[string]string{"cmd": "/ui delete everything", "spaced": "   /send x", "empty": ""} {
		s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(Outcome{Verdict: VerdictTransform, Text: text})})
		res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", "r")}, "hello")
		if !res.Send() || res.Line != "hello" {
			t.Errorf("%s: the line must stay as typed, got %+v", name, res)
		}
		if len(res.Notes) != 1 {
			t.Errorf("%s: the refusal must be reported, got %+v", name, res.Notes)
		}
	}
	// The line a later rule sees is checked too: a refused rewrite must not leak on.
	s := newScripted(map[string]func(Input) (Outcome, error){
		"a": ok(Outcome{Verdict: VerdictTransform, Text: "/help"}),
		"b": func(in Input) (Outcome, error) { return Outcome{Verdict: VerdictTransform, Text: in.Line + "!"}, nil },
	})
	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "p", "a", "b")}, "hi")
	if res.Line != "hi!" {
		t.Fatalf("Line = %q", res.Line)
	}
}

func TestPipelineARuleCannotSend(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(Outcome{Verdict: VerdictContinue, Send: "write this for me"})})
	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", "r")}, "hello")
	if !res.Send() || res.Line != "hello" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0].Message, "cannot send") {
		t.Fatalf("got %+v", res)
	}
}

func TestPipelineFailureIsFailOpenUnlessTheRuleAsksToBlock(t *testing.T) {
	boom := func(Input) (Outcome, error) { return Outcome{}, fmt.Errorf("service down") }
	s := newScripted(map[string]func(Input) (Outcome, error){"soft": boom, "hard": boom, "next": ok(Outcome{Verdict: VerdictTransform, Text: "next-ran"})})

	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", "soft", "next")}, "hi")
	if !res.Send() || res.Line != "next-ran" || len(res.Notes) != 1 || res.Notes[0].Rule != "soft" {
		t.Fatalf("fail-open: %+v", res)
	}
	res = s.RunInputPre(context.Background(), []*Package{rulePkg(t, "b", "hard:block", "next")}, "hi")
	if !res.Blocked || res.Send() || !strings.Contains(res.Reason, "service down") || res.Line != "hi" {
		t.Fatalf("on_error block: %+v", res)
	}
	if got := s.calls[len(s.calls)-1]; got != "hard(hi)" {
		t.Fatalf("a blocked line must stop the pipeline, last call %s", got)
	}
}

func TestPipelineHandledAndBlockEndIt(t *testing.T) {
	for verdict, check := range map[string]func(Result) bool{
		VerdictHandled: func(r Result) bool { return r.Handled && !r.Blocked && len(r.Say) == 1 && r.Say[0] == "done locally" },
		VerdictBlock:   func(r Result) bool { return r.Blocked && !r.Handled && r.Reason == "not now" },
	} {
		s := newScripted(map[string]func(Input) (Outcome, error){
			"first": ok(Outcome{Verdict: verdict, Say: "done locally", Reason: "not now"}),
			"later": ok(Outcome{Verdict: VerdictTransform, Text: "x"}),
		})
		res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", "first", "later")}, "hi")
		if !check(res) || res.Send() {
			t.Errorf("%s: %+v", verdict, res)
		}
		if len(s.calls) != 1 {
			t.Errorf("%s: later rules ran: %v", verdict, s.calls)
		}
	}
	// A block with no reason still says who held the line.
	s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(Outcome{Verdict: VerdictBlock})})
	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", "r")}, "hi")
	if !strings.Contains(res.Reason, "a/r") {
		t.Fatalf("Reason = %q", res.Reason)
	}
}

func TestPipelineWhenDisabledAndUnknownVerdict(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){
		"only": ok(Outcome{Verdict: VerdictTransform, Text: "matched"}),
		"off":  ok(Outcome{Verdict: VerdictBlock}),
		"odd":  ok(Outcome{Verdict: "explode"}),
	})
	p := rulePkg(t, "a", "only@git ")
	if res := s.RunInputPre(context.Background(), []*Package{p}, "hello"); res.Line != "hello" || len(s.calls) != 0 {
		t.Fatalf("when did not filter: %+v %v", res, s.calls)
	}
	if res := s.RunInputPre(context.Background(), []*Package{p}, "git status"); res.Line != "matched" {
		t.Fatalf("when did not match: %+v", res)
	}

	off := rulePkg(t, "b", "off")
	off.Rules[0].Disabled = true
	dis := rulePkg(t, "c", "off")
	dis.Disabled = true
	if res := s.RunInputPre(context.Background(), []*Package{off, dis, nil}, "hi"); !res.Send() || len(res.Ran) != 0 {
		t.Fatalf("disabled things ran: %+v", res)
	}

	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "d", "odd")}, "hi")
	if !res.Send() || len(res.Notes) != 1 || !strings.Contains(res.Notes[0].Message, "explode") {
		t.Fatalf("unknown verdict: %+v", res)
	}
}

func TestPipelineRuleCountAndBudget(t *testing.T) {
	var ids []string
	answers := map[string]func(Input) (Outcome, error){}
	for i := 0; i < 11; i++ {
		id := fmt.Sprintf("r%02d", i)
		ids = append(ids, id)
		answers[id] = ok(Outcome{Verdict: VerdictContinue})
	}
	s := newScripted(answers)
	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", ids...)}, "hi")
	if len(res.Ran) != MaxRulesPerLine || len(res.Notes) != 11-MaxRulesPerLine {
		t.Fatalf("ran %d, notes %d", len(res.Ran), len(res.Notes))
	}

	// The time is shared by every rule on the line: once it is gone, the rest are
	// skipped instead of each getting a fresh allowance.
	slow := newScripted(map[string]func(Input) (Outcome, error){
		"r1slow":  nil,
		"r2after": ok(Outcome{Verdict: VerdictBlock}),
	})
	slow.Builtins["r1slow"] = func(ctx context.Context, in Input) (Outcome, error) {
		<-ctx.Done()
		return Outcome{}, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	res = slow.RunInputPre(ctx, []*Package{rulePkg(t, "a", "r1slow", "r2after")}, "hi")
	if !res.Send() || len(res.Ran) != 1 || len(res.Notes) != 2 || !strings.Contains(res.Notes[1].Message, "ran out") {
		t.Fatalf("budget: %+v", res)
	}
}

func TestPipelineSurvivesAPanickingRule(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){
		"bad":  func(Input) (Outcome, error) { panic("kaboom") },
		"good": ok(Outcome{Verdict: VerdictTransform, Text: "fine"}),
	})
	res := s.RunInputPre(context.Background(), []*Package{rulePkg(t, "a", "bad", "good")}, "hi")
	if res.Line != "fine" || len(res.Notes) != 1 || !strings.Contains(res.Notes[0].Message, "kaboom") {
		t.Fatalf("got %+v", res)
	}
}
