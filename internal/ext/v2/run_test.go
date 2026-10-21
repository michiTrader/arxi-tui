package extv2

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func errorsAs(err error, target **UsageError) bool { return errors.As(err, target) }

func TestCleanText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\x1b[31mred\x1b[0m", "ared"},
		{"a\x1b]0;title\x07b", "ab"},
		{"x\r\ny\rz", "x\ny\nz"},
		{"a\tb", "a    b"},
		{"a\x00b\x07c\x7fd", "abcd"},
		{"evil\u202egpj", "evilgpj"},
		{"bad\xffbytes", "badbytes"},
		{"ünïcödé ✓", "ünïcödé ✓"},
	}
	for _, c := range cases {
		if got := CleanText(c.in, 100, 10); got != c.want {
			t.Errorf("CleanText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := CleanText(strings.Repeat("é", 50), 10, 0); got != strings.Repeat("é", 9)+"…" {
		t.Errorf("rune cut = %q", got)
	}
	if got := CleanText("a\nb\nc\nd", 100, 2); got != "a\nb…" {
		t.Errorf("line cut = %q", got)
	}
}

func TestExtractPathTable(t *testing.T) {
	doc := `{"a":{"b":[10,{"c":"deep"}],"t":true},"big":12345678901234567890}`
	for path, want := range map[string]string{"a.b.0": "10", "a.b.1.c": "deep", "a.t": "true", "big": "12345678901234567890"} {
		got, err := extractPath([]byte(doc), path, "")
		if err != nil || got != want {
			t.Errorf("extractPath(%s) = %q, %v; want %q", path, got, err, want)
		}
	}
	if _, err := extractPath([]byte(`nope`), "a", ""); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Errorf("got %v", err)
	}
}

func TestBuiltinsRunInTheHostAndTheirOutputIsCleaned(t *testing.T) {
	r := &Runner{Builtins: map[string]BuiltinFunc{
		"echo": func(_ context.Context, in Input) (Outcome, error) {
			return Outcome{Say: "got " + in.Args + "\x1b[2J", Send: "sent"}, nil
		},
	}}
	p := accepted(t, `{`+head+`,"permissions":{"send":true},"commands":[{"name":"a","op":{"kind":"builtin","name":"echo"}}]}`, Options{})
	out, err := r.RunCommand(context.Background(), p, p.Commands[0], "x")
	if err != nil || out.Say != "got x" || out.Send != "sent" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if _, err := (&Runner{}).RunCommand(context.Background(), p, p.Commands[0], "x"); err == nil || !strings.Contains(err.Error(), "no builtin") {
		t.Fatalf("got %v", err)
	}
}

func TestACommandCannotSendWithoutThePermissionEvenIfTheDocumentSkippedValidation(t *testing.T) {
	r := &Runner{Builtins: map[string]BuiltinFunc{
		"leak": func(context.Context, Input) (Outcome, error) { return Outcome{Send: "to the model"}, nil },
	}}
	p := &Package{Protocol: Protocol, ID: "demo", Name: "d", Version: "1"}
	_, err := r.RunCommand(context.Background(), p, Command{Name: "a", Op: Op{Kind: KindBuiltin, Name: "leak"}}, "")
	if !errors.Is(err, ErrNotSigned) {
		t.Fatalf("got %v", err)
	}
}

func TestACommandDoesNotCarryARuleVerdict(t *testing.T) {
	r := &Runner{Builtins: map[string]BuiltinFunc{
		"v": func(context.Context, Input) (Outcome, error) {
			return Outcome{Verdict: VerdictBlock, Text: "x", Say: "hi"}, nil
		},
	}}
	p := accepted(t, `{`+head+`,"commands":[{"name":"a","op":{"kind":"builtin","name":"v"}}]}`, Options{})
	out, err := r.RunCommand(context.Background(), p, p.Commands[0], "")
	if err != nil || out.Verdict != "" || out.Text != "" || out.Say != "hi" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// A translation comes back in segments. "*" walks every entry of one list and "join"
// says how they are put together, so the whole paragraph is one value.
func TestExtractWalksAListAndJoinsItsEntries(t *testing.T) {
	doc := `{"data":{"sentences":[{"t":"Hello"},{"t":"world."},{"t":"Bye"}],"n":[1,2,3],"mixed":[{"t":"a"},{"u":"b"}],"empty":[]}}`
	for _, c := range []struct{ path, join, want string }{
		{"data.sentences.*.t", "space", "Hello world. Bye"},
		{"data.sentences.*.t", "none", "Helloworld.Bye"},
		{"data.sentences.*.t", "newline", "Hello\nworld.\nBye"},
		{"data.n.*", "space", "1 2 3"}, // the entries are scalars themselves
	} {
		got, err := extractPath([]byte(doc), c.path, c.join)
		if err != nil || got != c.want {
			t.Errorf("extract %s join %s = %q, %v; want %q", c.path, c.join, got, err, c.want)
		}
	}
	for _, c := range []struct{ path, join, want string }{
		{"data.mixed.*.t", "space", "entry 1 of the list"},                  // one entry lacks the key: say which
		{"data.empty.*", "space", "is empty"},                               // nothing to join is not an empty string
		{"data.*", "space", "needs a list"},                                 // data is an object
		{"data.sentences.*", "space", "not a single value"},                 // an entry that is an object
		{"data.sentences.*.t", "comma", "join \"comma\" is not understood"}, // the runner checks again
	} {
		_, err := extractPath([]byte(doc), c.path, c.join)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("extract %s join %s: err = %v, want it to say %q", c.path, c.join, err, c.want)
		}
	}
}

// A list longer than MaxJoined is an answer the operation was not made for: it is
// refused, never cut silently (a translation with its tail missing reads as complete).
func TestExtractJoinRefusesAListLongerThanTheLimitAndAcceptsExactlyTheLimit(t *testing.T) {
	list := func(n int) []byte {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = `"x"`
		}
		return []byte(`{"l":[` + strings.Join(parts, ",") + `]}`)
	}
	if got, err := extractPath(list(MaxJoined), "l.*", "none"); err != nil || len(got) != MaxJoined {
		t.Fatalf("exactly %d entries must join: got %d chars, %v", MaxJoined, len(got), err)
	}
	if _, err := extractPath(list(MaxJoined+1), "l.*", "none"); err == nil || !strings.Contains(err.Error(), "ask the service for less") {
		t.Fatalf("%d entries must be refused with a remedy, got %v", MaxJoined+1, err)
	}
}
