package extv2

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const toolHead = `"protocol":"ext/v2","id":"demo","name":"Demo","version":"1"`

func toolDoc(tool string) string { return `{` + toolHead + `,"tools":[` + tool + `]}` }

const echoTool = `{"name":"say","description":"Repeat a text","usage":"the text","min_args":1,"op":{"kind":"builtin","name":"echo"}}`

func TestAToolIsValidatedLikeACommandWithItsOwnRules(t *testing.T) {
	p := accepted(t, toolDoc(echoTool), Options{})
	if len(p.Tools) != 1 || p.Tools[0].ModelName(p.ID) != "pkg_demo_say" {
		t.Fatalf("parsed %+v", p.Tools)
	}
	// A package of only tools has something to do; one of nothing still does not.
	refused(t, `{`+toolHead+`}`, Options{}, "no commands, rules or tools")

	cases := []struct {
		name, tool string
		want       []string
	}{
		{"no description", `{"name":"say","op":{"kind":"builtin","name":"echo"}}`, []string{"description is empty", "shown"}},
		{"bad name", `{"name":"Say","description":"d","op":{"kind":"builtin","name":"echo"}}`, []string{"not usable", "pkg_<id>_<name>"}},
		{"underscore", `{"name":"a_b","description":"d","op":{"kind":"builtin","name":"echo"}}`, []string{"not usable"}},
		{"min without usage", `{"name":"say","description":"d","min_args":1,"op":{"kind":"builtin","name":"echo"}}`, []string{"no usage", "model would not know"}},
		{"unknown builtin", `{"name":"say","description":"d","op":{"kind":"builtin","name":"nope"}}`, []string{"no builtin"}},
		{"send result", `{"name":"say","description":"d","op":{"kind":"exec","argv":["git"],"result":"send"}}`, []string{"not available here", "say"}},
		{"outcome result", `{"name":"say","description":"d","op":{"kind":"exec","argv":["git"],"result":"outcome"}}`, []string{"not available here"}},
		{"input placeholder", `{"name":"say","description":"d","op":{"kind":"http","url":"https://api.example.com/${input}"}}`, []string{"${input}"}},
		{"unknown kind", `{"name":"say","description":"d","op":{"kind":"service"}}`, []string{"reserved"}},
	}
	opt := Options{Builtins: func(n string) bool { return n == "echo" }}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := `{` + toolHead + `,"permissions":{"net":["api.example.com"],"exec":["git"]},"tools":[` + c.tool + `]}`
			refused(t, doc, opt, c.want...)
		})
	}
}

func TestToolNamesAreUniqueAndFitWhatAProviderAccepts(t *testing.T) {
	two := toolDoc(echoTool + "," + echoTool)
	refused(t, two, Options{}, "defined twice")
	long := `{"protocol":"ext/v2","id":"` + strings.Repeat("a", 40) + `","name":"D","version":"1","tools":[{"name":"` + strings.Repeat("b", 20) + `","description":"d","op":{"kind":"builtin","name":"echo"}}]}`
	refused(t, long, Options{}, "together", "64")
	// The longest that fits is accepted, so the bound is exact and not off by some.
	id := strings.Repeat("a", 30)
	name := strings.Repeat("b", MaxToolNameTotal-30)
	ok := `{"protocol":"ext/v2","id":"` + id + `","name":"D","version":"1","tools":[{"name":"` + name + `","description":"d","op":{"kind":"builtin","name":"echo"}}]}`
	p := accepted(t, ok, Options{})
	if n := len(p.Tools[0].ModelName(p.ID)); n != 64 {
		t.Fatalf("the longest accepted tool is called by a %d-character name; the bound should be exactly the 64 providers accept", n)
	}
	one := strings.Replace(ok, name, name+"b", 1)
	refused(t, one, Options{}, "together")
	var many []string
	for i := 0; i <= MaxTools; i++ {
		many = append(many, `{"name":"t`+strings.Repeat("a", i)+`","description":"d","op":{"kind":"builtin","name":"echo"}}`)
	}
	refused(t, toolDoc(strings.Join(many, ",")), Options{}, "tools:", "the most is")
}

func TestTheConsentTextSaysThePackageLetsTheModelCallTools(t *testing.T) {
	p := accepted(t, toolDoc(echoTool), Options{})
	var found bool
	for _, c := range p.Capabilities() {
		if strings.Contains(c, "model call say by itself") && strings.Contains(c, "read what they return") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a package with tools reads like one without: %q", p.Capabilities())
	}
	// A tool that runs a program of the package is a program the package ships.
	q := accepted(t, `{`+toolHead+`,"tools":[{"name":"go","description":"d","op":{"kind":"exec","argv":["./run.sh"]}}]}`, Options{})
	var ships bool
	for _, c := range q.Capabilities() {
		ships = ships || strings.Contains(c, "programs shipped inside the package")
	}
	if !ships {
		t.Fatalf("a tool's own program is missing from what the user approves: %q", q.Capabilities())
	}
}

func TestAToolChangesTheDigest(t *testing.T) {
	a := accepted(t, `{`+toolHead+`,"commands":[{"name":"hi","op":{"kind":"builtin","name":"echo"}}]}`, Options{})
	b := accepted(t, `{`+toolHead+`,"commands":[{"name":"hi","op":{"kind":"builtin","name":"echo"}}],"tools":[`+echoTool+`]}`, Options{})
	if a.Digest() == b.Digest() {
		t.Fatal("adding a tool left the digest alone, so a grant given before would cover a power the user never saw")
	}
	c := accepted(t, `{`+toolHead+`,"commands":[{"name":"hi","op":{"kind":"builtin","name":"echo"}}],"tools":[{"name":"say","description":"Changed","usage":"the text","min_args":1,"op":{"kind":"builtin","name":"echo"}}]}`, Options{})
	if b.Digest() == c.Digest() {
		t.Fatal("rewording a tool's description, which is what steers the model, left the digest alone")
	}
}

func toolRunner() *Runner {
	return &Runner{Builtins: map[string]BuiltinFunc{
		"echo": func(_ context.Context, in Input) (Outcome, error) {
			return Outcome{Say: "got " + in.Args + "\x1b[2J"}, nil
		},
		"send": func(context.Context, Input) (Outcome, error) { return Outcome{Send: "x"}, nil },
		"verd": func(context.Context, Input) (Outcome, error) { return Outcome{Verdict: VerdictBlock, Say: "x"}, nil },
		"text": func(context.Context, Input) (Outcome, error) { return Outcome{Text: "x"}, nil },
		"boom": func(context.Context, Input) (Outcome, error) { return Outcome{}, errors.New("it broke") },
	}}
}

func TestRunToolReturnsCleanTextToTheModel(t *testing.T) {
	p := accepted(t, toolDoc(echoTool), Options{})
	got, err := toolRunner().RunTool(context.Background(), p, p.Tools[0], "  hello world  ")
	if err != nil || got != "got hello world" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRunToolRefusesTooFewWordsBeforeRunningAnything(t *testing.T) {
	ran := false
	r := &Runner{Builtins: map[string]BuiltinFunc{"echo": func(context.Context, Input) (Outcome, error) { ran = true; return Outcome{}, nil }}}
	p := accepted(t, toolDoc(echoTool), Options{})
	_, err := r.RunTool(context.Background(), p, p.Tools[0], "   ")
	if err == nil || !strings.Contains(err.Error(), "at least 1") || !strings.Contains(err.Error(), "the text") || ran {
		t.Fatalf("err=%v ran=%v", err, ran)
	}
}

func TestRunToolBoundsWhatTheModelPasses(t *testing.T) {
	p := accepted(t, toolDoc(echoTool), Options{})
	if _, err := toolRunner().RunTool(context.Background(), p, p.Tools[0], strings.Repeat("a", MaxToolArgs)); err != nil {
		t.Fatalf("exactly the limit must pass: %v", err)
	}
	_, err := toolRunner().RunTool(context.Background(), p, p.Tools[0], strings.Repeat("a", MaxToolArgs+1))
	if err == nil || !strings.Contains(err.Error(), "most a tool takes") {
		t.Fatalf("got %v", err)
	}
}

// What a tool hands back is read by the model and by nothing else: even a document that
// skipped validation cannot make a tool send text, hold a line or rewrite one.
func TestAToolCannotReturnAnythingButText(t *testing.T) {
	for _, name := range []string{"send", "verd", "text"} {
		p := &Package{Protocol: Protocol, ID: "demo", Name: "d", Version: "1"}
		tl := Tool{Name: "t", Description: "d", Op: Op{Kind: KindBuiltin, Name: name}}
		_, err := toolRunner().RunTool(context.Background(), p, tl, "")
		if !errors.Is(err, ErrNotSigned) {
			t.Errorf("builtin %s: got %v", name, err)
		}
	}
}

func TestAFailingToolReportsTheErrorAndReturnsNoText(t *testing.T) {
	p := &Package{Protocol: Protocol, ID: "demo", Name: "d", Version: "1"}
	got, err := toolRunner().RunTool(context.Background(), p, Tool{Name: "t", Description: "d", Op: Op{Kind: KindBuiltin, Name: "boom"}}, "")
	if err == nil || got != "" || !strings.Contains(err.Error(), "it broke") {
		t.Fatalf("got %q, %v", got, err)
	}
}
