package extv2

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// postPkg is rulePkg for the output.post stage: "id[@prefix]" per rule.
func postPkg(t *testing.T, id string, rules ...string) *Package {
	t.Helper()
	var rs []string
	for _, spec := range rules {
		rid, when := spec, ""
		if i := strings.Index(rid, "@"); i >= 0 {
			rid, when = rid[:i], rid[i+1:]
		}
		r := `{"id":"` + rid + `","stage":"output.post"`
		if when != "" {
			r += `,"when":{"prefix":"` + when + `"}`
		}
		r += `,"op":{"kind":"builtin","name":"` + rid + `"}}`
		rs = append(rs, r)
	}
	return accepted(t, `{"protocol":"ext/v2","id":"`+id+`","name":"N","version":"1","rules":[`+strings.Join(rs, ",")+`]}`, Options{})
}

func reword(f func(string) string) func(Input) (Outcome, error) {
	return func(in Input) (Outcome, error) {
		return Outcome{Verdict: VerdictTransform, Text: f(in.Line)}, nil
	}
}

func hasNote(res PostResult, want string) bool {
	for _, n := range res.Notes {
		if strings.Contains(n.Message, want) {
			return true
		}
	}
	return false
}

func TestOutputPostStageIsValidatedLikeARuleBut(t *testing.T) {
	op := `"op":{"kind":"exec","argv":["git"]}`
	mk := func(rule string) string {
		return `{` + head + `,"permissions":{"exec":["git"]},"rules":[` + rule + `]}`
	}
	accepted(t, mk(`{"id":"r","stage":"output.post",`+op+`}`), Options{})
	// A "/" prefix is useless on a line (commands never reach a rule) but is ordinary text in an answer.
	accepted(t, mk(`{"id":"r","stage":"output.post","when":{"prefix":"/"},`+op+`}`), Options{})
	refused(t, mk(`{"id":"r","stage":"output.pre",`+op+`}`), Options{}, `stage "output.pre" does not exist`, "input.pre", "output.post")
	refused(t, mk(`{"id":"r","stage":"output.post","on_error":"block",`+op+`}`), Options{}, "on_error", "nothing to hold", "Remove on_error")
	// The same on_error stays valid where a line can be held back.
	accepted(t, mk(`{"id":"r","stage":"input.pre","on_error":"block",`+op+`}`), Options{})
	// input.pre keeps refusing the "/" prefix that can never match.
	refused(t, mk(`{"id":"r","stage":"input.pre","when":{"prefix":"/ui"},`+op+`}`), Options{}, "never sees a / line")
	// A rule reads the answer as ${input}, and ${text} stays an event's.
	accepted(t, `{`+head+`,"permissions":{"exec":["git"]},"rules":[{"id":"r","stage":"output.post","op":{"kind":"exec","argv":["git","${input}"]}}]}`, Options{})
	refused(t, `{`+head+`,"permissions":{"exec":["git"]},"rules":[{"id":"r","stage":"output.post","op":{"kind":"exec","argv":["git","${text}"]}}]}`, Options{}, "${text}", "only an event on turn.done")
}

func TestOutputPostConsentNamesEachStageSeparately(t *testing.T) {
	pre := rulePkg(t, "a", "r")
	post := postPkg(t, "b", "r")
	has := func(p *Package, sub string) bool {
		for _, c := range p.Capabilities() {
			if strings.Contains(c, sub) {
				return true
			}
		}
		return false
	}
	if !has(pre, "every line you type") || has(pre, "every answer the model gives") {
		t.Errorf("an input.pre package must say it reads what you type, and not that it reads answers: %v", pre.Capabilities())
	}
	if !has(post, "every answer the model gives") || has(post, "every line you type") {
		t.Errorf("an output.post package must say it reads answers, and not what you type, or the user approves a power they were not told: %v", post.Capabilities())
	}
}

func TestOutputPostRewordsOnlyWhatIsShown(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){
		"up": reword(strings.ToUpper),
	})
	p := postPkg(t, "a", "up")
	res := s.RunOutputPost(context.Background(), []*Package{p}, "hello there")
	if res.Text != "HELLO THERE" || len(res.Notes) != 0 {
		t.Fatalf("got %+v; the rule's wording must be what is shown", res)
	}
	if got := strings.Join(res.Ran, ","); got != "a/up" {
		t.Errorf("ran %q", got)
	}
}

func TestOutputPostOrderIsPackageThenRuleAndEachSeesThePreviousText(t *testing.T) {
	add := func(sfx string) func(Input) (Outcome, error) {
		return reword(func(s string) string { return s + sfx })
	}
	s := newScripted(map[string]func(Input) (Outcome, error){"b1": add("-b1"), "a2": add("-a2"), "a1": add("-a1")})
	pb := postPkg(t, "pkgb", "b1")
	pa := postPkg(t, "pkga", "a2", "a1")
	res := s.RunOutputPost(context.Background(), []*Package{pb, pa}, "x")
	if res.Text != "x-a1-a2-b1" {
		t.Errorf("got %q, want x-a1-a2-b1: package id, then rule id, each on the text before it; otherwise two packages give a different screen on each start", res.Text)
	}
}

func TestOutputPostAndInputPreNeverRunEachOthersRules(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){
		"pre":  reword(func(s string) string { return "PRE:" + s }),
		"post": reword(func(s string) string { return "POST:" + s }),
	})
	pre := rulePkg(t, "a", "pre")
	post := postPkg(t, "b", "post")
	pk := []*Package{pre, post}
	if res := s.RunInputPre(context.Background(), pk, "line"); res.Line != "PRE:line" {
		t.Errorf("input.pre ran an output.post rule, or skipped its own: %q", res.Line)
	}
	if res := s.RunOutputPost(context.Background(), pk, "answer"); res.Text != "POST:answer" {
		t.Errorf("output.post ran an input.pre rule, or skipped its own: %q", res.Text)
	}
}

func TestOutputPostAnAnswerThatStartsWithASlashIsOrdinaryText(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": reword(func(s string) string { return strings.ToUpper(s) })})
	res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "a", "r")}, "/etc/hosts is a file")
	if res.Text != "/ETC/HOSTS IS A FILE" {
		t.Errorf("got %q: the model writes paths and commands in answers, and a rule may reword them; only a line the user types is special", res.Text)
	}
	// And a rule may reword an answer into one that starts with "/": it is only shown.
	s2 := newScripted(map[string]func(Input) (Outcome, error){"r": reword(func(s string) string { return "/ui " + s })})
	if res := s2.RunOutputPost(context.Background(), []*Package{postPkg(t, "a", "r")}, "hi"); res.Text != "/ui hi" || len(res.Notes) != 0 {
		t.Errorf("got %+v: showing a line that looks like a command is harmless, nothing runs it", res)
	}
}

func TestOutputPostRulesLookAtTheAnswerWithWhen(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": reword(func(s string) string { return "X" })})
	p := postPkg(t, "a", "r@Error:")
	if res := s.RunOutputPost(context.Background(), []*Package{p}, "all fine"); res.Text != "all fine" || len(s.calls) != 0 {
		t.Errorf("a rule whose when does not match must not run: %+v %v", res, s.calls)
	}
	if res := s.RunOutputPost(context.Background(), []*Package{p}, "Error: boom"); res.Text != "X" {
		t.Errorf("a matching rule must run: %+v", res)
	}
}

func TestOutputPostCannotHoldBackConsumeOrSend(t *testing.T) {
	for _, c := range []struct {
		name string
		out  Outcome
		note string
	}{
		{"block", Outcome{Verdict: VerdictBlock, Reason: "no"}, "holds an answer back"},
		{"handled", Outcome{Verdict: VerdictHandled, Say: "done"}, "holds an answer back"},
		{"send", Outcome{Verdict: VerdictContinue, Send: "more"}, "cannot send"},
		{"empty", Outcome{Verdict: VerdictTransform, Text: ""}, "reworded the answer to nothing"},
		{"unknown", Outcome{Verdict: "allow"}, "not understood"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(c.out)})
			res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "a", "r")}, "the answer")
			if res.Text != "the answer" {
				t.Errorf("the text became %q: the model already answered, and a rule may only reword it", res.Text)
			}
			if len(res.Say) != 0 {
				t.Errorf("a refused verdict must not also speak: %v", res.Say)
			}
			if !hasNote(res, c.note) {
				t.Errorf("no note containing %q; notes %+v. The author must be told why their rule did nothing", c.note, res.Notes)
			}
		})
	}
}

func TestOutputPostAFailingRuleLeavesTheAnswerAndTheLaterRulesRun(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){
		"a": func(Input) (Outcome, error) { return Outcome{}, fmt.Errorf("the service is down") },
		"b": reword(func(s string) string { return s + "!" }),
		"c": func(Input) (Outcome, error) { panic("oops") },
	})
	res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "p", "a", "b", "c")}, "hi")
	if res.Text != "hi!" {
		t.Errorf("got %q, want hi!: a failure is fail-open and the next rule still runs", res.Text)
	}
	if !hasNote(res, "the service is down") || !hasNote(res, "crashed") {
		t.Errorf("both failures must be told: %+v", res.Notes)
	}
}

func TestOutputPostSaysWhatARuleSaysAndKeepsItWithTheText(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(Outcome{Verdict: VerdictTransform, Text: "new", Say: "reworded 1 thing"})})
	res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "a", "r")}, "old")
	if res.Text != "new" || len(res.Say) != 1 || res.Say[0] != "reworded 1 thing" {
		t.Errorf("got %+v", res)
	}
}

func TestOutputPostLeavesAnEmptyAnswerAlone(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": reword(func(string) string { return "X" })})
	for _, text := range []string{"", "  \n\t"} {
		if res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "a", "r")}, text); res.Text != text || len(s.calls) != 0 {
			t.Errorf("%q: got %+v, calls %v; there is nothing to reword", text, res, s.calls)
		}
	}
}

func TestOutputPostAnAnswerTooLongToReadWholeIsShownAsItWasSaid(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){
		"a": reword(func(s string) string { return "X" }),
		"b": reword(func(s string) string { return "Y" }),
	})
	long := strings.Repeat("é", MaxPostRunes+1)
	res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "p", "a", "b")}, long)
	if res.Text != long || len(s.calls) != 0 {
		t.Fatalf("a rule was given a cut answer, and would have thrown the rest away: calls %v", s.calls)
	}
	n := 0
	for _, note := range res.Notes {
		if strings.Contains(note.Message, "longer than") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the user must be told once, got %d notes %+v", n, res.Notes)
	}
	exact := strings.Repeat("é", MaxPostRunes)
	if res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "p", "a")}, exact); res.Text != "X" {
		t.Errorf("an answer of exactly %d characters is still readable whole: %q", MaxPostRunes, res.Text)
	}
}

func TestOutputPostRunsAtMostEightRulesAndStopsAtTheBudget(t *testing.T) {
	var ids []string
	answers := map[string]func(Input) (Outcome, error){}
	for i := 0; i < MaxRulesPerLine+2; i++ {
		id := fmt.Sprintf("r%02d", i)
		ids = append(ids, id)
		answers[id] = reword(func(s string) string { return s + "." })
	}
	s := newScripted(answers)
	res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "p", ids...)}, "x")
	if res.Text != "x"+strings.Repeat(".", MaxRulesPerLine) || len(res.Ran) != MaxRulesPerLine {
		t.Errorf("got %q after %d rules, want %d", res.Text, len(res.Ran), MaxRulesPerLine)
	}
	if !hasNote(res, "only 8 rules run per answer") {
		t.Errorf("the skipped rules must be told: %+v", res.Notes)
	}

	spent, cancel := context.WithCancel(context.Background())
	cancel()
	s2 := newScripted(answers)
	res = s2.RunOutputPost(spent, []*Package{postPkg(t, "p", "r00")}, "x")
	if res.Text != "x" || len(s2.calls) != 0 || !hasNote(res, "time for rules") {
		t.Errorf("a rule must not start once the time is gone: %+v calls %v", res, s2.calls)
	}
}

func TestOutputPostSkipsDisabledRulesAndPackages(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": reword(func(string) string { return "X" })})
	off := postPkg(t, "a", "r")
	off.Disabled = true
	rule := postPkg(t, "b", "r")
	rule.Rules[0].Disabled = true
	if res := s.RunOutputPost(context.Background(), []*Package{off, rule, nil}, "x"); res.Text != "x" || len(s.calls) != 0 {
		t.Errorf("got %+v calls %v: a switched-off package or rule must not run", res, s.calls)
	}
}

// A streamed answer reaches a rule in pieces, and a piece that starts with a space holds the
// space between two words. An operation's output is trimmed, so the edges are put back.
func TestOutputPostPutsBackTheSpaceAtTheEdgesOfAPiece(t *testing.T) {
	s := newScripted(map[string]func(Input) (Outcome, error){"r": ok(Outcome{Verdict: VerdictTransform, Text: "and second"})})
	res := s.RunOutputPost(context.Background(), []*Package{postPkg(t, "a", "r")}, " and  second \n")
	if res.Text != " and second \n" {
		t.Errorf("got %q: a piece's leading space joins it to the one before, and its trailing newline to the one after", res.Text)
	}
}
