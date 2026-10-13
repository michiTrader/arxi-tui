package extv2

import (
	"strings"
	"testing"
)

// mustParse parses a package and fails the test if it is not even well formed.
func mustParse(t *testing.T, doc string) *Package {
	t.Helper()
	p, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, doc)
	}
	return p
}

// refused asserts that doc parses but does not validate, and that the message holds
// every fragment in want (what is wrong and what to write instead).
func refused(t *testing.T, doc string, opt Options, want ...string) {
	t.Helper()
	p, err := Parse([]byte(doc))
	if err == nil {
		err = p.Validate(opt)
	}
	if err == nil {
		t.Fatalf("package was accepted, want a refusal containing %q\n%s", want, doc)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("refusal %q lacks %q", err, w)
		}
	}
}

func accepted(t *testing.T, doc string, opt Options) *Package {
	t.Helper()
	p := mustParse(t, doc)
	if err := p.Validate(opt); err != nil {
		t.Fatalf("package refused: %v\n%s", err, doc)
	}
	return p
}

const head = `"protocol":"ext/v2","id":"demo","name":"Demo","version":"1"`

func TestValidMinimalCommand(t *testing.T) {
	p := accepted(t, `{`+head+`,"commands":[{"name":"hi","op":{"kind":"builtin","name":"help.open"}}]}`, Options{})
	if len(p.Commands) != 1 || p.Commands[0].Name != "hi" {
		t.Fatalf("parsed %+v", p.Commands)
	}
}

func TestIdentityIsRequiredAndStrict(t *testing.T) {
	op := `"commands":[{"name":"hi","op":{"kind":"builtin","name":"x"}}]`
	refused(t, `{"id":"demo","name":"D","version":"1",`+op+`}`, Options{}, "protocol", "ext/v2")
	refused(t, `{"protocol":"ext/v1","id":"demo","name":"D","version":"1",`+op+`}`, Options{}, "not supported")
	refused(t, `{"protocol":"ext/v2","id":"Demo","name":"D","version":"1",`+op+`}`, Options{}, "not a legal identifier")
	refused(t, `{"protocol":"ext/v2","id":"demo","name":" ","version":"1",`+op+`}`, Options{}, "name is empty")
	refused(t, `{"protocol":"ext/v2","id":"demo","name":"D","version":"",`+op+`}`, Options{}, "version is empty")
	refused(t, `{`+head+`}`, Options{}, "no commands, rules, tools, events, panels or services")
}

func TestParseRefusesUnknownKeysAndReservedArtifacts(t *testing.T) {
	op := `"commands":[{"name":"hi","op":{"kind":"builtin","name":"x"}}]`
	// A key the contract does not know is a setting the author thinks is in force.
	refused(t, `{`+head+`,"permisions":{},`+op+`}`, Options{}, "malformed", "permisions")
	// Artifact kinds nobody has built must fail loudly instead of half-loading; with
	// events, panels and services built, an unbuilt one is simply an unknown key.
	for _, k := range []string{"timers", "widgets", "hooks"} {
		refused(t, `{`+head+`,"`+k+`":[],`+op+`}`, Options{}, "malformed", k)
	}
	refused(t, `[]`, Options{}, "not a JSON object")
	refused(t, `{`+head+`,`+op+`} {}`, Options{}, "more than one JSON value")
	if _, err := Parse(append([]byte(`{"x":"`), make([]byte, MaxDocBytes)...)); err == nil || !strings.Contains(err.Error(), "the most is") {
		t.Fatalf("an oversize document must be refused with its limit, got %v", err)
	}
	if _, err := Parse([]byte("{\"id\":\"\xff\"}")); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid UTF-8 must be refused, got %v", err)
	}
}

func TestCommandNamesAreReservedAndUnique(t *testing.T) {
	reserved := Options{ReservedCommands: func(n string) bool { return n == "help" }}
	refused(t, `{`+head+`,"commands":[{"name":"help","op":{"kind":"builtin","name":"x"}}]}`, reserved, "/help is already a command", "pick another name")
	refused(t, `{`+head+`,"commands":[{"name":"Bad Name","op":{"kind":"builtin","name":"x"}}]}`, Options{}, "not usable")
	refused(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"builtin","name":"x"}},{"name":"a","op":{"kind":"builtin","name":"y"}}]}`, Options{}, "/a is defined twice")
}

func TestUnknownBuiltinIsRefusedOnlyWhenTheHostKnowsTheList(t *testing.T) {
	doc := `{` + head + `,"commands":[{"name":"hi","op":{"kind":"builtin","name":"nope.nothing"}}]}`
	accepted(t, doc, Options{})
	refused(t, doc, Options{Builtins: func(string) bool { return false }}, `no builtin "nope.nothing"`)
}

func TestOpKinds(t *testing.T) {
	refused(t, `{`+head+`,"commands":[{"name":"a","op":{}}]}`, Options{}, "no kind")
	refused(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"shell"}}]}`, Options{}, `"shell" does not exist`)
	refused(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"service"}}]}`, Options{}, "not an operation kind", `"services"`)
}

func TestStraySettingsAreRefused(t *testing.T) {
	refused(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"builtin","name":"x","url":"https://a.com"}}]}`, Options{}, "does not use url")
	refused(t, `{`+head+`,"permissions":{"exec":["git"]},"commands":[{"name":"a","op":{"kind":"exec","argv":["git"],"url":"https://a.com"}}]}`, Options{}, "does not use url")
	refused(t, `{`+head+`,"permissions":{"net":["a.com"]},"commands":[{"name":"a","op":{"kind":"http","url":"https://a.com/","argv":["x"]}}]}`, Options{}, "does not use argv")
}

func TestLabelsAreSafeToShow(t *testing.T) {
	refused(t, "{"+head+`,"commands":[{"name":"a","description":"x\u001b[2Jy","op":{"kind":"builtin","name":"x"}}]}`, Options{}, "control character")
	refused(t, "{"+head+`,"commands":[{"name":"a","description":"evil\u202egpj","op":{"kind":"builtin","name":"x"}}]}`, Options{}, "control character")
	refused(t, "{"+head+`,"commands":[{"name":"a","description":"`+strings.Repeat("x", MaxDescLen+1)+`","op":{"kind":"builtin","name":"x"}}]}`, Options{}, "longer than")
}

func TestMinArgsNeedsUsage(t *testing.T) {
	refused(t, `{`+head+`,"commands":[{"name":"a","min_args":1,"op":{"kind":"builtin","name":"x"}}]}`, Options{}, "no usage", "/a <text>")
	refused(t, `{`+head+`,"commands":[{"name":"a","min_args":12,"usage":"u","op":{"kind":"builtin","name":"x"}}]}`, Options{}, "outside 0..9")
}

func TestLimits(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{` + head + `,"commands":[`)
	for i := 0; i <= MaxCommands; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"name":"c` + strings.Repeat("a", i%5) + string(rune('a'+i%26)) + `","op":{"kind":"builtin","name":"x"}}`)
	}
	b.WriteString(`]}`)
	refused(t, b.String(), Options{}, "the most is")
}

func TestPermissionsAreStrict(t *testing.T) {
	op := `"commands":[{"name":"a","op":{"kind":"builtin","name":"x"}}]`
	for _, bad := range []string{`"*.example.com"`, `"http://a.com"`, `"a.com/path"`, `"a.com:99999"`, `"127.0.0.1"`, `"10.0.0.1"`, `"localhost"`, `"a.123"`, `"user@a.com"`, `"a b.com"`, `"[::1]"`} {
		refused(t, `{`+head+`,"permissions":{"net":[`+bad+`]},`+op+`}`, Options{}, "permissions.net")
	}
	// A bare address is only reachable through the spelled-out local form.
	accepted(t, `{`+head+`,"permissions":{"net":["local:127.0.0.1:8080"]},`+op+`}`, Options{})
	refused(t, `{`+head+`,"permissions":{"net":["local:127.0.0.1"]},`+op+`}`, Options{}, "names its port")
	refused(t, `{`+head+`,"permissions":{"net":["a.com","a.com"]},`+op+`}`, Options{}, "twice")
	for _, bad := range []string{`"/usr/bin/git"`, `"../x"`, `"git status"`, `"./git"`, `"C:\\git"`} {
		refused(t, `{`+head+`,"permissions":{"exec":[`+bad+`]},`+op+`}`, Options{}, "permissions.exec")
	}
	refused(t, `{`+head+`,"permissions":{"open":["http://a.com"]},`+op+`}`, Options{}, "permissions.open")
}

func TestDigestIgnoresFormattingAndTracksMeaning(t *testing.T) {
	a := mustParse(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"builtin","name":"x"}}]}`)
	b := mustParse(t, "{\n  "+head+",\n \"commands\": [ {\"op\": {\"name\":\"x\",\"kind\":\"builtin\"}, \"name\":\"a\"} ]\n}")
	if a.Digest() == "" || a.Digest() != b.Digest() {
		t.Fatalf("same meaning, different digest: %q vs %q", a.Digest(), b.Digest())
	}
	c := mustParse(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"builtin","name":"y"}}]}`)
	if a.Digest() == c.Digest() {
		t.Fatal("a changed operation must change the digest, or a grant would survive an edit")
	}
	d := mustParse(t, `{`+head+`,"disabled":true,"commands":[{"name":"a","op":{"kind":"builtin","name":"x"}}]}`)
	if a.Digest() == d.Digest() {
		t.Fatal("the digest must cover every field")
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	a := accepted(t, `{`+head+`,"permissions":{"send":true},"commands":[{"name":"a","op":{"kind":"builtin","name":"x"}}]}`, Options{})
	b, err := Parse(a.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest() != b.Digest() {
		t.Fatal("Encode then Parse must not change the package")
	}
}
