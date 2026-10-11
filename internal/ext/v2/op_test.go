package extv2

import (
	"strings"
	"testing"
)

func cmdDoc(perms, op string, extra string) string {
	return `{` + head + `,"permissions":` + perms + `,"commands":[{"name":"a"` + extra + `,"op":` + op + `}]}`
}

func TestHTTPTargetIsFixedByThePackageAndNamedInPermissions(t *testing.T) {
	perms := `{"net":["api.example.com"]}`
	ok := `{"kind":"http","url":"https://api.example.com/v1?q=${args}"}`
	accepted(t, cmdDoc(perms, ok, ""), Options{})

	cases := []struct {
		name, url, perms string
		want             string
	}{
		{"host not listed", "https://other.example.com/", perms, "not in permissions.net"},
		{"placeholder as host", "https://${args}/", perms, "begin with its scheme and host"},
		{"placeholder right after host", "https://api.example.com${args}", perms, "put a '/' after the host"},
		{"credentials", "https://user:pw@api.example.com/", perms, "credentials"},
		{"userinfo trick", "https://api.example.com@evil.com/", perms, "credentials"},
		{"no scheme", "api.example.com/", perms, "no scheme"},
		{"ftp", "ftp://api.example.com/", perms, "not allowed"},
		{"plain http to a public host", "http://api.example.com/", perms, "in the clear"},
		{"wrong port", "https://api.example.com:8443/", perms, "not in permissions.net"},
		{"bracketed address", "https://[::1]/", perms, "bracketed"},
		{"backslash", `https://api.example.com\@evil.com/`, perms, "credentials"},
		{"unknown placeholder", "https://api.example.com/${foo}", perms, "not one this contract knows"},
		{"unterminated placeholder", "https://api.example.com/${args", perms, "never closed"},
		{"empty", "", perms, "needs a url"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := `{"kind":"http","url":` + jsonString(c.url) + `}`
			refused(t, cmdDoc(c.perms, op, ""), Options{}, c.want)
		})
	}
}

func jsonString(s string) string {
	return `"` + jsonEscape(s) + `"`
}

func TestHTTPLocalNeedsTheLocalPermissionAndMayUsePlainHTTP(t *testing.T) {
	accepted(t, cmdDoc(`{"net":["local:127.0.0.1:8080"]}`, `{"kind":"http","url":"http://127.0.0.1:8080/x"}`, ""), Options{})
	// The same address under a public-looking permission does not exist: a bare
	// address cannot be listed that way.
	refused(t, cmdDoc(`{"net":["127.0.0.1"]}`, `{"kind":"http","url":"http://127.0.0.1/x"}`, ""), Options{}, "permissions.net")
	// A local entry for another port does not cover this one.
	refused(t, cmdDoc(`{"net":["local:127.0.0.1:9090"]}`, `{"kind":"http","url":"http://127.0.0.1:8080/x"}`, ""), Options{}, "in the clear", `"local:127.0.0.1:8080"`)
}

func TestHTTPMethodsHeadersAndBody(t *testing.T) {
	p := `{"net":["a.example.com"]}`
	u := `"url":"https://a.example.com/"`
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"method":"DELETE"}`, ""), Options{}, "method \"DELETE\"")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"body":"{}"}`, ""), Options{}, "only sent with POST")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"body_encoding":"json"}`, ""), Options{}, "no body")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"method":"POST","body":"{}","body_encoding":"xml"}`, ""), Options{}, "not understood")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"headers":{"Host":"evil.com"}}`, ""), Options{}, "belongs to the transport")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"headers":{"Content-Length":"1"}}`, ""), Options{}, "belongs to the transport")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"headers":{"X Bad":"1"}}`, ""), Options{}, "not usable")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"headers":{"X-A":"a\r\nX-B: b"}}`, ""), Options{}, "control character")
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"headers":{"X-A":"${args}"}}`, ""), Options{}, "must not decide a header")
	accepted(t, cmdDoc(p, `{"kind":"http",`+u+`,"headers":{"Authorization":"Bearer abc"}}`, ""), Options{})
}

func TestHTTPJSONBodyKeepsPlaceholdersInsideStrings(t *testing.T) {
	p := `{"net":["a.example.com"]}`
	u := `"url":"https://a.example.com/","method":"POST"`
	accepted(t, cmdDoc(p, `{"kind":"http",`+u+`,"body":"{\"q\":\"${args}\",\"n\":1}"}`, ""), Options{})
	// Outside a string a value could be a number, an object or a key.
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"body":"{\"q\":${args}}"}`, ""), Options{}, "outside a JSON string")
	// Inside a string, even a key, the value is escaped and cannot change the shape
	// of the request, so it is allowed.
	accepted(t, cmdDoc(p, `{"kind":"http",`+u+`,"body":"{\"${args}\":1}"}`, ""), Options{})
	// A string that is never closed would leave the next placeholder outside it.
	refused(t, cmdDoc(p, `{"kind":"http",`+u+`,"body":"{\"a\":\"x\", \"b\": ${args}}"}`, ""), Options{}, "outside a JSON string")
}

func TestExecProgramsAreInsideThePackageOrListed(t *testing.T) {
	okPkg := `{"kind":"exec","argv":["./tool.sh","${args}"]}`
	accepted(t, cmdDoc(`{}`, okPkg, ""), Options{})
	accepted(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","status"]}`, ""), Options{})

	refused(t, cmdDoc(`{}`, `{"kind":"exec","argv":["git","status"]}`, ""), Options{}, "not in permissions.exec", "./git")
	refused(t, cmdDoc(`{}`, `{"kind":"exec","argv":["/bin/sh","-c","x"]}`, ""), Options{}, "outside the package")
	refused(t, cmdDoc(`{}`, `{"kind":"exec","argv":["../x"]}`, ""), Options{}, "outside the package")
	refused(t, cmdDoc(`{}`, `{"kind":"exec","argv":["./a/../../x"]}`, ""), Options{}, "outside the package")
	refused(t, cmdDoc(`{}`, `{"kind":"exec","argv":["C:\\x.exe"]}`, ""), Options{}, "outside the package")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["${args}"]}`, ""), Options{}, "typed text could choose what runs")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":[]}`, ""), Options{}, "argv is empty")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","a\u0000b"]}`, ""), Options{}, "NUL")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec"}`, ""), Options{}, "argv is empty")
}

func TestPlaceholdersAreCheckedForTheirPlace(t *testing.T) {
	// ${1} needs min_args >= 1, so a missing word is never an empty string.
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","${1}"]}`, ""), Options{}, "may be missing", "min_args")
	accepted(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","${1}"]}`, `,"min_args":1,"usage":"/a <x>"`), Options{})
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","${2}"]}`, `,"min_args":1,"usage":"/a <x>"`), Options{}, "min_args", "at least 2")
	// ${input} belongs to rules and ${args} to commands.
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","${input}"]}`, ""), Options{}, "a command has no such line")
	rule := func(argv string) string {
		return `{` + head + `,"permissions":{"exec":["git"]},"rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","argv":` + argv + `}}]}`
	}
	accepted(t, rule(`["git","${input}"]`), Options{})
	refused(t, rule(`["git","${args}"]`), Options{}, "a rule has no arguments")
	// A literal dollar sign needs no escaping unless it opens a brace.
	accepted(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git","costs $5 and $$"]}`, ""), Options{})
}

func TestResultsAreLimitedByPlaceAndPermission(t *testing.T) {
	// send needs the permission, so the consent screen can say it.
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"result":"send"}`, ""), Options{}, "permissions", `"send": true`)
	accepted(t, cmdDoc(`{"exec":["git"],"send":true}`, `{"kind":"exec","argv":["git"],"result":"send"}`, ""), Options{})
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"result":"transform"}`, ""), Options{}, "not available here")
	rule := func(res string, perms string) string {
		return `{` + head + `,"permissions":` + perms + `,"rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","argv":["git"],"result":"` + res + `"}}]}`
	}
	accepted(t, rule("transform", `{"exec":["git"]}`), Options{})
	accepted(t, rule("outcome", `{"exec":["git"]}`), Options{})
	// A rule can never be a plain "send": it already sits on the way to the model.
	refused(t, rule("send", `{"exec":["git"],"send":true}`), Options{}, "not available here")
	refused(t, rule("say", `{"exec":["git"]}`), Options{}, "not available here")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"extract":"a b"}`, ""), Options{}, "dotted path")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"result":"outcome","extract":"a.b"}`, ""), Options{}, "only one can")
}

func TestTimeoutsAreBoundedMoreTightlyForRules(t *testing.T) {
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"timeout_ms":999999}`, ""), Options{}, "timeout_ms")
	refused(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"timeout_ms":-1}`, ""), Options{}, "negative")
	rule := `{` + head + `,"permissions":{"exec":["git"]},"rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","argv":["git"],"timeout_ms":` + "5000" + `}}]}`
	refused(t, rule, Options{}, "timeout_ms 5000", "2000")
	accepted(t, cmdDoc(`{"exec":["git"]}`, `{"kind":"exec","argv":["git"],"timeout_ms":5000}`, ""), Options{})
}

func TestRulesValidation(t *testing.T) {
	mk := func(rule string) string {
		return `{` + head + `,"permissions":{"exec":["git"]},"rules":[` + rule + `]}`
	}
	op := `"op":{"kind":"exec","argv":["git"]}`
	accepted(t, mk(`{"id":"r","stage":"input.pre","when":{"prefix":"!"},`+op+`}`), Options{})
	refused(t, mk(`{"id":"r","stage":"output.post",`+op+`}`), Options{}, "stage \"output.post\" does not exist")
	refused(t, mk(`{"id":"R 1","stage":"input.pre",`+op+`}`), Options{}, "not usable")
	refused(t, mk(`{"id":"r","stage":"input.pre","on_error":"retry",`+op+`}`), Options{}, "on_error")
	refused(t, mk(`{"id":"r","stage":"input.pre","when":{},`+op+`}`), Options{}, "would match every line")
	refused(t, mk(`{"id":"r","stage":"input.pre","when":{"prefix":"/ui"},`+op+`}`), Options{}, "never sees a / line")
	refused(t, mk(`{"id":"r","stage":"input.pre","when":{"matches":"("},`+op+`}`), Options{}, "not a valid expression")
	refused(t, mk(`{"id":"r","stage":"input.pre",`+op+`},{"id":"r","stage":"input.pre",`+op+`}`), Options{}, "defined twice")
}

func TestCapabilitiesTellEveryPower(t *testing.T) {
	p := accepted(t, `{`+head+`,"permissions":{"net":["a.example.com","local:127.0.0.1:8080"],"exec":["git"],"open":["example.org"],"send":true},
	 "commands":[{"name":"a","op":{"kind":"exec","argv":["./t.sh"]}}],
	 "rules":[{"id":"r","stage":"input.pre","op":{"kind":"exec","argv":["git"]}}]}`, Options{})
	got := strings.Join(p.Capabilities(), "|")
	for _, w := range []string{"a.example.com:443", "127.0.0.1:8080 on this machine", "program git", "inside the package", "example.org", "send text to the model", "rewrite every line"} {
		if !strings.Contains(got, w) {
			t.Errorf("capabilities %q lack %q", got, w)
		}
	}
	none := accepted(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"builtin","name":"x"}}]}`, Options{})
	if c := none.Capabilities(); len(c) != 0 {
		t.Errorf("a package with no permissions listed %v", c)
	}
}
